package pipeline

import (
	"context"
	"log/slog"
	"strings"

	"arkloop/services/worker/internal/toolprovider"
	"arkloop/services/worker/internal/tools"
	webfetch "arkloop/services/worker/internal/tools/builtin/web_fetch"
	websearch "arkloop/services/worker/internal/tools/builtin/web_search"
)

type notConfiguredExecutor struct {
	groupName    string
	providerName string
	reason       string
	missing      []string
}

func (e notConfiguredExecutor) Execute(
	_ context.Context,
	_ string,
	_ map[string]any,
	_ tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	details := map[string]any{
		"group_name":    e.groupName,
		"provider_name": e.providerName,
	}
	if len(e.missing) > 0 {
		details["missing"] = append([]string{}, e.missing...)
	}
	if strings.TrimSpace(e.reason) != "" {
		details["reason"] = e.reason
	}

	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: "tool.not_configured",
			Message:    "tool provider not configured",
			Details:    details,
		},
	}
}

func NewToolProviderMiddleware(cache *toolprovider.Cache) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if cache == nil || rc == nil || rc.Pool == nil {
			return next(ctx, rc)
		}

		platformProviders, err := cache.GetPlatform(ctx, rc.Pool)
		if err != nil {
			slog.WarnContext(ctx, "tool provider: load platform failed, skipping", "err", err.Error())
			platformProviders = nil
		}

		var projectProviders []toolprovider.ActiveProviderConfig
		if rc.Run.ProjectID != nil {
			projectProviders, err = cache.GetProject(ctx, rc.Pool, *rc.Run.ProjectID)
			if err != nil {
				slog.WarnContext(ctx, "tool provider: load project failed, skipping", "project_id", *rc.Run.ProjectID, "err", err.Error())
				projectProviders = nil
			}
		}

		if len(platformProviders) == 0 && len(projectProviders) == 0 {
			return next(ctx, rc)
		}

		if rc.ActiveToolProviderByGroup == nil {
			rc.ActiveToolProviderByGroup = map[string]string{}
		}

		apply := func(cfg toolprovider.ActiveProviderConfig, override bool) {
			groupName := strings.TrimSpace(cfg.GroupName)
			providerName := strings.TrimSpace(cfg.ProviderName)
			if groupName == "" || providerName == "" {
				return
			}

			if _, exists := rc.ActiveToolProviderByGroup[groupName]; !exists {
				rc.ActiveToolProviderByGroup[groupName] = providerName
			} else if override {
				rc.ActiveToolProviderByGroup[groupName] = providerName
			} else if rc.ActiveToolProviderByGroup[groupName] != providerName {
				slog.WarnContext(ctx, "tool provider: duplicate active provider", "group_name", groupName, "provider_name", providerName)
			}
			exec := buildProviderExecutor(cfg)
			if exec != nil {
				rc.ToolExecutors[providerName] = exec
			}
		}

		// platform 兜底先注入，project 覆盖后注入。
		for _, cfg := range platformProviders {
			apply(cfg, false)
		}
		for _, cfg := range projectProviders {
			apply(cfg, true)
		}

		return next(ctx, rc)
	}
}

func buildProviderExecutor(cfg toolprovider.ActiveProviderConfig) tools.Executor {
	groupName := strings.TrimSpace(cfg.GroupName)
	providerName := strings.TrimSpace(cfg.ProviderName)

	switch providerName {
	case websearch.AgentSpecTavily.Name:
		key := ""
		if cfg.APIKeyValue != nil {
			key = strings.TrimSpace(*cfg.APIKeyValue)
		}
		if key == "" {
			return notConfiguredExecutor{groupName: groupName, providerName: providerName, missing: []string{"api_key"}}
		}
		provider := websearch.NewTavilyProvider(key)
		return websearch.NewToolExecutorWithProvider(provider)

	case websearch.AgentSpecSearxng.Name:
		baseURL := ""
		if cfg.BaseURL != nil {
			baseURL = strings.TrimRight(strings.TrimSpace(*cfg.BaseURL), "/")
		}
		if baseURL == "" {
			return notConfiguredExecutor{groupName: groupName, providerName: providerName, missing: []string{"base_url"}}
		}
		provider := websearch.NewSearxngProvider(baseURL)
		return websearch.NewToolExecutorWithProvider(provider)

	case webfetch.AgentSpecJina.Name:
		key := ""
		if cfg.APIKeyValue != nil {
			key = strings.TrimSpace(*cfg.APIKeyValue)
		}
		if key == "" {
			return notConfiguredExecutor{groupName: groupName, providerName: providerName, missing: []string{"api_key"}}
		}
		provider, err := webfetch.NewJinaProvider(key)
		if err != nil {
			return notConfiguredExecutor{groupName: groupName, providerName: providerName, reason: err.Error()}
		}
		return webfetch.NewToolExecutorWithProvider(provider)

	case webfetch.AgentSpecFirecrawl.Name:
		key := ""
		if cfg.APIKeyValue != nil {
			key = strings.TrimSpace(*cfg.APIKeyValue)
		}
		if key == "" {
			return notConfiguredExecutor{groupName: groupName, providerName: providerName, missing: []string{"api_key"}}
		}
		baseURL := ""
		if cfg.BaseURL != nil {
			baseURL = strings.TrimRight(strings.TrimSpace(*cfg.BaseURL), "/")
		}
		provider := webfetch.NewFirecrawlProvider(key, baseURL)
		return webfetch.NewToolExecutorWithProvider(provider)

	case webfetch.AgentSpecBasic.Name:
		provider := webfetch.NewBasicProvider()
		return webfetch.NewToolExecutorWithProvider(provider)
	}

	return nil
}
