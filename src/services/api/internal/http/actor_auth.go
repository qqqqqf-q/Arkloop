//go:build !desktop

package http

import (
	"context"
	"errors"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

const apiKeyLastUsedUpdateTimeout = 2 * time.Second

func authenticateActor(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
) (*actor, bool) {
	_ = membershipRepo

	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return nil, false
	}

	token, ok := parseBearerToken(w, r, traceID)
	if !ok {
		return nil, false
	}

	verified, err := authService.VerifyAccessTokenForActor(r.Context(), token)
	if err != nil {
		var expired auth.TokenExpiredError
		if errors.As(err, &expired) {
			WriteError(w, nethttp.StatusUnauthorized, "auth.token_expired", expired.Error(), traceID, nil)
			return nil, false
		}
		var invalid auth.TokenInvalidError
		if errors.As(err, &invalid) {
			WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", invalid.Error(), traceID, nil)
			return nil, false
		}
		var notFound auth.UserNotFoundError
		if errors.As(err, &notFound) {
			WriteError(w, nethttp.StatusUnauthorized, "auth.user_not_found", "user not found", traceID, nil)
			return nil, false
		}
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return nil, false
	}

	if verified.AccountID == uuid.Nil || strings.TrimSpace(verified.AccountRole) == "" {
		WriteError(w, nethttp.StatusForbidden, "auth.no_account_membership", "user has no account membership", traceID, nil)
		return nil, false
	}

	// v1：权限通过 PermissionsForRole 静态映射，无额外 DB 查询。
	// verified.AccountRole 为后续自定义角色动态加载预留，届时改为查询 rbac_roles 表。
	return &actor{
		AccountID:   verified.AccountID,
		UserID:      verified.UserID,
		AccountRole: verified.AccountRole,
		Permissions: auth.PermissionsForRole(verified.AccountRole),
	}, true
}

// resolveActor 支持 JWT 和 API Key 双路径鉴权。
// apiKeysRepo 为 nil 时退化为 JWT only。
func resolveActor(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) (*actor, bool) {
	token, ok := parseBearerToken(w, r, traceID)
	if !ok {
		return nil, false
	}

	if apiKeysRepo != nil && strings.HasPrefix(token, "ak-") {
		return resolveActorFromAPIKey(w, r, traceID, token, membershipRepo, apiKeysRepo, auditWriter)
	}

	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return nil, false
	}

	verified, err := authService.VerifyAccessTokenForActor(r.Context(), token)
	if err != nil {
		var expired auth.TokenExpiredError
		if errors.As(err, &expired) {
			WriteError(w, nethttp.StatusUnauthorized, "auth.token_expired", expired.Error(), traceID, nil)
			return nil, false
		}
		var invalid auth.TokenInvalidError
		if errors.As(err, &invalid) {
			WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", invalid.Error(), traceID, nil)
			return nil, false
		}
		var notFound auth.UserNotFoundError
		if errors.As(err, &notFound) {
			WriteError(w, nethttp.StatusUnauthorized, "auth.user_not_found", "user not found", traceID, nil)
			return nil, false
		}
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return nil, false
	}

	if verified.AccountID == uuid.Nil || strings.TrimSpace(verified.AccountRole) == "" {
		WriteError(w, nethttp.StatusForbidden, "auth.no_account_membership", "user has no account membership", traceID, nil)
		return nil, false
	}

	return &actor{
		AccountID:   verified.AccountID,
		UserID:      verified.UserID,
		AccountRole: verified.AccountRole,
		Permissions: auth.PermissionsForRole(verified.AccountRole),
	}, true
}

func resolveActorFromAPIKey(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	rawKey string,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) (*actor, bool) {
	keyHash := data.HashAPIKey(rawKey)

	apiKey, err := apiKeysRepo.GetByHash(r.Context(), keyHash)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return nil, false
	}
	if apiKey == nil || apiKey.RevokedAt != nil {
		WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_api_key", "invalid or revoked API key", traceID, nil)
		return nil, false
	}

	if membershipRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return nil, false
	}

	membership, err := membershipRepo.GetByOrgAndUser(r.Context(), apiKey.AccountID, apiKey.UserID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return nil, false
	}
	if membership == nil {
		WriteError(w, nethttp.StatusForbidden, "auth.no_account_membership", "user has no account membership", traceID, nil)
		return nil, false
	}

	keyID := apiKey.ID
	accountID := apiKey.AccountID
	userID := apiKey.UserID

	// 异步更新 last_used_at，不阻塞请求
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), apiKeyLastUsedUpdateTimeout)
		defer cancel()

		_ = apiKeysRepo.UpdateLastUsed(ctx, keyID)
	}()

	if auditWriter != nil {
		auditWriter.WriteAPIKeyUsed(r.Context(), traceID, accountID, userID, keyID, "api_key.used")
	}

	normalizedScopes, _ := auth.NormalizePermissions(apiKey.Scopes)
	effectivePermissions := auth.IntersectPermissions(auth.PermissionsForRole(membership.Role), normalizedScopes)

	return &actor{
		AccountID:   membership.AccountID,
		UserID:      apiKey.UserID,
		AccountRole: membership.Role,
		Permissions: effectivePermissions,
	}, true
}
