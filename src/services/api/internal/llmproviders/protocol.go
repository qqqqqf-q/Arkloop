package llmproviders

import (
	"fmt"
	"strings"

	"arkloop/services/api/internal/data"
	sharedoutbound "arkloop/services/shared/outboundurl"
)

type ProtocolKind string

const (
	ProtocolKindOpenAIChatCompletions ProtocolKind = "openai_chat_completions"
	ProtocolKindOpenAIResponses       ProtocolKind = "openai_responses"
	ProtocolKindAnthropicMessages     ProtocolKind = "anthropic_messages"
	ProtocolKindGeminiGenerateContent ProtocolKind = "gemini_generate_content"
)

type CatalogProtocolConfig struct {
	Kind       ProtocolKind
	BaseURL    string
	APIKey     string
	OpenAI     OpenAICatalogConfig
	Anthropic  AnthropicCatalogConfig
	Gemini     GeminiCatalogConfig
	Credential data.LlmCredential
}

type OpenAICatalogConfig struct {
	APIMode string
}

type AnthropicCatalogConfig struct {
	Version      string
	ExtraHeaders map[string]string
}

type GeminiCatalogConfig struct{}

func ResolveCatalogProtocolConfig(provider data.LlmCredential, apiKey string) (CatalogProtocolConfig, error) {
	providerName := strings.ToLower(strings.TrimSpace(provider.Provider))
	baseURL := strings.TrimSpace(stringValue(provider.BaseURL))
	apiKey = strings.TrimSpace(apiKey)

	switch providerName {
	case "openai":
		resolvedBaseURL := strings.TrimRight(baseURL, "/")
		if resolvedBaseURL == "" {
			resolvedBaseURL = defaultOpenAIBaseURL
		}
		apiMode := strings.TrimSpace(stringValue(provider.OpenAIAPIMode))
		kind, normalizedMode, err := resolveOpenAIProtocolKind(apiMode)
		if err != nil {
			return CatalogProtocolConfig{}, err
		}
		return CatalogProtocolConfig{
			Kind:       kind,
			BaseURL:    resolvedBaseURL,
			APIKey:     apiKey,
			OpenAI:     OpenAICatalogConfig{APIMode: normalizedMode},
			Credential: provider,
		}, nil
	case "anthropic":
		resolvedBaseURL := resolveAnthropicCatalogBaseURL(baseURL)
		version, extraHeaders, err := parseAnthropicCatalogAdvanced(provider.AdvancedJSON)
		if err != nil {
			return CatalogProtocolConfig{}, err
		}
		return CatalogProtocolConfig{
			Kind:    ProtocolKindAnthropicMessages,
			BaseURL: resolvedBaseURL,
			APIKey:  apiKey,
			Anthropic: AnthropicCatalogConfig{
				Version:      version,
				ExtraHeaders: extraHeaders,
			},
			Credential: provider,
		}, nil
	case "gemini":
		resolvedBaseURL := strings.TrimRight(baseURL, "/")
		if resolvedBaseURL == "" {
			resolvedBaseURL = defaultGeminiCatalogBaseURL
		}
		return CatalogProtocolConfig{
			Kind:       ProtocolKindGeminiGenerateContent,
			BaseURL:    resolvedBaseURL,
			APIKey:     apiKey,
			Gemini:     GeminiCatalogConfig{},
			Credential: provider,
		}, nil
	default:
		return CatalogProtocolConfig{}, fmt.Errorf("unsupported provider: %s", provider.Provider)
	}
}

func resolveOpenAIProtocolKind(apiMode string) (ProtocolKind, string, error) {
	mode := strings.TrimSpace(apiMode)
	if mode == "" {
		mode = "auto"
	}
	switch mode {
	case "chat_completions":
		return ProtocolKindOpenAIChatCompletions, mode, nil
	case "responses":
		return ProtocolKindOpenAIResponses, mode, nil
	case "auto":
		return ProtocolKindOpenAIResponses, mode, nil
	default:
		return "", "", fmt.Errorf("invalid openai_api_mode: %s", mode)
	}
}

func parseAnthropicCatalogAdvanced(advanced map[string]any) (string, map[string]string, error) {
	version := defaultAnthropicVersion
	extraHeaders := map[string]string{}
	if advanced == nil {
		return version, extraHeaders, nil
	}
	if err := validateAnthropicAdvancedJSON(advanced); err != nil {
		return "", nil, err
	}
	if rawVersion, ok := advanced[anthropicAdvancedVersionKey]; ok {
		version = strings.TrimSpace(rawVersion.(string))
	}
	if rawHeaders, ok := advanced[anthropicAdvancedExtraHeadersKey]; ok {
		headers := rawHeaders.(map[string]any)
		extraHeaders[anthropicBetaHeaderName] = strings.TrimSpace(headers[anthropicBetaHeaderName].(string))
	}
	return version, extraHeaders, nil
}

func resolveAnthropicCatalogBaseURL(raw string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(raw), "/")
	if baseURL == "" {
		return "https://api.anthropic.com"
	}
	return sharedoutbound.NormalizeAnthropicCompatibleBaseURL(baseURL)
}

func anthropicCatalogPath(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return "/models"
	}
	return "/v1/models"
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
