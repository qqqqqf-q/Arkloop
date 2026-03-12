package accountapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const apiKeysCacheTTL = 5 * time.Minute

type createAPIKeyRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

type apiKeyResponse struct {
	ID         string   `json:"id"`
	AccountID      string   `json:"account_id"`
	UserID     string   `json:"user_id"`
	Name       string   `json:"name"`
	KeyPrefix  string   `json:"key_prefix"`
	Scopes     []string `json:"scopes"`
	RevokedAt  *string  `json:"revoked_at,omitempty"`
	LastUsedAt *string  `json:"last_used_at,omitempty"`
	CreatedAt  string   `json:"created_at"`
}

type createAPIKeyResponse struct {
	apiKeyResponse
	Key string `json:"key"`
}

type apiKeyCacheEntry struct {
	AccountID   string `json:"account_id"`
	UserID  string `json:"user_id"`
	Revoked bool   `json:"revoked"`
}

func apiKeysEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	redisClient *redis.Client,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodPost:
			createAPIKey(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter, redisClient)
		case nethttp.MethodGet:
			listAPIKeys(w, r, traceID, authService, membershipRepo, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func apiKeyEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	redisClient *redis.Client,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/api-keys/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		keyID, err := uuid.Parse(tail)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid api key id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodDelete:
			revokeAPIKey(w, r, traceID, keyID, authService, membershipRepo, apiKeysRepo, auditWriter, redisClient)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func createAPIKey(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	redisClient *redis.Client,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if apiKeysRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataAPIKeysManage, w, traceID) {
		return
	}

	var req createAPIKeyRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name is required", traceID, nil)
		return
	}
	if len(req.Name) > 200 {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name too long", traceID, nil)
		return
	}
	if req.Scopes == nil {
		req.Scopes = []string{}
	}
	normalizedScopes, invalidScopes := auth.NormalizePermissions(req.Scopes)
	if len(invalidScopes) > 0 {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid scopes", traceID, map[string]any{"scopes": invalidScopes})
		return
	}
	rolePermissions := auth.PermissionsForRole(actor.AccountRole)
	if !auth.IsPermissionSubset(normalizedScopes, rolePermissions) {
		httpkit.WriteError(w, nethttp.StatusForbidden, "api_keys.scope_forbidden", "access denied", traceID, nil)
		return
	}

	apiKey, rawKey, err := apiKeysRepo.Create(r.Context(), actor.AccountID, actor.UserID, req.Name, normalizedScopes)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	syncAPIKeyCache(r.Context(), redisClient, apiKey, data.HashAPIKey(rawKey))

	if auditWriter != nil {
		auditWriter.WriteAPIKeyCreated(r.Context(), traceID, actor.AccountID, actor.UserID, apiKey.ID, apiKey.Name)
	}

	resp := createAPIKeyResponse{
		apiKeyResponse: toAPIKeyResponse(apiKey),
		Key:            rawKey,
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, resp)
}

func listAPIKeys(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if apiKeysRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataAPIKeysManage, w, traceID) {
		return
	}

	keys, err := listVisibleAPIKeys(r.Context(), actor, apiKeysRepo)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]apiKeyResponse, 0, len(keys))
	for _, k := range keys {
		resp = append(resp, toAPIKeyResponse(k))
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func revokeAPIKey(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	keyID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	redisClient *redis.Client,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if apiKeysRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataAPIKeysManage, w, traceID) {
		return
	}

	keyHash, err := revokeVisibleAPIKey(r.Context(), actor, apiKeysRepo, keyID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if keyHash == "" {
		httpkit.WriteError(w, nethttp.StatusNotFound, "api_keys.not_found", "api key not found", traceID, nil)
		return
	}

	invalidateAPIKeyCache(r.Context(), redisClient, keyHash)

	if auditWriter != nil {
		auditWriter.WriteAPIKeyRevoked(r.Context(), traceID, actor.AccountID, actor.UserID, keyID)
	}

	w.WriteHeader(nethttp.StatusNoContent)
}

// syncAPIKeyCache 将 API Key 元数据写入 Redis，供 Gateway 限流和 IP 过滤提取 account_id。
func syncAPIKeyCache(ctx context.Context, client *redis.Client, apiKey data.APIKey, keyHash string) {
	if client == nil {
		return
	}

	entry := apiKeyCacheEntry{
		AccountID:   apiKey.AccountID.String(),
		UserID:  apiKey.UserID.String(),
		Revoked: false,
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	key := fmt.Sprintf("arkloop:api_keys:%s", keyHash)
	_ = client.Set(ctx, key, raw, apiKeysCacheTTL).Err()
}

// invalidateAPIKeyCache 吊销时删除 Redis 缓存，使 Gateway 立即感知。
func invalidateAPIKeyCache(ctx context.Context, client *redis.Client, keyHash string) {
	if client == nil {
		return
	}
	key := fmt.Sprintf("arkloop:api_keys:%s", keyHash)
	_ = client.Del(ctx, key).Err()
}

func toAPIKeyResponse(k data.APIKey) apiKeyResponse {
	resp := apiKeyResponse{
		ID:        k.ID.String(),
		AccountID:     k.AccountID.String(),
		UserID:    k.UserID.String(),
		Name:      k.Name,
		KeyPrefix: k.KeyPrefix,
		Scopes:    normalizeAPIKeyScopes(k.Scopes),
		CreatedAt: k.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if k.RevokedAt != nil {
		s := k.RevokedAt.UTC().Format("2006-01-02T15:04:05Z")
		resp.RevokedAt = &s
	}
	if k.LastUsedAt != nil {
		s := k.LastUsedAt.UTC().Format("2006-01-02T15:04:05Z")
		resp.LastUsedAt = &s
	}
	return resp
}

func listVisibleAPIKeys(ctx context.Context, actor *httpkit.Actor, repo *data.APIKeysRepository) ([]data.APIKey, error) {
	if isAccountAPIKeyAdmin(actor) {
		return repo.ListByOrg(ctx, actor.AccountID)
	}
	return repo.ListByOrgAndUser(ctx, actor.AccountID, actor.UserID)
}

func revokeVisibleAPIKey(ctx context.Context, actor *httpkit.Actor, repo *data.APIKeysRepository, keyID uuid.UUID) (string, error) {
	if isAccountAPIKeyAdmin(actor) {
		return repo.Revoke(ctx, actor.AccountID, keyID)
	}
	return repo.RevokeOwned(ctx, actor.AccountID, actor.UserID, keyID)
}

func isAccountAPIKeyAdmin(actor *httpkit.Actor) bool {
	if actor == nil {
		return false
	}
	if actor.HasPermission(auth.PermPlatformAdmin) {
		return true
	}
	return actor.AccountRole == auth.RoleAccountAdmin || actor.AccountRole == "owner"
}

func normalizeAPIKeyScopes(scopes []string) []string {
	normalized, _ := auth.NormalizePermissions(scopes)
	return normalized
}
