package http

import (
	"errors"
	"strconv"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type createMessageRequest struct {
	Content string `json:"content"`
}

type messageResponse struct {
	ID              string  `json:"id"`
	OrgID           string  `json:"org_id"`
	ThreadID        string  `json:"thread_id"`
	CreatedByUserID *string `json:"created_by_user_id"`
	Role            string  `json:"role"`
	Content         string  `json:"content"`
	CreatedAt       string  `json:"created_at"`
}

func createThreadMessage(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil || messageRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "数据库未配置", traceID, nil)
			return
		}

		actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		var body createMessageRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
			return
		}
		if body.Content == "" || len(body.Content) > 20000 {
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

		if !authorizeThreadOrAudit(w, r, traceID, actor, "messages.create", thread, auditWriter) {
			return
		}

		message, err := messageRepo.Create(r.Context(), actor.OrgID, threadID, "user", body.Content, &actor.UserID)
		if err != nil {
			var threadNotFound data.ThreadNotFoundError
			if errors.As(err, &threadNotFound) {
				WriteError(w, nethttp.StatusNotFound, "threads.not_found", "Thread 不存在", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}

		writeJSON(w, traceID, nethttp.StatusCreated, toMessageResponse(message))
	}
}

func listThreadMessages(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	auditWriter *audit.Writer,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil || messageRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "数据库未配置", traceID, nil)
			return
		}

		actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		limit, ok := parseMessageLimit(w, traceID, r.URL.Query().Get("limit"))
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

		if !authorizeThreadOrAudit(w, r, traceID, actor, "messages.list", thread, auditWriter) {
			return
		}

		messages, err := messageRepo.ListByThread(r.Context(), actor.OrgID, threadID, limit)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal_error", "内部错误", traceID, nil)
			return
		}

		resp := make([]messageResponse, 0, len(messages))
		for _, item := range messages {
			resp = append(resp, toMessageResponse(item))
		}
		writeJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func parseMessageLimit(w nethttp.ResponseWriter, traceID string, raw string) (int, bool) {
	if strings.TrimSpace(raw) == "" {
		return 200, true
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed < 1 || parsed > 500 {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation_error", "请求参数校验失败", traceID, nil)
		return 0, false
	}
	return parsed, true
}

func toMessageResponse(message data.Message) messageResponse {
	var createdByUserID *string
	if message.CreatedByUserID != nil {
		value := message.CreatedByUserID.String()
		createdByUserID = &value
	}

	return messageResponse{
		ID:              message.ID.String(),
		OrgID:           message.OrgID.String(),
		ThreadID:        message.ThreadID.String(),
		CreatedByUserID: createdByUserID,
		Role:            message.Role,
		Content:         message.Content,
		CreatedAt:       message.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}
