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

const (
	childThreadTTL      = 7 * 24 * time.Hour
	defaultWaitInterval = 150 * time.Millisecond
)

type Control interface {
	Spawn(ctx context.Context, req SpawnRequest) (StatusSnapshot, error)
	SendInput(ctx context.Context, req SendInputRequest) (StatusSnapshot, error)
	Wait(ctx context.Context, req WaitRequest) (StatusSnapshot, error)
	Resume(ctx context.Context, req ResumeRequest) (StatusSnapshot, error)
	Close(ctx context.Context, req CloseRequest) (StatusSnapshot, error)
	Interrupt(ctx context.Context, req InterruptRequest) (StatusSnapshot, error)
	GetStatus(ctx context.Context, subAgentID uuid.UUID) (StatusSnapshot, error)
	ListChildren(ctx context.Context) ([]StatusSnapshot, error)
}

type SpawnRequest struct {
	PersonaID string
	Input     string
}

type SendInputRequest struct {
	SubAgentID uuid.UUID
	Input      string
	Interrupt  bool
}

type WaitRequest struct {
	SubAgentID uuid.UUID
	Timeout    time.Duration
}

type ResumeRequest struct {
	SubAgentID uuid.UUID
}

type CloseRequest struct {
	SubAgentID uuid.UUID
}

type InterruptRequest struct {
	SubAgentID uuid.UUID
	Reason     string
}

