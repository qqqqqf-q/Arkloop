package conversationapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/featureflag"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

func createThreadMessage(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	auditWriter *audit.Writer,
	apiKeysRepo *data.APIKeysRepository,
	flagService *featureflag.Service,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		slog.Debug("createThreadMessage", "thread_id", threadID)
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil || messageRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataThreadsWrite, w, traceID) {
			return
		}

		var body createMessageRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		_, projection, contentJSON, err := normalizeCreateMessagePayload(body)
		if err != nil {
			slog.Warn("createThreadMessage: normalize failed", "error", err)
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, map[string]any{"reason": err.Error()})
			return
		}
		slog.Debug("createThreadMessage: payload normalized", "projection_len", len(projection), "has_content_json", len(contentJSON) > 0)

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			slog.Error("createThreadMessage: GetByID failed", "thread_id", threadID, "error", err)
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if thread == nil {
			slog.Warn("createThreadMessage: thread not found", "thread_id", threadID)
			httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}
		slog.Debug("createThreadMessage: thread found", "thread_id", threadID, "account_id", thread.AccountID)

		authorized := authorizeThreadOrAudit(w, r, traceID, actor, "messages.create", thread, auditWriter)
		slog.Debug("createThreadMessage: authorization check", "authorized", authorized)
		if !authorized {
			return
		}

		// Use thread.AccountID to ensure message is created with the same account_id as the thread.
		// This is critical for desktop mode where actor.AccountID may differ from the thread's actual account_id
		// due to how interceptDesktopActor resolves the actor from a dynamic desktop token.
		slog.Debug("createThreadMessage: calling CreateStructured", "account_id", thread.AccountID, "thread_id", threadID, "role", "user", "projection_len", len(projection))
		message, err := messageRepo.CreateStructured(r.Context(), thread.AccountID, threadID, "user", projection, contentJSON, &actor.UserID)
		if err != nil {
			slog.Error("createThreadMessage: CreateStructured failed", "thread_id", threadID, "error", err)
			var threadNotFound data.ThreadNotFoundError
			if errors.As(err, &threadNotFound) {
				httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		slog.Debug("createThreadMessage: success", "message_id", message.ID)

		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toMessageResponse(message))
	}
}

func listThreadMessages(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	auditWriter *audit.Writer,
	apiKeysRepo *data.APIKeysRepository,
	flagService *featureflag.Service,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil || messageRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataThreadsRead, w, traceID) {
			return
		}

		limit, ok := parseMessageLimit(w, traceID, r.URL.Query().Get("limit"))
		if !ok {
			return
		}

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if thread == nil {
			httpkit.WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}

		if !authorizeThreadOrAudit(w, r, traceID, actor, "messages.list", thread, auditWriter) {
			return
		}

		messages, err := messageRepo.ListByThread(r.Context(), actor.AccountID, threadID, limit)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		resp := make([]messageResponse, 0, len(messages))
		for _, item := range messages {
			resp = append(resp, toMessageResponse(item))
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func parseMessageLimit(w nethttp.ResponseWriter, traceID string, raw string) (int, bool) {
	if strings.TrimSpace(raw) == "" {
		return 200, true
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed < 1 || parsed > 500 {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
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
	var runID *string
	if len(message.MetadataJSON) > 0 {
		var metadata struct {
			RunID string `json:"run_id"`
		}
		if err := json.Unmarshal(message.MetadataJSON, &metadata); err == nil {
			metadata.RunID = strings.TrimSpace(metadata.RunID)
			if metadata.RunID != "" {
				runID = &metadata.RunID
			}
		}
	}

	return messageResponse{
		ID:              message.ID.String(),
		AccountID:       message.AccountID.String(),
		ThreadID:        message.ThreadID.String(),
		CreatedByUserID: createdByUserID,
		RunID:           runID,
		Role:            message.Role,
		Content:         message.Content,
		ContentJSON:     message.ContentJSON,
		CreatedAt:       message.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}
