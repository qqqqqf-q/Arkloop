//go:build desktop

package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	shareddesktop "arkloop/services/shared/desktop"
	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/schedulekind"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DesktopScheduledJobsRepository 提供 desktop 侧的 scheduled_jobs CRUD（SQLite）。
type DesktopScheduledJobsRepository struct{}

type desktopScheduledJobsQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// GetJobByID 按 ID 加载 job 定义。
func (DesktopScheduledJobsRepository) GetJobByID(
	ctx context.Context,
	db DB,
	id uuid.UUID,
) (*ScheduledJob, error) {
	return desktopGetJobByID(ctx, db, id)
}

// ListByAccount 列出 account 下所有 job，附带 trigger 的 next_fire_at。
func (DesktopScheduledJobsRepository) ListByAccount(
	ctx context.Context,
	db DB,
	accountID uuid.UUID,
) ([]ScheduledJobWithTrigger, error) {
	rows, err := db.Query(ctx, `
		SELECT j.id, j.account_id, j.name, j.description, j.persona_key, j.prompt,
		       j.model, j.workspace_ref, j.work_dir, j.thread_id, j.schedule_kind,
		       j.interval_min, j.daily_time, j.monthly_day, j.monthly_time, j.weekly_day, j.timezone,
		       j.enabled, j.created_by_user_id, j.created_at, j.updated_at,
		       j.fire_at, j.cron_expr,
		       j.delete_after_run, j.reasoning_mode, j.timeout_seconds,
		       t.next_fire_at
		  FROM scheduled_jobs j
		  LEFT JOIN scheduled_triggers t ON t.job_id = j.id
		 WHERE j.account_id = $1
		 ORDER BY j.created_at DESC`, accountID.String())
	if err != nil {
		return nil, fmt.Errorf("list scheduled_jobs: %w", err)
	}
	defer rows.Close()

	var out []ScheduledJobWithTrigger
	for rows.Next() {
		var r ScheduledJobWithTrigger
		var idStr, accountStr string
		var threadIDStr, createdByStr *string
		var nextFireAt *time.Time
		if err := rows.Scan(
			&idStr, &accountStr, &r.Name, &r.Description, &r.PersonaKey, &r.Prompt,
			&r.Model, &r.WorkspaceRef, &r.WorkDir, &threadIDStr, &r.ScheduleKind,
			&r.IntervalMin, &r.DailyTime, &r.MonthlyDay, &r.MonthlyTime, &r.WeeklyDay, &r.Timezone,
			&r.Enabled, &createdByStr, &r.CreatedAt, &r.UpdatedAt,
			&r.FireAt, &r.CronExpr,
			&r.DeleteAfterRun, &r.ReasoningMode, &r.Timeout,
			&nextFireAt,
		); err != nil {
			return nil, fmt.Errorf("scan scheduled_jobs: %w", err)
		}
		r.ID, _ = uuid.Parse(idStr)
		r.AccountID, _ = uuid.Parse(accountStr)
		if threadIDStr != nil {
			parsed, _ := uuid.Parse(*threadIDStr)
			r.ThreadID = &parsed
		}
		if createdByStr != nil {
			parsed, _ := uuid.Parse(*createdByStr)
			r.CreatedByUserID = &parsed
		}
		r.NextFireAt = nextFireAt
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetByID 按 ID + accountID 获取 job，附带 trigger。
func (DesktopScheduledJobsRepository) GetByID(
	ctx context.Context,
	db DB,
	id, accountID uuid.UUID,
) (*ScheduledJobWithTrigger, error) {
	var r ScheduledJobWithTrigger
	var idStr, accountStr string
	var threadIDStr, createdByStr *string
	var nextFireAt *time.Time
	err := db.QueryRow(ctx, `
		SELECT j.id, j.account_id, j.name, j.description, j.persona_key, j.prompt,
		       j.model, j.workspace_ref, j.work_dir, j.thread_id, j.schedule_kind,
		       j.interval_min, j.daily_time, j.monthly_day, j.monthly_time, j.weekly_day, j.timezone,
		       j.enabled, j.created_by_user_id, j.created_at, j.updated_at,
		       j.fire_at, j.cron_expr,
		       j.delete_after_run, j.reasoning_mode, j.timeout_seconds,
		       t.next_fire_at
		  FROM scheduled_jobs j
		  LEFT JOIN scheduled_triggers t ON t.job_id = j.id
		 WHERE j.id = $1 AND j.account_id = $2`, id.String(), accountID.String(),
	).Scan(
		&idStr, &accountStr, &r.Name, &r.Description, &r.PersonaKey, &r.Prompt,
		&r.Model, &r.WorkspaceRef, &r.WorkDir, &threadIDStr, &r.ScheduleKind,
		&r.IntervalMin, &r.DailyTime, &r.MonthlyDay, &r.MonthlyTime, &r.WeeklyDay, &r.Timezone,
		&r.Enabled, &createdByStr, &r.CreatedAt, &r.UpdatedAt,
		&r.FireAt, &r.CronExpr,
		&r.DeleteAfterRun, &r.ReasoningMode, &r.Timeout,
		&nextFireAt,
	)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get scheduled_job: %w", err)
	}
	r.ID, _ = uuid.Parse(idStr)
	r.AccountID, _ = uuid.Parse(accountStr)
	if threadIDStr != nil {
		parsed, _ := uuid.Parse(*threadIDStr)
		r.ThreadID = &parsed
	}
	if createdByStr != nil {
		parsed, _ := uuid.Parse(*createdByStr)
		r.CreatedByUserID = &parsed
	}
	r.NextFireAt = nextFireAt
	return &r, nil
}

// CreateJob 创建 scheduled_job 并插入对应 trigger。
func (DesktopScheduledJobsRepository) CreateJob(
	ctx context.Context,
	db DB,
	job ScheduledJob,
) (created ScheduledJob, err error) {
	if job.AccountID == uuid.Nil {
		return ScheduledJob{}, fmt.Errorf("account_id must not be empty")
	}
	if err := validateScheduledJob(job); err != nil {
		return ScheduledJob{}, err
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ScheduledJob{}, err
	}
	defer finishScheduledJobsTx(ctx, tx, &err)

	now := time.Now().UTC()
	job.CreatedAt = now
	job.UpdatedAt = now

	var threadIDStr *string
	if job.ThreadID != nil {
		s := job.ThreadID.String()
		threadIDStr = &s
	}
	var createdByStr *string
	if job.CreatedByUserID != nil {
		s := job.CreatedByUserID.String()
		createdByStr = &s
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO scheduled_jobs
		    (id, account_id, name, description, persona_key, prompt, model,
		     workspace_ref, work_dir, thread_id, schedule_kind, interval_min,
		     daily_time, monthly_day, monthly_time, weekly_day, timezone, enabled, created_by_user_id,
		     fire_at, cron_expr, delete_after_run, reasoning_mode, timeout_seconds,
		     created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)`,
		job.ID.String(), job.AccountID.String(), job.Name, job.Description,
		job.PersonaKey, job.Prompt, job.Model, job.WorkspaceRef, job.WorkDir,
		threadIDStr, job.ScheduleKind, job.IntervalMin, job.DailyTime,
		job.MonthlyDay, job.MonthlyTime, job.WeeklyDay, job.Timezone, job.Enabled, createdByStr,
		job.FireAt, job.CronExpr,
		job.DeleteAfterRun, job.ReasoningMode, job.Timeout,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return ScheduledJob{}, fmt.Errorf("insert scheduled_jobs: %w", err)
	}

	if job.Enabled {
		if err := desktopInsertJobTrigger(ctx, tx, job); err != nil {
			return ScheduledJob{}, err
		}
	}

	return job, nil
}

// UpdateJob 部分更新 scheduled_job。
func (DesktopScheduledJobsRepository) UpdateJob(
	ctx context.Context,
	db DB,
	id, accountID uuid.UUID,
	upd UpdateJobParams,
) (err error) {
	setClauses := []string{}
	args := []any{}
	argIdx := 1

	addSet := func(col string, val any) {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, argIdx))
		args = append(args, val)
		argIdx++
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer finishScheduledJobsTx(ctx, tx, &err)

	current, err := desktopGetJobByID(ctx, tx, id)
	if err != nil {
		return err
	}
	if current == nil || current.AccountID != accountID {
		return fmt.Errorf("scheduled_job %s not found", id)
	}

	next := *current
	applyJobUpdate(&next, upd)

	scheduleChanged := false
	if upd.Name != nil {
		addSet("name", *upd.Name)
	}
	if upd.Description != nil {
		addSet("description", *upd.Description)
	}
	if upd.PersonaKey != nil {
		addSet("persona_key", *upd.PersonaKey)
	}
	if upd.Prompt != nil {
		addSet("prompt", *upd.Prompt)
	}
	if upd.Model != nil {
		addSet("model", *upd.Model)
	}
	if upd.WorkDir != nil {
		addSet("work_dir", *upd.WorkDir)
	}
	if upd.ScheduleKind != nil {
		addSet("schedule_kind", *upd.ScheduleKind)
		scheduleChanged = true
	}
	if upd.IntervalMin != nil {
		addSet("interval_min", *upd.IntervalMin)
		scheduleChanged = true
	}
	if upd.DailyTime != nil {
		addSet("daily_time", *upd.DailyTime)
		scheduleChanged = true
	}
	if upd.MonthlyDay != nil {
		addSet("monthly_day", *upd.MonthlyDay)
		scheduleChanged = true
	}
	if upd.MonthlyTime != nil {
		addSet("monthly_time", *upd.MonthlyTime)
		scheduleChanged = true
	}
	if upd.WeeklyDay != nil {
		addSet("weekly_day", *upd.WeeklyDay)
		scheduleChanged = true
	}
	if upd.Timezone != nil {
		addSet("timezone", next.Timezone)
		scheduleChanged = true
	}
	if upd.Enabled != nil {
		addSet("enabled", *upd.Enabled)
	}
	if upd.ThreadID != nil {
		var v *string
		if *upd.ThreadID != nil {
			s := (*upd.ThreadID).String()
			v = &s
		}
		addSet("thread_id", v)
	}
	if upd.FireAt != nil {
		addSet("fire_at", *upd.FireAt)
		scheduleChanged = true
	}
	if upd.CronExpr != nil {
		addSet("cron_expr", *upd.CronExpr)
		scheduleChanged = true
	}
	if upd.DeleteAfterRun != nil {
		addSet("delete_after_run", *upd.DeleteAfterRun)
	}
	if upd.ReasoningMode != nil {
		addSet("reasoning_mode", *upd.ReasoningMode)
	}
	if upd.Timeout != nil {
		addSet("timeout_seconds", *upd.Timeout)
	}

	if len(setClauses) == 0 {
		return nil
	}

	if scheduleChanged || shouldValidateEnabledTransition(upd, current.Enabled) {
		if err := validateScheduledJob(next); err != nil {
			return err
		}
	}

	addSet("updated_at", time.Now().UTC().Format(time.RFC3339Nano))

	whereID := fmt.Sprintf("$%d", argIdx)
	args = append(args, id.String())
	argIdx++
	whereAccount := fmt.Sprintf("$%d", argIdx)
	args = append(args, accountID.String())

	sql := fmt.Sprintf("UPDATE scheduled_jobs SET %s WHERE id = %s AND account_id = %s",
		strings.Join(setClauses, ", "), whereID, whereAccount)

	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update scheduled_jobs: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("scheduled_job %s not found", id)
	}

	// enabled 切换
	if upd.Enabled != nil && !*upd.Enabled {
		_, err := tx.Exec(ctx, `DELETE FROM scheduled_triggers WHERE job_id = $1`, id.String())
		return err
	}
	if upd.Enabled != nil && *upd.Enabled {
		return desktopInsertJobTrigger(ctx, tx, next)
	}

	// schedule 参数变更，重算 next_fire_at
	if scheduleChanged {
		if !next.Enabled {
			return nil
		}
		nextFire, err := desktopCalcJobNextFire(next)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE scheduled_triggers SET next_fire_at = $1, updated_at = $2 WHERE job_id = $3`,
			nextFire.Format(time.RFC3339Nano),
			time.Now().UTC().Format(time.RFC3339Nano),
			id.String())
		return err
	}

	return nil
}