type StatusSnapshot struct {
	SubAgentID         uuid.UUID  `json:"sub_agent_id"`
	ParentRunID        uuid.UUID  `json:"parent_run_id"`
	RootRunID          uuid.UUID  `json:"root_run_id"`
	Depth              int        `json:"depth"`
	Status             string     `json:"status"`
	PersonaID          *string    `json:"persona_id,omitempty"`
	Nickname           *string    `json:"nickname,omitempty"`
	CurrentRunID       *uuid.UUID `json:"current_run_id,omitempty"`
	LastCompletedRunID *uuid.UUID `json:"last_completed_run_id,omitempty"`
	LastOutputRef      *string    `json:"last_output_ref,omitempty"`
	LastOutput         *string    `json:"output,omitempty"`
	LastError          *string    `json:"last_error,omitempty"`
	LastEventSeq       *int64     `json:"last_event_seq,omitempty"`
	LastEventType      *string    `json:"last_event_type,omitempty"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	ClosedAt           *time.Time `json:"closed_at,omitempty"`
}

type Service struct {
	pool             *pgxpool.Pool
	rdb              *redis.Client
	jobQueue         queue.JobQueue
	parentRun        data.Run
	traceID          string
	waitPollInterval time.Duration
}

func CreateInitialRun(
	ctx context.Context,
	pool *pgxpool.Pool,
	rdb *redis.Client,
	jobQueue queue.JobQueue,
	parentRun data.Run,
	traceID string,
	forcedRunID uuid.UUID,
	personaID string,
	input string,
) error {
	service := NewService(pool, rdb, jobQueue, parentRun, traceID)
	_, err := service.spawn(ctx, SpawnRequest{PersonaID: personaID, Input: input}, &forcedRunID)
	return err
}

func NewService(pool *pgxpool.Pool, rdb *redis.Client, jobQueue queue.JobQueue, parentRun data.Run, traceID string) *Service {
	return &Service{
		pool:             pool,
		rdb:              rdb,
		jobQueue:         jobQueue,
		parentRun:        parentRun,
		traceID:          strings.TrimSpace(traceID),
		waitPollInterval: defaultWaitInterval,
	}
}

func MarkRunning(ctx context.Context, pool *pgxpool.Pool, runID uuid.UUID) error {
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	if runID == uuid.Nil {
		return fmt.Errorf("run_id must not be empty")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	appender := data.SubAgentEventAppender{}
	if err := (data.SubAgentRepository{}).TransitionToRunning(ctx, tx, runID); err != nil {
		return err
	}
	if _, _, err := appender.AppendForCurrentRun(ctx, tx, runID, data.SubAgentEventTypeRunStarted, map[string]any{
		"run_id": runID.String(),
	}, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func MarkRunFailed(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, childRunID uuid.UUID) {
	if pool == nil || childRunID == uuid.Nil {
		return
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)

	subAgentEventAppender := data.SubAgentEventAppender{}
	subAgent, err := (data.SubAgentRepository{}).GetByCurrentRunID(ctx, tx, childRunID)
	if err != nil {
		return
	}

	var seq int64
	if err := tx.QueryRow(ctx, `SELECT nextval('run_events_seq_global')`).Scan(&seq); err != nil {
		return
	}
	errData, _ := json.Marshal(map[string]any{
		"error_class": "worker.enqueue_failed",
		"message":     "failed to enqueue child run job",
	})
	if _, err := tx.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json)
		 VALUES ($1, $2, 'run.failed', $3::jsonb)`,
		childRunID, seq, string(errData),
	); err != nil {
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE runs SET status = 'failed', status_updated_at = now(), failed_at = now()
		 WHERE id = $1`,
		childRunID,
	); err != nil {
		return
	}
	message := "failed to enqueue child run job"
	if err := (data.SubAgentRepository{}).TransitionToTerminal(ctx, tx, childRunID, data.SubAgentStatusFailed, &message); err != nil {
		return
	}
	if subAgent != nil {
		if _, err := subAgentEventAppender.Append(ctx, tx, subAgent.ID, &childRunID, data.SubAgentEventTypeFailed, map[string]any{
			"run_id":      childRunID.String(),
			"message":     message,
			"error_class": "worker.enqueue_failed",
		}, stringPtr("worker.enqueue_failed")); err != nil {
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return
	}
	if rdb != nil {
		ch := fmt.Sprintf("run.child.%s.done", childRunID.String())
		_, _ = rdb.Publish(ctx, ch, "failed\n").Result()
	}
}

func (s *Service) Spawn(ctx context.Context, req SpawnRequest) (StatusSnapshot, error) {
	return s.spawn(ctx, req, nil)
}

func (s *Service) spawn(ctx context.Context, req SpawnRequest, forcedRunID *uuid.UUID) (StatusSnapshot, error) {
	if err := s.validateReady(); err != nil {
		return StatusSnapshot{}, err
	}
	personaID := strings.TrimSpace(req.PersonaID)
	input := strings.TrimSpace(req.Input)
	if personaID == "" {
		return StatusSnapshot{}, fmt.Errorf("persona_id must not be empty")
	}
	if input == "" {
		return StatusSnapshot{}, fmt.Errorf("input must not be empty")
	}
	if s.parentRun.ProjectID == nil || *s.parentRun.ProjectID == uuid.Nil {
		return StatusSnapshot{}, fmt.Errorf("parent run project_id must not be empty")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return StatusSnapshot{}, err
	}
	defer tx.Rollback(ctx)

	runsRepo := data.RunsRepository{}
	subAgentsRepo := data.SubAgentRepository{}
	appender := data.SubAgentEventAppender{}
	lineage, err := runsRepo.GetLineage(ctx, tx, s.parentRun.ID)
	if err != nil {
		return StatusSnapshot{}, fmt.Errorf("load parent run lineage: %w", err)
	}
	createdSubAgent, err := subAgentsRepo.Create(ctx, tx, data.SubAgentCreateParams{
		OrgID:          s.parentRun.OrgID,
		ParentRunID:    s.parentRun.ID,
		ParentThreadID: s.parentRun.ThreadID,
		RootRunID:      lineage.RootRunID,
		RootThreadID:   lineage.RootThreadID,
		Depth:          lineage.Depth + 1,
		PersonaID:      &personaID,
		SourceType:     data.SubAgentSourceTypeThreadSpawn,
		ContextMode:    data.SubAgentContextModeIsolated,
	})
	if err != nil {
		return StatusSnapshot{}, fmt.Errorf("create sub_agent: %w", err)
	}
	if _, err := appender.Append(ctx, tx, createdSubAgent.ID, nil, data.SubAgentEventTypeSpawnRequested, map[string]any{
		"parent_run_id":    s.parentRun.ID.String(),
		"parent_thread_id": s.parentRun.ThreadID.String(),
		"root_run_id":      lineage.RootRunID.String(),
		"root_thread_id":   lineage.RootThreadID.String(),
		"depth":            lineage.Depth + 1,
		"persona_id":       personaID,
		"context_mode":     data.SubAgentContextModeIsolated,
		"source_type":      data.SubAgentSourceTypeThreadSpawn,
	}, nil); err != nil {
		return StatusSnapshot{}, fmt.Errorf("append spawn_requested: %w", err)
	}

	childThreadID, err := s.createChildThread(ctx, tx)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if _, err := insertUserMessage(ctx, tx, s.parentRun.OrgID, childThreadID, input); err != nil {
		return StatusSnapshot{}, fmt.Errorf("insert child message: %w", err)
	}
	childRunID, err := s.createQueuedRun(ctx, tx, createdSubAgent, childThreadID, forcedRunID, data.SubAgentEventTypeSpawned, map[string]any{
		"thread_id": childThreadID.String(),
	}, nil)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return StatusSnapshot{}, err
	}
	if err := s.enqueueRun(ctx, childRunID); err != nil {
		MarkRunFailed(context.WithoutCancel(ctx), s.pool, s.rdb, childRunID)
		return StatusSnapshot{}, fmt.Errorf("enqueue child run: %w", err)
	}
	return s.GetStatus(ctx, createdSubAgent.ID)
}

func (s *Service) SendInput(ctx context.Context, req SendInputRequest) (StatusSnapshot, error) {
	input := strings.TrimSpace(req.Input)
	if req.SubAgentID == uuid.Nil {
		return StatusSnapshot{}, fmt.Errorf("sub_agent_id must not be empty")
	}
	if input == "" {
		return StatusSnapshot{}, fmt.Errorf("input must not be empty")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return StatusSnapshot{}, err
	}
	defer tx.Rollback(ctx)

	record, err := s.mustLoadOwnedSubAgent(ctx, tx, req.SubAgentID)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if record.Status != data.SubAgentStatusCompleted && record.Status != data.SubAgentStatusResumable && record.Status != data.SubAgentStatusWaitingInput {
		return StatusSnapshot{}, fmt.Errorf("send_input not allowed for sub_agent status: %s", record.Status)
	}
	threadID, runID, err := resolveSubAgentThread(ctx, tx, *record)
	if err != nil {
		return StatusSnapshot{}, err
	}
	messageID, err := insertUserMessage(ctx, tx, record.OrgID, threadID, input)
	if err != nil {
		return StatusSnapshot{}, fmt.Errorf("insert sub_agent input: %w", err)
	}
	if err := (data.SubAgentRepository{}).TransitionToResumable(ctx, tx, record.ID); err != nil {
		return StatusSnapshot{}, err
	}
	payload := map[string]any{
		"thread_id":   threadID.String(),
		"message_id":  messageID.String(),
		"input_bytes": len([]byte(input)),
		"interrupt":   req.Interrupt,
		"run_id":      runID.String(),
	}
	if _, err := (data.SubAgentEventAppender{}).Append(ctx, tx, record.ID, nil, data.SubAgentEventTypeInputSent, payload, nil); err != nil {
		return StatusSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return StatusSnapshot{}, err
	}
	return s.GetStatus(ctx, record.ID)
}

func (s *Service) Wait(ctx context.Context, req WaitRequest) (StatusSnapshot, error) {
	if req.SubAgentID == uuid.Nil {
		return StatusSnapshot{}, fmt.Errorf("sub_agent_id must not be empty")
	}
	waitCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	if err := s.appendLifecycleEvent(waitCtx, req.SubAgentID, data.SubAgentEventTypeWaitRequested, map[string]any{
		"timeout_ms": req.Timeout.Milliseconds(),
	}); err != nil {
		return StatusSnapshot{}, err
	}
	interval := s.waitPollInterval
	if interval <= 0 {
		interval = defaultWaitInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		snapshot, err := s.GetStatus(waitCtx, req.SubAgentID)
		if err != nil {
			return StatusSnapshot{}, err
		}
		if waitResolved(snapshot.Status) {
			payload := map[string]any{"status": snapshot.Status}
			if snapshot.CurrentRunID != nil {
				payload["run_id"] = snapshot.CurrentRunID.String()
			} else if snapshot.LastCompletedRunID != nil {
				payload["run_id"] = snapshot.LastCompletedRunID.String()
			}
			if snapshot.LastOutputRef != nil {
				payload["last_output_ref"] = *snapshot.LastOutputRef
			}
			if err := s.appendLifecycleEvent(context.WithoutCancel(waitCtx), req.SubAgentID, data.SubAgentEventTypeWaitResolved, payload); err != nil {
				return StatusSnapshot{}, err
			}
			return snapshot, nil
		}
		select {
		case <-waitCtx.Done():
			return StatusSnapshot{}, waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) Resume(ctx context.Context, req ResumeRequest) (StatusSnapshot, error) {
	if err := s.validateReady(); err != nil {
		return StatusSnapshot{}, err
	}
	if req.SubAgentID == uuid.Nil {
		return StatusSnapshot{}, fmt.Errorf("sub_agent_id must not be empty")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return StatusSnapshot{}, err
	}
	defer tx.Rollback(ctx)

	record, err := s.mustLoadOwnedSubAgent(ctx, tx, req.SubAgentID)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if record.Status != data.SubAgentStatusResumable {
		return StatusSnapshot{}, fmt.Errorf("resume not allowed for sub_agent status: %s", record.Status)
	}
	threadID, _, err := resolveSubAgentThread(ctx, tx, *record)
	if err != nil {
		return StatusSnapshot{}, err
	}
	childRunID, err := s.createQueuedRun(ctx, tx, *record, threadID, nil, data.SubAgentEventTypeResumed, map[string]any{
		"thread_id": threadID.String(),
	}, nil)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return StatusSnapshot{}, err
	}
	if err := s.enqueueRun(ctx, childRunID); err != nil {
		MarkRunFailed(context.WithoutCancel(ctx), s.pool, s.rdb, childRunID)
		return StatusSnapshot{}, fmt.Errorf("enqueue resumed child run: %w", err)
	}
	return s.GetStatus(ctx, record.ID)
}

func (s *Service) Close(ctx context.Context, req CloseRequest) (StatusSnapshot, error) {
	if req.SubAgentID == uuid.Nil {
		return StatusSnapshot{}, fmt.Errorf("sub_agent_id must not be empty")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return StatusSnapshot{}, err
	}
	defer tx.Rollback(ctx)

	record, err := s.mustLoadOwnedSubAgent(ctx, tx, req.SubAgentID)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if record.CurrentRunID != nil {
		return StatusSnapshot{}, fmt.Errorf("close not allowed while sub_agent run is active")
	}
	if err := (data.SubAgentRepository{}).TransitionToClosed(ctx, tx, record.ID); err != nil {
		return StatusSnapshot{}, err
	}
	if _, err := (data.SubAgentEventAppender{}).Append(ctx, tx, record.ID, nil, data.SubAgentEventTypeClosed, map[string]any{}, nil); err != nil {
		return StatusSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return StatusSnapshot{}, err
	}
	return s.GetStatus(ctx, record.ID)
}

func (s *Service) Interrupt(ctx context.Context, req InterruptRequest) (StatusSnapshot, error) {
	if req.SubAgentID == uuid.Nil {
		return StatusSnapshot{}, fmt.Errorf("sub_agent_id must not be empty")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return StatusSnapshot{}, err
	}
	defer tx.Rollback(ctx)

	record, err := s.mustLoadOwnedSubAgent(ctx, tx, req.SubAgentID)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if record.CurrentRunID == nil {
		return StatusSnapshot{}, fmt.Errorf("interrupt not allowed without active run")
	}
	if record.Status != data.SubAgentStatusQueued && record.Status != data.SubAgentStatusRunning {
		return StatusSnapshot{}, fmt.Errorf("interrupt not allowed for sub_agent status: %s", record.Status)
	}
	reason := strings.TrimSpace(req.Reason)
	payload := map[string]any{"run_id": record.CurrentRunID.String()}
	if reason != "" {
		payload["reason"] = reason
	}
	if _, err := (data.RunEventsRepository{}).AppendEvent(ctx, tx, *record.CurrentRunID, "run.cancel_requested", payload, nil, nil); err != nil {
		return StatusSnapshot{}, err
	}
	if _, err := (data.SubAgentEventAppender{}).Append(ctx, tx, record.ID, record.CurrentRunID, data.SubAgentEventTypeInterrupted, payload, nil); err != nil {
		return StatusSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return StatusSnapshot{}, err
	}
	return s.GetStatus(ctx, record.ID)
}

func (s *Service) GetStatus(ctx context.Context, subAgentID uuid.UUID) (StatusSnapshot, error) {
	if subAgentID == uuid.Nil {
		return StatusSnapshot{}, fmt.Errorf("sub_agent_id must not be empty")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return StatusSnapshot{}, err
	}
	defer tx.Rollback(ctx)

	record, err := s.mustLoadOwnedSubAgent(ctx, tx, subAgentID)
	if err != nil {
		return StatusSnapshot{}, err
	}
	return buildSnapshot(ctx, tx, *record)
}

func (s *Service) ListChildren(ctx context.Context) ([]StatusSnapshot, error) {
	records, err := (data.SubAgentRepository{}).ListByParentRun(ctx, s.pool, s.parentRun.ID)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	out := make([]StatusSnapshot, 0, len(records))
	for _, record := range records {
		snapshot, err := buildSnapshot(ctx, tx, record)
		if err != nil {
			return nil, err
		}
		out = append(out, snapshot)
	}
	return out, nil
}

func (s *Service) validateReady() error {
	if s.pool == nil {
		return fmt.Errorf("sub-agent control pool must not be nil")
	}
	if s.jobQueue == nil {
		return fmt.Errorf("sub-agent control job queue must not be nil")
	}
	return nil
}

func (s *Service) mustLoadOwnedSubAgent(ctx context.Context, tx pgx.Tx, subAgentID uuid.UUID) (*data.SubAgentRecord, error) {
	record, err := (data.SubAgentRepository{}).Get(ctx, tx, subAgentID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("sub_agent not found: %s", subAgentID)
	}
	if record.OrgID != s.parentRun.OrgID || record.ParentRunID != s.parentRun.ID {
		return nil, fmt.Errorf("sub_agent not owned by current run: %s", subAgentID)
	}
	return record, nil
}

func (s *Service) createChildThread(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	var childThreadID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO threads (org_id, project_id, is_private, expires_at)
		 VALUES ($1, $2, TRUE, now() + make_interval(secs => $3))
		 RETURNING id`,
		s.parentRun.OrgID,
		*s.parentRun.ProjectID,
		int64(childThreadTTL.Seconds()),
	).Scan(&childThreadID); err != nil {
		return uuid.Nil, fmt.Errorf("create child thread: %w", err)
	}
	return childThreadID, nil
}

