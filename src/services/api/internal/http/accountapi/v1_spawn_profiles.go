package accountapi

import (
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"
	sharedconfig "arkloop/services/shared/config"

	"github.com/google/uuid"
)

var allowedProfiles = map[string]struct{}{
	"explore": {},
	"task":    {},
	"strong":  {},
	"tool":    {},
}

type spawnProfileResponse struct {
	Profile       string `json:"profile"`
	ResolvedModel string `json:"resolved_model"`
	HasOverride   bool   `json:"has_override"`
	IsAuto        bool   `json:"is_auto,omitempty"`
	AutoModel     string `json:"auto_model,omitempty"`
}

type setSpawnProfileRequest struct {
	Model string `json:"model"`
}

func spawnProfilesEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	entitlementService *entitlement.Service,
	apiKeysRepo *data.APIKeysRepository,
	configResolver sharedconfig.Resolver,
	routesRepo *data.LlmRoutesRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodGet:
			listSpawnProfiles(w, r, authService, membershipRepo, entitlementsRepo, apiKeysRepo, configResolver, routesRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func spawnProfileEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	entitlementService *entitlement.Service,
	apiKeysRepo *data.APIKeysRepository,
	configResolver sharedconfig.Resolver,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		name := strings.TrimPrefix(r.URL.Path, "/v1/accounts/me/spawn-profiles/")
		name = strings.Trim(name, "/")
		if name == "" {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "profile name is required", traceID, nil)
			return
		}
		if _, ok := allowedProfiles[name]; !ok {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "validation.error", "profile must be one of: explore, task, strong, tool", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodPut:
			setSpawnProfile(w, r, traceID, name, authService, membershipRepo, entitlementsRepo, entitlementService, apiKeysRepo)
		case nethttp.MethodDelete:
			deleteSpawnProfile(w, r, traceID, name, authService, membershipRepo, entitlementsRepo, entitlementService, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func listSpawnProfiles(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	apiKeysRepo *data.APIKeysRepository,
	configResolver sharedconfig.Resolver,
	routesRepo *data.LlmRoutesRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}

	profiles := []string{"explore", "task", "strong", "tool"}
	result := make([]spawnProfileResponse, 0, len(profiles))

	for _, name := range profiles {
		key := "spawn.profile." + name
		hasOverride := false
		resolvedModel := ""

		if entitlementsRepo != nil {
			override, err := entitlementsRepo.GetOverride(r.Context(), actor.AccountID, key)
			if err == nil && override != nil {
				hasOverride = true
				resolvedModel = override.Value
			}
		}
		if resolvedModel == "" && configResolver != nil {
			val, err := configResolver.Resolve(r.Context(), key, sharedconfig.Scope{})
			if err == nil {
				resolvedModel = val
			}
		}

		entry := spawnProfileResponse{
			Profile:       name,
			ResolvedModel: resolvedModel,
			HasOverride:   hasOverride,
		}

		if name == "tool" && routesRepo != nil {
			if selector, err := routesRepo.GetDefaultSelector(r.Context(), actor.AccountID, data.LlmRouteScopeUser); err == nil && selector != "" {
				entry.AutoModel = selector
				if !hasOverride && resolvedModel == "" {
					entry.ResolvedModel = selector
					entry.IsAuto = true
				}
			}
		}

		result = append(result, entry)
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, result)
}

func setSpawnProfile(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	name string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	entitlementService *entitlement.Service,
	apiKeysRepo *data.APIKeysRepository,
) {
	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if entitlementsRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	var body setSpawnProfileRequest
	if err := httpkit.DecodeJSON(r, &body); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid request body", traceID, nil)
		return
	}
	body.Model = strings.TrimSpace(body.Model)
	if body.Model == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "model must not be empty", traceID, nil)
		return
	}
	if _, err := sharedconfig.ParseProfileValue(body.Model); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "model must be in provider^model format", traceID, nil)
		return
	}

	key := "spawn.profile." + name
	_, err := entitlementsRepo.CreateOverride(
		r.Context(), actor.AccountID, key, body.Model, "string",
		nil, nil, actor.UserID,
	)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if entitlementService != nil {
		entitlementService.InvalidateCache(r.Context(), actor.AccountID, key)
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func deleteSpawnProfile(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	name string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	entitlementsRepo *data.EntitlementsRepository,
	entitlementService *entitlement.Service,
	apiKeysRepo *data.APIKeysRepository,
) {
	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if entitlementsRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	key := "spawn.profile." + name
	existing, err := entitlementsRepo.GetOverrideByAccountAndKey(r.Context(), actor.AccountID, key)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if existing != nil {
		if err := entitlementsRepo.DeleteOverride(r.Context(), existing.ID, actor.AccountID); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if entitlementService != nil {
			entitlementService.InvalidateCache(r.Context(), actor.AccountID, key)
		}
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func resolveAccountID(actor *httpkit.Actor, r *nethttp.Request) uuid.UUID {
	if q := strings.TrimSpace(r.URL.Query().Get("account_id")); q != "" {
		if id, err := uuid.Parse(q); err == nil {
			return id
		}
	}
	return actor.AccountID
}
