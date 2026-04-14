package subagentctl

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

var callbackRunStartedDataKeys = []string{
	"persona_id",
	"role",
	"route_id",
	"output_route_id",
	"model",
	"work_dir",
	"reasoning_mode",
	"channel_delivery",
	"thread_tail_message_id",
}

type SubAgentStateProjector struct {
	pool     data.DB
	rdb      *redis.Client
	jobQueue queue.JobQueue
	factory  *SubAgentRunFactory
}

type TerminalProjection struct {
	NextRunID *uuid.UUID
	Callback  *data.ThreadSubAgentCallbackRecord
}

type callbackRunSeed struct {
	CreatedByUserID *uuid.UUID
	ProfileRef      *string
	WorkspaceRef    *string
	StartedData     map[string]any
}

func NewSubAgentStateProjector(pool data.DB, rdb *redis.Client, jobQueue queue.JobQueue) *SubAgentStateProjector {
	return &SubAgentStateProjector{
		pool:     pool,
		rdb:      rdb,
		jobQueue: jobQueue,
		factory:  NewSubAgentRunFactory(pool, NewSnapshotStorage()),
	}
}

func (p *SubAgentStateProjector) EnqueueRun(ctx context.Context, accountID uuid.UUID, runID uuid.UUID, traceID string, availableAt *time.Time, payload map[string]any) error {
	if p.jobQueue == nil {
		return fmt.Errorf("sub-agent control job queue must not be nil")
	}
	_, err := p.jobQueue.EnqueueRun(ctx, accountID, runID, strings.TrimSpace(traceID), queue.RunExecuteJobType, payload, availableAt)
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
	defer func() { _ = tx.Rollback(ctx) }()
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
	defer func() { _ = tx.Rollback(ctx) }()
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
) (TerminalProjection, error) {
	if tx == nil {
		return TerminalProjection{}, fmt.Errorf("tx must not be nil")
	}
	subAgent, err := (data.SubAgentRepository{}).GetByCurrentRunID(ctx, tx, run.ID)
	if err != nil {
		return TerminalProjection{}, err
	}
	if subAgent == nil {
		return TerminalProjection{}, nil
	}
	message := terminalMessage(status, eventData)
	var lastError *string
	if status != data.SubAgentStatusCompleted && message != "" {
		lastError = &message
	}
	if err := (data.SubAgentRepository{}).TransitionToTerminal(ctx, tx, run.ID, status, lastError); err != nil {
		return TerminalProjection{}, err
	}
	eventType, err := data.SubAgentTerminalEventType(status)
	if err != nil {
		return TerminalProjection{}, err
	}
	payload := map[string]any{"run_id": run.ID.String()}
	if message != "" {
		payload["message"] = message
	}
	if _, err := (data.SubAgentEventAppender{}).Append(ctx, tx, subAgent.ID, &run.ID, eventType, payload, errorClass); err != nil {
		return TerminalProjection{}, err
	}
	callback, err := (data.ThreadSubAgentCallbacksRepository{}).Insert(ctx, tx, data.ThreadSubAgentCallbackCreateParams{
		AccountID:   subAgent.AccountID,
		ThreadID:    subAgent.OwnerThreadID,
		SubAgentID:  subAgent.ID,
		SourceRunID: run.ID,
		Status:      status,
		PayloadJSON: map[string]any{"run_id": run.ID.String(), "status": status, "message": message},
	})
	if err != nil {
		return TerminalProjection{}, err
	}
	nextRunID, err := p.factory.CreateRunFromPendingInputs(ctx, tx, *subAgent)
	if err != nil {
		return TerminalProjection{}, err
	}
	return TerminalProjection{NextRunID: nextRunID, Callback: &callback}, nil
}

