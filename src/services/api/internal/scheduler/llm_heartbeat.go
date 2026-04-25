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

const schedulerReconnectDelay = 2 * time.Second

type TriggerScheduler struct {
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

func NewTriggerScheduler(
	pool *pgxpool.Pool,
	notifyPool *pgxpool.Pool,
	jobs *data.JobRepository,
	runs *data.RunEventRepository,
	threads *data.ThreadRepository,
	messages *data.MessageRepository,
	scheduledJobs *data.ScheduledJobsRepository,
	runLimiter *data.RunLimiter,
) *TriggerScheduler {
	if notifyPool == nil {
		notifyPool = pool
	}
	return &TriggerScheduler{
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

func (s *TriggerScheduler) Run(ctx context.Context) {
	if s.pool == nil || s.jobs == nil || s.runs == nil || s.threads == nil {
		return
	}

	wakeCh := make(chan struct{}, 1)
	s.signalWake(wakeCh)
	if s.notifyPool != nil {
		go s.listenForWake(ctx, wakeCh)
	}

	for {
		dueAt, err := s.triggers.GetEarliestDue(ctx, s.pool)
		if err != nil {
			slog.ErrorContext(ctx, "trigger_scheduler_due_lookup_failed", "error", err)
			if !waitForWake(ctx, wakeCh, schedulerReconnectDelay) {
				return
			}
			continue
		}
		hasSchedule := dueAt != nil
		slog.DebugContext(ctx, "trigger_scheduler_next_scheduled", "next_fire_at", formatNextFire(dueAt), "has_schedule", hasSchedule)

		if dueAt == nil {
			slog.DebugContext(ctx, "trigger_scheduler_waiting", "state", "idle")
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
		slog.DebugContext(ctx, "trigger_scheduler_timer_armed", "next_fire_at", dueAt.UTC(), "delay", delay)
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

		rows, err := s.triggers.ClaimDueTriggers(ctx, s.pool, 8)
		if err != nil {
			slog.ErrorContext(ctx, "trigger_scheduler_claim_failed", "error", err)
			continue
		}
		if len(rows) > 0 {
			slog.InfoContext(ctx, "trigger_scheduler_due_claimed", "count", len(rows))
		}
		for _, row := range rows {
			s.fireOne(ctx, row)
		}
	}
}

func (s *TriggerScheduler) listenForWake(ctx context.Context, wakeCh chan<- struct{}) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := s.listenOnce(ctx, wakeCh); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "trigger_scheduler_notify_lost", "error", err)
		}
		s.signalWake(wakeCh)
		select {
		case <-ctx.Done():
			return
		case <-time.After(schedulerReconnectDelay):
		}
	}
}

func (s *TriggerScheduler) listenOnce(ctx context.Context, wakeCh chan<- struct{}) error {
	conn, err := s.notifyPool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `LISTEN "`+pgnotify.ChannelHeartbeat+`"`); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, `LISTEN "`+pgnotify.ChannelScheduledJobs+`"`); err != nil {
		return err
	}
	for {
		if _, err := conn.Conn().WaitForNotification(ctx); err != nil {
			return err
		}
		s.signalWake(wakeCh)
	}
}

func (s *TriggerScheduler) signalWake(wakeCh chan<- struct{}) {
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

func (s *TriggerScheduler) fireOne(ctx context.Context, row data.ScheduledTriggerRow) {
	if row.TriggerKind == "job" {
		s.fireJob(ctx, row)
		return
	}
	s.fireHeartbeat(ctx, row)
}

func (s *TriggerScheduler) fireHeartbeat(ctx context.Context, row data.ScheduledTriggerRow) {
	ctxData, err := s.triggers.ResolveHeartbeatThread(ctx, s.pool, row)
	if err != nil {
		_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 2*time.Minute)
		return
	}
	if ctxData == nil {
		_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 2*time.Minute)
		return
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.ErrorContext(ctx, "trigger_scheduler_tx_begin_failed", "error", err)
		_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 2*time.Minute)
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
			_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, idleIntervalForCooldown(row.CooldownLevel))
			return
		}
		slog.ErrorContext(ctx, "trigger_scheduler_create_run_failed", "error", err)
		_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 90*time.Second)
		return
	}

	if s.runLimiter != nil {
		if !s.runLimiter.TryAcquire(ctx, ctxData.AccountID) {
			_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 45*time.Second)
			return
		}
		defer s.runLimiter.Release(context.Background(), ctxData.AccountID)
	}

	traceID := observability.NewTraceID()
	payload := map[string]any{
		"source":                     "trigger_scheduler",
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
		slog.ErrorContext(ctx, "trigger_scheduler_enqueue_failed", "error", err)
		_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 90*time.Second)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "trigger_scheduler_commit_failed", "error", err)
		_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 90*time.Second)
		return
	}
}

