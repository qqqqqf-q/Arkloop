package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/scheduledjobs"
	"arkloop/services/shared/schedulekind"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ScheduledJobsRepository struct{}

type ScheduledJobValidationError struct {
	msg string
}

func (e *ScheduledJobValidationError) Error() string {
	return e.msg
}

func newScheduledJobValidationError(err error) error {
	if err == nil {
		return nil
	}
	return &ScheduledJobValidationError{msg: err.Error()}
}

func IsScheduledJobValidationError(err error) bool {
	var target *ScheduledJobValidationError
	return errors.As(err, &target)
}

func (ScheduledJobsRepository) CreateJob(
	ctx context.Context,
	db Querier,
	job scheduledjobs.ScheduledJob,
) (created scheduledjobs.ScheduledJob, err error) {
	if job.AccountID == uuid.Nil {
		return scheduledjobs.ScheduledJob{}, errors.New("account_id must not be empty")
	}
	if err := validateScheduledJob(job); err != nil {
		return scheduledjobs.ScheduledJob{}, newScheduledJobValidationError(err)
	}

	tx, ownedTx, err := beginScheduledJobsTx(ctx, db)
	if err != nil {
		return scheduledjobs.ScheduledJob{}, err
	}
	if ownedTx {
		defer finishScheduledJobsTx(ctx, tx, &err)
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO scheduled_jobs
		    (id, account_id, name, description, persona_key, prompt, model,
		     workspace_ref, work_dir, thread_id, schedule_kind, interval_min,
		     daily_time, monthly_day, monthly_time, weekly_day, timezone, enabled, created_by_user_id,
		     fire_at, cron_expr, delete_after_run, reasoning_mode, timeout_seconds,
		     created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,now(),now())
		RETURNING id, created_at, updated_at`,
		job.ID, job.AccountID, job.Name, job.Description, job.PersonaKey, job.Prompt,
		job.Model, job.WorkspaceRef, job.WorkDir, job.ThreadID, job.ScheduleKind,
		job.IntervalMin, job.DailyTime, job.MonthlyDay, job.MonthlyTime, job.WeeklyDay,
		job.Timezone, job.Enabled, job.CreatedByUserID, job.FireAt, job.CronExpr,
		job.DeleteAfterRun, job.ReasoningMode, job.Timeout,
	).Scan(&job.ID, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return scheduledjobs.ScheduledJob{}, fmt.Errorf("insert scheduled_jobs: %w", err)
	}

	if job.Enabled {
		if err := insertJobTrigger(ctx, tx, job); err != nil {
			return scheduledjobs.ScheduledJob{}, err
		}
	}

	return job, nil
}

func (ScheduledJobsRepository) ListByAccount(
	ctx context.Context,
	db Querier,
	accountID uuid.UUID,
) ([]scheduledjobs.ScheduledJobWithTrigger, error) {
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
		 ORDER BY j.created_at DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []scheduledjobs.ScheduledJobWithTrigger
	for rows.Next() {
		var r scheduledjobs.ScheduledJobWithTrigger
		if err := rows.Scan(
			&r.ID, &r.AccountID, &r.Name, &r.Description, &r.PersonaKey, &r.Prompt,
			&r.Model, &r.WorkspaceRef, &r.WorkDir, &r.ThreadID, &r.ScheduleKind,
			&r.IntervalMin, &r.DailyTime, &r.MonthlyDay, &r.MonthlyTime, &r.WeeklyDay, &r.Timezone,
			&r.Enabled, &r.CreatedByUserID, &r.CreatedAt, &r.UpdatedAt,
			&r.FireAt, &r.CronExpr,
			&r.DeleteAfterRun, &r.ReasoningMode, &r.Timeout,
			&r.NextFireAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (ScheduledJobsRepository) GetByID(
	ctx context.Context,
	db Querier,
	id, accountID uuid.UUID,
) (*scheduledjobs.ScheduledJobWithTrigger, error) {
	var r scheduledjobs.ScheduledJobWithTrigger
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
		 WHERE j.id = $1 AND j.account_id = $2`, id, accountID,
	).Scan(
		&r.ID, &r.AccountID, &r.Name, &r.Description, &r.PersonaKey, &r.Prompt,
		&r.Model, &r.WorkspaceRef, &r.WorkDir, &r.ThreadID, &r.ScheduleKind,
		&r.IntervalMin, &r.DailyTime, &r.MonthlyDay, &r.MonthlyTime, &r.WeeklyDay, &r.Timezone,
		&r.Enabled, &r.CreatedByUserID, &r.CreatedAt, &r.UpdatedAt,
		&r.FireAt, &r.CronExpr,
		&r.DeleteAfterRun, &r.ReasoningMode, &r.Timeout,
		&r.NextFireAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func (ScheduledJobsRepository) GetJobByID(
	ctx context.Context,
	db Querier,
	id uuid.UUID,
) (*scheduledjobs.ScheduledJob, error) {
	var r scheduledjobs.ScheduledJob
	err := db.QueryRow(ctx, `
		SELECT id, account_id, name, description, persona_key, prompt,
		       model, workspace_ref, work_dir, thread_id, schedule_kind,
		       interval_min, daily_time, monthly_day, monthly_time, weekly_day, timezone,
		       enabled, created_by_user_id, created_at, updated_at,
		       fire_at, cron_expr,
		       delete_after_run, reasoning_mode, timeout_seconds
		  FROM scheduled_jobs
		 WHERE id = $1`, id,
	).Scan(
		&r.ID, &r.AccountID, &r.Name, &r.Description, &r.PersonaKey, &r.Prompt,
		&r.Model, &r.WorkspaceRef, &r.WorkDir, &r.ThreadID, &r.ScheduleKind,
		&r.IntervalMin, &r.DailyTime, &r.MonthlyDay, &r.MonthlyTime, &r.WeeklyDay, &r.Timezone,
		&r.Enabled, &r.CreatedByUserID, &r.CreatedAt, &r.UpdatedAt,
		&r.FireAt, &r.CronExpr,
		&r.DeleteAfterRun, &r.ReasoningMode, &r.Timeout,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func getJobByID(
	ctx context.Context,
	db Querier,
	id uuid.UUID,
) (*scheduledjobs.ScheduledJob, error) {
	var r scheduledjobs.ScheduledJob
	err := db.QueryRow(ctx, `
		SELECT id, account_id, name, description, persona_key, prompt,
		       model, workspace_ref, work_dir, thread_id, schedule_kind,
		       interval_min, daily_time, monthly_day, monthly_time, weekly_day, timezone,
		       enabled, created_by_user_id, created_at, updated_at,
		       fire_at, cron_expr,
		       delete_after_run, reasoning_mode, timeout_seconds
		  FROM scheduled_jobs
		 WHERE id = $1`, id,
	).Scan(
		&r.ID, &r.AccountID, &r.Name, &r.Description, &r.PersonaKey, &r.Prompt,
		&r.Model, &r.WorkspaceRef, &r.WorkDir, &r.ThreadID, &r.ScheduleKind,
		&r.IntervalMin, &r.DailyTime, &r.MonthlyDay, &r.MonthlyTime, &r.WeeklyDay, &r.Timezone,
		&r.Enabled, &r.CreatedByUserID, &r.CreatedAt, &r.UpdatedAt,
		&r.FireAt, &r.CronExpr,
		&r.DeleteAfterRun, &r.ReasoningMode, &r.Timeout,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func (ScheduledJobsRepository) UpdateJob(
	ctx context.Context,
	db Querier,
	id, accountID uuid.UUID,
	upd scheduledjobs.UpdateJobParams,
) (err error) {
	setClauses := []string{}
	args := []any{}
	argIdx := 1

	addSet := func(col string, val any) {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, argIdx))
		args = append(args, val)
		argIdx++
	}

	tx, ownedTx, err := beginScheduledJobsTx(ctx, db)
	if err != nil {
		return err
	}
	if ownedTx {
		defer finishScheduledJobsTx(ctx, tx, &err)
	}

	current, err := getJobByID(ctx, tx, id)
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
	if upd.WorkspaceRef != nil {
		addSet("workspace_ref", *upd.WorkspaceRef)
	}
	if upd.WorkDir != nil {
		addSet("work_dir", *upd.WorkDir)
	}
	if upd.ThreadID != nil {
		addSet("thread_id", *upd.ThreadID)
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
	if upd.FireAt != nil {
		addSet("fire_at", *upd.FireAt)
		scheduleChanged = true
	}
	if upd.CronExpr != nil {
		addSet("cron_expr", *upd.CronExpr)
		scheduleChanged = true
	}
	if upd.Enabled != nil {
		addSet("enabled", *upd.Enabled)
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
			return newScheduledJobValidationError(err)
		}
	}

	addSet("updated_at", time.Now().UTC())

	whereID := fmt.Sprintf("$%d", argIdx)
	args = append(args, id)
	argIdx++
	whereAccount := fmt.Sprintf("$%d", argIdx)
	args = append(args, accountID)

	sql := fmt.Sprintf("UPDATE scheduled_jobs SET %s WHERE id = %s AND account_id = %s",
		strings.Join(setClauses, ", "), whereID, whereAccount)

	cmd, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update scheduled_jobs: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("scheduled_job %s not found", id)
	}

	// enabled 切换逻辑
	if upd.Enabled != nil && !*upd.Enabled {
		_, err := tx.Exec(ctx, `DELETE FROM scheduled_triggers WHERE job_id = $1`, id)
		return err
	}
	if upd.Enabled != nil && *upd.Enabled {
		return insertJobTrigger(ctx, tx, next)
	}

	// schedule 参数变更，重算 next_fire_at
	if scheduleChanged {
		if !next.Enabled {
			return nil
		}
		nextFire, err := calcJobNextFire(next)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE scheduled_triggers SET next_fire_at = $1, updated_at = now() WHERE job_id = $2`,
			nextFire, id)
		return err
	}

	return nil
}

func (ScheduledJobsRepository) DeleteJob(
	ctx context.Context,
	db Querier,
	id, accountID uuid.UUID,
) error {
	cmd, err := db.Exec(ctx,
		`DELETE FROM scheduled_jobs WHERE id = $1 AND account_id = $2`, id, accountID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("scheduled_job %s not found", id)
	}
	return nil
}

func (ScheduledJobsRepository) SetJobEnabled(
	ctx context.Context,
	db Querier,
	id, accountID uuid.UUID,
	enabled bool,
) (err error) {
	tx, ownedTx, err := beginScheduledJobsTx(ctx, db)
	if err != nil {
		return err
	}
	if ownedTx {
		defer finishScheduledJobsTx(ctx, tx, &err)
	}

	job, err := getJobByID(ctx, tx, id)
	if err != nil {
		return err
	}
	if job == nil || job.AccountID != accountID {
		return fmt.Errorf("scheduled_job %s not found", id)
	}

	if !enabled {
		_, err = tx.Exec(ctx, `
			UPDATE scheduled_jobs SET enabled = false, updated_at = now()
			 WHERE id = $1 AND account_id = $2`, id, accountID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM scheduled_triggers WHERE job_id = $1`, id)
		return err
	}

	if err := validateScheduledJob(*job); err != nil {
		return newScheduledJobValidationError(err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE scheduled_jobs SET enabled = true, updated_at = now()
		 WHERE id = $1 AND account_id = $2`, id, accountID)
	if err != nil {
		return err
	}
	job.Enabled = true
	return insertJobTrigger(ctx, tx, *job)
}

// insertJobTrigger 为 job 计算 next_fire_at 并插入 trigger（ON CONFLICT 忽略）。
func insertJobTrigger(ctx context.Context, db Querier, job scheduledjobs.ScheduledJob) error {
	nextFire, err := calcJobNextFire(job)
	if err != nil {
		return fmt.Errorf("calc next fire: %w", err)
	}
	triggerID := uuid.New()
	_, err = db.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, trigger_kind, job_id, channel_id, channel_identity_id,
		     persona_key, account_id, model, interval_min, next_fire_at, created_at, updated_at)
		VALUES ($1, 'job', $2, $3, $4, $5, $6, $7, $8, $9, now(), now())
		ON CONFLICT (job_id) WHERE job_id IS NOT NULL DO UPDATE
		    SET next_fire_at = excluded.next_fire_at,
		        persona_key  = excluded.persona_key,
		        model        = excluded.model,
		        interval_min = excluded.interval_min,
		        updated_at   = now()`,
		triggerID, job.ID, uuid.Nil, uuid.Nil,
		job.PersonaKey, job.AccountID, job.Model, derefIntOr(job.IntervalMin, 0),
		nextFire,
	)
	if err != nil {
		return fmt.Errorf("insert job trigger: %w", err)
	}
	return nil
}

func calcJobNextFire(job scheduledjobs.ScheduledJob) (time.Time, error) {
	return schedulekind.CalcNextFire(
		job.ScheduleKind,
		derefIntOr(job.IntervalMin, 0),
		job.DailyTime,
		derefIntOr(job.MonthlyDay, 1),
		job.MonthlyTime,
		derefIntOr(job.WeeklyDay, 0),
		derefTime(job.FireAt),
		job.CronExpr,
		job.Timezone,
		time.Now().UTC(),
	)
}

// InferThreadContext returns the persona_key and model from the thread's most recent run.
func (ScheduledJobsRepository) InferThreadContext(
	ctx context.Context,
	db Querier,
	accountID uuid.UUID,
	threadID uuid.UUID,
) (personaKey string, model string, err error) {
	var p, m *string
	err = db.QueryRow(ctx, `
		SELECT persona_id, model
		FROM runs
		WHERE thread_id = $1 AND account_id = $2 AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, threadID, accountID,
	).Scan(&p, &m)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("infer thread context: %w", err)
	}
	if p != nil {
		personaKey = *p
	}
	if m != nil {
		model = *m
	}
	return personaKey, model, nil
}

func derefIntOr(p *int, def int) int {
	if p != nil {
		return *p
	}
	return def
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func validateScheduledJob(job scheduledjobs.ScheduledJob) error {
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
	if err := schedulekind.Validate(
		job.ScheduleKind,
		job.IntervalMin,
		job.DailyTime,
		job.MonthlyDay,
		job.MonthlyTime,
		job.WeeklyDay,
		job.FireAt,
		job.CronExpr,
		job.Timezone,
	); err != nil {
		return err
	}
	return nil
}

func applyJobUpdate(job *scheduledjobs.ScheduledJob, upd scheduledjobs.UpdateJobParams) {
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
	if upd.WorkspaceRef != nil {
		job.WorkspaceRef = *upd.WorkspaceRef
	}
	if upd.WorkDir != nil {
		job.WorkDir = *upd.WorkDir
	}
	if upd.ThreadID != nil {
		job.ThreadID = *upd.ThreadID
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
	if upd.FireAt != nil {
		job.FireAt = *upd.FireAt
	}
	if upd.CronExpr != nil {
		job.CronExpr = *upd.CronExpr
	}
	if upd.Enabled != nil {
		job.Enabled = *upd.Enabled
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

func shouldValidateEnabledTransition(upd scheduledjobs.UpdateJobParams, currentEnabled bool) bool {
	return upd.Enabled != nil && *upd.Enabled && !currentEnabled
}

func normalizeScheduledJobsTimezone(tz string) string {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return "UTC"
	}
	return tz
}

func beginScheduledJobsTx(ctx context.Context, db Querier) (pgx.Tx, bool, error) {
	if tx, ok := db.(pgx.Tx); ok {
		return tx, false, nil
	}
	beginner, ok := db.(TxStarter)
	if !ok {
		return nil, false, fmt.Errorf("querier does not support transactions")
	}
	tx, err := beginner.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, false, err
	}
	return tx, true, nil
}

func finishScheduledJobsTx(ctx context.Context, tx pgx.Tx, errp *error) {
	if *errp != nil {
		_ = tx.Rollback(ctx)
		return
	}
	*errp = tx.Commit(ctx)
}
