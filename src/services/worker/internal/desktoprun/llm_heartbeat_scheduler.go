//go:build desktop

package desktoprun

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/runkind"
	"arkloop/services/shared/schedulekind"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const desktopSchedulerReconnectDelay = 2 * time.Second

func startDesktopTriggerScheduler(
	ctx context.Context,
	db data.DesktopDB,
	q queue.JobQueue,
	bus eventbus.EventBus,
) {
	if db == nil || q == nil {
		slog.WarnContext(ctx, "desktop_trigger_scheduler: db or queue is nil, skipping")
		return
	}

	wakeCh := make(chan struct{}, 1)
	if bus != nil {
		go listenDesktopSchedulerWake(ctx, bus, wakeCh)
		go listenDesktopSchedulerJobsWake(ctx, bus, wakeCh)
	}

	for {
		dueAt, err := data.ScheduledTriggersRepository{}.GetEarliestDue(ctx, db)
		if err != nil {
			slog.ErrorContext(ctx, "desktop_trigger_due_lookup_failed", "error", err)
			if !waitForDesktopWake(ctx, wakeCh, desktopSchedulerReconnectDelay) {
				return
			}
			continue
		}
		if dueAt == nil {
			slog.DebugContext(ctx, "desktop_trigger_waiting", "state", "idle")
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
		slog.DebugContext(ctx, "desktop_trigger_timer_armed", "next_fire_at", dueAt.UTC(), "delay", delay)
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

		desktopTriggerTick(ctx, db, q)
	}
}

func listenDesktopSchedulerWake(ctx context.Context, bus eventbus.EventBus, wakeCh chan struct{}) {
	listenDesktopWake(ctx, bus, pgnotify.ChannelHeartbeat, wakeCh)
}

func listenDesktopSchedulerJobsWake(ctx context.Context, bus eventbus.EventBus, wakeCh chan struct{}) {
	listenDesktopWake(ctx, bus, pgnotify.ChannelScheduledJobs, wakeCh)
}

func listenDesktopWake(ctx context.Context, bus eventbus.EventBus, topic string, wakeCh chan struct{}) {
	for {
		sub, err := bus.Subscribe(ctx, topic)
		if err != nil {
			if !waitForDesktopWake(ctx, wakeCh, desktopSchedulerReconnectDelay) {
				return
			}
			continue
		}
		for {
			select {
			case <-ctx.Done():
				_ = sub.Close()
				return
			case _, ok := <-sub.Channel():
				if !ok {
					_ = sub.Close()
					if !waitForDesktopWake(ctx, wakeCh, desktopSchedulerReconnectDelay) {
						return
					}
					goto retry
				}
				signalDesktopWake(wakeCh)
			}
		}
	retry:
	}
}

func signalDesktopWake(wakeCh chan<- struct{}) {
	select {
	case wakeCh <- struct{}{}:
	default:
	}
}

func waitForDesktopWake(ctx context.Context, wakeCh <-chan struct{}, delay time.Duration) bool {
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

func desktopTriggerTick(
	ctx context.Context,
	db data.DesktopDB,
	q queue.JobQueue,
) {
	repo := data.ScheduledTriggersRepository{}
	rows, err := repo.ClaimDueTriggers(ctx, db, 8)
	if err != nil {
		slog.ErrorContext(ctx, "desktop_trigger_claim_failed", "error", err)
		return
	}
	if len(rows) > 0 {
		slog.InfoContext(ctx, "desktop_trigger_due_claimed", "count", len(rows))
	}
	for _, row := range rows {
		switch row.TriggerKind {
		case "heartbeat", "":
			desktopFireTrigger(ctx, db, q, row)
		case "job":
			desktopFireJob(ctx, db, q, row)
		default:
			slog.WarnContext(ctx, "desktop_scheduler_unknown_trigger_kind",
				"trigger_kind", row.TriggerKind,
				"trigger_id", row.ID.String(),
			)
			_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
		}
	}
}

func desktopFireTrigger(
	ctx context.Context,
	db data.DesktopDB,
	q queue.JobQueue,
	row data.ScheduledTriggerRow,
) {
	repo := data.ScheduledTriggersRepository{}
	ctxData, err := repo.ResolveHeartbeatThread(ctx, db, row)
	if err != nil {
		if errors.Is(err, data.ErrHeartbeatIdentityGone) {
			slog.WarnContext(ctx, "desktop_trigger_stale_trigger_removed",
				"channel_identity_id", row.ChannelIdentityID.String(),
			)
			_ = repo.DeleteHeartbeat(ctx, db, row.ChannelID, row.ChannelIdentityID)
		} else {
			slog.ErrorContext(ctx, "desktop_trigger_thread_resolution_failed",
				"channel_identity_id", row.ChannelIdentityID.String(),
				"error", err,
			)
			_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
		}
		return
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.ErrorContext(ctx, "desktop_trigger_tx_begin_failed", "error", err)
		_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
		return
	}

	result, err := repo.InsertHeartbeatRunInTx(ctx, tx, row, ctxData, row.Model)
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, data.ErrThreadBusy) {
			delayMin := row.IntervalMin
			if delayMin <= 0 {
				delayMin = runkind.DefaultHeartbeatIntervalMinutes
			}
			_ = repo.PostponeTrigger(ctx, db, row.ID, time.Duration(delayMin)*time.Minute)
			return
		}
		if errors.Is(err, data.ErrHeartbeatIdentityGone) {
			slog.WarnContext(ctx, "desktop_trigger_stale_trigger_removed",
				"channel_identity_id", row.ChannelIdentityID.String(),
			)
			_ = repo.DeleteHeartbeat(ctx, db, row.ChannelID, row.ChannelIdentityID)
			return
		}
		slog.ErrorContext(ctx, "desktop_trigger_create_run_failed",
			"channel_identity_id", row.ChannelIdentityID.String(),
			"error", err,
		)
		_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "desktop_trigger_commit_failed", "error", err)
		_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
		return
	}

	payload := map[string]any{
		"source":                     "desktop_trigger_scheduler",
		"run_kind":                   runkind.Heartbeat,
		"trigger_interval_minutes":   row.IntervalMin,
		"trigger_reason":             "interval",
		"persona_key":                row.PersonaKey,
		"model":                      row.Model,
		"channel_delivery": map[string]any{
			"channel_id":                 result.ChannelID,
			"channel_type":               result.ChannelType,
			"sender_channel_identity_id": result.IdentityID,
			"conversation_type":          result.ConversationType,
			"conversation_ref": map[string]any{
				"target": result.PlatformChatID,
			},
		},
	}
	traceID := uuid.NewString()
	if _, err := q.EnqueueRun(ctx, row.AccountID, result.RunID, traceID, queue.RunExecuteJobType, payload, nil); err != nil {
		slog.ErrorContext(ctx, "desktop_trigger_enqueue_failed",
			"run_id", result.RunID.String(),
			"error", err,
		)
		_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
	}
}

