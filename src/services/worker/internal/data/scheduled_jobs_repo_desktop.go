//go:build desktop

package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/schedulekind"

	"github.com/google/uuid"
)

// DesktopScheduledJobsRepository 提供 desktop 侧的 scheduled_jobs CRUD（SQLite）。
type DesktopScheduledJobsRepository struct{}

// GetJobByID 按 ID 加载 job 定义。
func (DesktopScheduledJobsRepository) GetJobByID(
	ctx context.Context,
	db DesktopDB,
	id uuid.UUID,
) (*ScheduledJob, error) {
	return desktopGetJobByID(ctx, db, id)
}

// ListByAccount 列出 account 下所有 job，附带 trigger 的 next_fire_at。
func (DesktopScheduledJobsRepository) ListByAccount(
	ctx context.Context,
	db DesktopDB,
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
	db DesktopDB,
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
		       t.next_fire_at
		  FROM scheduled_jobs j
		  LEFT JOIN scheduled_triggers t ON t.job_id = j.id
		 WHERE j.id = $1 AND j.account_id = $2`, id.String(), accountID.String(),
	).Scan(
		&idStr, &accountStr, &r.Name, &r.Description, &r.PersonaKey, &r.Prompt,
		&r.Model, &r.WorkspaceRef, &r.WorkDir, &threadIDStr, &r.ScheduleKind,
		&r.IntervalMin, &r.DailyTime, &r.MonthlyDay, &r.MonthlyTime, &r.WeeklyDay, &r.Timezone,
		&r.Enabled, &createdByStr, &r.CreatedAt, &r.UpdatedAt,
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
	db DesktopDB,
	job ScheduledJob,
) (ScheduledJob, error) {
	if job.AccountID == uuid.Nil {
		return ScheduledJob{}, fmt.Errorf("account_id must not be empty")
	}
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

	_, err := db.Exec(ctx, `
		INSERT INTO scheduled_jobs
		    (id, account_id, name, description, persona_key, prompt, model,
		     workspace_ref, work_dir, thread_id, schedule_kind, interval_min,
		     daily_time, monthly_day, monthly_time, weekly_day, timezone, enabled, created_by_user_id,
		     created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
		job.ID.String(), job.AccountID.String(), job.Name, job.Description,
		job.PersonaKey, job.Prompt, job.Model, job.WorkspaceRef, job.WorkDir,
		threadIDStr, job.ScheduleKind, job.IntervalMin, job.DailyTime,
		job.MonthlyDay, job.MonthlyTime, job.WeeklyDay, job.Timezone, job.Enabled, createdByStr,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return ScheduledJob{}, fmt.Errorf("insert scheduled_jobs: %w", err)
	}

	if job.Enabled {
		if err := desktopInsertJobTrigger(ctx, db, job); err != nil {
			return ScheduledJob{}, err
		}
	}

	return job, nil
}

// UpdateJob 部分更新 scheduled_job。
func (DesktopScheduledJobsRepository) UpdateJob(
	ctx context.Context,
	db DesktopDB,
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
		var v *string
		if *upd.ThreadID != nil {
			s := (*upd.ThreadID).String()
			v = &s
		}
		addSet("thread_id", v)
	}

	if len(setClauses) == 0 {
		return nil
	}

	addSet("updated_at", time.Now().UTC().Format(time.RFC3339Nano))

	whereID := fmt.Sprintf("$%d", argIdx)
	args = append(args, id.String())
	argIdx++
	whereAccount := fmt.Sprintf("$%d", argIdx)
	args = append(args, accountID.String())

	sql := fmt.Sprintf("UPDATE scheduled_jobs SET %s WHERE id = %s AND account_id = %s",
		strings.Join(setClauses, ", "), whereID, whereAccount)

	tag, err := db.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update scheduled_jobs: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("scheduled_job %s not found", id)
	}

	// enabled 切换
	if upd.Enabled != nil && !*upd.Enabled {
		_, err := db.Exec(ctx, `DELETE FROM scheduled_triggers WHERE job_id = $1`, id.String())
		return err
	}
	if upd.Enabled != nil && *upd.Enabled {
		job, err := desktopGetJobByID(ctx, db, id)
		if err != nil {
			return err
		}
		if job == nil {
			return nil
		}
		return desktopInsertJobTrigger(ctx, db, *job)
	}

	// schedule 参数变更，重算 next_fire_at
	if scheduleChanged {
		job, err := desktopGetJobByID(ctx, db, id)
		if err != nil {
			return err
		}
		if job == nil || !job.Enabled {
			return nil
		}
		nextFire, err := desktopCalcJobNextFire(*job)
		if err != nil {
			return err
		}
		_, err = db.Exec(ctx, `
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
	db DesktopDB,
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

func desktopGetJobByID(ctx context.Context, db DesktopDB, id uuid.UUID) (*ScheduledJob, error) {
	var r ScheduledJob
	var idStr, accountStr string
	var threadIDStr, createdByStr *string
	err := db.QueryRow(ctx, `
		SELECT id, account_id, name, description, persona_key, prompt,
		       model, workspace_ref, work_dir, thread_id, schedule_kind,
		       interval_min, daily_time, monthly_day, monthly_time, weekly_day, timezone,
		       enabled, created_by_user_id, created_at, updated_at
		  FROM scheduled_jobs
		 WHERE id = $1`, id.String(),
	).Scan(
		&idStr, &accountStr, &r.Name, &r.Description, &r.PersonaKey, &r.Prompt,
		&r.Model, &r.WorkspaceRef, &r.WorkDir, &threadIDStr, &r.ScheduleKind,
		&r.IntervalMin, &r.DailyTime, &r.MonthlyDay, &r.MonthlyTime, &r.WeeklyDay, &r.Timezone,
		&r.Enabled, &createdByStr, &r.CreatedAt, &r.UpdatedAt,
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

func desktopInsertJobTrigger(ctx context.Context, db DesktopDB, job ScheduledJob) error {
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
		job.Timezone,
		time.Now().UTC(),
	)
}

func desktopDerefIntOr(p *int, def int) int {
	if p != nil {
		return *p
	}
	return def
}
