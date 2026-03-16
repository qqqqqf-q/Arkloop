package conversationapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/featureflag"
	"arkloop/services/api/internal/http/featuregate"
	"arkloop/services/api/internal/observability"
)

const (
	shareSessionDuration = 24 * time.Hour
	shareSessionSecret   = "arkloop-share-session" // 作为 HMAC key 的前缀，实际 key = secret + share.ID
)

type sharedThreadResponse struct {
	RequiresPassword bool                `json:"requires_password"`
	Thread           *sharedThreadInfo   `json:"thread,omitempty"`
	Messages         []sharedMessageItem `json:"messages,omitempty"`
}

type sharedThreadInfo struct {
	Title     *string `json:"title"`
	CreatedAt string  `json:"created_at"`
}

type sharedMessageItem struct {
	ID          string          `json:"id"`
	Role        string          `json:"role"`
	Content     string          `json:"content"`
	ContentJSON json.RawMessage `json:"content_json,omitempty"`
	CreatedAt   string          `json:"created_at"`
}

type verifyShareRequest struct {
	Password string `json:"password"`
}

type verifyShareResponse struct {
	SessionToken string `json:"session_token"`
}

// publicShareEntry 处理 /v1/s/ 下的公开端点，无需认证。
func publicShareEntry(
	threadShareRepo *data.ThreadShareRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	flagService *featureflag.Service,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/s/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		parts := strings.SplitN(tail, "/", 2)
		token := parts[0]

		if len(parts) == 2 && parts[1] == "verify" {
			if r.Method != nethttp.MethodPost {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			handleShareVerify(w, r, traceID, token, threadShareRepo, threadRepo, flagService)
			return
		}

		if len(parts) == 1 {
			if r.Method != nethttp.MethodGet {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			handleShareGet(w, r, traceID, token, threadShareRepo, threadRepo, messageRepo, flagService)
			return
		}

		httpkit.WriteNotFound(w, r)
	}
}

func handleShareGet(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	token string,
	threadShareRepo *data.ThreadShareRepository,
	threadRepo *data.ThreadRepository,
	messageRepo *data.MessageRepository,
	flagService *featureflag.Service,
) {
	if threadShareRepo == nil || threadRepo == nil || messageRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	share, err := threadShareRepo.GetByToken(r.Context(), token)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if share == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "shares.not_found", "share not found", traceID, nil)
		return
	}

	// 密码保护：检查 session_token
	if share.AccessType == "password" {
		sessionToken := r.URL.Query().Get("session_token")
		if sessionToken == "" {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, sharedThreadResponse{RequiresPassword: true})
			return
		}
		if !validateShareSession(sessionToken, share) {
			httpkit.WriteError(w, nethttp.StatusForbidden, "shares.invalid_session", "invalid or expired session", traceID, nil)
			return
		}
	}

	thread, err := threadRepo.GetByID(r.Context(), share.ThreadID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if thread == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "shares.not_found", "share not found", traceID, nil)
		return
	}
	if !featuregate.EnsureClawEnabledForThread(w, traceID, r.Context(), thread, flagService) {
		return
	}

	limit := 10000
	if !share.LiveUpdate {
		limit = share.SnapshotMessageCount
		if limit <= 0 {
			limit = 1
		}
	}
	messages, err := messageRepo.ListByThread(r.Context(), thread.AccountID, thread.ID, limit)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := sharedThreadResponse{
		RequiresPassword: false,
		Thread: &sharedThreadInfo{
			Title:     thread.Title,
			CreatedAt: thread.CreatedAt.UTC().Format(time.RFC3339Nano),
		},
		Messages: make([]sharedMessageItem, 0, len(messages)),
	}

	for _, msg := range messages {
		resp.Messages = append(resp.Messages, sharedMessageItem{
			ID:          msg.ID.String(),
			Role:        msg.Role,
			Content:     msg.Content,
			ContentJSON: msg.ContentJSON,
			CreatedAt:   msg.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func handleShareVerify(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	token string,
	threadShareRepo *data.ThreadShareRepository,
	threadRepo *data.ThreadRepository,
	flagService *featureflag.Service,
) {
	if threadShareRepo == nil || threadRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	var body verifyShareRequest
	if err := httpkit.DecodeJSON(r, &body); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	if body.Password == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "password is required", traceID, nil)
		return
	}

	share, err := threadShareRepo.GetByToken(r.Context(), token)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if share == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "shares.not_found", "share not found", traceID, nil)
		return
	}
	thread, err := threadRepo.GetByID(r.Context(), share.ThreadID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if thread == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "shares.not_found", "share not found", traceID, nil)
		return
	}
	if !featuregate.EnsureClawEnabledForThread(w, traceID, r.Context(), thread, flagService) {
		return
	}

	if share.AccessType != "password" || share.Password == nil {
		httpkit.WriteError(w, nethttp.StatusBadRequest, "shares.not_password_protected", "share is not password protected", traceID, nil)
		return
	}

	if *share.Password != body.Password {
		httpkit.WriteError(w, nethttp.StatusForbidden, "shares.wrong_password", "incorrect password", traceID, nil)
		return
	}

	sessionToken := generateShareSession(share)

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, verifyShareResponse{SessionToken: sessionToken})
}

// generateShareSession 为验证通过的密码保护分享生成临时 session token。
// 格式：hex(HMAC-SHA256(key, token:expiry)):expiry
func generateShareSession(share *data.ThreadShare) string {
	expiry := time.Now().Add(shareSessionDuration).Unix()
	payload := fmt.Sprintf("%s:%d", share.Token, expiry)
	key := []byte(shareSessionSecret + share.ID.String())
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s:%d", sig, expiry)
}

func validateShareSession(sessionToken string, share *data.ThreadShare) bool {
	parts := strings.SplitN(sessionToken, ":", 2)
	if len(parts) != 2 {
		return false
	}

	sig := parts[0]
	expiryStr := parts[1]

	expiry, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix() > expiry {
		return false
	}

	payload := fmt.Sprintf("%s:%d", share.Token, expiry)
	key := []byte(shareSessionSecret + share.ID.String())
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expected))
}
