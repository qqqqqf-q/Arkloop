package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/runkind"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const heartbeatSchedulerReconnectDelay = 2 * time.Second

type LLMHeartbeat struct {
	pool       *pgxpool.Pool
	notifyPool *pgxpool.Pool
	jobs       *data.JobRepository
	runs       *data.RunEventRepository
	threads    *data.ThreadRepository
	runLimiter *data.RunLimiter
	triggers   data.ScheduledTriggersRepository
}

func NewLLMHeartbeat(
	pool *pgxpool.Pool,
	notifyPool *pgxpool.Pool,
	jobs *data.JobRepository,
	runs *data.RunEventRepository,
	threads *data.ThreadRepository,
	runLimiter *data.RunLimiter,
) *LLMHeartbeat {
	if notifyPool == nil {
		notifyPool = pool
	}
	return &LLMHeartbeat{
		pool:       pool,
		notifyPool: notifyPool,
		jobs:       jobs,
		runs:       runs,
		threads:    threads,
		runLimiter: runLimiter,
	}
}

func (s *LLMHeartbeat) Run(ctx context.Context) {
	if s.pool == nil || s.jobs == nil || s.runs == nil || s.threads == nil {
		return
	}

	wakeCh := make(chan struct{}, 1)
	s.signalWake(wakeCh)
	if s.notifyPool != nil {
		go s.listenForWake(ctx, wakeCh)
	}

	for {
		dueAt, err := s.triggers.GetEarliestHeartbeatDue(ctx, s.pool)
		if err != nil {
			slog.ErrorContext(ctx, "llm_heartbeat_due_lookup_failed", "error", err)
			if !waitForWake(ctx, wakeCh, heartbeatSchedulerReconnectDelay) {
				return
			}
			continue
		}
		hasSchedule := dueAt != nil
		slog.DebugContext(ctx, "llm_heartbeat_next_scheduled", "next_fire_at", formatNextFire(dueAt), "has_schedule", hasSchedule)

		if dueAt == nil {
			slog.DebugContext(ctx, "llm_heartbeat_waiting", "state", "idle")
			select {
			case <-ctx.Done():
				return
			case <-wakeCh:
				continue
			}
		}

		delay := time.Until(dueAt.UTC())
		if delay < 0 {
			delay = 0
		}
		slog.DebugContext(ctx, "llm_heartbeat_timer_armed", "next_fire_at", dueAt.UTC(), "delay", delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-wakeCh:
			if !timer.Stop() {
				<-timer.C
			}
			continue
		case <-timer.C:
		}

		rows, err := s.triggers.ClaimDueHeartbeats(ctx, s.pool, 8)
		if err != nil {
			slog.ErrorContext(ctx, "llm_heartbeat_claim_failed", "error", err)
			continue
		}
		if len(rows) > 0 {
			slog.InfoContext(ctx, "llm_heartbeat_due_claimed", "count", len(rows))
		}
		for _, row := range rows {
			s.fireOne(ctx, row)
		}
	}
}

func (s *LLMHeartbeat) listenForWake(ctx context.Context, wakeCh chan<- struct{}) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := s.listenOnce(ctx, wakeCh); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "llm_heartbeat_notify_lost", "error", err)
		}
		s.signalWake(wakeCh)
		select {
		case <-ctx.Done():
			return
		case <-time.After(heartbeatSchedulerReconnectDelay):
		}
	}
}

func (s *LLMHeartbeat) listenOnce(ctx context.Context, wakeCh chan<- struct{}) error {
	conn, err := s.notifyPool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `LISTEN "`+pgnotify.ChannelHeartbeat+`"`); err != nil {
		return err
	}
	for {
		if _, err := conn.Conn().WaitForNotification(ctx); err != nil {
			return err
		}
		s.signalWake(wakeCh)
	}
}

func (s *LLMHeartbeat) signalWake(wakeCh chan<- struct{}) {
	select {
	case wakeCh <- struct{}{}:
	default:
	}
}

func waitForWake(ctx context.Context, wakeCh <-chan struct{}, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-wakeCh:
		return true
	case <-timer.C:
		return true
	}
}

func formatNextFire(next *time.Time) any {
	if next == nil {
		return nil
	}
	return next.UTC()
}

func (s *LLMHeartbeat) fireOne(ctx context.Context, row data.ScheduledTriggerRow) {
	th, err := s.triggers.GetThreadByHeartbeatTrigger(ctx, s.pool, row)
	if err != nil {
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 2*time.Minute)
		return
	}
	if th == nil || th.DeletedAt != nil {
		_ = s.triggers.DeleteHeartbeat(ctx, s.pool, row.ChannelIdentityID)
		return
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.ErrorContext(ctx, "llm_heartbeat_tx_begin_failed", "error", err)
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 2*time.Minute)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	runRepoTx := s.runs.WithTx(tx)
	jobRepoTx := s.jobs.WithTx(tx)

	run, _, err := runRepoTx.CreateRootRunWithClaim(
		ctx,
		th.AccountID,
		th.ID,
		th.CreatedByUserID,
		"run.started",
		map[string]any{"persona_id": row.PersonaKey, "model": row.Model, "run_kind": runkind.Heartbeat},
	)
	if err != nil {
		if errors.Is(err, data.ErrThreadBusy) {
			delayMin := row.IntervalMin
			if delayMin <= 0 {
				delayMin = runkind.DefaultHeartbeatIntervalMinutes
			}
			_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, time.Duration(delayMin)*time.Minute)
			return
		}
		slog.ErrorContext(ctx, "llm_heartbeat_create_run_failed", "error", err)
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 90*time.Second)
		return
	}

	if s.runLimiter != nil {
		if !s.runLimiter.TryAcquire(ctx, th.AccountID) {
			_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 45*time.Second)
			return
		}
		defer s.runLimiter.Release(context.Background(), th.AccountID)
	}

	traceID := observability.NewTraceID()
	payload := map[string]any{
		"source":                     "llm_heartbeat_scheduler",
		"run_kind":                   runkind.Heartbeat,
		"heartbeat_interval_minutes": row.IntervalMin,
		"heartbeat_reason":           "interval",
		"persona_key":                row.PersonaKey,
		"model":                      row.Model,
	}
	if _, err := jobRepoTx.EnqueueRun(
		ctx,
		th.AccountID,
		run.ID,
		traceID,
		data.RunExecuteJobType,
		payload,
		nil,
	); err != nil {
		slog.ErrorContext(ctx, "llm_heartbeat_enqueue_failed", "error", err)
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 90*time.Second)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "llm_heartbeat_commit_failed", "error", err)
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 90*time.Second)
		return
	}
}
