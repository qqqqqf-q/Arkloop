package platformapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type notificationResponse struct {
	ID          string         `json:"id"`
	UserID      string         `json:"user_id"`
	AccountID       string         `json:"account_id"`
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	PayloadJSON map[string]any `json:"payload"`
	ReadAt      *string        `json:"read_at,omitempty"`
	CreatedAt   string         `json:"created_at"`
}

func notificationsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	notifRepo *data.NotificationsRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodGet:
			listNotifications(w, r, authService, membershipRepo, notifRepo, apiKeysRepo)
		case nethttp.MethodPatch:
			markAllNotificationsRead(w, r, authService, membershipRepo, notifRepo, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func notificationEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	notifRepo *data.NotificationsRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPatch {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
		markNotificationRead(w, r, authService, membershipRepo, notifRepo, apiKeysRepo)
	}
}

func listNotifications(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	notifRepo *data.NotificationsRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())

	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if notifRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}

	unreadOnly := r.URL.Query().Get("unread_only") == "true"

	var items []data.Notification
	var err error
	if unreadOnly {
		items, err = notifRepo.ListUnread(r.Context(), actor.UserID)
	} else {
		items, err = notifRepo.List(r.Context(), actor.UserID, 100)
	}
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", "failed to list notifications", traceID, nil)
		return
	}

	typeFilter := r.URL.Query().Get("type")

	resp := make([]notificationResponse, 0, len(items))
	for _, n := range items {
		if typeFilter != "" && n.Type != typeFilter {
			continue
		}
		resp = append(resp, toNotificationResponse(n))
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"data": resp})
}

func markNotificationRead(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	notifRepo *data.NotificationsRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())

	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if notifRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	tail := strings.TrimPrefix(r.URL.Path, "/v1/notifications/")
	tail = strings.Trim(tail, "/")
	if tail == "" {
		httpkit.WriteNotFound(w, r)
		return
	}

	notifID, err := uuid.Parse(tail)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid notification id", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}

	if err := notifRepo.MarkRead(r.Context(), actor.UserID, notifID); err != nil {
		if err == pgx.ErrNoRows {
			httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", "notification not found or already read", traceID, nil)
			return
		}
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", "failed to mark notification as read", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func markAllNotificationsRead(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	notifRepo *data.NotificationsRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())

	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if notifRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}

	count, err := notifRepo.MarkAllRead(r.Context(), actor.UserID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", "failed to mark notifications as read", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"ok": true, "count": count})
}

func toNotificationResponse(n data.Notification) notificationResponse {
	resp := notificationResponse{
		ID:          n.ID.String(),
		UserID:      n.UserID.String(),
		AccountID:       n.AccountID.String(),
		Type:        n.Type,
		Title:       n.Title,
		Body:        n.Body,
		PayloadJSON: n.PayloadJSON,
		CreatedAt:   n.CreatedAt.UTC().Format(time.RFC3339),
	}
	if n.PayloadJSON == nil {
		resp.PayloadJSON = map[string]any{}
	}
	if n.ReadAt != nil {
		s := n.ReadAt.UTC().Format(time.RFC3339)
		resp.ReadAt = &s
	}
	return resp
}
