package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/runkind"
	"arkloop/services/shared/schedulekind"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const heartbeatSchedulerReconnectDelay = 2 * time.Second

type LLMHeartbeat struct {
	pool          *pgxpool.Pool
	notifyPool    *pgxpool.Pool
	jobs          *data.JobRepository
	runs          *data.RunEventRepository
	threads       *data.ThreadRepository
	messages      *data.MessageRepository
	scheduledJobs *data.ScheduledJobsRepository
	runLimiter    *data.RunLimiter
	triggers      data.ScheduledTriggersRepository
}

func NewLLMHeartbeat(
	pool *pgxpool.Pool,
	notifyPool *pgxpool.Pool,
	jobs *data.JobRepository,
	runs *data.RunEventRepository,
	threads *data.ThreadRepository,
	messages *data.MessageRepository,
	scheduledJobs *data.ScheduledJobsRepository,
	runLimiter *data.RunLimiter,
) *LLMHeartbeat {
	if notifyPool == nil {
		notifyPool = pool
	}
	return &LLMHeartbeat{
		pool:          pool,
		notifyPool:    notifyPool,
		jobs:          jobs,
		runs:          runs,
		threads:       threads,
		messages:      messages,
		scheduledJobs: scheduledJobs,
		runLimiter:    runLimiter,
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
	if row.TriggerKind == "job" {
		s.fireJob(ctx, row)
		return
	}
	s.fireHeartbeat(ctx, row)
}

func (s *LLMHeartbeat) fireHeartbeat(ctx context.Context, row data.ScheduledTriggerRow) {
	ctxData, err := s.triggers.ResolveHeartbeatThread(ctx, s.pool, row)
	if err != nil {
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 2*time.Minute)
		return
	}
	if ctxData == nil {
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 2*time.Minute)
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
		ctxData.AccountID,
		ctxData.ThreadID,
		ctxData.CreatedByUserID,
		"run.started",
		map[string]any{
			"persona_id": row.PersonaKey,
			"model":      row.Model,
			"run_kind":   runkind.Heartbeat,
			"channel_delivery": map[string]any{
				"channel_id":                 ctxData.ChannelID.String(),
				"channel_type":               ctxData.ChannelType,
				"sender_channel_identity_id": ctxData.IdentityID.String(),
				"conversation_type":          ctxData.ConversationType,
				"conversation_ref": map[string]any{
					"target": ctxData.PlatformChatID,
				},
			},
		},
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
		if !s.runLimiter.TryAcquire(ctx, ctxData.AccountID) {
			_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 45*time.Second)
			return
		}
		defer s.runLimiter.Release(context.Background(), ctxData.AccountID)
	}

	traceID := observability.NewTraceID()
	payload := map[string]any{
		"source":                     "llm_heartbeat_scheduler",
		"run_kind":                   runkind.Heartbeat,
		"heartbeat_interval_minutes": row.IntervalMin,
		"heartbeat_reason":           "interval",
		"persona_key":                row.PersonaKey,
		"model":                      row.Model,
		"channel_delivery": map[string]any{
			"channel_id":                 ctxData.ChannelID.String(),
			"channel_type":               ctxData.ChannelType,
			"sender_channel_identity_id": ctxData.IdentityID.String(),
			"conversation_type":          ctxData.ConversationType,
			"conversation_ref": map[string]any{
				"target": ctxData.PlatformChatID,
			},
		},
	}
	if _, err := jobRepoTx.EnqueueRun(
		ctx,
		ctxData.AccountID,
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

func (s *LLMHeartbeat) fireJob(ctx context.Context, row data.ScheduledTriggerRow) {
	job, err := s.scheduledJobs.GetJobByID(ctx, s.pool, row.JobID)
	if err != nil {
		slog.ErrorContext(ctx, "scheduled_job_fetch_failed", "error", err, "job_id", row.JobID)
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 2*time.Minute)
		return
	}
	if job == nil || !job.Enabled {
		if delErr := s.triggers.DeleteTriggerByJobID(ctx, s.pool, row.JobID); delErr != nil {
			slog.ErrorContext(ctx, "scheduled_job_delete_trigger_failed", "error", delErr, "job_id", row.JobID)
		}
		return
	}

	var threadID uuid.UUID
	if job.ThreadID != nil {
		threadID = *job.ThreadID
	} else {
		var projectID uuid.UUID
		err := s.pool.QueryRow(ctx,
			`SELECT id FROM projects WHERE account_id = $1 AND deleted_at IS NULL ORDER BY created_at ASC LIMIT 1`,
			job.AccountID,
		).Scan(&projectID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				slog.ErrorContext(ctx, "scheduled_job_no_project", "account_id", job.AccountID)
			} else {
				slog.ErrorContext(ctx, "scheduled_job_project_lookup_failed", "error", err, "account_id", job.AccountID)
			}
			_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 2*time.Minute)
			return
		}
		thread, err := s.threads.Create(ctx, job.AccountID, job.CreatedByUserID, projectID, nil, false)
		if err != nil {
			slog.ErrorContext(ctx, "scheduled_job_create_thread_failed", "error", err, "account_id", job.AccountID)
			_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 2*time.Minute)
			return
		}
		threadID = thread.ID
	}

	model := job.Model
	if model == "" && job.ThreadID != nil {
		var lastModel string
		if err := s.pool.QueryRow(ctx,
			`SELECT model FROM runs WHERE thread_id = $1 AND model IS NOT NULL AND model <> '' ORDER BY created_at DESC LIMIT 1`,
			*job.ThreadID,
		).Scan(&lastModel); err == nil && lastModel != "" {
			model = lastModel
		}
	}

	personaKey := job.PersonaKey
	if personaKey == "" && job.ThreadID != nil {
		var lastPersona string
		if err := s.pool.QueryRow(ctx,
			`SELECT persona_id FROM runs WHERE thread_id = $1 AND persona_id IS NOT NULL AND persona_id <> '' ORDER BY created_at DESC LIMIT 1`,
			*job.ThreadID,
		).Scan(&lastPersona); err == nil && lastPersona != "" {
			personaKey = lastPersona
		}
	}
	if personaKey == "" {
		personaKey = runkind.DefaultPersonaKey
	}

	// 复用 RunEventRepository 的 busy 检查逻辑
	active, err := s.runs.GetActiveRootRunForThread(ctx, threadID)
	if err != nil {
		slog.ErrorContext(ctx, "scheduled_job_active_run_check_failed", "error", err, "thread_id", threadID)
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 2*time.Minute)
		return
	}
	if active != nil {
		delayMin := row.IntervalMin
		if delayMin <= 0 {
			delayMin = runkind.DefaultHeartbeatIntervalMinutes
		}
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, time.Duration(delayMin)*time.Minute)
		return
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.ErrorContext(ctx, "scheduled_job_tx_begin_failed", "error", err)
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 2*time.Minute)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	runRepoTx := s.runs.WithTx(tx)
	jobRepoTx := s.jobs.WithTx(tx)

	if strings.TrimSpace(job.Prompt) != "" && s.messages != nil {
		messagesTx := s.messages.WithTx(tx)
		if _, err := messagesTx.Create(ctx, job.AccountID, threadID, "user", job.Prompt, job.CreatedByUserID); err != nil {
			slog.ErrorContext(ctx, "scheduled_job_insert_user_message_failed", "error", err)
			_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 90*time.Second)
			return
		}
	}

	startedData := map[string]any{
		"persona_id":           personaKey,
		"model":                model,
		"run_kind":             runkind.ScheduledJob,
		"scheduled_job_id":     job.ID.String(),
		"scheduled_job_name":   job.Name,
		"scheduled_job_prompt": job.Prompt,
		"workspace_ref":        job.WorkspaceRef,
		"work_dir":             job.WorkDir,
	}
	run, _, err := runRepoTx.CreateRootRunWithClaim(
		ctx,
		job.AccountID,
		threadID,
		job.CreatedByUserID,
		"run.started",
		startedData,
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
		slog.ErrorContext(ctx, "scheduled_job_create_run_failed", "error", err)
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 90*time.Second)
		return
	}

	if s.runLimiter != nil {
		if !s.runLimiter.TryAcquire(ctx, job.AccountID) {
			_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 45*time.Second)
			return
		}
		defer s.runLimiter.Release(context.Background(), job.AccountID)
	}

	traceID := observability.NewTraceID()
	payload := map[string]any{
		"source":               "scheduled_job_scheduler",
		"run_kind":             runkind.ScheduledJob,
		"scheduled_job_id":     job.ID.String(),
		"scheduled_job_name":   job.Name,
		"scheduled_job_prompt": job.Prompt,
		"persona_id":           personaKey,
		"model":                model,
		"workspace_ref":        job.WorkspaceRef,
		"work_dir":             job.WorkDir,
	}
	if _, err := jobRepoTx.EnqueueRun(
		ctx,
		job.AccountID,
		run.ID,
		traceID,
		data.RunExecuteJobType,
		payload,
		nil,
	); err != nil {
		slog.ErrorContext(ctx, "scheduled_job_enqueue_failed", "error", err)
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 90*time.Second)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "scheduled_job_commit_failed", "error", err)
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 90*time.Second)
		return
	}

	nextFire, err := schedulekind.CalcNextFire(
		job.ScheduleKind,
		derefInt(job.IntervalMin),
		job.DailyTime,
		derefInt(job.MonthlyDay),
		job.MonthlyTime,
		derefInt(job.WeeklyDay),
		job.Timezone,
		time.Now().UTC(),
	)
	if err != nil {
		slog.ErrorContext(ctx, "scheduled_job_calc_next_fire_failed", "error", err)
		_ = s.triggers.PostponeHeartbeat(ctx, s.pool, row.ID, 2*time.Minute)
		return
	}
	if err := s.triggers.UpdateTriggerNextFire(ctx, s.pool, row.ID, nextFire); err != nil {
		slog.ErrorContext(ctx, "scheduled_job_update_trigger_failed", "error", err)
	}
}

func derefInt(p *int) int {
	if p != nil {
		return *p
	}
	return 0
}
