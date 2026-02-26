package http

import (
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type createShareRequest struct {
	AccessType string  `json:"access_type"` // "public" | "password"
	Password   *string `json:"password"`
}

type shareResponse struct {
	Token      string `json:"token"`
	URL        string `json:"url"`
	AccessType string `json:"access_type"`
	CreatedAt  string `json:"created_at"`
}

func handleThreadShare(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *actor,
	thread *data.Thread,
	threadShareRepo *data.ThreadShareRepository,
	messageRepo *data.MessageRepository,
) {
	switch r.Method {
	case nethttp.MethodPost:
		createOrUpdateShare(w, r, traceID, actor, thread, threadShareRepo, messageRepo)
	case nethttp.MethodGet:
		getShareInfo(w, r, traceID, actor, thread, threadShareRepo)
	case nethttp.MethodDelete:
		deleteShare(w, r, traceID, actor, thread, threadShareRepo)
	default:
		writeMethodNotAllowed(w, r)
	}
}

func createOrUpdateShare(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *actor,
	thread *data.Thread,
	threadShareRepo *data.ThreadShareRepository,
	messageRepo *data.MessageRepository,
) {
	if threadShareRepo == nil || messageRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	var body createShareRequest
	if err := decodeJSON(r, &body); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	if body.AccessType == "" {
		body.AccessType = "public"
	}
	if body.AccessType != "public" && body.AccessType != "password" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "access_type must be 'public' or 'password'", traceID, nil)
		return
	}
	if body.AccessType == "password" {
		if body.Password == nil || strings.TrimSpace(*body.Password) == "" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "password is required for password-protected shares", traceID, nil)
			return
		}
		if len(*body.Password) > 128 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "password too long", traceID, nil)
			return
		}
	}

	// 统计当前消息数作为快照基准
	messages, err := messageRepo.ListByThread(r.Context(), thread.OrgID, thread.ID, 10000)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	snapshotCount := len(messages)

	token, err := data.GenerateShareToken()
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	var passwordHash *string
	if body.AccessType == "password" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*body.Password), bcrypt.DefaultCost)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		h := string(hash)
		passwordHash = &h
	}

	share, err := threadShareRepo.Upsert(
		r.Context(), thread.ID, token, body.AccessType, passwordHash, snapshotCount, actor.UserID,
	)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusOK, shareResponse{
		Token:      share.Token,
		URL:        "/s/" + share.Token,
		AccessType: share.AccessType,
		CreatedAt:  share.CreatedAt.UTC().Format(time.RFC3339Nano),
	})
}

func getShareInfo(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *actor,
	thread *data.Thread,
	threadShareRepo *data.ThreadShareRepository,
) {
	if threadShareRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	share, err := threadShareRepo.GetByThreadID(r.Context(), thread.ID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if share == nil {
		WriteError(w, nethttp.StatusNotFound, "shares.not_found", "no share link exists", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusOK, shareResponse{
		Token:      share.Token,
		URL:        "/s/" + share.Token,
		AccessType: share.AccessType,
		CreatedAt:  share.CreatedAt.UTC().Format(time.RFC3339Nano),
	})
}

func deleteShare(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *actor,
	thread *data.Thread,
	threadShareRepo *data.ThreadShareRepository,
) {
	if threadShareRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	deleted, err := threadShareRepo.DeleteByThreadID(r.Context(), thread.ID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if !deleted {
		WriteError(w, nethttp.StatusNotFound, "shares.not_found", "no share link exists", traceID, nil)
		return
	}

	w.WriteHeader(nethttp.StatusNoContent)
}

// shareEntry 作为认证端点的入口，在 threadEntry 的 :share action 中调用。
func shareEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	threadRepo *data.ThreadRepository,
	threadShareRepo *data.ThreadShareRepository,
	messageRepo *data.MessageRepository,
	auditWriter *audit.Writer,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request, uuid.UUID) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request, threadID uuid.UUID) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}

		thread, err := threadRepo.GetByID(r.Context(), threadID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if thread == nil {
			WriteError(w, nethttp.StatusNotFound, "threads.not_found", "thread not found", traceID, nil)
			return
		}

		if !authorizeThreadOrAudit(w, r, traceID, actor, "threads.share", thread, auditWriter) {
			return
		}

		handleThreadShare(w, r, traceID, actor, thread, threadShareRepo, messageRepo)
	}
}
