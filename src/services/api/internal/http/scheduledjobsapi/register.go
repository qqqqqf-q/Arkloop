package scheduledjobsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	nethttp "net/http"
	"regexp"
	"strings"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/scheduledjobs"
	"arkloop/services/shared/schedulekind"

	"github.com/google/uuid"
)

type Deps struct {
	AuthService           *auth.Service
	AccountMembershipRepo *data.AccountMembershipRepository
	APIKeysRepo           *data.APIKeysRepository
	ScheduledJobsRepo     *data.ScheduledJobsRepository
	ThreadRepo            *data.ThreadRepository
	Pool                  data.DB
}

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("/v1/scheduled-jobs", scheduledJobsEntry(deps))
	mux.HandleFunc("/v1/scheduled-jobs/", scheduledJobEntry(deps))
}

// --- collection: GET(list) / POST(create) ---

func scheduledJobsEntry(deps Deps) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodGet:
			listJobs(w, r, traceID, deps)
		case nethttp.MethodPost:
			createJob(w, r, traceID, deps)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

// --- item: GET / PUT / DELETE + /pause /resume ---

func scheduledJobEntry(deps Deps) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/scheduled-jobs/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		// 检查子操作: {id}/pause 或 {id}/resume
		parts := strings.SplitN(tail, "/", 2)
		idStr := parts[0]
		action := ""
		if len(parts) == 2 {
			action = parts[1]
		}

		jobID, err := uuid.Parse(idStr)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid job id", traceID, nil)
			return
		}

		switch {
		case action == "" && r.Method == nethttp.MethodGet:
			getJob(w, r, traceID, deps, jobID)
		case action == "" && r.Method == nethttp.MethodPut:
			updateJob(w, r, traceID, deps, jobID)
		case action == "" && r.Method == nethttp.MethodDelete:
			deleteJob(w, r, traceID, deps, jobID)
		case action == "pause" && r.Method == nethttp.MethodPost:
			pauseJob(w, r, traceID, deps, jobID)
		case action == "resume" && r.Method == nethttp.MethodPost:
			resumeJob(w, r, traceID, deps, jobID)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

// --- request/response ---

type createJobRequest struct {
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	PersonaKey     string  `json:"persona_key"`
	Prompt         string  `json:"prompt"`
	Model          string  `json:"model"`
	WorkDir        string  `json:"work_dir"`
	ThreadID       *string `json:"thread_id"`
	ScheduleKind   string  `json:"schedule_kind"`
	IntervalMin    *int    `json:"interval_min"`
	DailyTime      string  `json:"daily_time"`
	MonthlyDay     *int    `json:"monthly_day"`
	MonthlyTime    string  `json:"monthly_time"`
	WeeklyDay      *int    `json:"weekly_day"`
	FireAt         *string `json:"fire_at"`
	CronExpr       string  `json:"cron_expr"`
	Timezone       string  `json:"timezone"`
	DeleteAfterRun bool    `json:"delete_after_run"`
	ReasoningMode  string  `json:"reasoning_mode"`
	Timeout        int     `json:"timeout"`
}

type updateJobRequest struct {
	Name           *string  `json:"name"`
	Description    *string  `json:"description"`
	PersonaKey     *string  `json:"persona_key"`
	Prompt         *string  `json:"prompt"`
	Model          *string  `json:"model"`
	WorkDir        *string  `json:"work_dir"`
	ThreadID       **string `json:"thread_id"`
	ScheduleKind   *string  `json:"schedule_kind"`
	IntervalMin    **int    `json:"interval_min"`
	DailyTime      *string  `json:"daily_time"`
	MonthlyDay     **int    `json:"monthly_day"`
	MonthlyTime    *string  `json:"monthly_time"`
	WeeklyDay      **int    `json:"weekly_day"`
	FireAt         *string  `json:"fire_at"`
	CronExpr       *string  `json:"cron_expr"`
	Timezone       *string  `json:"timezone"`
	DeleteAfterRun *bool    `json:"delete_after_run"`
	ReasoningMode  *string  `json:"reasoning_mode"`
	Timeout        *int     `json:"timeout"`
}

type jobResponse struct {
	ID             uuid.UUID  `json:"id"`
	AccountID      uuid.UUID  `json:"account_id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	PersonaKey     string     `json:"persona_key"`
	Prompt         string     `json:"prompt"`
	Model          string     `json:"model"`
	WorkDir        string     `json:"work_dir"`
	ThreadID       *uuid.UUID `json:"thread_id"`
	ScheduleKind   string     `json:"schedule_kind"`
	IntervalMin    *int       `json:"interval_min,omitempty"`
	DailyTime      string     `json:"daily_time,omitempty"`
	MonthlyDay     *int       `json:"monthly_day,omitempty"`
	MonthlyTime    string     `json:"monthly_time,omitempty"`
	WeeklyDay      *int       `json:"weekly_day,omitempty"`
	FireAt         *time.Time `json:"fire_at,omitempty"`
	CronExpr       string     `json:"cron_expr,omitempty"`
	Timezone       string     `json:"timezone"`
	DeleteAfterRun bool       `json:"delete_after_run"`
	ReasoningMode  string     `json:"reasoning_mode,omitempty"`
	Timeout        int        `json:"timeout"`
	Enabled        bool       `json:"enabled"`
	NextFireAt     *time.Time `json:"next_fire_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type listJobsResponse struct {
	Jobs []jobResponse `json:"jobs"`
}

func toJobResponse(j scheduledjobs.ScheduledJobWithTrigger) jobResponse {
	return jobResponse{
		ID:             j.ID,
		AccountID:      j.AccountID,
		Name:           j.Name,
		Description:    j.Description,
		PersonaKey:     j.PersonaKey,
		Prompt:         j.Prompt,
		Model:          j.Model,
		WorkDir:        j.WorkDir,
		ThreadID:       j.ThreadID,
		ScheduleKind:   j.ScheduleKind,
		IntervalMin:    j.IntervalMin,
		DailyTime:      j.DailyTime,
		MonthlyDay:     j.MonthlyDay,
		MonthlyTime:    j.MonthlyTime,
		WeeklyDay:      j.WeeklyDay,
		FireAt:         j.FireAt,
		CronExpr:       j.CronExpr,
		Timezone:       j.Timezone,
		DeleteAfterRun: j.DeleteAfterRun,
		ReasoningMode:  j.ReasoningMode,
		Timeout:        j.Timeout,
		Enabled:        j.Enabled,
		NextFireAt:     j.NextFireAt,
		CreatedAt:      j.CreatedAt,
		UpdatedAt:      j.UpdatedAt,
	}
}

// --- handlers ---

func listJobs(w nethttp.ResponseWriter, r *nethttp.Request, traceID string, deps Deps) {
	actor, ok := resolveActor(w, r, traceID, deps)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataScheduledJobsManage, w, traceID) {
		return
	}

	jobs, err := deps.ScheduledJobsRepo.ListByAccount(r.Context(), deps.Pool, actor.AccountID)
	if err != nil {
		slog.Error("list scheduled jobs", "error", err, "trace_id", traceID)
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := listJobsResponse{Jobs: make([]jobResponse, 0, len(jobs))}
	for _, j := range jobs {
		resp.Jobs = append(resp.Jobs, toJobResponse(j))
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func createJob(w nethttp.ResponseWriter, r *nethttp.Request, traceID string, deps Deps) {
	actor, ok := resolveActor(w, r, traceID, deps)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataScheduledJobsManage, w, traceID) {
		return
	}

	var req createJobRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid request body", traceID, nil)
		return
	}

	if errs := validateCreate(req); len(errs) > 0 {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, errs)
		return
	}

	var threadID *uuid.UUID
	if req.ThreadID != nil {
		parsed, err := uuid.Parse(*req.ThreadID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid thread_id", traceID, nil)
			return
		}
		threadID = &parsed
		if !validateOwnedThread(r.Context(), deps.ThreadRepo, actor.AccountID, parsed) {
			httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}
	}

	personaKey := req.PersonaKey
	model := req.Model
	if threadID != nil {
		if strings.TrimSpace(personaKey) == "" || strings.TrimSpace(model) == "" {
			inferredPersona, inferredModel, err := deps.ScheduledJobsRepo.InferThreadContext(r.Context(), deps.Pool, actor.AccountID, *threadID)
			if err != nil {
				slog.ErrorContext(r.Context(), "infer_thread_context_failed", "error", err)
			}
			if strings.TrimSpace(personaKey) == "" && inferredPersona != "" {
				personaKey = inferredPersona
			}
			if strings.TrimSpace(model) == "" && inferredModel != "" {
				model = inferredModel
			}
		}
	}

	tz := normalizeScheduledJobTimezone(req.Timezone)

	job := scheduledjobs.ScheduledJob{
		ID:              uuid.New(),
		AccountID:       actor.AccountID,
		Name:            req.Name,
		Description:     req.Description,
		PersonaKey:      personaKey,
		Prompt:          req.Prompt,
		Model:           model,
		WorkDir:         req.WorkDir,
		ThreadID:        threadID,
		ScheduleKind:    req.ScheduleKind,
		IntervalMin:     req.IntervalMin,
		DailyTime:       req.DailyTime,
		MonthlyDay:      req.MonthlyDay,
		MonthlyTime:     req.MonthlyTime,
		WeeklyDay:       req.WeeklyDay,
		CronExpr:        req.CronExpr,
		Timezone:        tz,
		Enabled:         true,
		DeleteAfterRun:  req.DeleteAfterRun,
		ReasoningMode:   normalizeScheduledJobReasoningMode(req.ReasoningMode),
		Timeout:         req.Timeout,
		CreatedByUserID: &actor.UserID,
	}

	// 解析 fire_at
	if req.FireAt != nil && *req.FireAt != "" {
		t, err := time.Parse(time.RFC3339, *req.FireAt)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid fire_at format, expected RFC3339", traceID, nil)
			return
		}
		job.FireAt = &t
	}
	if err := validateScheduledJobDefinition(job); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}

	created, err := deps.ScheduledJobsRepo.CreateJob(r.Context(), deps.Pool, job)
	if err != nil {
		if data.IsScheduledJobValidationError(err) {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}
		slog.Error("create scheduled job", "error", err, "trace_id", traceID)
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	notifyScheduler(r.Context(), deps.Pool)

	// 重新查询以获取 trigger 信息
	full, err := deps.ScheduledJobsRepo.GetByID(r.Context(), deps.Pool, created.ID, actor.AccountID)
	if err != nil || full == nil {
		// fallback: 返回无 trigger 的响应
		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toJobResponse(scheduledjobs.ScheduledJobWithTrigger{ScheduledJob: created}))
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toJobResponse(*full))
}

