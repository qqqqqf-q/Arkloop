package http

import (
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	sharedconfig "arkloop/services/shared/config"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type orgSettingResponse struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
}

// orgSettingsListEntry handles GET /v1/orgs/{orgID}/settings
func orgSettingsListEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	orgSettingsRepo *data.OrgSettingsRepository,
	apiKeysRepo *data.APIKeysRepository,
	registry *sharedconfig.Registry,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		orgID := r.PathValue("orgID")
		if orgID == "" {
			WriteError(w, nethttp.StatusBadRequest, "validation.error", "org_id is required", traceID, nil)
			return
		}

		items, err := orgSettingsRepo.List(r.Context(), orgID)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		result := make([]orgSettingResponse, 0, len(items))
		for _, s := range items {
			result = append(result, orgSettingResponse{
				Key:       s.Key,
				Value:     maskIfSensitive(s.Key, s.Value, registry),
				UpdatedAt: s.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		writeJSON(w, traceID, nethttp.StatusOK, result)
	}
}

// orgSettingEntry handles GET/PUT/DELETE /v1/orgs/{orgID}/settings/{key...}
func orgSettingEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	orgSettingsRepo *data.OrgSettingsRepository,
	apiKeysRepo *data.APIKeysRepository,
	rdb *redis.Client,
	invalidator sharedconfig.Invalidator,
	registry *sharedconfig.Registry,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}

		orgID := r.PathValue("orgID")
		key := r.PathValue("key")
		if orgID == "" || key == "" {
			WriteError(w, nethttp.StatusBadRequest, "validation.error", "org_id and key are required", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			setting, err := orgSettingsRepo.Get(r.Context(), orgID, key)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if setting == nil {
				WriteError(w, nethttp.StatusNotFound, "org_settings.not_found", "setting not found", traceID, nil)
				return
			}
			writeJSON(w, traceID, nethttp.StatusOK, orgSettingResponse{
				Key:       setting.Key,
				Value:     maskIfSensitive(setting.Key, setting.Value, registry),
				UpdatedAt: setting.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})

		case nethttp.MethodPut:
			var body updatePlatformSettingRequest
			if err := decodeJSON(r, &body); err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
			body.Value = strings.TrimSpace(body.Value)
			if body.Value == "" {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "value must not be empty", traceID, nil)
				return
			}

			setting, err := orgSettingsRepo.Set(r.Context(), orgID, key, body.Value)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if invalidator != nil {
				uid, _ := uuid.Parse(orgID)
				_ = invalidator.Invalidate(r.Context(), key, sharedconfig.Scope{OrgID: &uid})
			}
			if shouldInvalidateEntitlementCache(key) {
				invalidateEntitlementCacheByKey(r.Context(), rdb, key)
			}
			writeJSON(w, traceID, nethttp.StatusOK, orgSettingResponse{
				Key:       setting.Key,
				Value:     maskIfSensitive(setting.Key, setting.Value, registry),
				UpdatedAt: setting.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})

		case nethttp.MethodDelete:
			if err := orgSettingsRepo.Delete(r.Context(), orgID, key); err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if invalidator != nil {
				uid, _ := uuid.Parse(orgID)
				_ = invalidator.Invalidate(r.Context(), key, sharedconfig.Scope{OrgID: &uid})
			}
			if shouldInvalidateEntitlementCache(key) {
				invalidateEntitlementCacheByKey(r.Context(), rdb, key)
			}
			w.WriteHeader(nethttp.StatusNoContent)

		default:
			writeMethodNotAllowed(w, r)
		}
	}
}
