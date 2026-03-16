package webfetch

import (
	"fmt"
	"os"
	"strings"
)

const (
	webFetchProviderEnv = "ARKLOOP_WEB_FETCH_PROVIDER"
	firecrawlAPIKeyEnv  = "ARKLOOP_WEB_FETCH_FIRECRAWL_API_KEY"
	firecrawlBaseURLEnv = "ARKLOOP_WEB_FETCH_FIRECRAWL_BASE_URL"
	jinaAPIKeyEnv       = "ARKLOOP_WEB_FETCH_JINA_API_KEY"
)

const (
	settingProvider     = "web_fetch.provider"
	settingFirecrawlKey = "web_fetch.firecrawl_api_key"
	settingFirecrawlURL = "web_fetch.firecrawl_base_url"
	settingJinaKey      = "web_fetch.jina_api_key"
)

type ProviderKind string

const (
	ProviderKindBasic     ProviderKind = "basic"
	ProviderKindFirecrawl ProviderKind = "firecrawl"
	ProviderKindJina      ProviderKind = "jina"
)

type Config struct {
	ProviderKind     ProviderKind
	FirecrawlAPIKey  string
	FirecrawlBaseURL string
	JinaAPIKey       string
}

func ConfigFromEnv(required bool) (*Config, error) {
	raw := strings.TrimSpace(os.Getenv(webFetchProviderEnv))
	if raw == "" {
		if required {
			return nil, fmt.Errorf("missing environment variable %s", webFetchProviderEnv)
		}
		return nil, nil
	}

	kind, err := parseProviderKind(raw)
	if err != nil {
		return nil, err
	}

	switch kind {
	case ProviderKindBasic:
		return &Config{ProviderKind: kind}, nil
	case ProviderKindFirecrawl:
		apiKey := strings.TrimSpace(os.Getenv(firecrawlAPIKeyEnv))
		baseURL := strings.TrimSpace(os.Getenv(firecrawlBaseURLEnv))
		baseURL = strings.TrimRight(baseURL, "/")
		return &Config{
			ProviderKind:     kind,
			FirecrawlAPIKey:  apiKey,
			FirecrawlBaseURL: baseURL,
		}, nil
	case ProviderKindJina:
		apiKey := strings.TrimSpace(os.Getenv(jinaAPIKeyEnv))
		return &Config{
			ProviderKind: kind,
			JinaAPIKey:   apiKey,
		}, nil
	default:
		return nil, fmt.Errorf("%s must be basic/firecrawl/jina", webFetchProviderEnv)
	}
}

func parseProviderKind(raw string) (ProviderKind, error) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "-", "_"))
	switch normalized {
	case "basic":
		return ProviderKindBasic, nil
	case "firecrawl":
		return ProviderKindFirecrawl, nil
	case "jina":
		return ProviderKindJina, nil
	default:
		return "", fmt.Errorf("%s must be basic/firecrawl/jina", webFetchProviderEnv)
	}
}
