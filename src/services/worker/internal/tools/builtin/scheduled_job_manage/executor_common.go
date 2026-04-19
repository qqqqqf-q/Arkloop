package scheduled_job_manage

import (
	"context"
	"fmt"
	"time"

	"arkloop/services/shared/schedulekind"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

type scheduledJobRepo interface {
	ListByAccount(ctx context.Context, db data.DB, accountID uuid.UUID) ([]data.ScheduledJobWithTrigger, error)
	GetByID(ctx context.Context, db data.DB, id, accountID uuid.UUID) (*data.ScheduledJobWithTrigger, error)
	CreateJob(ctx context.Context, db data.DB, job data.ScheduledJob) (data.ScheduledJob, error)
	UpdateJob(ctx context.Context, db data.DB, id, accountID uuid.UUID, upd data.UpdateJobParams) error
	DeleteJob(ctx context.Context, db data.DB, id, accountID uuid.UUID) error
	SetTriggerFireNow(ctx context.Context, db data.DB, jobID uuid.UUID) error
	NotifyScheduler(ctx context.Context, db data.DB) error
	ListRunsByJobID(ctx context.Context, db data.DB, jobID uuid.UUID, limit int) ([]map[string]any, error)
}

type executorCommon struct {
	db   data.DB
	repo scheduledJobRepo
}

func (e *executorCommon) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	if toolName != ToolName {
		return errResult("unexpected tool name", started)
	}
	if e.db == nil {
		return errResult("database not available", started)
	}
	if execCtx.AccountID == nil {
		return errResult("account_id not available", started)
	}

	accountID := *execCtx.AccountID

	action, _ := args["action"].(string)
	switch action {
	case "list":
		return e.doList(ctx, accountID, started)
	case "get":
		return e.doGet(ctx, accountID, args, started)
	case "create":
		return e.doCreate(ctx, accountID, args, execCtx, started)
	case "update":
		return e.doUpdate(ctx, accountID, args, started)
	case "delete":
		return e.doDelete(ctx, accountID, args, started)
	case "run":
		return e.doRun(ctx, args, execCtx, started)
	case "status":
		return e.doStatus(ctx, execCtx, started)
	case "runs":
		return e.doRuns(ctx, args, execCtx, started)
	case "wake":
		return e.doWake(ctx, started)
	default:
		return errResult(fmt.Sprintf("unknown action: %s", action), started)
	}
}

func (e *executorCommon) doList(
	ctx context.Context,
	accountID uuid.UUID,
	started time.Time,
) tools.ExecutionResult {
	jobs, err := e.repo.ListByAccount(ctx, e.db, accountID)
	if err != nil {
		return errResult(fmt.Sprintf("list jobs: %v", err), started)
	}
	items := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		items = append(items, jobToMap(j))
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true, "jobs": items},
		DurationMs: ms(started),
	}
}

func (e *executorCommon) doGet(
	ctx context.Context,
	accountID uuid.UUID,
	args map[string]any,
	started time.Time,
) tools.ExecutionResult {
	jobID, err := parseUUID(args, "job_id")
	if err != nil {
		return errResult(err.Error(), started)
	}
	job, err := e.repo.GetByID(ctx, e.db, jobID, accountID)
	if err != nil {
		return errResult(fmt.Sprintf("get job: %v", err), started)
	}
	if job == nil {
		return errResult("job not found", started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true, "job": jobToMap(*job)},
		DurationMs: ms(started),
	}
}

func (e *executorCommon) doCreate(
	ctx context.Context,
	accountID uuid.UUID,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	job := data.ScheduledJob{
		ID:           uuid.New(),
		AccountID:    accountID,
		Name:         strVal(args, "name"),
		Description:  strVal(args, "description"),
		Prompt:       strVal(args, "prompt"),
		ScheduleKind: strVal(args, "schedule_kind"),
		DailyTime:    strVal(args, "daily_time"),
		MonthlyTime:  strVal(args, "monthly_time"),
		Timezone:     strVal(args, "timezone"),
		Enabled:      true,
	}

	// persona_key 优先从 args 读取，否则继承当前 Agent 的 persona
	if v, ok := args["persona_key"].(string); ok && v != "" {
		job.PersonaKey = v
	} else {
		job.PersonaKey = execCtx.PersonaID
	}
	job.Model = strVal(args, "model")
	job.WorkDir = strVal(args, "work_dir")

	if execCtx.UserID != nil {
		job.CreatedByUserID = execCtx.UserID
	}
	if v, ok := args["thread_id"].(string); ok {
		parsed, err := uuid.Parse(v)
		if err != nil {
			return errResult("invalid thread_id", started)
		}
		job.ThreadID = &parsed
	}
	if v, ok := args["interval_min"].(float64); ok {
		iv := int(v)
		job.IntervalMin = &iv
	}
	if v, ok := args["monthly_day"].(float64); ok {
		iv := int(v)
		job.MonthlyDay = &iv
	}
	if v, ok := args["weekly_day"].(float64); ok {
		iv := int(v)
		job.WeeklyDay = &iv
	}
	if v, ok := args["fire_at"].(string); ok && v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return errResult(fmt.Sprintf("invalid fire_at: %v", err), started)
		}
		job.FireAt = &t
	}
	if v, ok := args["cron_expr"].(string); ok {
		job.CronExpr = v
	}
	if v, ok := args["enabled"].(bool); ok {
		job.Enabled = v
	}

	// validate schedule-kind specific fields
	if job.ScheduleKind == schedulekind.Cron && job.CronExpr != "" {
		p := cronParser()
		if _, err := p.Parse(job.CronExpr); err != nil {
			return errResult(fmt.Sprintf("invalid cron expression: %v", err), started)
		}
	}
	if job.ScheduleKind == schedulekind.At && (job.FireAt == nil || job.FireAt.IsZero()) {
		return errResult("fire_at is required for 'at' schedule kind", started)
	}

	created, err := e.repo.CreateJob(ctx, e.db, job)
	if err != nil {
		return errResult(fmt.Sprintf("create job: %v", err), started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true, "job_id": created.ID.String()},
		DurationMs: ms(started),
	}
}