func getJob(w nethttp.ResponseWriter, r *nethttp.Request, traceID string, deps Deps, jobID uuid.UUID) {
	actor, ok := resolveActor(w, r, traceID, deps)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataScheduledJobsManage, w, traceID) {
		return
	}

	job, err := deps.ScheduledJobsRepo.GetByID(r.Context(), deps.Pool, jobID, actor.AccountID)
	if err != nil {
		slog.Error("get scheduled job", "error", err, "trace_id", traceID)
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if job == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", "scheduled job not found", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toJobResponse(*job))
}

func updateJob(w nethttp.ResponseWriter, r *nethttp.Request, traceID string, deps Deps, jobID uuid.UUID) {
	actor, ok := resolveActor(w, r, traceID, deps)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataScheduledJobsManage, w, traceID) {
		return
	}

	existing, err := deps.ScheduledJobsRepo.GetByID(r.Context(), deps.Pool, jobID, actor.AccountID)
	if err != nil {
		slog.Error("get scheduled job", "error", err, "trace_id", traceID)
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if existing == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", "scheduled job not found", traceID, nil)
		return
	}

	// 用 json.RawMessage 做部分解码，支持 null vs absent 区分
	var raw map[string]json.RawMessage
	if err := httpkit.DecodeJSON(r, &raw); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid request body", traceID, nil)
		return
	}

	params, errs := buildUpdateParams(raw)
	if len(errs) > 0 {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, errs)
		return
	}

	// thread_id 变更时，自动推断 persona_key 和 model
	if params.ThreadID != nil && *params.ThreadID != nil {
		if !validateOwnedThread(r.Context(), deps.ThreadRepo, actor.AccountID, **params.ThreadID) {
			httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}
		if (params.PersonaKey != nil && *params.PersonaKey == "") || (params.Model != nil && *params.Model == "") {
			inferredPersona, inferredModel, err := deps.ScheduledJobsRepo.InferThreadContext(r.Context(), deps.Pool, actor.AccountID, **params.ThreadID)
			if err == nil {
				if params.PersonaKey != nil && *params.PersonaKey == "" && inferredPersona != "" {
					*params.PersonaKey = inferredPersona
				}
				if params.Model != nil && *params.Model == "" && inferredModel != "" {
					*params.Model = inferredModel
				}
			}
		}
	}
	if shouldValidateScheduledJobUpdate(params) {
		merged := applyJobUpdatePreview(*existing, params)
		if err := validateScheduledJobDefinition(merged); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}
	}

	if err := deps.ScheduledJobsRepo.UpdateJob(r.Context(), deps.Pool, jobID, actor.AccountID, params); err != nil {
		if data.IsScheduledJobValidationError(err) {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}
		if strings.Contains(err.Error(), "not found") {
			httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", "scheduled job not found", traceID, nil)
			return
		}
		slog.Error("update scheduled job", "error", err, "trace_id", traceID)
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	notifyScheduler(r.Context(), deps.Pool)

	job, err := deps.ScheduledJobsRepo.GetByID(r.Context(), deps.Pool, jobID, actor.AccountID)
	if err != nil || job == nil {
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]string{"status": "updated"})
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toJobResponse(*job))
}

