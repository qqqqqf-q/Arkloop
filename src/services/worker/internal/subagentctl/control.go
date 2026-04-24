package subagentctl

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/rollout"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	GetRolloutRecorder(subAgentID uuid.UUID) (*rollout.Recorder, bool)
}

type Service struct {
	pool             data.DB
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
	blobStore        objectstore.BlobStore
	recorders        sync.Map // map[uuid.UUID]*rollout.Recorder, keyed by subAgentID
}

func CreateInitialRun(
	ctx context.Context,
	pool data.DB,
	rdb *redis.Client,
	jobQueue queue.JobQueue,
	parentRun data.Run,
	traceID string,
	forcedRunID uuid.UUID,
	personaID string,
	input string,
) error {
	service := NewService(pool, rdb, jobQueue, parentRun, traceID, SubAgentLimits{}, BackpressureConfig{}, nil)
	_, err := service.spawn(ctx, SpawnRequest{PersonaID: personaID, ContextMode: data.SubAgentContextModeIsolated, Input: input}, &forcedRunID)
	return err
}

func NewService(pool data.DB, rdb *redis.Client, jobQueue queue.JobQueue, parentRun data.Run, traceID string, limits SubAgentLimits, bp BackpressureConfig, blobStore objectstore.BlobStore) *Service {
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
		governor:         NewSpawnGovernor(limits, bp),
		blobStore:        blobStore,
	}
}

func MarkRunning(ctx context.Context, pool data.DB, runID uuid.UUID) error {
	return NewSubAgentStateProjector(pool, nil, nil).MarkRunning(ctx, runID)
}

