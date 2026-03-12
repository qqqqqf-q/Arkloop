package subagentctl

import (
	"context"
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

const defaultWaitInterval = 150 * time.Millisecond

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

type Service struct {
	pool             *pgxpool.Pool
	rdb              *redis.Client
	jobQueue         queue.JobQueue
	parentRun        data.Run
	traceID          string
	waitPollInterval time.Duration
	planner          *ChildRunPlanner
	factory          *SubAgentRunFactory
	projector        *SubAgentStateProjector
	snapshotBuilder  *SnapshotBuilder
	snapshotStorage  *SnapshotStorage
	governor         *SpawnGovernor
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
	service := NewService(pool, rdb, jobQueue, parentRun, traceID, SubAgentLimits{})
	_, err := service.spawn(ctx, SpawnRequest{PersonaID: personaID, ContextMode: data.SubAgentContextModeIsolated, Input: input}, &forcedRunID)
	return err
}

func NewService(pool *pgxpool.Pool, rdb *redis.Client, jobQueue queue.JobQueue, parentRun data.Run, traceID string, limits SubAgentLimits) *Service {
	snapshotStorage := NewSnapshotStorage()
	factory := NewSubAgentRunFactory(pool, snapshotStorage)
	projector := NewSubAgentStateProjector(pool, rdb, jobQueue)
	projector.factory = factory
	return &Service{
		pool:             pool,
		rdb:              rdb,
		jobQueue:         jobQueue,
		parentRun:        parentRun,
		traceID:          strings.TrimSpace(traceID),
		waitPollInterval: defaultWaitInterval,
		planner:          NewChildRunPlanner(),
		factory:          factory,
		projector:        projector,
		snapshotBuilder:  NewSnapshotBuilder(),
		snapshotStorage:  snapshotStorage,
		governor:         NewSpawnGovernor(limits),
	}
}

func MarkRunning(ctx context.Context, pool *pgxpool.Pool, runID uuid.UUID) error {
	return NewSubAgentStateProjector(pool, nil, nil).MarkRunning(ctx, runID)
}

func MarkRunFailed(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, childRunID uuid.UUID) {
	_ = NewSubAgentStateProjector(pool, rdb, nil).MarkRunFailed(ctx, childRunID, "failed to enqueue child run job")
}

func (s *Service) Spawn(ctx context.Context, req SpawnRequest) (StatusSnapshot, error) {
	return s.spawn(ctx, req, nil)
}

func (s *Service) spawn(ctx context.Context, req SpawnRequest, forcedRunID *uuid.UUID) (StatusSnapshot, error) {
	if err := s.validateReady(); err != nil {
		return StatusSnapshot{}, err
	}
	plan, err := s.planner.PlanSpawn(req)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if plan.Spawn == nil {
		return StatusSnapshot{}, fmt.Errorf("spawn plan missing resolved request")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return StatusSnapshot{}, err
	}
	defer tx.Rollback(ctx)

	lineage, err := (data.RunsRepository{}).GetLineage(ctx, tx, s.parentRun.ID)
	if err != nil {
		return StatusSnapshot{}, fmt.Errorf("get lineage for governance: %w", err)
	}
	if err := s.governor.ValidateSpawn(ctx, tx, s.parentRun, lineage.RootRunID, lineage.Depth+1); err != nil {
		return StatusSnapshot{}, err
	}

	snapshot, err := s.snapshotBuilder.Build(ctx, tx, s.parentRun, *plan.Spawn)
	if err != nil {
		return StatusSnapshot{}, err
	}
	record, childRunID, err := s.factory.CreateSpawnRun(ctx, tx, s.parentRun, *plan.Spawn, snapshot, forcedRunID)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return StatusSnapshot{}, err
	}
	if err := s.projector.EnqueueRun(ctx, s.parentRun.OrgID, childRunID, s.traceID); err != nil {
		_ = s.projector.MarkRunFailed(context.Background(), childRunID, "failed to enqueue child run job")
		return StatusSnapshot{}, fmt.Errorf("enqueue child run: %w", err)
	}
	return s.GetStatus(ctx, record.ID)
}

