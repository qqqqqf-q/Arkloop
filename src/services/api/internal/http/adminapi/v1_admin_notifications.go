package adminapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"log/slog"
	nethttp "net/http"
	"strings"
	"time"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type broadcastResponse struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	TargetType  string         `json:"target_type"`
	TargetID    *string        `json:"target_id,omitempty"`
	PayloadJSON map[string]any `json:"payload"`
	Status      string         `json:"status"`
	SentCount   int            `json:"sent_count"`
	CreatedBy   string         `json:"created_by"`
	CreatedAt   string         `json:"created_at"`
}

func toBroadcastResponse(b data.NotificationBroadcast) broadcastResponse {
	resp := broadcastResponse{
		ID:          b.ID.String(),
		Type:        b.Type,
		Title:       b.Title,
		Body:        b.Body,
		TargetType:  b.TargetType,
		PayloadJSON: b.PayloadJSON,
		Status:      b.Status,
		SentCount:   b.SentCount,
		CreatedBy:   b.CreatedBy.String(),
		CreatedAt:   b.CreatedAt.UTC().Format(time.RFC3339),
	}
	if resp.PayloadJSON == nil {
		resp.PayloadJSON = map[string]any{}
	}
	if b.TargetID != nil {
		s := b.TargetID.String()
		resp.TargetID = &s
	}
	return resp
}

func adminBroadcastsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	notifRepo *data.NotificationsRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	pool data.DB,
	logger *slog.Logger,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodGet:
			listBroadcasts(authService, membershipRepo, notifRepo, apiKeysRepo)(w, r)
		case nethttp.MethodPost:
			createBroadcast(authService, membershipRepo, notifRepo, apiKeysRepo, auditWriter, pool, logger)(w, r)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func adminBroadcastEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	notifRepo *data.NotificationsRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/admin/notifications/broadcasts/")
		tail = strings.Trim(tail, "/")
		if tail == "" || strings.Contains(tail, "/") {
			httpkit.WriteNotFound(w, r)
			return
		}

		broadcastID, err := uuid.Parse(tail)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid broadcast id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			getBroadcast(authService, membershipRepo, notifRepo, apiKeysRepo)(w, r, broadcastID)
		case nethttp.MethodDelete:
			deleteBroadcast(authService, membershipRepo, notifRepo, apiKeysRepo)(w, r, broadcastID)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

type createBroadcastRequest struct {
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	Target      string         `json:"target"`
	PayloadJSON map[string]any `json:"payload"`
}

func createBroadcast(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	notifRepo *data.NotificationsRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	pool data.DB,
	logger *slog.Logger,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		var req createBroadcastRequest
		if err := httpkit.DecodeJSON(r, &req); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid request body", traceID, nil)
			return
		}

		if req.Type == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "type is required", traceID, nil)
			return
		}
		if req.Title == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "title is required", traceID, nil)
			return
		}

		// target 格式: "all" 或 account UUID
		targetType := "all"
		var targetID *uuid.UUID
		if req.Target != "" && req.Target != "all" {
			parsed, err := uuid.Parse(req.Target)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "target must be 'all' or a valid account_id", traceID, nil)
				return
			}
			targetType = "account"
			targetID = &parsed
		}

		broadcast, err := notifRepo.CreateBroadcast(
			r.Context(),
			req.Type, req.Title, req.Body,
			targetType, targetID,
			req.PayloadJSON,
			actor.UserID,
		)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "failed to create broadcast", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteBroadcastCreated(r.Context(), traceID, actor.UserID, broadcast.ID, targetType, targetID)
		}

		// 后台 goroutine 异步展开通知
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			bgRepo, err := data.NewNotificationsRepository(pool)
			if err != nil {
				if logger != nil {
					logger.Error("broadcast: failed to create bg repo",
						"trace_id", traceID,
						"error", err.Error(),
					)
				}
				return
			}

			var sentCount int
			var execErr error
			if broadcast.TargetType == "all" {
				sentCount, execErr = bgRepo.BroadcastToAll(bgCtx, broadcast)
			} else {
				sentCount, execErr = bgRepo.BroadcastToAccount(bgCtx, broadcast, *broadcast.TargetID)
			}

			status := "completed"
			if execErr != nil {
				status = "failed"
				if logger != nil {
					logger.Error("broadcast: execution failed",
						"trace_id", traceID,
						"broadcast_id", broadcast.ID.String(),
						"error", execErr.Error(),
					)
				}
			}

			if updateErr := bgRepo.UpdateBroadcastStatus(bgCtx, broadcast.ID, status, sentCount); updateErr != nil {
				if logger != nil {
					logger.Error("broadcast: status update failed",
						"trace_id", traceID,
						"broadcast_id", broadcast.ID.String(),
						"error", updateErr.Error(),
					)
				}
			}
		}()

		httpkit.WriteJSON(w, traceID, nethttp.StatusAccepted, toBroadcastResponse(broadcast))
	}
}

func listBroadcasts(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	notifRepo *data.NotificationsRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		limit, ok := httpkit.ParseLimit(w, traceID, r.URL.Query().Get("limit"))
		if !ok {
			return
		}
		beforeCreatedAt, beforeID, ok := httpkit.ParseThreadCursor(w, traceID, r.URL.Query())
		if !ok {
			return
		}

		items, err := notifRepo.ListBroadcasts(r.Context(), limit, beforeCreatedAt, beforeID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "failed to list broadcasts", traceID, nil)
			return
		}

		resp := make([]broadcastResponse, 0, len(items))
		for _, b := range items {
			resp = append(resp, toBroadcastResponse(b))
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func getBroadcast(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	notifRepo *data.NotificationsRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, broadcastID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		b, err := notifRepo.GetBroadcast(r.Context(), broadcastID)
		if err != nil {
			if err == pgx.ErrNoRows {
				httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", "broadcast not found", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "failed to get broadcast", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toBroadcastResponse(b))
	}
}

func deleteBroadcast(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	notifRepo *data.NotificationsRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, broadcastID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		err := notifRepo.DeleteBroadcast(r.Context(), broadcastID)
		if err != nil {
			if err == pgx.ErrNoRows {
				httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", "broadcast not found", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "failed to delete broadcast", traceID, nil)
			return
		}

		w.WriteHeader(nethttp.StatusNoContent)
	}
}