func (p *SubAgentStateProjector) BuildSnapshot(ctx context.Context, tx pgx.Tx, record data.SubAgentRecord) (StatusSnapshot, error) {
	snapshot := StatusSnapshot{
		SubAgentID:         record.ID,
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

func (p *SubAgentStateProjector) EnqueueCallbackRunIfIdle(ctx context.Context, callback data.ThreadSubAgentCallbackRecord, traceID string) error {
	return p.enqueueCallbackRunIfIdle(ctx, callback, traceID, true)
}

func (p *SubAgentStateProjector) enqueueCallbackRunIfIdle(ctx context.Context, callback data.ThreadSubAgentCallbackRecord, traceID string, recoverSkipped bool) error {
	if p.pool == nil || p.jobQueue == nil {
		return nil
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var threadCreatedByUserID *uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT created_by_user_id
		   FROM threads
		  WHERE id = $1
		  FOR UPDATE`,
		callback.ThreadID,
	).Scan(&threadCreatedByUserID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("owner thread not found: %s", callback.ThreadID)
		}
		return err
	}

	var activeRunID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT id
		   FROM runs
		  WHERE thread_id = $1
		    AND status IN ('running', 'cancelling')
		  ORDER BY created_at DESC, id DESC
		  LIMIT 1`,
		callback.ThreadID,
	).Scan(&activeRunID)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == nil && activeRunID != uuid.Nil {
		return tx.Commit(ctx)
	}

	seed, err := loadCallbackRunSeed(ctx, tx, callback.ThreadID)
	if err != nil {
		return err
	}

	var tailMessageID *uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT id
		   FROM messages
		  WHERE thread_id = $1
		    AND hidden = FALSE
		    AND deleted_at IS NULL
		    AND COALESCE(compacted, false) = false
		  ORDER BY thread_seq DESC
		  LIMIT 1`,
		callback.ThreadID,
	).Scan(&tailMessageID)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}

	runID := uuid.New()
	createdByUserID := seed.CreatedByUserID
	if createdByUserID == nil && threadCreatedByUserID != nil {
		createdByUserID = cloneUUIDPtr(threadCreatedByUserID)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO runs (id, account_id, thread_id, created_by_user_id, profile_ref, workspace_ref, status)
		 VALUES ($1, $2, $3, $4, $5, $6, 'running')`,
		runID,
		callback.AccountID,
		callback.ThreadID,
		createdByUserID,
		seed.ProfileRef,
		seed.WorkspaceRef,
	); err != nil {
		return err
	}
	startedData := cloneStringAnyMap(seed.StartedData)
	startedData["source"] = runkind.SubagentCallback
	startedData["run_kind"] = runkind.SubagentCallback
	startedData["callback_id"] = callback.ID.String()
	startedData["sub_agent_id"] = callback.SubAgentID.String()
	startedData["continuation_source"] = "none"
	startedData["continuation_loop"] = false
	startedData["continuation_response"] = false
	if tailMessageID == nil || *tailMessageID == uuid.Nil {
		delete(startedData, "thread_tail_message_id")
	}
	if tailMessageID != nil && *tailMessageID != uuid.Nil {
		startedData["thread_tail_message_id"] = tailMessageID.String()
	}
	if len(startedData) == 0 {
		startedData = map[string]any{
			"source":                runkind.SubagentCallback,
			"run_kind":              runkind.SubagentCallback,
			"callback_id":           callback.ID.String(),
			"sub_agent_id":          callback.SubAgentID.String(),
			"continuation_source":   "none",
			"continuation_loop":     false,
			"continuation_response": false,
		}
	}
	encoded, err := json.Marshal(startedData)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json)
		 VALUES ($1, 1, 'run.started', $2)`,
		runID,
		string(encoded),
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	_, err = p.jobQueue.EnqueueRun(ctx, callback.AccountID, runID, strings.TrimSpace(traceID), queue.RunExecuteJobType, map[string]any{
		"source":       runkind.SubagentCallback,
		"run_kind":     runkind.SubagentCallback,
		"callback_id":  callback.ID.String(),
		"sub_agent_id": callback.SubAgentID.String(),
	}, nil)
	if err != nil {
		if markErr := p.markCallbackRunEnqueueFailed(context.Background(), runID, err); markErr != nil {
			return fmt.Errorf("enqueue callback run: %w (mark failed: %v)", err, markErr)
		}
		if recoverSkipped {
			if wakeErr := p.enqueueNextPendingCallbackIfIdle(context.Background(), callback.ThreadID, callback.ID, traceID); wakeErr != nil {
				return fmt.Errorf("enqueue callback run: %w (wake next pending: %v)", err, wakeErr)
			}
		}
	}
	return err
}

func (p *SubAgentStateProjector) EnqueueOldestPendingCallbackIfIdle(ctx context.Context, threadID uuid.UUID, traceID string) error {
	if p.pool == nil || p.jobQueue == nil || threadID == uuid.Nil {
		return nil
	}
	callbacks, err := (data.ThreadSubAgentCallbacksRepository{}).ListPendingByThread(ctx, p.pool, threadID)
	if err != nil {
		return err
	}
	if len(callbacks) == 0 {
		return nil
	}
	return p.EnqueueCallbackRunIfIdle(ctx, callbacks[0], traceID)
}

func (p *SubAgentStateProjector) enqueueNextPendingCallbackIfIdle(ctx context.Context, threadID uuid.UUID, excludedCallbackID uuid.UUID, traceID string) error {
	if p.pool == nil || p.jobQueue == nil || threadID == uuid.Nil {
		return nil
	}
	callbacks, err := (data.ThreadSubAgentCallbacksRepository{}).ListPendingByThread(ctx, p.pool, threadID)
	if err != nil {
		return err
	}
	for _, callback := range callbacks {
		if callback.ID == uuid.Nil || callback.ID == excludedCallbackID {
			continue
		}
		return p.enqueueCallbackRunIfIdle(ctx, callback, traceID, false)
	}
	return nil
}

func (p *SubAgentStateProjector) markCallbackRunEnqueueFailed(ctx context.Context, runID uuid.UUID, enqueueErr error) error {
	if p.pool == nil || runID == uuid.Nil {
		return nil
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := (data.RunsRepository{}).LockRunRow(ctx, tx, runID); err != nil {
		return err
	}
	run, err := (data.RunsRepository{}).GetRun(ctx, tx, runID)
	if err != nil {
		return err
	}
	if run == nil || run.Status != "running" {
		return tx.Commit(ctx)
	}

	message := "failed to enqueue subagent callback run job"
	if trimmed := strings.TrimSpace(errorString(enqueueErr)); trimmed != "" {
		message = trimmed
	}
	if _, err := (data.RunEventsRepository{}).AppendEvent(ctx, tx, runID, "run.failed", map[string]any{
		"error_class": "worker.enqueue_failed",
		"message":     message,
	}, nil, stringPtr("worker.enqueue_failed")); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE runs
		    SET status = 'failed',
		        status_updated_at = now(),
		        failed_at = now()
		  WHERE id = $1`,
		runID,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func loadCallbackRunSeed(ctx context.Context, tx pgx.Tx, threadID uuid.UUID) (callbackRunSeed, error) {
	var (
		runID           uuid.UUID
		createdByUserID *uuid.UUID
		profileRef      *string
		workspaceRef    *string
	)
	err := tx.QueryRow(ctx,
		`SELECT id, created_by_user_id, profile_ref, workspace_ref
		   FROM runs
		  WHERE thread_id = $1
		  ORDER BY created_at DESC, id DESC
		  LIMIT 1`,
		threadID,
	).Scan(&runID, &createdByUserID, &profileRef, &workspaceRef)
	if err != nil {
		if err == pgx.ErrNoRows {
			return callbackRunSeed{StartedData: map[string]any{}}, nil
		}
		return callbackRunSeed{}, err
	}
	_, rawData, err := (data.RunEventsRepository{}).FirstEventData(ctx, tx, runID)
	if err != nil {
		return callbackRunSeed{}, err
	}
	return callbackRunSeed{
		CreatedByUserID: cloneUUIDPtr(createdByUserID),
		ProfileRef:      cloneStringPtr(profileRef),
		WorkspaceRef:    cloneStringPtr(workspaceRef),
		StartedData:     filterCallbackRunStartedData(rawData),
	}, nil
}

func filterCallbackRunStartedData(source map[string]any) map[string]any {
	out := make(map[string]any)
	if len(source) == 0 {
		return out
	}
	for _, key := range callbackRunStartedDataKeys {
		value, ok := source[key]
		if !ok || value == nil {
			continue
		}
		out[key] = cloneJSONValue(value)
	}
	return out
}

func cloneStringAnyMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = cloneJSONValue(value)
	}
	return out
}

func cloneJSONValue(value any) any {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned any
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return value
	}
	return cloned
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