func desktopFireJob(
	ctx context.Context,
	db data.DesktopDB,
	q queue.JobQueue,
	row data.ScheduledTriggerRow,
) {
	repo := data.ScheduledTriggersRepository{}
	jobsRepo := data.DesktopScheduledJobsRepository{}

	job, err := jobsRepo.GetJobByID(ctx, db, row.JobID)
	if err != nil {
		slog.ErrorContext(ctx, "desktop_job_get_failed",
			"job_id", row.JobID.String(),
			"error", err,
		)
		_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
		return
	}
	if job == nil || !job.Enabled {
		_ = repo.DeleteTriggerByJobID(ctx, db, row.JobID)
		return
	}

	var threadID uuid.UUID
	if job.ThreadID != nil {
		threadID = *job.ThreadID
	} else {
		var projectIDStr string
		err := db.QueryRow(ctx,
			`SELECT id FROM projects WHERE account_id = $1 AND deleted_at IS NULL ORDER BY created_at ASC LIMIT 1`,
			job.AccountID.String(),
		).Scan(&projectIDStr)
		if err != nil {
			if isNoRows(err) {
				slog.ErrorContext(ctx, "desktop_job_no_project",
					"account_id", job.AccountID.String(),
				)
			}
			_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
			return
		}
		threadID = uuid.New()
		_, err = db.Exec(ctx,
			`INSERT INTO threads (id, account_id, project_id, is_private, created_at) VALUES ($1, $2, $3, 1, datetime('now'))`,
			threadID.String(), job.AccountID.String(), projectIDStr,
		)
		if err != nil {
			slog.ErrorContext(ctx, "desktop_job_create_thread_failed",
				"error", err,
			)
			_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
			return
		}
	}

	model := job.Model
	if model == "" && job.ThreadID != nil {
		var lastModel string
		if err := db.QueryRow(ctx,
			`SELECT model FROM runs WHERE thread_id = $1 AND model IS NOT NULL AND model <> '' ORDER BY created_at DESC LIMIT 1`,
			threadID.String(),
		).Scan(&lastModel); err == nil && lastModel != "" {
			model = lastModel
		}
	}

	personaKey := job.PersonaKey
	if personaKey == "" && job.ThreadID != nil {
		var lastPersona string
		if err := db.QueryRow(ctx,
			`SELECT persona_id FROM runs WHERE thread_id = $1 AND persona_id IS NOT NULL AND persona_id <> '' ORDER BY created_at DESC LIMIT 1`,
			threadID.String(),
		).Scan(&lastPersona); err == nil && lastPersona != "" {
			personaKey = lastPersona
		}
	}
	if personaKey == "" {
		personaKey = runkind.DefaultPersonaKey
	}

	var busy int
	err = db.QueryRow(ctx,
		`SELECT 1 FROM runs WHERE thread_id = $1 AND parent_run_id IS NULL AND status IN ('running','cancelling') LIMIT 1`,
		threadID.String(),
	).Scan(&busy)
	if err != nil && !isNoRows(err) {
		slog.ErrorContext(ctx, "desktop_job_check_thread_busy_failed",
			"thread_id", threadID.String(),
			"error", err,
		)
		_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
		return
	}
	if busy == 1 {
		delayMin := 2
		if job.IntervalMin != nil && *job.IntervalMin > 0 {
			delayMin = *job.IntervalMin
		}
		_ = repo.PostponeTrigger(ctx, db, row.ID, time.Duration(delayMin)*time.Minute)
		return
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.ErrorContext(ctx, "desktop_job_tx_begin_failed", "error", err)
		_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	runID := uuid.New()
	var createdByUserID *uuid.UUID
	if job.CreatedByUserID != nil {
		createdByUserID = job.CreatedByUserID
	}
	if strings.TrimSpace(job.Prompt) != "" {
		if _, err := (data.MessagesRepository{}).InsertThreadMessage(
			ctx, tx, job.AccountID, threadID, "user", job.Prompt, nil, createdByUserID,
		); err != nil {
			slog.ErrorContext(ctx, "desktop_job_insert_user_message_failed", "error", err)
			_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
			return
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO runs (id, account_id, thread_id, created_by_user_id, status) VALUES ($1, $2, $3, $4, 'running')`,
		runID.String(), job.AccountID.String(), threadID.String(), createdByUserID,
	); err != nil {
		slog.ErrorContext(ctx, "desktop_job_insert_run_failed", "error", err)
		_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
		return
	}

	eventsRepo := data.DesktopRunEventsRepository{}
	startedData := map[string]any{
		"run_kind":             runkind.ScheduledJob,
		"persona_id":           personaKey,
		"scheduled_job_id":     job.ID.String(),
		"scheduled_job_name":   job.Name,
		"scheduled_job_prompt": job.Prompt,
		"model":                model,
		"workspace_ref":        job.WorkspaceRef,
		"work_dir":             job.WorkDir,
	}
	if _, err := eventsRepo.AppendEvent(ctx, tx, runID, "run.started", startedData, nil, nil); err != nil {
		slog.ErrorContext(ctx, "desktop_job_append_started_failed", "error", err)
		_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
		return
	}

	if job.ScheduleKind == schedulekind.At {
		if _, err := tx.Exec(ctx,
			`UPDATE scheduled_jobs SET enabled = 0, updated_at = datetime('now') WHERE id = $1`,
			job.ID.String(),
		); err != nil {
			slog.ErrorContext(ctx, "desktop_job_disable_at_failed", "error", err)
			_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "desktop_job_commit_failed", "error", err)
		_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
		return
	}

	payload := map[string]any{
		"source":               "desktop_trigger_scheduler",
		"run_kind":             runkind.ScheduledJob,
		"scheduled_job_id":     job.ID.String(),
		"scheduled_job_name":   job.Name,
		"scheduled_job_prompt": job.Prompt,
		"persona_id":           personaKey,
		"model":                model,
		"workspace_ref":        job.WorkspaceRef,
		"work_dir":             job.WorkDir,
	}
	traceID := uuid.NewString()
	if _, err := q.EnqueueRun(ctx, job.AccountID, runID, traceID, queue.RunExecuteJobType, payload, nil); err != nil {
		slog.ErrorContext(ctx, "desktop_job_enqueue_failed",
			"run_id", runID.String(),
			"error", err,
		)
		_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
		return
	}

	if job.ScheduleKind == schedulekind.At {
		_ = repo.DeleteTriggerByJobID(ctx, db, row.JobID)
		return
	}

	nextFire, err := schedulekind.CalcNextFire(
		job.ScheduleKind,
		derefInt(job.IntervalMin),
		job.DailyTime,
		derefInt(job.MonthlyDay),
		job.MonthlyTime,
		derefInt(job.WeeklyDay),
		derefFireAt(job.FireAt),
		job.CronExpr,
		job.Timezone,
		time.Now().UTC(),
	)
	if err != nil {
		slog.ErrorContext(ctx, "desktop_job_calc_next_fire_failed", "error", err)
		_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
		return
	}
	if err := repo.UpdateTriggerNextFire(ctx, db, row.ID, nextFire); err != nil {
		slog.ErrorContext(ctx, "desktop_job_update_next_fire_failed", "error", err)
		_ = repo.PostponeTrigger(ctx, db, row.ID, 2*time.Minute)
		return
	}
}

func derefInt(p *int) int {
	if p != nil {
		return *p
	}
	return 0
}

func derefFireAt(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