func deleteJob(w nethttp.ResponseWriter, r *nethttp.Request, traceID string, deps Deps, jobID uuid.UUID) {
	actor, ok := resolveActor(w, r, traceID, deps)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataScheduledJobsManage, w, traceID) {
		return
	}

	if err := deps.ScheduledJobsRepo.DeleteJob(r.Context(), deps.Pool, jobID, actor.AccountID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", "scheduled job not found", traceID, nil)
			return
		}
		slog.Error("delete scheduled job", "error", err, "trace_id", traceID)
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	w.WriteHeader(nethttp.StatusNoContent)
}

func pauseJob(w nethttp.ResponseWriter, r *nethttp.Request, traceID string, deps Deps, jobID uuid.UUID) {
	actor, ok := resolveActor(w, r, traceID, deps)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataScheduledJobsManage, w, traceID) {
		return
	}

	if err := deps.ScheduledJobsRepo.SetJobEnabled(r.Context(), deps.Pool, jobID, actor.AccountID, false); err != nil {
		if strings.Contains(err.Error(), "not found") {
			httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", "scheduled job not found", traceID, nil)
			return
		}
		slog.Error("pause scheduled job", "error", err, "trace_id", traceID)
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	job, err := deps.ScheduledJobsRepo.GetByID(r.Context(), deps.Pool, jobID, actor.AccountID)
	if err != nil || job == nil {
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]string{"status": "paused"})
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toJobResponse(*job))
}

