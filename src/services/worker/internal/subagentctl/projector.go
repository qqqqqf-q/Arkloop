package subagentctl

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type SubAgentStateProjector struct {
	pool     *pgxpool.Pool
	rdb      *redis.Client
	jobQueue queue.JobQueue
	factory  *SubAgentRunFactory
}

func NewSubAgentStateProjector(pool *pgxpool.Pool, rdb *redis.Client, jobQueue queue.JobQueue) *SubAgentStateProjector {
	return &SubAgentStateProjector{
		pool:     pool,
		rdb:      rdb,
		jobQueue: jobQueue,
		factory:  NewSubAgentRunFactory(pool, NewSnapshotStorage()),
	}
}

func (p *SubAgentStateProjector) EnqueueRun(ctx context.Context, accountID uuid.UUID, runID uuid.UUID, traceID string, availableAt *time.Time) error {
	if p.jobQueue == nil {
		return fmt.Errorf("sub-agent control job queue must not be nil")
	}
	_, err := p.jobQueue.EnqueueRun(ctx, accountID, runID, strings.TrimSpace(traceID), queue.RunExecuteJobType, map[string]any{}, availableAt)
	return err
}

func (p *SubAgentStateProjector) MarkRunning(ctx context.Context, runID uuid.UUID) error {
	if p.pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	if runID == uuid.Nil {
		return fmt.Errorf("run_id must not be empty")
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := (data.SubAgentRepository{}).TransitionToRunning(ctx, tx, runID); err != nil {
		return err
	}
	if _, _, err := (data.SubAgentEventAppender{}).AppendForCurrentRun(ctx, tx, runID, data.SubAgentEventTypeRunStarted, map[string]any{
		"run_id": runID.String(),
	}, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *SubAgentStateProjector) MarkRunFailed(ctx context.Context, childRunID uuid.UUID, message string) error {
	if p.pool == nil || childRunID == uuid.Nil {
		return nil
	}
	trimmedMessage := strings.TrimSpace(message)
	if trimmedMessage == "" {
		trimmedMessage = "failed to enqueue child run job"
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	subAgent, err := (data.SubAgentRepository{}).GetByCurrentRunID(ctx, tx, childRunID)
	if err != nil {
		return err
	}
	// Lock the run row to serialize per-run seq allocation
	if _, err := tx.Exec(ctx, `SELECT 1 FROM runs WHERE id = $1 FOR UPDATE`, childRunID); err != nil {
		return err
	}
	var seq int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM run_events WHERE run_id = $1`,
		childRunID,
	).Scan(&seq); err != nil {
		return err
	}
	errData, _ := json.Marshal(map[string]any{
		"error_class": "worker.enqueue_failed",
		"message":     trimmedMessage,
	})
	if _, err := tx.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json)
		 VALUES ($1, $2, 'run.failed', $3::jsonb)`,
		childRunID, seq, string(errData),
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE runs SET status = 'failed', status_updated_at = now(), failed_at = now()
		 WHERE id = $1`,
		childRunID,
	); err != nil {
		return err
	}
	if err := (data.SubAgentRepository{}).TransitionToTerminal(ctx, tx, childRunID, data.SubAgentStatusFailed, &trimmedMessage); err != nil {
		return err
	}
	if subAgent != nil {
		if _, err := (data.SubAgentEventAppender{}).Append(ctx, tx, subAgent.ID, &childRunID, data.SubAgentEventTypeFailed, map[string]any{
			"run_id":      childRunID.String(),
			"message":     trimmedMessage,
			"error_class": "worker.enqueue_failed",
		}, stringPtr("worker.enqueue_failed")); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if p.rdb != nil {
		ch := fmt.Sprintf("run.child.%s.done", childRunID.String())
		_, _ = p.rdb.Publish(ctx, ch, "failed\n").Result()
	}
	return nil
}

func (p *SubAgentStateProjector) ProjectRunTerminal(
	ctx context.Context,
	tx pgx.Tx,
	run data.Run,
	status string,
	eventData map[string]any,
	errorClass *string,
) (*uuid.UUID, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	subAgent, err := (data.SubAgentRepository{}).GetByCurrentRunID(ctx, tx, run.ID)
	if err != nil {
		return nil, err
	}
	if subAgent == nil {
		return nil, nil
	}
	message := terminalMessage(status, eventData)
	var lastError *string
	if status != data.SubAgentStatusCompleted && message != "" {
		lastError = &message
	}
	if err := (data.SubAgentRepository{}).TransitionToTerminal(ctx, tx, run.ID, status, lastError); err != nil {
		return nil, err
	}
	eventType, err := data.SubAgentTerminalEventType(status)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{"run_id": run.ID.String()}
	if message != "" {
		payload["message"] = message
	}
	if _, err := (data.SubAgentEventAppender{}).Append(ctx, tx, subAgent.ID, &run.ID, eventType, payload, errorClass); err != nil {
		return nil, err
	}
	return p.factory.CreateRunFromPendingInputs(ctx, tx, *subAgent)
}

func (p *SubAgentStateProjector) BuildSnapshot(ctx context.Context, tx pgx.Tx, record data.SubAgentRecord) (StatusSnapshot, error) {
	snapshot := StatusSnapshot{
		SubAgentID:         record.ID,
		ParentRunID:        record.ParentRunID,
		RootRunID:          record.RootRunID,
		Depth:              record.Depth,
		Status:             record.Status,
		Role:               cloneStringPtr(record.Role),
		PersonaID:          cloneStringPtr(record.PersonaID),
		Nickname:           cloneStringPtr(record.Nickname),
		ContextMode:        record.ContextMode,
		CurrentRunID:       cloneUUIDPtr(record.CurrentRunID),
		LastCompletedRunID: cloneUUIDPtr(record.LastCompletedRunID),
		LastOutputRef:      cloneStringPtr(record.LastOutputRef),
		LastError:          cloneStringPtr(record.LastError),
		StartedAt:          cloneTimePtr(record.StartedAt),
		CompletedAt:        cloneTimePtr(record.CompletedAt),
		ClosedAt:           cloneTimePtr(record.ClosedAt),
	}
	var (
		seq       int64
		eventType string
	)
	err := tx.QueryRow(ctx,
		`SELECT seq, type
		 FROM sub_agent_events
		 WHERE sub_agent_id = $1
		 ORDER BY seq DESC
		 LIMIT 1`,
		record.ID,
	).Scan(&seq, &eventType)
	if err == nil {
		snapshot.LastEventSeq = &seq
		snapshot.LastEventType = &eventType
	} else if err != pgx.ErrNoRows {
		return StatusSnapshot{}, err
	}
	if record.LastCompletedRunID != nil {
		messageID, output, err := (data.MessagesRepository{}).FindAssistantMessageByRunID(ctx, tx, *record.LastCompletedRunID)
		if err != nil {
			return StatusSnapshot{}, err
		}
		if messageID != nil {
			ref := "message:" + messageID.String()
			if snapshot.LastOutputRef == nil {
				snapshot.LastOutputRef = &ref
			}
		}
		if strings.TrimSpace(output) != "" {
			trimmed := strings.TrimSpace(output)
			snapshot.LastOutput = &trimmed
		}
	}
	return snapshot, nil
}

func terminalMessage(status string, eventData map[string]any) string {
	if status == data.SubAgentStatusCompleted {
		return ""
	}
	if len(eventData) == 0 {
		return ""
	}
	if raw, ok := eventData["message"].(string); ok {
		return strings.TrimSpace(raw)
	}
	if raw, ok := eventData["error"].(string); ok {
		return strings.TrimSpace(raw)
	}
	return ""
}