func (s *Service) SendInput(ctx context.Context, req SendInputRequest) (StatusSnapshot, error) {
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
	plan, err := s.planner.PlanSendInput(*record, req)
	if err != nil {
		return StatusSnapshot{}, err
	}

	var childRunID *uuid.UUID
	switch plan.Mode {
	case childRunPlanModeQueue:
		if err := s.governor.ValidatePendingInput(ctx, tx, record.RootRunID); err != nil {
			return StatusSnapshot{}, err
		}
		queuedInput, err := (data.SubAgentPendingInputsRepository{}).Enqueue(ctx, tx, record.ID, plan.Input, plan.Priority)
		if err != nil {
			return StatusSnapshot{}, err
		}
		payload := map[string]any{
			"input_bytes": len([]byte(plan.Input)),
			"interrupt":   plan.InterruptActiveRun,
			"queued":      true,
			"pending_seq": queuedInput.Seq,
		}
		if record.CurrentRunID != nil {
			payload["run_id"] = record.CurrentRunID.String()
		}
		if _, err := (data.SubAgentEventAppender{}).Append(ctx, tx, record.ID, record.CurrentRunID, data.SubAgentEventTypeInputSent, payload, nil); err != nil {
			return StatusSnapshot{}, err
		}
		if plan.InterruptActiveRun {
			if record.CurrentRunID == nil {
				return StatusSnapshot{}, fmt.Errorf("interrupt not allowed without active run")
			}
			if _, err := (data.RunEventsRepository{}).AppendEvent(ctx, tx, *record.CurrentRunID, "run.cancel_requested", map[string]any{"run_id": record.CurrentRunID.String()}, nil, nil); err != nil {
				return StatusSnapshot{}, err
			}
			if _, err := (data.SubAgentEventAppender{}).Append(ctx, tx, record.ID, record.CurrentRunID, data.SubAgentEventTypeInterrupted, map[string]any{
				"run_id": record.CurrentRunID.String(),
			}, nil); err != nil {
				return StatusSnapshot{}, err
			}
		}
	case childRunPlanModeCreateRun:
		runID, err := s.factory.CreateRunForExistingSubAgent(
			ctx,
			tx,
			*record,
			plan.Input,
			nil,
			plan.PrimaryEventType,
			map[string]any{"interrupt": req.Interrupt},
			nil,
		)
		if err != nil {
			return StatusSnapshot{}, err
		}
		childRunID = &runID
	default:
		return StatusSnapshot{}, fmt.Errorf("unsupported send_input plan mode: %s", plan.Mode)
	}

	if err := tx.Commit(ctx); err != nil {
		return StatusSnapshot{}, err
	}
	if childRunID != nil {
		if err := s.projector.EnqueueRun(ctx, s.parentRun.OrgID, *childRunID, s.traceID); err != nil {
			_ = s.projector.MarkRunFailed(context.Background(), *childRunID, "failed to enqueue child run job")
			return StatusSnapshot{}, fmt.Errorf("enqueue child run: %w", err)
		}
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
	plan, err := s.planner.PlanResume(*record)
	if err != nil {
		return StatusSnapshot{}, err
	}
	childRunID, err := s.factory.CreateRunForExistingSubAgent(ctx, tx, *record, "", nil, plan.PrimaryEventType, map[string]any{}, nil)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return StatusSnapshot{}, err
	}
	if err := s.projector.EnqueueRun(ctx, s.parentRun.OrgID, childRunID, s.traceID); err != nil {
		_ = s.projector.MarkRunFailed(context.Background(), childRunID, "failed to enqueue child run job")
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
	return s.projector.BuildSnapshot(ctx, tx, *record)
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
		snapshot, err := s.projector.BuildSnapshot(ctx, tx, record)
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
	if s.snapshotBuilder == nil {
		return fmt.Errorf("sub-agent snapshot builder must not be nil")
	}
	if s.snapshotStorage == nil {
		return fmt.Errorf("sub-agent snapshot storage must not be nil")
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

func waitResolved(status string) bool {
	switch strings.TrimSpace(status) {
	case data.SubAgentStatusCreated, data.SubAgentStatusQueued, data.SubAgentStatusRunning:
		return false
	default:
		return true
	}
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
