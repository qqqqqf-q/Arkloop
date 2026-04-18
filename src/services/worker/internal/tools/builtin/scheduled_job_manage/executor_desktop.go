//go:build desktop

package scheduled_job_manage

import (
	"context"
	"fmt"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type desktopExecutor struct {
	db data.DesktopDB
}

// New 返回 desktop 下的 scheduled_job_manage executor。
func New(_ *pgxpool.Pool) tools.Executor {
	return &desktopExecutor{}
}

// SetDesktopDB 注入 DesktopDB（desktop 模式下由 composition 调用）。
func (e *desktopExecutor) SetDesktopDB(db data.DesktopDB) {
	e.db = db
}

func (e *desktopExecutor) Execute(
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
	repo := data.DesktopScheduledJobsRepository{}

	action, _ := args["action"].(string)
	switch action {
	case "list":
		return e.doList(ctx, repo, accountID, started)
	case "get":
		return e.doGet(ctx, repo, accountID, args, started)
	case "create":
		return e.doCreate(ctx, repo, accountID, args, execCtx, started)
	case "update":
		return e.doUpdate(ctx, repo, accountID, args, started)
	case "delete":
		return e.doDelete(ctx, repo, accountID, args, started)
	default:
		return errResult(fmt.Sprintf("unknown action: %s", action), started)
	}
}

func (e *desktopExecutor) doList(
	ctx context.Context,
	repo data.DesktopScheduledJobsRepository,
	accountID uuid.UUID,
	started time.Time,
) tools.ExecutionResult {
	jobs, err := repo.ListByAccount(ctx, e.db, accountID)
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

func (e *desktopExecutor) doGet(
	ctx context.Context,
	repo data.DesktopScheduledJobsRepository,
	accountID uuid.UUID,
	args map[string]any,
	started time.Time,
) tools.ExecutionResult {
	jobID, err := parseUUID(args, "job_id")
	if err != nil {
		return errResult(err.Error(), started)
	}
	job, err := repo.GetByID(ctx, e.db, jobID, accountID)
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

func (e *desktopExecutor) doCreate(
	ctx context.Context,
	repo data.DesktopScheduledJobsRepository,
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
		PersonaKey:   execCtx.PersonaID,
		Prompt:       strVal(args, "prompt"),
		ScheduleKind: strVal(args, "schedule_kind"),
		DailyTime:    strVal(args, "daily_time"),
		MonthlyTime:  strVal(args, "monthly_time"),
		Timezone:     strVal(args, "timezone"),
		Enabled:      true,
	}

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
	if v, ok := args["enabled"].(bool); ok {
		job.Enabled = v
	}

	created, err := repo.CreateJob(ctx, e.db, job)
	if err != nil {
		return errResult(fmt.Sprintf("create job: %v", err), started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true, "job_id": created.ID.String()},
		DurationMs: ms(started),
	}
}

func (e *desktopExecutor) doUpdate(
	ctx context.Context,
	repo data.DesktopScheduledJobsRepository,
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

	if err := repo.UpdateJob(ctx, e.db, jobID, accountID, upd); err != nil {
		return errResult(fmt.Sprintf("update job: %v", err), started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true},
		DurationMs: ms(started),
	}
}

func (e *desktopExecutor) doDelete(
	ctx context.Context,
	repo data.DesktopScheduledJobsRepository,
	accountID uuid.UUID,
	args map[string]any,
	started time.Time,
) tools.ExecutionResult {
	jobID, err := parseUUID(args, "job_id")
	if err != nil {
		return errResult(err.Error(), started)
	}
	if err := repo.DeleteJob(ctx, e.db, jobID, accountID); err != nil {
		return errResult(fmt.Sprintf("delete job: %v", err), started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true},
		DurationMs: ms(started),
	}
}

// -- helpers (duplicated from !desktop because build tags isolate them) --

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

func jobToMap(j data.ScheduledJobWithTrigger) map[string]any {
	m := map[string]any{
		"id":            j.ID.String(),
		"name":          j.Name,
		"description":   j.Description,
		"persona_key":   j.PersonaKey,
		"prompt":        j.Prompt,
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
	return m
}