// DeleteJob 删除 scheduled_job（trigger 由 ON DELETE CASCADE 或手动删除）。
func (DesktopScheduledJobsRepository) DeleteJob(
	ctx context.Context,
	db DB,
	id, accountID uuid.UUID,
) error {
	// 先删 trigger
	if _, err := db.Exec(ctx,
		`DELETE FROM scheduled_triggers WHERE job_id = $1`, id.String()); err != nil {
		return fmt.Errorf("delete job trigger: %w", err)
	}
	tag, err := db.Exec(ctx,
		`DELETE FROM scheduled_jobs WHERE id = $1 AND account_id = $2`, id.String(), accountID.String())
	if err != nil {
		return fmt.Errorf("delete scheduled_job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("scheduled_job %s not found", id)
	}
	return nil
}

// -- internal helpers --

func desktopGetJobByID(ctx context.Context, db desktopScheduledJobsQuerier, id uuid.UUID) (*ScheduledJob, error) {
	var r ScheduledJob
	var idStr, accountStr string
	var threadIDStr, createdByStr *string
	err := db.QueryRow(ctx, `
		SELECT id, account_id, name, description, persona_key, prompt,
		       model, workspace_ref, work_dir, thread_id, schedule_kind,
		       interval_min, daily_time, monthly_day, monthly_time, weekly_day, timezone,
		       enabled, created_by_user_id, created_at, updated_at,
		       fire_at, cron_expr,
		       delete_after_run, reasoning_mode, timeout_seconds
		  FROM scheduled_jobs
		 WHERE id = $1`, id.String(),
	).Scan(
		&idStr, &accountStr, &r.Name, &r.Description, &r.PersonaKey, &r.Prompt,
		&r.Model, &r.WorkspaceRef, &r.WorkDir, &threadIDStr, &r.ScheduleKind,
		&r.IntervalMin, &r.DailyTime, &r.MonthlyDay, &r.MonthlyTime, &r.WeeklyDay, &r.Timezone,
		&r.Enabled, &createdByStr, &r.CreatedAt, &r.UpdatedAt,
		&r.FireAt, &r.CronExpr,
		&r.DeleteAfterRun, &r.ReasoningMode, &r.Timeout,
	)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get scheduled_job by id: %w", err)
	}
	r.ID, _ = uuid.Parse(idStr)
	r.AccountID, _ = uuid.Parse(accountStr)
	if threadIDStr != nil {
		parsed, _ := uuid.Parse(*threadIDStr)
		r.ThreadID = &parsed
	}
	if createdByStr != nil {
		parsed, _ := uuid.Parse(*createdByStr)
		r.CreatedByUserID = &parsed
	}
	return &r, nil
}

