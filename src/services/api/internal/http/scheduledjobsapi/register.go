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

	"github.com/google/uuid"
)

type Deps struct {
	AuthService           *auth.Service
	AccountMembershipRepo *data.AccountMembershipRepository
	APIKeysRepo           *data.APIKeysRepository
	ScheduledJobsRepo     *data.ScheduledJobsRepository
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
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	PersonaKey   string  `json:"persona_key"`
	Prompt       string  `json:"prompt"`
	Model        string  `json:"model"`
	WorkspaceRef string  `json:"workspace_ref"`
	WorkDir      string  `json:"work_dir"`
	ThreadID     *string `json:"thread_id"`
	ScheduleKind string  `json:"schedule_kind"`
	IntervalMin  *int    `json:"interval_min"`
	DailyTime    string  `json:"daily_time"`
	MonthlyDay   *int    `json:"monthly_day"`
	MonthlyTime  string  `json:"monthly_time"`
	WeeklyDay    *int    `json:"weekly_day"`
	Timezone     string  `json:"timezone"`
}

type updateJobRequest struct {
	Name         *string  `json:"name"`
	Description  *string  `json:"description"`
	PersonaKey   *string  `json:"persona_key"`
	Prompt       *string  `json:"prompt"`
	Model        *string  `json:"model"`
	WorkspaceRef *string  `json:"workspace_ref"`
	WorkDir      *string  `json:"work_dir"`
	ThreadID     **string `json:"thread_id"`
	ScheduleKind *string  `json:"schedule_kind"`
	IntervalMin  **int    `json:"interval_min"`
	DailyTime    *string  `json:"daily_time"`
	MonthlyDay   **int    `json:"monthly_day"`
	MonthlyTime  *string  `json:"monthly_time"`
	WeeklyDay    **int    `json:"weekly_day"`
	Timezone     *string  `json:"timezone"`
}

type jobResponse struct {
	ID           uuid.UUID  `json:"id"`
	AccountID    uuid.UUID  `json:"account_id"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	PersonaKey   string     `json:"persona_key"`
	Prompt       string     `json:"prompt"`
	Model        string     `json:"model"`
	WorkspaceRef string     `json:"workspace_ref"`
	WorkDir      string     `json:"work_dir"`
	ThreadID     *uuid.UUID `json:"thread_id"`
	ScheduleKind string     `json:"schedule_kind"`
	IntervalMin  *int       `json:"interval_min,omitempty"`
	DailyTime    string     `json:"daily_time,omitempty"`
	MonthlyDay   *int       `json:"monthly_day,omitempty"`
	MonthlyTime  string     `json:"monthly_time,omitempty"`
	WeeklyDay    *int       `json:"weekly_day,omitempty"`
	Timezone     string     `json:"timezone"`
	Enabled      bool       `json:"enabled"`
	NextFireAt   *time.Time `json:"next_fire_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type listJobsResponse struct {
	Jobs []jobResponse `json:"jobs"`
}

func toJobResponse(j data.ScheduledJobWithTrigger) jobResponse {
	return jobResponse{
		ID:           j.ID,
		AccountID:    j.AccountID,
		Name:         j.Name,
		Description:  j.Description,
		PersonaKey:   j.PersonaKey,
		Prompt:       j.Prompt,
		Model:        j.Model,
		WorkspaceRef: j.WorkspaceRef,
		WorkDir:      j.WorkDir,
		ThreadID:     j.ThreadID,
		ScheduleKind: j.ScheduleKind,
		IntervalMin:  j.IntervalMin,
		DailyTime:    j.DailyTime,
		MonthlyDay:   j.MonthlyDay,
		MonthlyTime:  j.MonthlyTime,
		WeeklyDay:    j.WeeklyDay,
		Timezone:     j.Timezone,
		Enabled:      j.Enabled,
		NextFireAt:   j.NextFireAt,
		CreatedAt:    j.CreatedAt,
		UpdatedAt:    j.UpdatedAt,
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
	}

	personaKey := req.PersonaKey
	model := req.Model
	if threadID != nil {
		if strings.TrimSpace(personaKey) == "" || strings.TrimSpace(model) == "" {
			inferredPersona, inferredModel, err := deps.ScheduledJobsRepo.InferThreadContext(r.Context(), deps.Pool, *threadID)
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

	tz := req.Timezone
	if tz == "" {
		tz = "UTC"
	}

	job := data.ScheduledJob{
		ID:              uuid.New(),
		AccountID:       actor.AccountID,
		Name:            req.Name,
		Description:     req.Description,
		PersonaKey:      personaKey,
		Prompt:          req.Prompt,
		Model:           model,
		WorkspaceRef:    req.WorkspaceRef,
		WorkDir:         req.WorkDir,
		ThreadID:        threadID,
		ScheduleKind:    req.ScheduleKind,
		IntervalMin:     req.IntervalMin,
		DailyTime:       req.DailyTime,
		MonthlyDay:      req.MonthlyDay,
		MonthlyTime:     req.MonthlyTime,
		WeeklyDay:       req.WeeklyDay,
		Timezone:        tz,
		Enabled:         true,
		CreatedByUserID: &actor.UserID,
	}

	created, err := deps.ScheduledJobsRepo.CreateJob(r.Context(), deps.Pool, job)
	if err != nil {
		slog.Error("create scheduled job", "error", err, "trace_id", traceID)
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	notifyScheduler(r.Context(), deps.Pool)

	// 重新查询以获取 trigger 信息
	full, err := deps.ScheduledJobsRepo.GetByID(r.Context(), deps.Pool, created.ID, actor.AccountID)
	if err != nil || full == nil {
		// fallback: 返回无 trigger 的响应
		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toJobResponse(data.ScheduledJobWithTrigger{ScheduledJob: created}))
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
		if (params.PersonaKey != nil && *params.PersonaKey == "") || (params.Model != nil && *params.Model == "") {
			inferredPersona, inferredModel, err := deps.ScheduledJobsRepo.InferThreadContext(r.Context(), deps.Pool, **params.ThreadID)
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

	if err := deps.ScheduledJobsRepo.UpdateJob(r.Context(), deps.Pool, jobID, actor.AccountID, params); err != nil {
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
	default:
		errs = append(errs, "schedule_kind must be interval, daily, monthly, weekdays, or weekly")
	}

	if req.Timezone != "" {
		if _, err := time.LoadLocation(req.Timezone); err != nil {
			errs = append(errs, fmt.Sprintf("invalid timezone: %s", req.Timezone))
		}
	}

	return errs
}

func buildUpdateParams(raw map[string]json.RawMessage) (data.UpdateJobParams, []string) {
	var p data.UpdateJobParams
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
	if v, ok := raw["workspace_ref"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			p.WorkspaceRef = &s
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
			case "interval", "daily", "monthly", "weekdays", "weekly":
				p.ScheduleKind = &s
			default:
				errs = append(errs, "schedule_kind must be interval, daily, monthly, weekdays, or weekly")
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
			if s != "" {
				if _, err := time.LoadLocation(s); err != nil {
					errs = append(errs, fmt.Sprintf("invalid timezone: %s", s))
				} else {
					p.Timezone = &s
				}
			} else {
				p.Timezone = &s
			}
		}
	}

	return p, errs
}
