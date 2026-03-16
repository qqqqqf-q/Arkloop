package websearch

import (
	"fmt"
	"os"
	"strings"
)

const (
	webSearchProviderEnv        = "ARKLOOP_WEB_SEARCH_PROVIDER"
	searxngBaseURLEnv           = "ARKLOOP_WEB_SEARCH_SEARXNG_BASE_URL"
	tavilyAPIKeyEnv             = "ARKLOOP_WEB_SEARCH_TAVILY_API_KEY"
	desktopCallbackAddrEnv      = "ARKLOOP_WEB_SEARCH_DESKTOP_CALLBACK_ADDR"
)

const (
	settingProvider             = "web_search.provider"
	settingSearxngURL           = "web_search.searxng_base_url"
	settingTavilyKey            = "web_search.tavily_api_key"
	settingDesktopCallbackAddr  = "web_search.desktop_callback_addr"
)

type ProviderKind string

const (
	ProviderKindSearxng ProviderKind = "searxng"
	ProviderKindTavily  ProviderKind = "tavily"
	ProviderKindSerper  ProviderKind = "serper"
	// ProviderKindBrowser delegates search to the Electron host browser via
	// a local HTTP callback server. Requires ARKLOOP_WEB_SEARCH_DESKTOP_CALLBACK_ADDR.
	ProviderKindBrowser ProviderKind = "browser"
)

type Config struct {
	ProviderKind        ProviderKind
	SearxngBaseURL      string
	TavilyAPIKey        string
	DesktopCallbackAddr string
}

func ConfigFromEnv(required bool) (*Config, error) {
	raw := strings.TrimSpace(os.Getenv(webSearchProviderEnv))
	if raw == "" {
		if required {
			return nil, fmt.Errorf("missing environment variable %s", webSearchProviderEnv)
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
			return nil, fmt.Errorf("missing environment variable %s", searxngBaseURLEnv)
		}
		baseURL = strings.TrimRight(baseURL, "/")
		return &Config{
			ProviderKind:   kind,
			SearxngBaseURL: baseURL,
		}, nil
	case ProviderKindTavily:
		apiKey := strings.TrimSpace(os.Getenv(tavilyAPIKeyEnv))
		if apiKey == "" {
			return nil, fmt.Errorf("missing environment variable %s", tavilyAPIKeyEnv)
		}
		return &Config{
			ProviderKind: kind,
			TavilyAPIKey: apiKey,
		}, nil
	case ProviderKindSerper:
		return &Config{ProviderKind: kind}, nil
	case ProviderKindBrowser:
		addr := strings.TrimSpace(os.Getenv(desktopCallbackAddrEnv))
		return &Config{
			ProviderKind:        kind,
			DesktopCallbackAddr: addr,
		}, nil
	default:
		return nil, fmt.Errorf("%s must be searxng/tavily/serper/browser", webSearchProviderEnv)
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
	case "browser":
		return ProviderKindBrowser, nil
	default:
		return "", fmt.Errorf("%s must be searxng/tavily/serper/browser", webSearchProviderEnv)
	}
}
