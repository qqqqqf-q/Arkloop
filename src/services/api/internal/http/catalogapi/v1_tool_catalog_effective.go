package catalogapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
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
	membershipRepo *data.AccountMembershipRepository,
	overridesRepo *data.ToolDescriptionOverridesRepository,
	pool *pgxpool.Pool,
	mcpCache *effectiveToolCatalogCache,
	artifactStoreAvailable bool,
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
		actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService, membershipRepo)
		if !ok {
			return
		}

		projectID := uuid.Nil
		if projectRepo != nil {
			project, err := projectRepo.GetOrCreateDefaultByOwner(r.Context(), actor.AccountID, actor.UserID)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			projectID = project.ID
		}

		catalog, err := buildEffectiveToolCatalog(r.Context(), actor.AccountID, projectID, overridesRepo, pool, mcpCache, artifactStoreAvailable)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, catalog)
	}
}

func buildEffectiveToolCatalog(
	ctx context.Context,
	accountID uuid.UUID,
	projectID uuid.UUID,
	overridesRepo *data.ToolDescriptionOverridesRepository,
	pool *pgxpool.Pool,
	mcpCache *effectiveToolCatalogCache,
	artifactStoreAvailable bool,
) (toolCatalogResponse, error) {
	available := buildEffectiveBuiltinToolNameSet(ctx, pool, artifactStoreAvailable)
	platformByName, projectByName := loadEffectiveToolDescriptionOverrides(ctx, overridesRepo, projectID)
	platformDisabledByName, projectDisabledByName := loadEffectiveToolDisabledOverrides(ctx, overridesRepo, projectID)
	mcpTools := []toolCatalogItem{}
	if mcpCache != nil {
		if envTools, err := mcpCache.GetEnv(ctx); err == nil {
			mcpTools = append(mcpTools, envTools...)
		} else {
			slog.WarnContext(ctx, "effective tool catalog: env mcp discovery failed", "err", err.Error())
		}
		if accountTools, err := mcpCache.GetAccount(ctx, pool, accountID); err == nil {
			mcpTools = append(mcpTools, accountTools...)
		} else {
			slog.WarnContext(ctx, "effective tool catalog: account mcp discovery failed", "account_id", accountID, "err", err.Error())
		}
	}

	groups := make([]toolCatalogGroup, 0, len(sharedtoolmeta.GroupOrder())+1)
	for _, group := range sharedtoolmeta.Catalog() {
		items := make([]toolCatalogItem, 0, len(group.Tools))
		for _, meta := range group.Tools {
			if _, ok := available[meta.Name]; !ok {
				continue
			}
			if platformDisabledByName[meta.Name] || projectDisabledByName[meta.Name] {
				continue
			}
			description := meta.LLMDescription
			hasOverride := false
			source := toolDescriptionSourceDefault
			if override, ok := projectByName[meta.Name]; ok {
				description = override
				hasOverride = true
				source = toolDescriptionSourceProject
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
	projectID uuid.UUID,
) (map[string]string, map[string]string) {
	if overridesRepo == nil {
		return nil, nil
	}
	overrides, err := overridesRepo.List(ctx)
	if err != nil {
		overrides = nil
	}
	return buildToolDescriptionOverrideMap(overrides), nil
}

func loadEffectiveToolDisabledOverrides(
	ctx context.Context,
	overridesRepo *data.ToolDescriptionOverridesRepository,
	projectID uuid.UUID,
) (map[string]bool, map[string]bool) {
	if overridesRepo == nil {
		return nil, nil
	}
	overrides, err := overridesRepo.List(ctx)
	if err != nil {
		overrides = nil
	}
	return buildToolDisabledOverrideMap(overrides), nil
}
