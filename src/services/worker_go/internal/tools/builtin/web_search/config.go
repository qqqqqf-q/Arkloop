package websearch

import (
	"fmt"
	"os"
	"strings"
)

const (
	webSearchProviderEnv    = "ARKLOOP_WEB_SEARCH_PROVIDER"
	searxngBaseURLEnv       = "ARKLOOP_WEB_SEARCH_SEARXNG_BASE_URL"
	tavilyAPIKeyEnv         = "ARKLOOP_WEB_SEARCH_TAVILY_API_KEY"
)

type ProviderKind string

const (
	ProviderKindSearxng ProviderKind = "searxng"
	ProviderKindTavily  ProviderKind = "tavily"
	ProviderKindSerper  ProviderKind = "serper"
)

type Config struct {
	ProviderKind  ProviderKind
	SearxngBaseURL string
	TavilyAPIKey   string
}

func ConfigFromEnv(required bool) (*Config, error) {
	raw := strings.TrimSpace(os.Getenv(webSearchProviderEnv))
	if raw == "" {
		if required {
			return nil, fmt.Errorf("缺少环境变量 %s", webSearchProviderEnv)
		}
		return nil, nil
	}

	kind, err := parseProviderKind(raw)
	if err != nil {
		return nil, err
	}

	switch kind {
	case ProviderKindSearxng:
		baseURL := strings.TrimSpace(os.Getenv(searxngBaseURLEnv))
		if baseURL == "" {
			return nil, fmt.Errorf("缺少环境变量 %s", searxngBaseURLEnv)
		}
		baseURL = strings.TrimRight(baseURL, "/")
		return &Config{
			ProviderKind:  kind,
			SearxngBaseURL: baseURL,
		}, nil
	case ProviderKindTavily:
		apiKey := strings.TrimSpace(os.Getenv(tavilyAPIKeyEnv))
		if apiKey == "" {
			return nil, fmt.Errorf("缺少环境变量 %s", tavilyAPIKeyEnv)
		}
		return &Config{
			ProviderKind: kind,
			TavilyAPIKey: apiKey,
		}, nil
	case ProviderKindSerper:
		return &Config{ProviderKind: kind}, nil
	default:
		return nil, fmt.Errorf("%s 必须为 searxng/tavily/serper", webSearchProviderEnv)
	}
}

func parseProviderKind(raw string) (ProviderKind, error) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "-", "_"))
	switch normalized {
	case "searxng":
		return ProviderKindSearxng, nil
	case "tavily":
		return ProviderKindTavily, nil
	case "serper":
		return ProviderKindSerper, nil
	default:
		return "", fmt.Errorf("%s 必须为 searxng/tavily/serper", webSearchProviderEnv)
	}
}