func (e *executorCommon) doUpdate(
	ctx context.Context,
	accountID uuid.UUID,
	args map[string]any,
	started time.Time,
) tools.ExecutionResult {
	jobID, err := parseUUID(args, "job_id")
	if err != nil {
		return errResult(err.Error(), started)
	}

	var upd data.UpdateJobParams
	if v, ok := args["name"].(string); ok {
		upd.Name = &v
	}
	if v, ok := args["description"].(string); ok {
		upd.Description = &v
	}
	if v, ok := args["prompt"].(string); ok {
		upd.Prompt = &v
	}
	if v, ok := args["persona_key"].(string); ok {
		upd.PersonaKey = &v
	}
	if v, ok := args["model"].(string); ok {
		upd.Model = &v
	}
	if v, ok := args["work_dir"].(string); ok {
		upd.WorkDir = &v
	}
	if v, ok := args["schedule_kind"].(string); ok {
		upd.ScheduleKind = &v
	}
	if v, ok := args["daily_time"].(string); ok {
		upd.DailyTime = &v
	}
	if v, ok := args["monthly_time"].(string); ok {
		upd.MonthlyTime = &v
	}
	if v, ok := args["timezone"].(string); ok {
		upd.Timezone = &v
	}
	if v, ok := args["interval_min"].(float64); ok {
		iv := int(v)
		p := &iv
		upd.IntervalMin = &p
	}
	if v, ok := args["monthly_day"].(float64); ok {
		iv := int(v)
		p := &iv
		upd.MonthlyDay = &p
	}
	if v, ok := args["weekly_day"].(float64); ok {
		iv := int(v)
		p := &iv
		upd.WeeklyDay = &p
	}
	if v, ok := args["fire_at"].(string); ok && v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return errResult(fmt.Sprintf("invalid fire_at: %v", err), started)
		}
		tp := &parsed
		upd.FireAt = &tp
	}
	if v, ok := args["cron_expr"].(string); ok {
		upd.CronExpr = &v
		// validate cron expression when updating
		p := cronParser()
		if _, err := p.Parse(v); err != nil {
			return errResult(fmt.Sprintf("invalid cron expression: %v", err), started)
		}
	}
	if v, ok := args["enabled"].(bool); ok {
		upd.Enabled = &v
	}
	if v, ok := args["thread_id"].(string); ok {
		parsed, parseErr := uuid.Parse(v)
		if parseErr != nil {
			return errResult("invalid thread_id", started)
		}
		p := &parsed
		upd.ThreadID = &p
	}

	if err := e.repo.UpdateJob(ctx, e.db, jobID, accountID, upd); err != nil {
		return errResult(fmt.Sprintf("update job: %v", err), started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true},
		DurationMs: ms(started),
	}
}

func (e *executorCommon) doDelete(
	ctx context.Context,
	accountID uuid.UUID,
	args map[string]any,
	started time.Time,
) tools.ExecutionResult {
	jobID, err := parseUUID(args, "job_id")
	if err != nil {
		return errResult(err.Error(), started)
	}
	if err := e.repo.DeleteJob(ctx, e.db, jobID, accountID); err != nil {
		return errResult(fmt.Sprintf("delete job: %v", err), started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true},
		DurationMs: ms(started),
	}
}

// -- helpers --