func resumeJob(w nethttp.ResponseWriter, r *nethttp.Request, traceID string, deps Deps, jobID uuid.UUID) {
	actor, ok := resolveActor(w, r, traceID, deps)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataScheduledJobsManage, w, traceID) {
		return
	}

	if err := deps.ScheduledJobsRepo.SetJobEnabled(r.Context(), deps.Pool, jobID, actor.AccountID, true); err != nil {
		if data.IsScheduledJobValidationError(err) {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}
		if strings.Contains(err.Error(), "not found") {
			httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", "scheduled job not found", traceID, nil)
			return
		}
		slog.Error("resume scheduled job", "error", err, "trace_id", traceID)
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	notifyScheduler(r.Context(), deps.Pool)

	job, err := deps.ScheduledJobsRepo.GetByID(r.Context(), deps.Pool, jobID, actor.AccountID)
	if err != nil || job == nil {
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]string{"status": "resumed"})
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toJobResponse(*job))
}

// --- helpers ---

func resolveActor(w nethttp.ResponseWriter, r *nethttp.Request, traceID string, deps Deps) (*httpkit.Actor, bool) {
	return httpkit.ResolveActor(w, r, traceID, deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, nil)
}

func notifyScheduler(ctx context.Context, pool data.DB) {
	if _, err := pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelScheduledJobs, ""); err != nil {
		slog.Warn("pg_notify scheduled_jobs failed", "error", err)
	}
}