func (s *Service) createQueuedRun(
	ctx context.Context,
	tx pgx.Tx,
	subAgent data.SubAgentRecord,
	threadID uuid.UUID,
	forcedRunID *uuid.UUID,
	primaryEventType string,
	primaryPayload map[string]any,
	errorClass *string,
) (uuid.UUID, error) {
	childRunID := uuid.New()
	if forcedRunID != nil && *forcedRunID != uuid.Nil {
		childRunID = *forcedRunID
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO runs (id, org_id, thread_id, parent_run_id, created_by_user_id, profile_ref, workspace_ref, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'running')`,
		childRunID,
		s.parentRun.OrgID,
		threadID,
		s.parentRun.ID,
		s.parentRun.CreatedByUserID,
		s.parentRun.ProfileRef,
		s.parentRun.WorkspaceRef,
	); err != nil {
		return uuid.Nil, fmt.Errorf("insert child run: %w", err)
	}

	var seq int64
	if err := tx.QueryRow(ctx, `SELECT nextval('run_events_seq_global')`).Scan(&seq); err != nil {
		return uuid.Nil, fmt.Errorf("alloc seq: %w", err)
	}
	personaID := derefString(subAgent.PersonaID)
	eventData, err := json.Marshal(map[string]any{"persona_id": personaID})
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal run.started data: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json)
		 VALUES ($1, $2, 'run.started', $3::jsonb)`,
		childRunID, seq, string(eventData),
	); err != nil {
		return uuid.Nil, fmt.Errorf("insert run.started: %w", err)
	}
	if err := (data.SubAgentRepository{}).TransitionToQueued(ctx, tx, subAgent.ID, childRunID); err != nil {
		return uuid.Nil, fmt.Errorf("mark sub_agent queued: %w", err)
	}
	payload := map[string]any{
		"run_id":     childRunID.String(),
		"thread_id":  threadID.String(),
		"persona_id": personaID,
	}
	for key, value := range primaryPayload {
		payload[key] = value
	}
	appender := data.SubAgentEventAppender{}
	if _, err := appender.Append(ctx, tx, subAgent.ID, &childRunID, primaryEventType, payload, errorClass); err != nil {
		return uuid.Nil, fmt.Errorf("append %s: %w", primaryEventType, err)
	}
	if _, err := appender.Append(ctx, tx, subAgent.ID, &childRunID, data.SubAgentEventTypeRunQueued, payload, nil); err != nil {
		return uuid.Nil, fmt.Errorf("append run_queued: %w", err)
	}
	return childRunID, nil
}

