package http

import (
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

type platformSettingResponse struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
}

type updatePlatformSettingRequest struct {
	Value string `json:"value"`
}

func platformSettingsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	settingsRepo *data.PlatformSettingsRepository,
	apiKeysRepo *data.APIKeysRepository,
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

		items, err := settingsRepo.List(r.Context())
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		result := make([]platformSettingResponse, 0, len(items))
		for _, s := range items {
			result = append(result, platformSettingResponse{
				Key:       s.Key,
				Value:     s.Value,
				UpdatedAt: s.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		writeJSON(w, traceID, nethttp.StatusOK, result)
	}
}

func platformSettingEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	settingsRepo *data.PlatformSettingsRepository,
	apiKeysRepo *data.APIKeysRepository,
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

		key := strings.TrimPrefix(r.URL.Path, "/v1/admin/platform-settings/")
		if key == "" {
			WriteError(w, nethttp.StatusBadRequest, "validation.error", "key is required", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			setting, err := settingsRepo.Get(r.Context(), key)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if setting == nil {
				WriteError(w, nethttp.StatusNotFound, "platform_settings.not_found", "setting not found", traceID, nil)
				return
			}
			writeJSON(w, traceID, nethttp.StatusOK, platformSettingResponse{
				Key:       setting.Key,
				Value:     setting.Value,
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

			setting, err := settingsRepo.Set(r.Context(), key, body.Value)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			writeJSON(w, traceID, nethttp.StatusOK, platformSettingResponse{
				Key:       setting.Key,
				Value:     setting.Value,
				UpdatedAt: setting.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})

		default:
			writeMethodNotAllowed(w, r)
		}
	}
}