// --- validation ---

func ptrIntPtr(n int) **int {
	p := &n
	return &p
}

func nilIntPtr() **int {
	var p *int
	return &p
}

func ptrUUIDPtr(u uuid.UUID) **uuid.UUID {
	p := &u
	return &p
}

func nilUUIDPtr() **uuid.UUID {
	var p *uuid.UUID
	return &p
}

func nilTimePtr() **time.Time {
	var p *time.Time
	return &p
}

var timeHHMMRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func validateCreate(req createJobRequest) []string {
	var errs []string

	if strings.TrimSpace(req.Name) == "" {
		errs = append(errs, "name is required")
	} else if len(req.Name) > 200 {
		errs = append(errs, "name must be <= 200 characters")
	}
	if strings.TrimSpace(req.PersonaKey) == "" && (req.ThreadID == nil || strings.TrimSpace(*req.ThreadID) == "") {
		errs = append(errs, "persona_key is required when thread_id is not set")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		errs = append(errs, "prompt is required")
	}

	switch req.ScheduleKind {
	case "interval":
		if req.IntervalMin == nil || *req.IntervalMin < 1 {
			errs = append(errs, "interval_min must be >= 1 for interval schedule")
		}
	case "daily":
		if !timeHHMMRe.MatchString(req.DailyTime) {
			errs = append(errs, "daily_time must be HH:MM format")
		}
	case "monthly":
		if req.MonthlyDay == nil || *req.MonthlyDay < 1 || *req.MonthlyDay > 28 {
			errs = append(errs, "monthly_day must be 1-28")
		}
		if !timeHHMMRe.MatchString(req.MonthlyTime) {
			errs = append(errs, "monthly_time must be HH:MM format")
		}
	case "weekdays":
		if !timeHHMMRe.MatchString(req.DailyTime) {
			errs = append(errs, "daily_time must be HH:MM format")
		}
	case "weekly":
		if req.WeeklyDay == nil || *req.WeeklyDay < 0 || *req.WeeklyDay > 6 {
			errs = append(errs, "weekly_day must be 0-6")
		}
		if !timeHHMMRe.MatchString(req.DailyTime) {
			errs = append(errs, "daily_time must be HH:MM format")
		}
	case "at":
		if req.FireAt == nil || strings.TrimSpace(*req.FireAt) == "" {
			errs = append(errs, "fire_at is required for at schedule")
		}
	case "cron":
		if strings.TrimSpace(req.CronExpr) == "" {
			errs = append(errs, "cron_expr is required for cron schedule")
		}
	default:
		errs = append(errs, "schedule_kind must be interval, daily, monthly, weekdays, weekly, at, or cron")
	}

	if req.Timezone != "" {
		if _, err := time.LoadLocation(req.Timezone); err != nil {
			errs = append(errs, fmt.Sprintf("invalid timezone: %s", req.Timezone))
		}
	}

	if req.ReasoningMode != "" && normalizeScheduledJobReasoningMode(req.ReasoningMode) == "" {
		errs = append(errs, "invalid reasoning_mode")
	}
	if req.DeleteAfterRun && !schedulekind.SupportsDeleteAfterRun(req.ScheduleKind) {
		errs = append(errs, "delete_after_run is only supported for at schedule")
	}

	return errs
}

