package pipeline

import (
	"context"
	"log/slog"
	"strings"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
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

func (notConfiguredExecutor) IsNotConfigured() bool { return true }

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
			ErrorClass: "config.missing",
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

		var userProviders []toolprovider.ActiveProviderConfig
		if rc.Run.CreatedByUserID != nil {
			userProviders, err = cache.GetUser(ctx, rc.Pool, *rc.Run.CreatedByUserID)
			if err != nil {
				slog.WarnContext(ctx, "tool provider: load user failed, skipping", "user_id", *rc.Run.CreatedByUserID, "err", err.Error())
				userProviders = nil
			}
		}

		if len(platformProviders) == 0 && len(userProviders) == 0 {
			return next(ctx, rc)
		}

		if rc.ActiveToolProviderByGroup == nil {
			rc.ActiveToolProviderByGroup = map[string]string{}
		}
		if rc.ActiveToolProviderConfigsByGroup == nil {
			rc.ActiveToolProviderConfigsByGroup = map[string]sharedtoolruntime.ProviderConfig{}
		}

		apply := func(cfg toolprovider.ActiveProviderConfig, override bool) {
			groupName := strings.TrimSpace(cfg.GroupName)
			providerName := strings.TrimSpace(cfg.ProviderName)
			if groupName == "" || providerName == "" {
				return
			}

			exec := buildProviderExecutor(cfg)

			_, exists := rc.ActiveToolProviderByGroup[groupName]
			if exists && override {
				if nc, ok := exec.(tools.NotConfiguredChecker); ok && nc.IsNotConfigured() {
					slog.WarnContext(ctx, "tool provider: user provider not configured, keeping platform provider",
						"group_name", groupName, "provider_name", providerName)
					return
				}
			}

			if !exists {
				rc.ActiveToolProviderByGroup[groupName] = providerName
				rc.ActiveToolProviderConfigsByGroup[groupName] = toRuntimeProviderConfig(cfg)
			} else if override {
				rc.ActiveToolProviderByGroup[groupName] = providerName
				rc.ActiveToolProviderConfigsByGroup[groupName] = toRuntimeProviderConfig(cfg)
			} else if rc.ActiveToolProviderByGroup[groupName] != providerName {
				slog.WarnContext(ctx, "tool provider: duplicate active provider", "group_name", groupName, "provider_name", providerName)
			}
			if exec != nil {
				rc.ToolExecutors[providerName] = exec
			}
		}

		// platform 兜底先注入，project 覆盖后注入。
		for _, cfg := range platformProviders {
			apply(cfg, false)
		}
		for _, cfg := range userProviders {
			apply(cfg, true)
		}

		return next(ctx, rc)
	}
}

func toRuntimeProviderConfig(cfg toolprovider.ActiveProviderConfig) sharedtoolruntime.ProviderConfig {
	return sharedtoolruntime.ProviderConfig{
		GroupName:    strings.TrimSpace(cfg.GroupName),
		ProviderName: strings.TrimSpace(cfg.ProviderName),
		BaseURL:      cfg.BaseURL,
		APIKeyValue:  cfg.APIKeyValue,
		ConfigJSON:   copyJSONMap(cfg.ConfigJSON),
	}
}

func copyJSONMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
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
		baseURL := ""
		if cfg.BaseURL != nil {
			baseURL = strings.TrimRight(strings.TrimSpace(*cfg.BaseURL), "/")
		}
		if key == "" && baseURL == "" {
			return notConfiguredExecutor{groupName: groupName, providerName: providerName, missing: []string{"api_key"}}
		}
		provider := webfetch.NewFirecrawlProvider(key, baseURL)
		return webfetch.NewToolExecutorWithProvider(provider)

	case webfetch.AgentSpecBasic.Name:
		provider := webfetch.NewBasicProvider()
		return webfetch.NewToolExecutorWithProvider(provider)
	}

	return nil
}
