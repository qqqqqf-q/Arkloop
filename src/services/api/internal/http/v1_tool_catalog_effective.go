package http

import (
	"context"
	"os"
	"strings"
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
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	pool *pgxpool.Pool,
	mcpCache *effectiveToolCatalogCache,
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

		catalog, err := buildEffectiveToolCatalog(r.Context(), actor.OrgID, overridesRepo, toolProvidersRepo, pool, mcpCache)
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
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	pool *pgxpool.Pool,
	mcpCache *effectiveToolCatalogCache,
) (toolCatalogResponse, error) {
	available := buildEffectiveToolNameSet(ctx, toolProvidersRepo, pool)
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

func buildEffectiveToolNameSet(
	ctx context.Context,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	pool *pgxpool.Pool,
) map[string]struct{} {
	available := map[string]struct{}{
		"web_search":       {},
		"web_fetch":        {},
		"timeline_title":   {},
		"spawn_agent":      {},
		"summarize_thread": {},
		"echo":             {},
		"noop":             {},
	}
	if pool != nil {
		available["conversation_search"] = struct{}{}
	}
	if strings.TrimSpace(os.Getenv("ARKLOOP_BROWSER_BASE_URL")) != "" {
		for _, name := range []string{"browser_navigate", "browser_interact", "browser_extract", "browser_screenshot", "browser_session_close"} {
			available[name] = struct{}{}
		}
	}
	platformProviders := map[string]data.ToolProviderConfig{}
	if toolProvidersRepo != nil {
		configs, err := toolProvidersRepo.ListByScope(ctx, uuid.Nil, "platform")
		if err == nil {
			for _, cfg := range configs {
				if !cfg.IsActive {
					continue
				}
				platformProviders[cfg.ProviderName] = cfg
			}
		}
	}
	sandboxBaseURL := strings.TrimSpace(os.Getenv("ARKLOOP_SANDBOX_BASE_URL"))
	if sandboxBaseURL == "" {
		if cfg, ok := platformProviders["sandbox.docker"]; ok && cfg.BaseURL != nil && strings.TrimSpace(*cfg.BaseURL) != "" {
			sandboxBaseURL = strings.TrimSpace(*cfg.BaseURL)
		}
		if sandboxBaseURL == "" {
			if cfg, ok := platformProviders["sandbox.firecracker"]; ok && cfg.BaseURL != nil && strings.TrimSpace(*cfg.BaseURL) != "" {
				sandboxBaseURL = strings.TrimSpace(*cfg.BaseURL)
			}
		}
	}
	if sandboxBaseURL != "" {
		for _, name := range []string{"python_execute", "exec_command", "write_stdin"} {
			available[name] = struct{}{}
		}
	}
	memoryBaseURL := strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_BASE_URL"))
	if memoryBaseURL == "" {
		if cfg, ok := platformProviders["memory.openviking"]; ok && cfg.BaseURL != nil && strings.TrimSpace(*cfg.BaseURL) != "" {
			memoryBaseURL = strings.TrimSpace(*cfg.BaseURL)
		}
	}
	if memoryBaseURL != "" {
		for _, name := range []string{"memory_search", "memory_read", "memory_write", "memory_forget"} {
			available[name] = struct{}{}
		}
	}
	if strings.TrimSpace(os.Getenv("ARKLOOP_S3_ENDPOINT")) != "" {
		available["document_write"] = struct{}{}
	}
	return available
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