func (e *executorCommon) doRun(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	jobID, err := parseUUID(args, "job_id")
	if err != nil {
		return errResult(err.Error(), started)
	}
	if execCtx.AccountID == nil {
		return errResult("account_id not available", started)
	}
	job, err := e.repo.GetByID(ctx, e.db, jobID, *execCtx.AccountID)
	if err != nil {
		return errResult(fmt.Sprintf("job not found: %v", err), started)
	}
	if job == nil {
		return errResult("job not found", started)
	}
	if !job.Enabled {
		return errResult("job is disabled, enable it first", started)
	}
	if err := e.repo.SetTriggerFireNow(ctx, e.db, jobID); err != nil {
		return errResult(fmt.Sprintf("failed to trigger job: %v", err), started)
	}
	_ = e.repo.NotifyScheduler(ctx, e.db)
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true, "triggered": true, "job_id": jobID.String()},
		DurationMs: ms(started),
	}
}

func (e *executorCommon) doStatus(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	if execCtx.AccountID == nil {
		return errResult("account_id not available", started)
	}
	jobs, err := e.repo.ListByAccount(ctx, e.db, *execCtx.AccountID)
	if err != nil {
		return errResult(fmt.Sprintf("failed to list jobs: %v", err), started)
	}
	summary := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		m := map[string]any{
			"job_id":        j.ID.String(),
			"name":          j.Name,
			"enabled":       j.Enabled,
			"schedule_kind": j.ScheduleKind,
		}
		if j.NextFireAt != nil {
			m["next_fire_at"] = j.NextFireAt.Format(time.RFC3339)
		}
		summary = append(summary, m)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true, "jobs": summary, "count": len(summary)},
		DurationMs: ms(started),
	}
}

func (e *executorCommon) doRuns(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	jobID, err := parseUUID(args, "job_id")
	if err != nil {
		return errResult(err.Error(), started)
	}
	// Verify job belongs to current account before listing runs
	if execCtx.AccountID == nil {
		return errResult("account_id not available", started)
	}
	job, err := e.repo.GetByID(ctx, e.db, jobID, *execCtx.AccountID)
	if err != nil {
		return errResult(fmt.Sprintf("job not found: %v", err), started)
	}
	if job == nil {
		return errResult("job not found", started)
	}
	runs, err := e.repo.ListRunsByJobID(ctx, e.db, jobID, 20)
	if err != nil {
		return errResult(fmt.Sprintf("failed to list runs: %v", err), started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true, "runs": runs, "count": len(runs)},
		DurationMs: ms(started),
	}
}

func (e *executorCommon) doWake(
	ctx context.Context,
	started time.Time,
) tools.ExecutionResult {
	if err := e.repo.NotifyScheduler(ctx, e.db); err != nil {
		return errResult(fmt.Sprintf("failed to wake scheduler: %v", err), started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true},
		DurationMs: ms(started),
	}
}

func errResult(msg string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: tools.ErrorClassToolExecutionFailed,
			Message:    msg,
		},
		DurationMs: ms(started),
	}
}

func ms(started time.Time) int {
	return int(time.Since(started).Milliseconds())
}

func parseUUID(args map[string]any, key string) (uuid.UUID, error) {
	v, ok := args[key].(string)
	if !ok || v == "" {
		return uuid.Nil, fmt.Errorf("%s is required", key)
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid %s: %w", key, err)
	}
	return id, nil
}

func strVal(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func cronParser() cron.Parser {
	return cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
}

func jobToMap(j data.ScheduledJobWithTrigger) map[string]any {
	m := map[string]any{
		"id":            j.ID.String(),
		"name":          j.Name,
		"description":   j.Description,
		"persona_key":   j.PersonaKey,
		"prompt":        j.Prompt,
		"model":         j.Model,
		"work_dir":      j.WorkDir,
		"schedule_kind": j.ScheduleKind,
		"timezone":      j.Timezone,
		"enabled":       j.Enabled,
		"created_at":    j.CreatedAt.Format(time.RFC3339),
		"updated_at":    j.UpdatedAt.Format(time.RFC3339),
	}
	if j.ThreadID != nil {
		m["thread_id"] = j.ThreadID.String()
	}
	if j.IntervalMin != nil {
		m["interval_min"] = *j.IntervalMin
	}
	if j.DailyTime != "" {
		m["daily_time"] = j.DailyTime
	}
	if j.MonthlyDay != nil {
		m["monthly_day"] = *j.MonthlyDay
	}
	if j.MonthlyTime != "" {
		m["monthly_time"] = j.MonthlyTime
	}
	if j.WeeklyDay != nil {
		m["weekly_day"] = *j.WeeklyDay
	}
	if j.NextFireAt != nil {
		m["next_fire_at"] = j.NextFireAt.Format(time.RFC3339)
	}
	if j.FireAt != nil {
		m["fire_at"] = j.FireAt.Format(time.RFC3339)
	}
	m["cron_expr"] = j.CronExpr
	return m
}
