package catalogapi

import (
	"context"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"strings"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	sharedtoolmeta "arkloop/services/shared/toolmeta"

	nethttp "net/http"

	"github.com/google/uuid"
)

type toolDescriptionSource string

const (
	toolDescriptionSourceDefault  toolDescriptionSource = "default"
	toolDescriptionSourcePlatform toolDescriptionSource = "platform"
	toolDescriptionSourceProject toolDescriptionSource = "project"
)

type toolCatalogItem struct {
	Name              string                `json:"name"`
	Label             string                `json:"label"`
	LLMDescription    string                `json:"llm_description"`
	HasOverride       bool                  `json:"has_override"`
	DescriptionSource toolDescriptionSource `json:"description_source"`
	IsDisabled        bool                  `json:"is_disabled"`
}

type toolCatalogGroup struct {
	Group string            `json:"group"`
	Tools []toolCatalogItem `json:"tools"`
}

type toolCatalogResponse struct {
	Groups []toolCatalogGroup `json:"groups"`
}

func buildToolCatalog(
	scope string,
	platformOverrides []data.ToolDescriptionOverride,
	projectOverrides []data.ToolDescriptionOverride,
) toolCatalogResponse {
	platformByName := buildToolDescriptionOverrideMap(platformOverrides)
	projectByName := buildToolDescriptionOverrideMap(projectOverrides)
	platformDisabledByName := buildToolDisabledOverrideMap(platformOverrides)
	projectDisabledByName := buildToolDisabledOverrideMap(projectOverrides)

	groups := make([]toolCatalogGroup, 0, len(sharedtoolmeta.GroupOrder()))
	for _, group := range sharedtoolmeta.Catalog() {
		items := make([]toolCatalogItem, 0, len(group.Tools))
		for _, meta := range group.Tools {
			description := meta.LLMDescription
			hasOverride := false
			source := toolDescriptionSourceDefault

			if scope == "user" {
				if override, ok := projectByName[meta.Name]; ok {
					description = override
					hasOverride = true
					source = toolDescriptionSourceProject
				} else if override, ok := platformByName[meta.Name]; ok {
					description = override
					source = toolDescriptionSourcePlatform
				}
			} else if override, ok := platformByName[meta.Name]; ok {
				description = override
				hasOverride = true
				source = toolDescriptionSourcePlatform
			}

			items = append(items, toolCatalogItem{
				Name:              meta.Name,
				Label:             meta.Label,
				LLMDescription:    description,
				HasOverride:       hasOverride,
				DescriptionSource: source,
				IsDisabled:        platformDisabledByName[meta.Name] || projectDisabledByName[meta.Name],
			})
		}
		groups = append(groups, toolCatalogGroup{Group: group.Name, Tools: items})
	}
	return toolCatalogResponse{Groups: groups}
}

func toolCatalogEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	overridesRepo *data.ToolDescriptionOverridesRepository,
	projectRepo *data.ProjectRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
		if !ok {
			return
		}

		scope, _, ok := resolveToolCatalogScope(r.Context(), w, r, traceID, actor, projectRepo)
		if !ok {
			return
		}

		var platformOverrides []data.ToolDescriptionOverride
		var projectOverrides []data.ToolDescriptionOverride
		if overridesRepo != nil {
			var err error
			platformOverrides, err = overridesRepo.List(r.Context())
			if err != nil {
				platformOverrides = nil
			}
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, buildToolCatalog(scope, platformOverrides, projectOverrides))
	}
}

type updateToolDescriptionRequest struct {
	Description string `json:"description"`
}

type updateToolDisabledRequest struct {
	Disabled bool `json:"disabled"`
}

func toolCatalogItemEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	overridesRepo *data.ToolDescriptionOverridesRepository,
	projectRepo *data.ProjectRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/tool-catalog/")
		parts := strings.SplitN(strings.Trim(tail, "/"), "/", 2)
		if len(parts) < 1 || parts[0] == "" {
			httpkit.WriteNotFound(w, r)
			return
		}
		toolName := parts[0]
		action := ""
		if len(parts) == 2 {
			action = parts[1]
		}
		if _, ok := sharedtoolmeta.Lookup(toolName); !ok {
			httpkit.WriteNotFound(w, r)
			return
		}
		if action != "description" && action != "disabled" {
			httpkit.WriteNotFound(w, r)
			return
		}

		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
		if !ok {
			return
		}

		_, _, ok = resolveToolCatalogScope(r.Context(), w, r, traceID, actor, projectRepo)
		if !ok {
			return
		}

		if overridesRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodPut:
			if action == "description" {
				var req updateToolDescriptionRequest
				if err := httpkit.DecodeJSON(r, &req); err != nil {
					httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid request body", traceID, nil)
					return
				}
				if strings.TrimSpace(req.Description) == "" {
					httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "description must not be empty", traceID, nil)
					return
				}
				if err := overridesRepo.Upsert(r.Context(), toolName, req.Description); err != nil {
					httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
					return
				}
				w.WriteHeader(nethttp.StatusNoContent)
				return
			}

			var req updateToolDisabledRequest
			if err := httpkit.DecodeJSON(r, &req); err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid request body", traceID, nil)
				return
			}
			if err := overridesRepo.SetDisabled(r.Context(), toolName, req.Disabled); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			w.WriteHeader(nethttp.StatusNoContent)

		case nethttp.MethodDelete:
			if action != "description" {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			if err := overridesRepo.Delete(r.Context(), toolName); err != nil {
				httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", "no override found", traceID, nil)
				return
			}
			w.WriteHeader(nethttp.StatusNoContent)

		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func resolveToolCatalogScope(
	ctx context.Context,
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *httpkit.Actor,
	projectRepo *data.ProjectRepository,
) (string, uuid.UUID, bool) {
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "platform"
	}
	if scope != "user" && scope != "project" && scope != "platform" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "scope must be user or platform", traceID, nil)
		return "", uuid.Nil, false
	}
	if scope == "project" {
		scope = "user"
	}

	if scope == "platform" {
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return "", uuid.Nil, false
		}
		return scope, uuid.Nil, true
	}

	if !httpkit.RequirePerm(actor, auth.PermDataSecrets, w, traceID) {
		return "", uuid.Nil, false
	}
	if projectRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return "", uuid.Nil, false
	}
	project, err := projectRepo.GetOrCreateDefaultByOwner(ctx, actor.AccountID, actor.UserID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return "", uuid.Nil, false
	}
	return scope, project.ID, true
}

func buildToolDescriptionOverrideMap(overrides []data.ToolDescriptionOverride) map[string]string {
	out := make(map[string]string, len(overrides))
	for _, override := range overrides {
		if strings.TrimSpace(override.Description) == "" {
			continue
		}
		out[override.ToolName] = override.Description
	}
	return out
}

func buildToolDisabledOverrideMap(overrides []data.ToolDescriptionOverride) map[string]bool {
	out := make(map[string]bool, len(overrides))
	for _, override := range overrides {
		if override.IsDisabled {
			out[override.ToolName] = true
		}
	}
	return out
}
