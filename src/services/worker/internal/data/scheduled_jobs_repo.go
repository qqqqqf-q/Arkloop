//go:build !desktop

package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/schedulekind"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ScheduledJobsRepository 提供 scheduled_jobs CRUD（cloud / Postgres）。
type ScheduledJobsRepository struct{}

func (ScheduledJobsRepository) ListByAccount(
	ctx context.Context,
	db DB,
	accountID uuid.UUID,
) ([]ScheduledJobWithTrigger, error) {
	rows, err := db.Query(ctx, `
		SELECT j.id, j.account_id, j.name, j.description, j.persona_key, j.prompt,
		       j.model, j.workspace_ref, j.work_dir, j.thread_id, j.schedule_kind,
		       j.interval_min, j.daily_time, j.monthly_day, j.monthly_time, j.weekly_day, j.timezone,
		       j.enabled, j.created_by_user_id, j.created_at, j.updated_at,
		       t.next_fire_at
		  FROM scheduled_jobs j
		  LEFT JOIN scheduled_triggers t ON t.job_id = j.id
		 WHERE j.account_id = $1
		 ORDER BY j.created_at DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ScheduledJobWithTrigger
	for rows.Next() {
		var r ScheduledJobWithTrigger
		if err := rows.Scan(
			&r.ID, &r.AccountID, &r.Name, &r.Description, &r.PersonaKey, &r.Prompt,
			&r.Model, &r.WorkspaceRef, &r.WorkDir, &r.ThreadID, &r.ScheduleKind,
			&r.IntervalMin, &r.DailyTime, &r.MonthlyDay, &r.MonthlyTime, &r.WeeklyDay, &r.Timezone,
			&r.Enabled, &r.CreatedByUserID, &r.CreatedAt, &r.UpdatedAt,
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
	db DB,
	id, accountID uuid.UUID,
) (*ScheduledJobWithTrigger, error) {
	var r ScheduledJobWithTrigger
	err := db.QueryRow(ctx, `
		SELECT j.id, j.account_id, j.name, j.description, j.persona_key, j.prompt,
		       j.model, j.workspace_ref, j.work_dir, j.thread_id, j.schedule_kind,
		       j.interval_min, j.daily_time, j.monthly_day, j.monthly_time, j.weekly_day, j.timezone,
		       j.enabled, j.created_by_user_id, j.created_at, j.updated_at,
		       t.next_fire_at
		  FROM scheduled_jobs j
		  LEFT JOIN scheduled_triggers t ON t.job_id = j.id
		 WHERE j.id = $1 AND j.account_id = $2`, id, accountID,
	).Scan(
		&r.ID, &r.AccountID, &r.Name, &r.Description, &r.PersonaKey, &r.Prompt,
		&r.Model, &r.WorkspaceRef, &r.WorkDir, &r.ThreadID, &r.ScheduleKind,
		&r.IntervalMin, &r.DailyTime, &r.MonthlyDay, &r.MonthlyTime, &r.WeeklyDay, &r.Timezone,
		&r.Enabled, &r.CreatedByUserID, &r.CreatedAt, &r.UpdatedAt,
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

func (ScheduledJobsRepository) CreateJob(
	ctx context.Context,
	db DB,
	job ScheduledJob,
) (ScheduledJob, error) {
	if job.AccountID == uuid.Nil {
		return ScheduledJob{}, errors.New("account_id must not be empty")
	}

	err := db.QueryRow(ctx, `
		INSERT INTO scheduled_jobs
		    (id, account_id, name, description, persona_key, prompt, model,
		     workspace_ref, work_dir, thread_id, schedule_kind, interval_min,
		     daily_time, monthly_day, monthly_time, weekly_day, timezone, enabled, created_by_user_id,
		     created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,now(),now())
		RETURNING id, created_at, updated_at`,
		job.ID, job.AccountID, job.Name, job.Description, job.PersonaKey, job.Prompt,
		job.Model, job.WorkspaceRef, job.WorkDir, job.ThreadID, job.ScheduleKind,
		job.IntervalMin, job.DailyTime, job.MonthlyDay, job.MonthlyTime, job.WeeklyDay,
		job.Timezone, job.Enabled, job.CreatedByUserID,
	).Scan(&job.ID, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return ScheduledJob{}, fmt.Errorf("insert scheduled_jobs: %w", err)
	}

	if job.Enabled {
		if err := insertJobTrigger(ctx, db, job); err != nil {
			return ScheduledJob{}, err
		}
	}

	return job, nil
}

func (ScheduledJobsRepository) UpdateJob(
	ctx context.Context,
	db DB,
	id, accountID uuid.UUID,
	upd UpdateJobParams,
) error {
	setClauses := []string{}
	args := []any{}
	argIdx := 1

	addSet := func(col string, val any) {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, argIdx))
		args = append(args, val)
		argIdx++
	}

	scheduleChanged := false
	if upd.Name != nil {
		addSet("name", *upd.Name)
	}
	if upd.Description != nil {
		addSet("description", *upd.Description)
	}
	if upd.Prompt != nil {
		addSet("prompt", *upd.Prompt)
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
		addSet("timezone", *upd.Timezone)
		scheduleChanged = true
	}
	if upd.Enabled != nil {
		addSet("enabled", *upd.Enabled)
	}
	if upd.ThreadID != nil {
		addSet("thread_id", *upd.ThreadID)
	}

	if len(setClauses) == 0 {
		return nil
	}

	addSet("updated_at", time.Now().UTC())

	whereID := fmt.Sprintf("$%d", argIdx)
	args = append(args, id)
	argIdx++
	whereAccount := fmt.Sprintf("$%d", argIdx)
	args = append(args, accountID)

	sql := fmt.Sprintf("UPDATE scheduled_jobs SET %s WHERE id = %s AND account_id = %s",
		strings.Join(setClauses, ", "), whereID, whereAccount)

	cmd, err := db.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update scheduled_jobs: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("scheduled_job %s not found", id)
	}

	// enabled 切换
	if upd.Enabled != nil && !*upd.Enabled {
		_, err := db.Exec(ctx, `DELETE FROM scheduled_triggers WHERE job_id = $1`, id)
		return err
	}
	if upd.Enabled != nil && *upd.Enabled {
		job, err := getJobByID(ctx, db, id)
		if err != nil {
			return err
		}
		return insertJobTrigger(ctx, db, *job)
	}

	// schedule 参数变更，重算 next_fire_at
	if scheduleChanged {
		job, err := getJobByID(ctx, db, id)
		if err != nil {
			return err
		}
		if !job.Enabled {
			return nil
		}
		nextFire, err := calcJobNextFire(*job)
		if err != nil {
			return err
		}
		_, err = db.Exec(ctx, `
			UPDATE scheduled_triggers SET next_fire_at = $1, updated_at = now() WHERE job_id = $2`,
			nextFire, id)
		return err
	}

	return nil
}

func (ScheduledJobsRepository) DeleteJob(
	ctx context.Context,
	db DB,
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

// -- internal helpers --

func getJobByID(ctx context.Context, db DB, id uuid.UUID) (*ScheduledJob, error) {
	var r ScheduledJob
	err := db.QueryRow(ctx, `
		SELECT id, account_id, name, description, persona_key, prompt,
		       model, workspace_ref, work_dir, thread_id, schedule_kind,
		       interval_min, daily_time, monthly_day, monthly_time, weekly_day, timezone,
		       enabled, created_by_user_id, created_at, updated_at
		  FROM scheduled_jobs
		 WHERE id = $1`, id,
	).Scan(
		&r.ID, &r.AccountID, &r.Name, &r.Description, &r.PersonaKey, &r.Prompt,
		&r.Model, &r.WorkspaceRef, &r.WorkDir, &r.ThreadID, &r.ScheduleKind,
		&r.IntervalMin, &r.DailyTime, &r.MonthlyDay, &r.MonthlyTime, &r.WeeklyDay, &r.Timezone,
		&r.Enabled, &r.CreatedByUserID, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func insertJobTrigger(ctx context.Context, db DB, job ScheduledJob) error {
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

func calcJobNextFire(job ScheduledJob) (time.Time, error) {
	return schedulekind.CalcNextFire(
		job.ScheduleKind,
		derefIntOr(job.IntervalMin, 0),
		job.DailyTime,
		derefIntOr(job.MonthlyDay, 1),
		job.MonthlyTime,
		derefIntOr(job.WeeklyDay, 0),
		job.Timezone,
		time.Now().UTC(),
	)
}

func derefIntOr(p *int, def int) int {
	if p != nil {
		return *p
	}
	return def
}
