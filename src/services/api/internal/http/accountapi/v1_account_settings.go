package accountapi

import (
	"encoding/json"
	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"
)

const pipelineTraceEnabledSettingKey = "pipeline_trace_enabled"

type accountSettingsResponse struct {
	PipelineTraceEnabled bool `json:"pipeline_trace_enabled"`
}

type patchAccountSettingsRequest struct {
	PipelineTraceEnabled *bool `json:"pipeline_trace_enabled"`
}

func accountSettingsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	accountRepo *data.AccountRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if accountRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			account, err := accountRepo.GetByID(r.Context(), actor.AccountID)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if account == nil {
				httpkit.WriteNotFound(w, r)
				return
			}
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, accountSettingsResponse{
				PipelineTraceEnabled: pipelineTraceEnabledFromJSON(account.SettingsJSON),
			})
		case nethttp.MethodPatch:
			var body patchAccountSettingsRequest
			if err := httpkit.DecodeJSON(r, &body); err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid request body", traceID, nil)
				return
			}
			if body.PipelineTraceEnabled == nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "pipeline_trace_enabled is required", traceID, nil)
				return
			}
			if err := accountRepo.UpdateSettings(r.Context(), actor.AccountID, pipelineTraceEnabledSettingKey, *body.PipelineTraceEnabled); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, accountSettingsResponse{
				PipelineTraceEnabled: *body.PipelineTraceEnabled,
			})
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func pipelineTraceEnabledFromJSON(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	enabled, _ := payload[pipelineTraceEnabledSettingKey].(bool)
	return enabled
}
