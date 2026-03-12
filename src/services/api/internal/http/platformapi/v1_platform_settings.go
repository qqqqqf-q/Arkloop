package platformapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	sharedconfig "arkloop/services/shared/config"
	sharedent "arkloop/services/shared/entitlement"

	"github.com/redis/go-redis/v9"
)

const maskedSensitiveValue = "******"

type platformSettingResponse struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
}

type updatePlatformSettingRequest struct {
	Value string `json:"value"`
}

func maskIfSensitive(key, value string, registry *sharedconfig.Registry) string {
	if registry == nil {
		registry = sharedconfig.DefaultRegistry()
	}
	entry, ok := registry.Get(key)
	if !ok || !entry.Sensitive {
		return value
	}
	if strings.TrimSpace(value) == "" {
		return value
	}
	return maskedSensitiveValue
}

func platformSettingsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	settingsRepo *data.PlatformSettingsRepository,
	apiKeysRepo *data.APIKeysRepository,
	registry *sharedconfig.Registry,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		items, err := settingsRepo.List(r.Context())
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		result := make([]platformSettingResponse, 0, len(items))
		for _, s := range items {
			result = append(result, platformSettingResponse{
				Key:       s.Key,
				Value:     maskIfSensitive(s.Key, s.Value, registry),
				UpdatedAt: s.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, result)
	}
}

func platformSettingEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	settingsRepo *data.PlatformSettingsRepository,
	apiKeysRepo *data.APIKeysRepository,
	rdb *redis.Client,
	invalidator sharedconfig.Invalidator,
	registry *sharedconfig.Registry,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		key := strings.TrimPrefix(r.URL.Path, "/v1/admin/platform-settings/")
		if key == "" {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "key is required", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			setting, err := settingsRepo.Get(r.Context(), key)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if setting == nil {
				httpkit.WriteError(w, nethttp.StatusNotFound, "platform_settings.not_found", "setting not found", traceID, nil)
				return
			}
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, platformSettingResponse{
				Key:       setting.Key,
				Value:     maskIfSensitive(setting.Key, setting.Value, registry),
				UpdatedAt: setting.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})

		case nethttp.MethodPut:
			var body updatePlatformSettingRequest
			if err := httpkit.DecodeJSON(r, &body); err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
			body.Value = strings.TrimSpace(body.Value)
			if body.Value == "" {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "value must not be empty", traceID, nil)
				return
			}

			setting, err := settingsRepo.Set(r.Context(), key, body.Value)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if invalidator != nil {
				_ = invalidator.Invalidate(r.Context(), key, sharedconfig.Scope{})
			}
			if shouldInvalidateEntitlementCache(key) {
				invalidateEntitlementCacheByKey(r.Context(), rdb, key)
			}
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, platformSettingResponse{
				Key:       setting.Key,
				Value:     maskIfSensitive(setting.Key, setting.Value, registry),
				UpdatedAt: setting.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})

		case nethttp.MethodDelete:
			if err := settingsRepo.Delete(r.Context(), key); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if invalidator != nil {
				_ = invalidator.Invalidate(r.Context(), key, sharedconfig.Scope{})
			}
			if shouldInvalidateEntitlementCache(key) {
				invalidateEntitlementCacheByKey(r.Context(), rdb, key)
			}
			w.WriteHeader(nethttp.StatusNoContent)

		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func shouldInvalidateEntitlementCache(key string) bool {
	switch {
	case strings.HasPrefix(key, "quota."):
		return true
	case strings.HasPrefix(key, "limit."):
		return true
	case strings.HasPrefix(key, "feature."):
		return true
	case strings.HasPrefix(key, "invite."):
		return true
	case strings.HasPrefix(key, "credit."):
		return true
	default:
		return false
	}
}

func invalidateEntitlementCacheByKey(ctx context.Context, rdb *redis.Client, key string) {
	if rdb == nil {
		return
	}
	if !sharedent.EntitlementCacheSigningEnabled() {
		return
	}
	pattern := "arkloop:entitlement:*:" + key
	var cursor uint64
	for {
		keys, next, err := rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			toDelete := make([]string, 0, len(keys)*2)
			for _, k := range keys {
				toDelete = append(toDelete, k, k+sharedent.EntitlementCacheSignatureSuffix)
			}
			_ = rdb.Del(ctx, toDelete...).Err()
		}
		cursor = next
		if cursor == 0 {
			return
		}
	}
}