// normalizeScheduledJobReasoningMode 与 conversationapi.normalizeRunReasoningMode 保持一致，
// 把多种别名归并到稳定枚举值；空串表示沿用 persona 默认值。
func normalizeScheduledJobReasoningMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case "auto":
		return "auto"
	case "enabled":
		return "enabled"
	case "disabled":
		return "disabled"
	case "none", "off":
		return "none"
	case "minimal":
		return "minimal"
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	case "max", "xhigh", "extra_high", "extra-high", "extra high":
		return "xhigh"
	default:
		return ""
	}
}

func buildUpdateParams(raw map[string]json.RawMessage) (scheduledjobs.UpdateJobParams, []string) {
	var p scheduledjobs.UpdateJobParams
	var errs []string

	if v, ok := raw["name"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			if strings.TrimSpace(s) == "" {
				errs = append(errs, "name cannot be empty")
			} else if len(s) > 200 {
				errs = append(errs, "name must be <= 200 characters")
			} else {
				p.Name = &s
			}
		}
	}
	if v, ok := raw["description"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			p.Description = &s
		}
	}
	if v, ok := raw["persona_key"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			p.PersonaKey = &s
		}
	}
	if v, ok := raw["prompt"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			if strings.TrimSpace(s) == "" {
				errs = append(errs, "prompt cannot be empty")
			} else {
				p.Prompt = &s
			}
		}
	}
	if v, ok := raw["model"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			p.Model = &s
		}
	}
	if v, ok := raw["work_dir"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			p.WorkDir = &s
		}
	}
	if v, ok := raw["thread_id"]; ok {
		if string(v) == "null" {
			p.ThreadID = nilUUIDPtr()
		} else {
			var s string
			if json.Unmarshal(v, &s) == nil {
				parsed, err := uuid.Parse(s)
				if err != nil {
					errs = append(errs, "invalid thread_id")
				} else {
					p.ThreadID = ptrUUIDPtr(parsed)
				}
			}
		}
	}
	if v, ok := raw["schedule_kind"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			switch s {
			case "interval", "daily", "monthly", "weekdays", "weekly", "at", "cron":
				p.ScheduleKind = &s
			default:
				errs = append(errs, "schedule_kind must be interval, daily, monthly, weekdays, weekly, at, or cron")
			}
		}
	}
	if v, ok := raw["interval_min"]; ok {
		if string(v) == "null" {
			p.IntervalMin = nilIntPtr()
		} else {
			var n int
			if json.Unmarshal(v, &n) == nil {
				if n < 1 {
					errs = append(errs, "interval_min must be >= 1")
				} else {
					p.IntervalMin = ptrIntPtr(n)
				}
			}
		}
	}
	if v, ok := raw["daily_time"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			if s != "" && !timeHHMMRe.MatchString(s) {
				errs = append(errs, "daily_time must be HH:MM format")
			} else {
				p.DailyTime = &s
			}
		}
	}
	if v, ok := raw["monthly_day"]; ok {
		if string(v) == "null" {
			p.MonthlyDay = nilIntPtr()
		} else {
			var n int
			if json.Unmarshal(v, &n) == nil {
				if n < 1 || n > 28 {
					errs = append(errs, "monthly_day must be 1-28")
				} else {
					p.MonthlyDay = ptrIntPtr(n)
				}
			}
		}
	}
	if v, ok := raw["monthly_time"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			if s != "" && !timeHHMMRe.MatchString(s) {
				errs = append(errs, "monthly_time must be HH:MM format")
			} else {
				p.MonthlyTime = &s
			}
		}
	}
	if v, ok := raw["weekly_day"]; ok {
		if string(v) == "null" {
			p.WeeklyDay = nilIntPtr()
		} else {
			var n int
			if json.Unmarshal(v, &n) == nil {
				if n < 0 || n > 6 {
					errs = append(errs, "weekly_day must be 0-6")
				} else {
					p.WeeklyDay = ptrIntPtr(n)
				}
			}
		}
	}
	if v, ok := raw["timezone"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			s = normalizeScheduledJobTimezone(s)
			if _, err := time.LoadLocation(s); err != nil {
				errs = append(errs, fmt.Sprintf("invalid timezone: %s", s))
			} else {
				p.Timezone = &s
			}
		}
	}
	if v, ok := raw["fire_at"]; ok {
		if string(v) == "null" {
			p.FireAt = nilTimePtr()
		} else {
			var s string
			if json.Unmarshal(v, &s) == nil {
				parsed, err := time.Parse(time.RFC3339, s)
				if err != nil {
					errs = append(errs, "fire_at must be RFC3339 format")
				} else {
					tp := &parsed
					p.FireAt = &tp
				}
			}
		}
	}
	if v, ok := raw["cron_expr"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			p.CronExpr = &s
		}
	}
	if v, ok := raw["delete_after_run"]; ok {
		var b bool
		if json.Unmarshal(v, &b) == nil {
			p.DeleteAfterRun = &b
		}
	}
	if v, ok := raw["reasoning_mode"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			normalized := normalizeScheduledJobReasoningMode(s)
			if s != "" && normalized == "" {
				errs = append(errs, "invalid reasoning_mode")
			} else {
				p.ReasoningMode = &normalized
			}
		}
	}
	if v, ok := raw["timeout"]; ok {
		var n int
		if json.Unmarshal(v, &n) == nil {
			p.Timeout = &n
		}
	}

	return p, errs
}