func MarkRunFailed(ctx context.Context, pool data.DB, rdb *redis.Client, childRunID uuid.UUID) {
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
	defer func() { _ = tx.Rollback(ctx) }()
	ownerThreadID := s.parentRun.ThreadID
	depth := 1
	if parentSubAgent, err := (data.SubAgentRepository{}).GetByCurrentRunID(ctx, tx, s.parentRun.ID); err != nil {
		return StatusSnapshot{}, fmt.Errorf("get parent sub-agent for governance: %w", err)
	} else if parentSubAgent != nil {
		depth = parentSubAgent.Depth + 1
	}
	if err := s.governor.ValidateSpawn(ctx, tx, ownerThreadID, depth); err != nil {
		return StatusSnapshot{}, err
	}
	if err := s.governor.ValidateBackpressureForSpawn(ctx, tx, ownerThreadID); err != nil {
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

	// 背压 pause 策略下延迟入队
	var availableAt *time.Time
	if bpTx, bpErr := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}); bpErr == nil {
		bp, _ := s.governor.EvaluateBackpressure(ctx, bpTx, ownerThreadID)
		_ = bpTx.Rollback(ctx)
		if bp.Level == BackpressureCritical && bp.Strategy == BackpressureStrategyPause {
			t := time.Now().Add(5 * time.Second)
			availableAt = &t
		}
	}

	// Build job payload with subAgentID for rollout recorder lookup
	jobPayload := map[string]any{"sub_agent_id": record.ID.String()}

	if err := s.projector.EnqueueRun(ctx, s.parentRun.AccountID, childRunID, s.traceID, availableAt, jobPayload); err != nil {
		_ = s.projector.MarkRunFailed(context.Background(), childRunID, "failed to enqueue child run job")
		return StatusSnapshot{}, fmt.Errorf("enqueue child run: %w", err)
	}

	// Create and start rollout recorder if blobStore is available (non-desktop mode)
	if s.rdb != nil && s.blobStore != nil {
		recorder := rollout.NewRecorder(s.blobStore, childRunID)
		recorder.Start(ctx)
		s.recorders.Store(record.ID, recorder)
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
	defer func() { _ = tx.Rollback(ctx) }()

	record, err := s.mustLoadOwnedSubAgent(ctx, tx, req.SubAgentID)
	if err != nil {
		return StatusSnapshot{}, err
	}
	plan, err := s.planner.PlanSendInput(*record, req)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if err := s.governor.ValidateBackpressureForSendInput(ctx, tx, record.OwnerThreadID, req.Interrupt); err != nil {
		return StatusSnapshot{}, err
	}

	var childRunID *uuid.UUID
	switch plan.Mode {
	case childRunPlanModeQueue:
		if err := s.governor.ValidatePendingInput(ctx, tx, record.OwnerThreadID); err != nil {
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
		jobPayload := map[string]any{"sub_agent_id": record.ID.String()}
		if err := s.projector.EnqueueRun(ctx, s.parentRun.AccountID, *childRunID, s.traceID, nil, jobPayload); err != nil {
			_ = s.projector.MarkRunFailed(context.Background(), *childRunID, "failed to enqueue child run job")
			return StatusSnapshot{}, fmt.Errorf("enqueue child run: %w", err)
		}
	}
	return s.GetStatus(ctx, record.ID)
}

func (s *Service) Wait(ctx context.Context, req WaitRequest) (StatusSnapshot, error) {
	if len(req.SubAgentIDs) == 0 {
		return StatusSnapshot{}, fmt.Errorf("sub_agent_ids must not be empty")
	}

	waitCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	for _, id := range req.SubAgentIDs {
		_ = s.appendLifecycleEvent(waitCtx, id, data.SubAgentEventTypeWaitRequested, map[string]any{
			"timeout_ms": req.Timeout.Milliseconds(),
		})
	}

	// 单 ID 快速路径
	if len(req.SubAgentIDs) == 1 {
		return s.waitOne(waitCtx, req.Timeout, req.SubAgentIDs[0])
	}

	// 多 ID：并发等待，返回第一个终态的
	type result struct {
		snapshot StatusSnapshot
		err      error
	}
	raceCtx, raceCancel := context.WithCancel(waitCtx)
	defer raceCancel()

	ch := make(chan result, len(req.SubAgentIDs))
	var wg sync.WaitGroup
	for _, id := range req.SubAgentIDs {
		wg.Add(1)
		go func(subAgentID uuid.UUID) {
			defer wg.Done()
			snap, err := s.waitOne(raceCtx, req.Timeout, subAgentID)
			select {
			case ch <- result{snap, err}:
			case <-raceCtx.Done():
			}
		}(id)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	for r := range ch {
		if r.err != nil {
			continue
		}
		if waitResolved(r.snapshot.Status) {
			raceCancel()
			return r.snapshot, nil
		}
	}

	return StatusSnapshot{}, waitCtx.Err()
}

// waitOne 等待单个子代理进入终态。
func (s *Service) waitOne(waitCtx context.Context, timeout time.Duration, subAgentID uuid.UUID) (StatusSnapshot, error) {
	snapshot, err := s.GetStatus(waitCtx, subAgentID)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if waitResolved(snapshot.Status) {
		return s.resolveWait(waitCtx, subAgentID, timeout, snapshot)
	}

	if s.rdb != nil && snapshot.CurrentRunID != nil {
		return s.waitOneByRedis(waitCtx, timeout, subAgentID, snapshot)
	}
	return s.waitOneByPoll(waitCtx, timeout, subAgentID, snapshot)
}

// waitOneByRedis 通过 Redis Subscribe 等待单个子代理终态。
func (s *Service) waitOneByRedis(waitCtx context.Context, timeout time.Duration, subAgentID uuid.UUID, lastSnapshot StatusSnapshot) (StatusSnapshot, error) {
	ch := fmt.Sprintf("run.child.%s.done", lastSnapshot.CurrentRunID.String())
	sub := s.rdb.Subscribe(waitCtx, ch)
	defer func() { _ = sub.Close() }()

	// Subscribe 后立即重检，防止事件遗漏
	snapshot, err := s.GetStatus(waitCtx, subAgentID)
	if err != nil {
		return lastSnapshot, err
	}
	lastSnapshot = snapshot
	if waitResolved(snapshot.Status) {
		return s.resolveWait(waitCtx, subAgentID, timeout, snapshot)
	}

	msgCh := sub.Channel()
	for {
		select {
		case <-waitCtx.Done():
			_ = s.appendLifecycleEvent(context.WithoutCancel(waitCtx), subAgentID, data.SubAgentEventTypeWaitTimeout, map[string]any{
				"status":     lastSnapshot.Status,
				"timeout_ms": timeout.Milliseconds(),
			})
			return lastSnapshot, waitCtx.Err()
		case <-msgCh:
		}

		snapshot, err := s.GetStatus(waitCtx, subAgentID)
		if err != nil {
			return lastSnapshot, err
		}
		lastSnapshot = snapshot
		if waitResolved(snapshot.Status) {
			return s.resolveWait(waitCtx, subAgentID, timeout, snapshot)
		}
	}
}

// waitOneByPoll 轮询等待单个子代理终态（desktop 模式）。
func (s *Service) waitOneByPoll(waitCtx context.Context, timeout time.Duration, subAgentID uuid.UUID, lastSnapshot StatusSnapshot) (StatusSnapshot, error) {
	interval := s.waitPollInterval
	if interval <= 0 {
		interval = defaultWaitInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			_ = s.appendLifecycleEvent(context.WithoutCancel(waitCtx), subAgentID, data.SubAgentEventTypeWaitTimeout, map[string]any{
				"status":     lastSnapshot.Status,
				"timeout_ms": timeout.Milliseconds(),
			})
			return lastSnapshot, waitCtx.Err()
		case <-ticker.C:
		}

		snapshot, err := s.GetStatus(waitCtx, subAgentID)
		if err != nil {
			return lastSnapshot, err
		}
		lastSnapshot = snapshot
		if waitResolved(snapshot.Status) {
			return s.resolveWait(waitCtx, subAgentID, timeout, snapshot)
		}
	}
}

// resolveWait 记录 wait_resolved 生命周期事件并返回快照。
func (s *Service) resolveWait(waitCtx context.Context, subAgentID uuid.UUID, timeout time.Duration, snapshot StatusSnapshot) (StatusSnapshot, error) {
	payload := map[string]any{"status": snapshot.Status}
	if snapshot.CurrentRunID != nil {
		payload["run_id"] = snapshot.CurrentRunID.String()
	} else if snapshot.LastCompletedRunID != nil {
		payload["run_id"] = snapshot.LastCompletedRunID.String()
	}
	if snapshot.LastOutputRef != nil {
		payload["last_output_ref"] = *snapshot.LastOutputRef
	}
	if err := s.appendLifecycleEvent(context.WithoutCancel(waitCtx), subAgentID, data.SubAgentEventTypeWaitResolved, payload); err != nil {
		return StatusSnapshot{}, err
	}
	return snapshot, nil
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
	defer func() { _ = tx.Rollback(ctx) }()

	record, err := s.mustLoadOwnedSubAgent(ctx, tx, req.SubAgentID)
	if err != nil {
		return StatusSnapshot{}, err
	}
	plan, err := s.planner.PlanResume(*record)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if err := s.governor.ValidateBackpressureForResume(ctx, tx, record.OwnerThreadID); err != nil {
		return StatusSnapshot{}, err
	}

	var reconstructedMessages []ContextSnapshotMessage
	if req.RolloutStore != nil && record.LastCompletedRunID != nil {
		items, readErr := rollout.NewReader(req.RolloutStore).ReadRollout(ctx, *record.LastCompletedRunID)
		if readErr != nil {
			slog.Warn("resume: failed to read rollout, falling back to snapshot", "err", readErr, "sub_agent_id", req.SubAgentID, "run_id", record.LastCompletedRunID)
		} else {
			state := rollout.NewReader(req.RolloutStore).Reconstruct(items)
			reconstructedMessages = make([]ContextSnapshotMessage, 0, len(state.Messages))
			for _, rawMsg := range state.Messages {
				var assistant rollout.AssistantMessage
				if err := json.Unmarshal(rawMsg, &assistant); err != nil {
					slog.Warn("resume: failed to unmarshal assistant message, skipping", "err", err)
					continue
				}
				reconstructedMessages = append(reconstructedMessages, ContextSnapshotMessage{
					Role:        "assistant",
					Content:     assistant.Content,
					ContentJSON: assistant.ToolCalls,
				})
			}
			slog.Info("resume: reconstructed messages from rollout", "count", len(reconstructedMessages), "sub_agent_id", req.SubAgentID)
		}
	}

	childRunID, err := s.factory.CreateRunForExistingSubAgent(ctx, tx, *record, "", nil, plan.PrimaryEventType, map[string]any{}, nil, reconstructedMessages)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return StatusSnapshot{}, err
	}
	// Build job payload with subAgentID for rollout recorder lookup
	jobPayload := map[string]any{"sub_agent_id": req.SubAgentID.String()}
	if err := s.projector.EnqueueRun(ctx, s.parentRun.AccountID, childRunID, s.traceID, nil, jobPayload); err != nil {
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
	defer func() { _ = tx.Rollback(ctx) }()

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
	defer func() { _ = tx.Rollback(ctx) }()

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
	defer func() { _ = tx.Rollback(ctx) }()

	record, err := s.mustLoadOwnedSubAgent(ctx, tx, subAgentID)
	if err != nil {
		return StatusSnapshot{}, err
	}
	snapshot, err := s.projector.BuildSnapshot(ctx, tx, *record)
	if err != nil {
		return StatusSnapshot{}, err
	}
	bp, _ := s.governor.EvaluateBackpressure(ctx, tx, record.OwnerThreadID)
	if bp.Level == BackpressureCritical {
		snapshot.Degraded = true
	}
	return snapshot, nil
}

func (s *Service) ListChildren(ctx context.Context) ([]StatusSnapshot, error) {
	records, err := (data.SubAgentRepository{}).ListByOwnerThread(ctx, s.pool, s.parentRun.ThreadID)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

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
	if record.AccountID != s.parentRun.AccountID || record.OwnerThreadID != s.parentRun.ThreadID {
		return nil, fmt.Errorf("sub_agent not owned by current run: %s", subAgentID)
	}
	return record, nil
}

func (s *Service) appendLifecycleEvent(ctx context.Context, subAgentID uuid.UUID, eventType string, payload map[string]any) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
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

// GetRolloutRecorder returns the recorder for the given subAgentID, if any.
func (s *Service) GetRolloutRecorder(subAgentID uuid.UUID) (*rollout.Recorder, bool) {
	if s.blobStore == nil {
		return nil, false
	}
	rec, ok := s.recorders.Load(subAgentID)
	if !ok {
		return nil, false
	}
	return rec.(*rollout.Recorder), true
}

var _ Control = (*Service)(nil)