func (s *Service) enqueueRun(ctx context.Context, runID uuid.UUID) error {
	_, err := s.jobQueue.EnqueueRun(ctx, s.parentRun.OrgID, runID, s.traceID, queue.RunExecuteJobType, map[string]any{}, nil)
	return err
}

func (s *Service) appendLifecycleEvent(ctx context.Context, subAgentID uuid.UUID, eventType string, payload map[string]any) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	record, err := s.mustLoadOwnedSubAgent(ctx, tx, subAgentID)
	if err != nil {
		return err
	}
	if _, err := (data.SubAgentEventAppender{}).Append(ctx, tx, record.ID, record.CurrentRunID, eventType, payload, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func buildSnapshot(ctx context.Context, tx pgx.Tx, record data.SubAgentRecord) (StatusSnapshot, error) {
	snapshot := StatusSnapshot{
		SubAgentID:         record.ID,
		ParentRunID:        record.ParentRunID,
		RootRunID:          record.RootRunID,
		Depth:              record.Depth,
		Status:             record.Status,
		PersonaID:          cloneStringPtr(record.PersonaID),
		Nickname:           cloneStringPtr(record.Nickname),
		CurrentRunID:       cloneUUIDPtr(record.CurrentRunID),
		LastCompletedRunID: cloneUUIDPtr(record.LastCompletedRunID),
		LastOutputRef:      cloneStringPtr(record.LastOutputRef),
		LastError:          cloneStringPtr(record.LastError),
		StartedAt:          cloneTimePtr(record.StartedAt),
		CompletedAt:        cloneTimePtr(record.CompletedAt),
		ClosedAt:           cloneTimePtr(record.ClosedAt),
	}
	events, err := (data.SubAgentEventsRepository{}).ListBySubAgent(ctx, tx, record.ID, 0, 1)
	if err == nil && len(events) == 1 {
		seq := events[0].Seq
		eventType := events[0].Type
		snapshot.LastEventSeq = &seq
		snapshot.LastEventType = &eventType
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

func resolveSubAgentThread(ctx context.Context, tx pgx.Tx, record data.SubAgentRecord) (uuid.UUID, uuid.UUID, error) {
	candidateRunID := uuid.Nil
	if record.CurrentRunID != nil {
		candidateRunID = *record.CurrentRunID
	} else if record.LastCompletedRunID != nil {
		candidateRunID = *record.LastCompletedRunID
	}
	if candidateRunID == uuid.Nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("sub_agent has no run context")
	}
	run, err := (data.RunsRepository{}).GetRun(ctx, tx, candidateRunID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if run == nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("run not found: %s", candidateRunID)
	}
	return run.ThreadID, run.ID, nil
}

func waitResolved(status string) bool {
	switch strings.TrimSpace(status) {
	case data.SubAgentStatusCreated, data.SubAgentStatusQueued, data.SubAgentStatusRunning:
		return false
	default:
		return true
	}
}

func insertUserMessage(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, threadID uuid.UUID, content string) (uuid.UUID, error) {
	var messageID uuid.UUID
	err := tx.QueryRow(ctx,
		`INSERT INTO messages (org_id, thread_id, role, content, metadata_json)
		 VALUES ($1, $2, 'user', $3, '{}'::jsonb)
		 RETURNING id`,
		orgID,
		threadID,
		strings.TrimSpace(content),
	).Scan(&messageID)
	return messageID, err
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := strings.TrimSpace(*value)
	return &cloned
}

func cloneUUIDPtr(value *uuid.UUID) *uuid.UUID {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

var _ Control = (*Service)(nil)
