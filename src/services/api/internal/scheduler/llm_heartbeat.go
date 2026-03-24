package scheduler

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/runkind"

	"github.com/jackc/pgx/v5/pgxpool"
)

type LLMHeartbeat struct {
	pool         *pgxpool.Pool
	jobs         *data.JobRepository
	runs         *data.RunEventRepository
	threads      *data.ThreadRepository
	runLimiter   *data.RunLimiter
	triggers     data.ScheduledTriggersRepository
	tickInterval time.Duration
}

func NewLLMHeartbeat(
	pool *pgxpool.Pool,
	jobs *data.JobRepository,
	runs *data.RunEventRepository,
	threads *data.ThreadRepository,
	runLimiter *data.RunLimiter,
) *LLMHeartbeat {
	interval := 30 * time.Second
	if raw := strings.TrimSpace(os.Getenv("ARKLOOP_LLM_HEARTBEAT_TICK_SECONDS")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			interval = time.Duration(v) * time.Second
		}
	}
	return &LLMHeartbeat{
		pool:         pool,
		jobs:         jobs,
		runs:         runs,
		threads:      threads,
		runLimiter:   runLimiter,
		tickInterval: interval,
	}
}

func (s *LLMHeartbeat) Run(ctx context.Context) {
	if s.pool == nil || s.jobs == nil || s.runs == nil || s.threads == nil {
		return
	}
	ticker := time.NewTicker(s.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *LLMHeartbeat) tick(ctx context.Context) {
	rows, err := s.triggers.ClaimDueHeartbeats(ctx, s.pool, 8)
	if err != nil {
		slog.ErrorContext(ctx, "llm_heartbeat_claim_failed", "error", err)
		return
	}
	for _, row := range rows {
		s.fireOne(ctx, row)
	}
}

func (s *LLMHeartbeat) fireOne(ctx context.Context, row data.ScheduledTriggerRow) {
	th, err := s.triggers.GetThreadByChannelIdentity(ctx, s.pool, row.ChannelIdentityID)
	if err != nil {
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 2*time.Minute)
		return
	}
	if th == nil || th.DeletedAt != nil {
		_ = s.triggers.DeleteHeartbeat(ctx, s.pool, row.ChannelIdentityID)
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
	started := map[string]any{"persona_id": row.PersonaKey, "model": row.Model}
	run, _, err := s.runs.CreateRunWithStartedEvent(ctx, th.AccountID, th.ID, th.CreatedByUserID, "run.started", started)
	if err != nil {
		slog.ErrorContext(ctx, "llm_heartbeat_create_run_failed", "error", err)
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 90*time.Second)
		return
	}

	payload := map[string]any{
		"source":                     "llm_heartbeat_scheduler",
		"run_kind":                   runkind.Heartbeat,
		"heartbeat_interval_minutes": row.IntervalMin,
		"heartbeat_reason":           "interval",
		"persona_key":                row.PersonaKey,
		"model":                      row.Model,
	}
	if _, err := s.jobs.EnqueueRun(ctx, th.AccountID, run.ID, traceID, data.RunExecuteJobType, payload, nil); err != nil {
		slog.ErrorContext(ctx, "llm_heartbeat_enqueue_failed", "error", err)
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 90*time.Second)
	}
}