func (s *TriggerScheduler) fireJob(ctx context.Context, row data.ScheduledTriggerRow) {
	job, err := s.scheduledJobs.GetJobByID(ctx, s.pool, row.JobID)
	if err != nil {
		slog.ErrorContext(ctx, "scheduled_job_fetch_failed", "error", err, "job_id", row.JobID)
		_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 2*time.Minute)
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
			_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 2*time.Minute)
			return
		}
		thread, err := s.threads.Create(ctx, job.AccountID, job.CreatedByUserID, projectID, nil, false)
		if err != nil {
			slog.ErrorContext(ctx, "scheduled_job_create_thread_failed", "error", err, "account_id", job.AccountID)
			_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 2*time.Minute)
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

	// 提前短路: 若 thread 已有活跃 run 则跳过后续开销较大的事务流程。
	// 真正的并发安全由 CreateRootRunWithClaim 内部的 LockThreadRow 行级锁保证，
	// 此处检查仅为优化，不构成互斥屏障。
	active, err := s.runs.GetActiveRootRunForThread(ctx, threadID)
	if err != nil {
		slog.ErrorContext(ctx, "scheduled_job_active_run_check_failed", "error", err, "thread_id", threadID)
		_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 2*time.Minute)
		return
	}
	if active != nil {
		delayMin := row.IntervalMin
		if delayMin <= 0 {
			delayMin = runkind.DefaultHeartbeatIntervalMinutes
		}
		_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, time.Duration(delayMin)*time.Minute)
		return
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.ErrorContext(ctx, "scheduled_job_tx_begin_failed", "error", err)
		_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 2*time.Minute)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	runRepoTx := s.runs.WithTx(tx)
	jobRepoTx := s.jobs.WithTx(tx)

	if strings.TrimSpace(job.Prompt) != "" && s.messages != nil {
		messagesTx := s.messages.WithTx(tx)
		if _, err := messagesTx.Create(ctx, job.AccountID, threadID, "user", job.Prompt, job.CreatedByUserID); err != nil {
			slog.ErrorContext(ctx, "scheduled_job_insert_user_message_failed", "error", err)
			_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 90*time.Second)
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
		"timeout_seconds":      job.Timeout,
	}
	if strings.TrimSpace(job.ReasoningMode) != "" {
		startedData["reasoning_mode"] = job.ReasoningMode
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
			_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, time.Duration(delayMin)*time.Minute)
			return
		}
		slog.ErrorContext(ctx, "scheduled_job_create_run_failed", "error", err)
		_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 90*time.Second)
		return
	}

	if s.runLimiter != nil {
		if !s.runLimiter.TryAcquire(ctx, job.AccountID) {
			_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 45*time.Second)
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
		_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 90*time.Second)
		return
	}

	// At 类型: one-shot job 在事务内完成最终状态切换，然后提交并返回
	if job.ScheduleKind == schedulekind.At {
		if job.DeleteAfterRun {
			if _, err := tx.Exec(ctx, `DELETE FROM scheduled_jobs WHERE id = $1`, job.ID); err != nil {
				slog.ErrorContext(ctx, "scheduled_job_delete_after_run_failed", "error", err, "job_id", job.ID)
				_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 90*time.Second)
				return
			}
		} else {
			if _, err := tx.Exec(ctx, `UPDATE scheduled_jobs SET enabled = false, updated_at = now() WHERE id = $1`, job.ID); err != nil {
				slog.ErrorContext(ctx, "scheduled_job_disable_at_failed", "error", err)
				_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 90*time.Second)
				return
			}
			if err := s.triggers.DeleteTriggerByJobID(ctx, tx, row.JobID); err != nil {
				slog.ErrorContext(ctx, "scheduled_job_delete_at_trigger_failed", "error", err)
				_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 90*time.Second)
				return
			}
		}
		if err := tx.Commit(ctx); err != nil {
			slog.ErrorContext(ctx, "scheduled_job_commit_failed", "error", err)
			_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 90*time.Second)
		}
		return
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "scheduled_job_commit_failed", "error", err)
		_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 90*time.Second)
		return
	}

	nextFire, err := schedulekind.CalcNextFire(
		job.ScheduleKind,
		derefInt(job.IntervalMin),
		job.DailyTime,
		derefInt(job.MonthlyDay),
		job.MonthlyTime,
		derefInt(job.WeeklyDay),
		derefTime(job.FireAt),
		job.CronExpr,
		job.Timezone,
		time.Now().UTC(),
	)
	if err != nil {
		slog.ErrorContext(ctx, "scheduled_job_calc_next_fire_failed", "error", err)
		_ = s.triggers.PostponeTrigger(ctx, s.pool, row.ID, 2*time.Minute)
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

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func idleIntervalForCooldown(_ int) time.Duration {
	return time.Minute
}