func validateOwnedThread(ctx context.Context, threadRepo *data.ThreadRepository, accountID, threadID uuid.UUID) bool {
	if threadRepo == nil {
		return false
	}
	thread, err := threadRepo.GetByID(ctx, threadID)
	if err != nil || thread == nil {
		return false
	}
	return thread.AccountID == accountID
}

func validateScheduledJobDefinition(job scheduledjobs.ScheduledJob) error {
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
	job.Timezone = normalizeScheduledJobTimezone(job.Timezone)
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

func applyJobUpdatePreview(existing scheduledjobs.ScheduledJobWithTrigger, upd scheduledjobs.UpdateJobParams) scheduledjobs.ScheduledJob {
	job := existing.ScheduledJob
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
	if upd.FireAt != nil {
		job.FireAt = *upd.FireAt
	}
	if upd.CronExpr != nil {
		job.CronExpr = *upd.CronExpr
	}
	if upd.Timezone != nil {
		job.Timezone = normalizeScheduledJobTimezone(*upd.Timezone)
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
	return job
}

func shouldValidateScheduledJobUpdate(upd scheduledjobs.UpdateJobParams) bool {
	return upd.PersonaKey != nil ||
		upd.ThreadID != nil ||
		upd.ScheduleKind != nil ||
		upd.IntervalMin != nil ||
		upd.DailyTime != nil ||
		upd.MonthlyDay != nil ||
		upd.MonthlyTime != nil ||
		upd.WeeklyDay != nil ||
		upd.FireAt != nil ||
		upd.CronExpr != nil ||
		upd.Timezone != nil ||
		(upd.Enabled != nil && *upd.Enabled)
}

func normalizeScheduledJobTimezone(tz string) string {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return "UTC"
	}
	return tz
}

func derefIntPtr(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func derefIntPtrOr(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}

func derefTimePtr(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return *v
}
