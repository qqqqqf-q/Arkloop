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
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type actor struct {
	OrgID       uuid.UUID
	UserID      uuid.UUID
	OrgRole     string
	Permissions []string
}

const apiKeyLastUsedUpdateTimeout = 2 * time.Second

func (a *actor) HasPermission(perm string) bool {
	for _, p := range a.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

func authenticateActor(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
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

	if verified.OrgID == uuid.Nil || strings.TrimSpace(verified.OrgRole) == "" {
		WriteError(w, nethttp.StatusForbidden, "auth.no_org_membership", "user has no org membership", traceID, nil)
		return nil, false
	}

	// v1：权限通过 PermissionsForRole 静态映射，无额外 DB 查询。
	// verified.OrgRole 为后续自定义角色动态加载预留，届时改为查询 rbac_roles 表。
	return &actor{
		OrgID:       verified.OrgID,
		UserID:      verified.UserID,
		OrgRole:     verified.OrgRole,
		Permissions: auth.PermissionsForRole(verified.OrgRole),
	}, true
}

// resolveActor 支持 JWT 和 API Key 双路径鉴权。
// apiKeysRepo 为 nil 时退化为 JWT only。
func resolveActor(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
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

	if verified.OrgID == uuid.Nil || strings.TrimSpace(verified.OrgRole) == "" {
		WriteError(w, nethttp.StatusForbidden, "auth.no_org_membership", "user has no org membership", traceID, nil)
		return nil, false
	}

	return &actor{
		OrgID:       verified.OrgID,
		UserID:      verified.UserID,
		OrgRole:     verified.OrgRole,
		Permissions: auth.PermissionsForRole(verified.OrgRole),
	}, true
}

func resolveActorFromAPIKey(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	rawKey string,
	membershipRepo *data.OrgMembershipRepository,
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

	membership, err := membershipRepo.GetDefaultForUser(r.Context(), apiKey.UserID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return nil, false
	}
	if membership == nil {
		WriteError(w, nethttp.StatusForbidden, "auth.no_org_membership", "user has no org membership", traceID, nil)
		return nil, false
	}

	// 确保 key 所属 org 和 membership org 一致（防止跨租户）
	if membership.OrgID != apiKey.OrgID {
		WriteError(w, nethttp.StatusForbidden, "auth.org_mismatch", "API key org does not match membership", traceID, nil)
		return nil, false
	}

	keyID := apiKey.ID
	orgID := apiKey.OrgID
	userID := apiKey.UserID

	// 异步更新 last_used_at，不阻塞请求
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), apiKeyLastUsedUpdateTimeout)
		defer cancel()

		_ = apiKeysRepo.UpdateLastUsed(ctx, keyID)
	}()

	if auditWriter != nil {
		auditWriter.WriteAPIKeyUsed(r.Context(), traceID, orgID, userID, keyID, "api_key.used")
	}

	// v1：同 authenticateActor，静态映射；RoleID 为自定义角色预留。
	return &actor{
		OrgID:       membership.OrgID,
		UserID:      apiKey.UserID,
		OrgRole:     membership.Role,
		Permissions: auth.PermissionsForRole(membership.Role),
	}, true
}

func writeNotFound(w nethttp.ResponseWriter, r *nethttp.Request) {
	traceID := observability.TraceIDFromContext(r.Context())
	WriteError(w, nethttp.StatusNotFound, "http.method_not_allowed", "Not Found", traceID, nil)
}
