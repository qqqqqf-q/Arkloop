package http

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api_go/internal/audit"
	"arkloop/services/api_go/internal/auth"
	"arkloop/services/api_go/internal/data"
	"arkloop/services/api_go/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type createThreadRequest struct {
	Title *string `json:"title"`
}

type updateThreadRequest struct {
	Title optionalString `json:"title"`
}

type threadResponse struct {
	ID              string  `json:"id"`
	OrgID           string  `json:"org_id"`
	CreatedByUserID *string `json:"created_by_user_id"`
	Title           *string `json:"title"`
	CreatedAt       string  `json:"created_at"`
}

type optionalString struct {
	Present bool
	Value   *string
}

func (s *optionalString) UnmarshalJSON(raw []byte) error {
	s.Present = true
	if string(raw) == "null" {
		s.Value = nil
		return nil
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	s.Value = &value
	return nil
}

func createThread(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "数据库未配置", traceID, nil)
			return
		}

		actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		var body createThreadRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
			return
		}

		if body.Title != nil && len(*body.Title) > 200 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
			return
		}

		thread, err := threadRepo.Create(r.Context(), actor.OrgID, &actor.UserID, body.Title)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}

		writeJSON(w, traceID, nethttp.StatusCreated, toThreadResponse(thread))
	}
}

func listThreads(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "数据库未配置", traceID, nil)
			return
		}

		actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		limit, ok := parseLimit(w, traceID, r.URL.Query().Get("limit"))
		if !ok {
			return
		}

		beforeCreatedAt, beforeID, ok := parseThreadCursor(w, traceID, r.URL.Query())
		if !ok {
			return
		}

		threads, err := threadRepo.ListByOwner(
			r.Context(),
			actor.OrgID,
			actor.UserID,
			limit,
			beforeCreatedAt,
			beforeID,
		)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}

		resp := make([]threadResponse, 0, len(threads))
		for _, item := range threads {
			resp = append(resp, toThreadResponse(item))
		}
		writeJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func getThread(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "数据库未配置", traceID, nil)
			return
		}

		actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}
		if thread == nil {
			WriteError(w, nethttp.StatusNotFound, "threads.not_found", "Thread 不存在", traceID, nil)
			return
		}

		if !authorizeThreadOrAudit(w, r, traceID, actor, "threads.get", thread, auditWriter) {
			return
		}

		writeJSON(w, traceID, nethttp.StatusOK, toThreadResponse(*thread))
	}
}

func patchThread(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "数据库未配置", traceID, nil)
			return
		}

		actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		var body updateThreadRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
			return
		}
		if !body.Title.Present {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
			return
		}
		if body.Title.Value != nil && len(*body.Title.Value) > 200 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
			return
		}

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}
		if thread == nil {
			WriteError(w, nethttp.StatusNotFound, "threads.not_found", "Thread 不存在", traceID, nil)
			return
		}

		if !authorizeThreadOrAudit(w, r, traceID, actor, "threads.update", thread, auditWriter) {
			return
		}

		updated, err := threadRepo.UpdateTitle(r.Context(), threadID, body.Title.Value)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}
		if updated == nil {
			WriteError(w, nethttp.StatusNotFound, "threads.not_found", "Thread 不存在", traceID, nil)
			return
		}

		writeJSON(w, traceID, nethttp.StatusOK, toThreadResponse(*updated))
	}
}

func threadsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	create := createThread(authService, membershipRepo, threadRepo)
	list := listThreads(authService, membershipRepo, threadRepo)
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			create(w, r)
		case nethttp.MethodGet:
			list(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func threadEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
	pool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	get := getThread(authService, membershipRepo, threadRepo, auditWriter)
	patch := patchThread(authService, membershipRepo, threadRepo, auditWriter)
	createMessage := createThreadMessage(authService, membershipRepo, threadRepo, messageRepo, auditWriter)
	listMessages := listThreadMessages(authService, membershipRepo, threadRepo, messageRepo, auditWriter)
	createRun := createThreadRun(authService, membershipRepo, threadRepo, auditWriter, pool)
	listRuns := listThreadRuns(authService, membershipRepo, threadRepo, runRepo, auditWriter)
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path == "/v1/threads/" {
			threadsEntry(authService, membershipRepo, threadRepo)(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/threads/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}

		// 最多分两段：{uuid} 和可选的 sub-resource
		parts := strings.SplitN(tail, "/", 2)
		threadID, err := uuid.Parse(parts[0])
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
			return
		}

		if len(parts) == 1 {
			switch r.Method {
			case nethttp.MethodGet:
				get(w, r, threadID)
			case nethttp.MethodPatch:
				patch(w, r, threadID)
			default:
				writeMethodNotAllowed(w, r)
			}
			return
		}

		// sub-resource 分发，P06/P07 在此接入各自 handler
		switch parts[1] {
		case "messages":
			// P06: thread messages
			switch r.Method {
			case nethttp.MethodPost:
				createMessage(w, r, threadID)
			case nethttp.MethodGet:
				listMessages(w, r, threadID)
			default:
				writeMethodNotAllowed(w, r)
			}
		case "runs":
			// P07: thread runs
			switch r.Method {
			case nethttp.MethodPost:
				createRun(w, r, threadID)
			case nethttp.MethodGet:
				listRuns(w, r, threadID)
			default:
				writeMethodNotAllowed(w, r)
			}
		default:
			writeNotFound(w, r)
		}
	}
}

func parseLimit(w nethttp.ResponseWriter, traceID string, raw string) (int, bool) {
	if strings.TrimSpace(raw) == "" {
		return 50, true
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed < 1 || parsed > 200 {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
		return 0, false
	}
	return parsed, true
}

func parseThreadCursor(
	w nethttp.ResponseWriter,
	traceID string,
	values url.Values,
) (*time.Time, *uuid.UUID, bool) {
	beforeCreatedAtRaw := strings.TrimSpace(first(values, "before_created_at"))
	beforeIDRaw := strings.TrimSpace(first(values, "before_id"))

	if (beforeCreatedAtRaw == "") != (beforeIDRaw == "") {
		WriteError(
			w,
			nethttp.StatusUnprocessableEntity,
			"validation_error",
			"请求参数校验失败",
			traceID,
			map[string]any{"reason": "cursor_incomplete", "required": []string{"before_created_at", "before_id"}},
		)
		return nil, nil, false
	}
	if beforeCreatedAtRaw == "" {
		return nil, nil, true
	}

	parsedTime, err := time.Parse(time.RFC3339Nano, beforeCreatedAtRaw)
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
		return nil, nil, false
	}
	parsedID, err := uuid.Parse(beforeIDRaw)
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
		return nil, nil, false
	}

	return &parsedTime, &parsedID, true
}

func first(values url.Values, key string) string {
	raw := values[key]
	if len(raw) == 0 {
		return ""
	}
	return raw[0]
}

func authorizeThreadOrAudit(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *actor,
	action string,
	thread *data.Thread,
	auditWriter *audit.Writer,
) bool {
	if actor == nil || thread == nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
		return false
	}

	denyReason := "owner_mismatch"
	if actor.OrgID != thread.OrgID {
		denyReason = "org_mismatch"
	} else if thread.CreatedByUserID == nil {
		denyReason = "no_owner"
	} else if *thread.CreatedByUserID == actor.UserID {
		return true
	}

	if auditWriter != nil {
		auditWriter.WriteAccessDenied(
			r.Context(),
			traceID,
			actor.OrgID,
			actor.UserID,
			action,
			"thread",
			thread.ID.String(),
			thread.OrgID,
			thread.CreatedByUserID,
			denyReason,
		)
	}

	WriteError(
		w,
		nethttp.StatusForbidden,
		"policy.denied",
		"无权限",
		traceID,
		map[string]any{"action": action},
	)
	return false
}

func toThreadResponse(thread data.Thread) threadResponse {
	var createdByUserID *string
	if thread.CreatedByUserID != nil {
		value := thread.CreatedByUserID.String()
		createdByUserID = &value
	}
	return threadResponse{
		ID:              thread.ID.String(),
		OrgID:           thread.OrgID.String(),
		CreatedByUserID: createdByUserID,
		Title:           thread.Title,
		CreatedAt:       thread.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}
