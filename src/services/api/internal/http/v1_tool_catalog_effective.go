package http

import (
	"context"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	sharedtoolmeta "arkloop/services/shared/toolmeta"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	nethttp "net/http"
)

const effectiveToolCatalogTTL = 30 * time.Second

func toolCatalogEffectiveEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	overridesRepo *data.ToolDescriptionOverridesRepository,
	pool *pgxpool.Pool,
	mcpCache *effectiveToolCatalogCache,
	artifactStoreAvailable bool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}
		actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		catalog, err := buildEffectiveToolCatalog(r.Context(), actor.OrgID, overridesRepo, pool, mcpCache, artifactStoreAvailable)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		writeJSON(w, traceID, nethttp.StatusOK, catalog)
	}
}

func buildEffectiveToolCatalog(
	ctx context.Context,
	orgID uuid.UUID,
	overridesRepo *data.ToolDescriptionOverridesRepository,
	pool *pgxpool.Pool,
	mcpCache *effectiveToolCatalogCache,
	artifactStoreAvailable bool,
) (toolCatalogResponse, error) {
	available := buildEffectiveBuiltinToolNameSet(ctx, pool, artifactStoreAvailable)
	platformByName, orgByName := loadEffectiveToolDescriptionOverrides(ctx, overridesRepo, orgID)
	mcpTools := []toolCatalogItem{}
	if mcpCache != nil {
		if envTools, err := mcpCache.GetEnv(ctx); err == nil {
			mcpTools = append(mcpTools, envTools...)
		} else {
			slog.WarnContext(ctx, "effective tool catalog: env mcp discovery failed", "err", err.Error())
		}
		if orgTools, err := mcpCache.GetOrg(ctx, pool, orgID); err == nil {
			mcpTools = append(mcpTools, orgTools...)
		} else {
			slog.WarnContext(ctx, "effective tool catalog: org mcp discovery failed", "org_id", orgID, "err", err.Error())
		}
	}

	groups := make([]toolCatalogGroup, 0, len(sharedtoolmeta.GroupOrder())+1)
	for _, group := range sharedtoolmeta.Catalog() {
		items := make([]toolCatalogItem, 0, len(group.Tools))
		for _, meta := range group.Tools {
			if _, ok := available[meta.Name]; !ok {
				continue
			}
			description := meta.LLMDescription
			hasOverride := false
			source := toolDescriptionSourceDefault
			if override, ok := orgByName[meta.Name]; ok {
				description = override
				hasOverride = true
				source = toolDescriptionSourceOrg
			} else if override, ok := platformByName[meta.Name]; ok {
				description = override
				source = toolDescriptionSourcePlatform
			}
			items = append(items, toolCatalogItem{
				Name:              meta.Name,
				Label:             meta.Label,
				LLMDescription:    description,
				HasOverride:       hasOverride,
				DescriptionSource: source,
			})
		}
		if len(items) == 0 {
			continue
		}
		groups = append(groups, toolCatalogGroup{Group: group.Name, Tools: items})
	}
	if len(mcpTools) > 0 {
		groups = append(groups, toolCatalogGroup{Group: effectiveToolCatalogMCPGroup, Tools: mcpTools})
	}
	return toolCatalogResponse{Groups: groups}, nil
}

func loadEffectiveToolDescriptionOverrides(
	ctx context.Context,
	overridesRepo *data.ToolDescriptionOverridesRepository,
	orgID uuid.UUID,
) (map[string]string, map[string]string) {
	if overridesRepo == nil {
		return nil, nil
	}
	platformOverrides, err := overridesRepo.ListByScope(ctx, uuid.Nil, "platform")
	if err != nil {
		platformOverrides = nil
	}
	orgOverrides, err := overridesRepo.ListByScope(ctx, orgID, "org")
	if err != nil {
		orgOverrides = nil
	}
	return buildToolDescriptionOverrideMap(platformOverrides), buildToolDescriptionOverrideMap(orgOverrides)
}