func desktopInsertJobTrigger(ctx context.Context, db desktopScheduledJobsQuerier, job ScheduledJob) error {
	nextFire, err := desktopCalcJobNextFire(job)
	if err != nil {
		return fmt.Errorf("calc next fire: %w", err)
	}
	triggerID := uuid.New()
	now := time.Now().UTC()
	_, err = db.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, trigger_kind, job_id, channel_id, channel_identity_id,
		     persona_key, account_id, model, interval_min, next_fire_at, created_at, updated_at)
		VALUES ($1, 'job', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (job_id) WHERE job_id IS NOT NULL DO UPDATE
		    SET next_fire_at = excluded.next_fire_at,
		        persona_key  = excluded.persona_key,
		        model        = excluded.model,
		        interval_min = excluded.interval_min,
		        updated_at   = excluded.updated_at`,
		triggerID.String(), job.ID.String(),
		uuid.Nil.String(), uuid.Nil.String(),
		job.PersonaKey, job.AccountID.String(), job.Model,
		desktopDerefIntOr(job.IntervalMin, 0),
		nextFire.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert job trigger: %w", err)
	}
	return nil
}

func desktopCalcJobNextFire(job ScheduledJob) (time.Time, error) {
	return schedulekind.CalcNextFire(
		job.ScheduleKind,
		desktopDerefIntOr(job.IntervalMin, 0),
		job.DailyTime,
		desktopDerefIntOr(job.MonthlyDay, 1),
		job.MonthlyTime,
		desktopDerefIntOr(job.WeeklyDay, 0),
		desktopDerefTime(job.FireAt),
		job.CronExpr,
		job.Timezone,
		time.Now().UTC(),
	)
}

func validateScheduledJob(job ScheduledJob) error {
	if strings.TrimSpace(job.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(job.Prompt) == "" {
		return fmt.Errorf("prompt is required")
	}
	if job.ThreadID == nil && strings.TrimSpace(job.PersonaKey) == "" {
		return fmt.Errorf("persona_key is required when thread_id is not set")
	}
	if job.DeleteAfterRun && !schedulekind.SupportsDeleteAfterRun(job.ScheduleKind) {
		return fmt.Errorf("delete_after_run is only supported for at schedule")
	}
	return schedulekind.Validate(
		job.ScheduleKind,
		job.IntervalMin,
		job.DailyTime,
		job.MonthlyDay,
		job.MonthlyTime,
		job.WeeklyDay,
		job.FireAt,
		job.CronExpr,
		job.Timezone,
	)
}

func applyJobUpdate(job *ScheduledJob, upd UpdateJobParams) {
	if upd.Name != nil {
		job.Name = *upd.Name
	}
	if upd.Description != nil {
		job.Description = *upd.Description
	}
	if upd.PersonaKey != nil {
		job.PersonaKey = *upd.PersonaKey
	}
	if upd.Prompt != nil {
		job.Prompt = *upd.Prompt
	}
	if upd.Model != nil {
		job.Model = *upd.Model
	}
	if upd.WorkDir != nil {
		job.WorkDir = *upd.WorkDir
	}
	if upd.ScheduleKind != nil {
		job.ScheduleKind = *upd.ScheduleKind
	}
	if upd.IntervalMin != nil {
		job.IntervalMin = *upd.IntervalMin
	}
	if upd.DailyTime != nil {
		job.DailyTime = *upd.DailyTime
	}
	if upd.MonthlyDay != nil {
		job.MonthlyDay = *upd.MonthlyDay
	}
	if upd.MonthlyTime != nil {
		job.MonthlyTime = *upd.MonthlyTime
	}
	if upd.WeeklyDay != nil {
		job.WeeklyDay = *upd.WeeklyDay
	}
	if upd.Timezone != nil {
		job.Timezone = normalizeScheduledJobsTimezone(*upd.Timezone)
	}
	if upd.Enabled != nil {
		job.Enabled = *upd.Enabled
	}
	if upd.ThreadID != nil {
		job.ThreadID = *upd.ThreadID
	}
	if upd.FireAt != nil {
		job.FireAt = *upd.FireAt
	}
	if upd.CronExpr != nil {
		job.CronExpr = *upd.CronExpr
	}
	if upd.DeleteAfterRun != nil {
		job.DeleteAfterRun = *upd.DeleteAfterRun
	}
	if upd.ReasoningMode != nil {
		job.ReasoningMode = *upd.ReasoningMode
	}
	if upd.Timeout != nil {
		job.Timeout = *upd.Timeout
	}
}

func shouldValidateEnabledTransition(upd UpdateJobParams, currentEnabled bool) bool {
	return upd.Enabled != nil && *upd.Enabled && !currentEnabled
}

func normalizeScheduledJobsTimezone(tz string) string {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return "UTC"
	}
	return tz
}

func desktopDerefIntOr(p *int, def int) int {
	if p != nil {
		return *p
	}
	return def
}

func desktopDerefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func finishScheduledJobsTx(ctx context.Context, tx pgx.Tx, errp *error) {
	if *errp != nil {
		_ = tx.Rollback(ctx)
		return
	}
	*errp = tx.Commit(ctx)
}

// SetTriggerFireNow schedules the trigger for immediate firing.
func (DesktopScheduledJobsRepository) SetTriggerFireNow(ctx context.Context, db DB, jobID uuid.UUID) error {
	tag, err := db.Exec(ctx, `UPDATE scheduled_triggers SET next_fire_at = datetime('now') WHERE job_id = $1`, jobID.String())
	if err != nil {
		return fmt.Errorf("set trigger fire now: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no trigger found for job %s (job may be disabled)", jobID)
	}
	return nil
}

// NotifyScheduler wakes the desktop scheduler through the shared in-process event bus.
func (DesktopScheduledJobsRepository) NotifyScheduler(ctx context.Context, _ DB) error {
	bus, ok := shareddesktop.GetEventBus().(eventbus.EventBus)
	if !ok || bus == nil {
		return nil
	}
	return bus.Publish(ctx, pgnotify.ChannelScheduledJobs, "")
}

// ListRunsByJobID returns the most recent runs for a scheduled job.
func (DesktopScheduledJobsRepository) ListRunsByJobID(ctx context.Context, db DB, jobID uuid.UUID, limit int) ([]map[string]any, error) {
	rows, err := db.Query(ctx, `
		SELECT r.id, r.status, r.created_at, r.status_updated_at
		  FROM runs r
		  JOIN run_events e ON e.run_id = r.id
		 WHERE e.type = 'run.started'
		   AND json_extract(e.data_json, '$.scheduled_job_id') = $1
		 ORDER BY r.created_at DESC
		 LIMIT $2`, jobID.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var id, status string
		var createdAt time.Time
		var updatedAt *time.Time
		if err := rows.Scan(&id, &status, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		m := map[string]any{
			"id":         id,
			"status":     status,
			"created_at": createdAt.Format(time.RFC3339),
		}
		if updatedAt != nil {
			m["updated_at"] = updatedAt.Format(time.RFC3339)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
