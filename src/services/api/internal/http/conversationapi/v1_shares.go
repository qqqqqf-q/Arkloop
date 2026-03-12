package conversationapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type createShareRequest struct {
	AccessType string  `json:"access_type"` // "public" | "password"
	Password   *string `json:"password"`
	LiveUpdate *bool   `json:"live_update"`
}

type shareResponse struct {
	ID                string  `json:"id"`
	Token             string  `json:"token"`
	URL               string  `json:"url"`
	AccessType        string  `json:"access_type"`
	Password          *string `json:"password,omitempty"`
	LiveUpdate        bool    `json:"live_update"`
	SnapshotTurnCount int     `json:"snapshot_turn_count"`
	CreatedAt         string  `json:"created_at"`
}

func handleThreadShare(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *httpkit.Actor,
	thread *data.Thread,
	threadShareRepo *data.ThreadShareRepository,
	messageRepo *data.MessageRepository,
) {
	switch r.Method {
	case nethttp.MethodPost:
		createShare(w, r, traceID, actor, thread, threadShareRepo, messageRepo)
	case nethttp.MethodGet:
		listShares(w, r, traceID, actor, thread, threadShareRepo)
	case nethttp.MethodDelete:
		deleteShare(w, r, traceID, actor, thread, threadShareRepo)
	default:
		httpkit.WriteMethodNotAllowed(w, r)
	}
}

func createShare(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *httpkit.Actor,
	thread *data.Thread,
	threadShareRepo *data.ThreadShareRepository,
	messageRepo *data.MessageRepository,
) {
	if threadShareRepo == nil || messageRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	var body createShareRequest
	if err := httpkit.DecodeJSON(r, &body); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	if body.AccessType == "" {
		body.AccessType = "public"
	}
	if body.AccessType != "public" && body.AccessType != "password" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "access_type must be 'public' or 'password'", traceID, nil)
		return
	}
	if body.AccessType == "password" {
		if body.Password == nil || strings.TrimSpace(*body.Password) == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "password is required for password-protected shares", traceID, nil)
			return
		}
		if len(*body.Password) > 128 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "password too long", traceID, nil)
			return
		}
	}

	messages, err := messageRepo.ListByThread(r.Context(), thread.AccountID, thread.ID, 10000)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	snapshotCount := len(messages)

	turnCount := 0
	for _, msg := range messages {
		if msg.Role == "user" {
			turnCount++
		}
	}

	liveUpdate := false
	if body.LiveUpdate != nil {
		liveUpdate = *body.LiveUpdate
	}

	token, err := data.GenerateShareToken()
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	var password *string
	if body.AccessType == "password" {
		p := strings.TrimSpace(*body.Password)
		password = &p
	}

	share, err := threadShareRepo.Create(
		r.Context(), thread.ID, token, body.AccessType, password,
		snapshotCount, liveUpdate, turnCount, actor.UserID,
	)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, shareResponse{
		ID:                share.ID.String(),
		Token:             share.Token,
		URL:               "/s/" + share.Token,
		AccessType:        share.AccessType,
		Password:          share.Password,
		LiveUpdate:        share.LiveUpdate,
		SnapshotTurnCount: share.SnapshotTurnCount,
		CreatedAt:         share.CreatedAt.UTC().Format(time.RFC3339Nano),
	})
}

func listShares(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *httpkit.Actor,
	thread *data.Thread,
	threadShareRepo *data.ThreadShareRepository,
) {
	if threadShareRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	shares, err := threadShareRepo.ListByThreadID(r.Context(), thread.ID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]shareResponse, 0, len(shares))
	for _, s := range shares {
		resp = append(resp, shareResponse{
			ID:                s.ID.String(),
			Token:             s.Token,
			URL:               "/s/" + s.Token,
			AccessType:        s.AccessType,
			Password:          s.Password,
			LiveUpdate:        s.LiveUpdate,
			SnapshotTurnCount: s.SnapshotTurnCount,
			CreatedAt:         s.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func deleteShare(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *httpkit.Actor,
	thread *data.Thread,
	threadShareRepo *data.ThreadShareRepository,
) {
	if threadShareRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	shareIDStr := r.URL.Query().Get("id")
	if shareIDStr == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "share id is required", traceID, nil)
		return
	}
	shareID, err := uuid.Parse(shareIDStr)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid share id", traceID, nil)
		return
	}

	deleted, err := threadShareRepo.DeleteByID(r.Context(), shareID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if !deleted {
		httpkit.WriteError(w, nethttp.StatusNotFound, "shares.not_found", "no share link exists", traceID, nil)
		return
	}

	w.WriteHeader(nethttp.StatusNoContent)
}

// shareEntry 作为认证端点的入口，在 threadEntry 的 :share action 中调用。
func shareEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	threadRepo *data.ThreadRepository,
	threadShareRepo *data.ThreadShareRepository,
	messageRepo *data.MessageRepository,
	auditWriter *audit.Writer,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
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

		if !authorizeThreadOrAudit(w, r, traceID, actor, "threads.share", thread, auditWriter) {
			return
		}

		handleThreadShare(w, r, traceID, actor, thread, threadShareRepo, messageRepo)
	}
}
