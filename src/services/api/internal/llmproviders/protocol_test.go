package llmproviders

import (
	"testing"

	"arkloop/services/api/internal/data"
)

func TestResolveCatalogProtocolConfigOpenAI(t *testing.T) {
	mode := "responses"
	cfg, err := ResolveCatalogProtocolConfig(data.LlmCredential{
		Provider:      "openai",
		OpenAIAPIMode: &mode,
	}, "sk-test")
	if err != nil {
		t.Fatalf("ResolveCatalogProtocolConfig() error = %v", err)
	}
	if cfg.Kind != ProtocolKindOpenAIResponses {
		t.Fatalf("Kind = %q, want %q", cfg.Kind, ProtocolKindOpenAIResponses)
	}
	if cfg.BaseURL != defaultOpenAIBaseURL {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, defaultOpenAIBaseURL)
	}
}

func TestResolveCatalogProtocolConfigOpenAIRejectsInvalidMode(t *testing.T) {
	mode := "bad-mode"
	_, err := ResolveCatalogProtocolConfig(data.LlmCredential{
		Provider:      "openai",
		OpenAIAPIMode: &mode,
	}, "sk-test")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "invalid openai_api_mode: bad-mode" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveCatalogProtocolConfigAnthropicDefaultsToHostBase(t *testing.T) {
	cfg, err := ResolveCatalogProtocolConfig(data.LlmCredential{
		Provider: "anthropic",
	}, "sk-ant")
	if err != nil {
		t.Fatalf("ResolveCatalogProtocolConfig() error = %v", err)
	}
	if cfg.Kind != ProtocolKindAnthropicMessages {
		t.Fatalf("Kind = %q, want %q", cfg.Kind, ProtocolKindAnthropicMessages)
	}
	if cfg.BaseURL != defaultAnthropicBaseURL {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, defaultAnthropicBaseURL)
	}
	if anthropicCatalogPath(cfg.BaseURL) != "/v1/models" {
		t.Fatalf("anthropicCatalogPath(%q) = %q, want /v1/models", cfg.BaseURL, anthropicCatalogPath(cfg.BaseURL))
	}
}

func TestResolveCatalogProtocolConfigAnthropicNormalizesMiniMaxRoot(t *testing.T) {
	baseURL := "https://api.minimaxi.com/anthropic"
	cfg, err := ResolveCatalogProtocolConfig(data.LlmCredential{
		Provider: "anthropic",
		BaseURL:  &baseURL,
	}, "sk-ant")
	if err != nil {
		t.Fatalf("ResolveCatalogProtocolConfig() error = %v", err)
	}
	if cfg.BaseURL != "https://api.minimaxi.com/anthropic/v1" {
		t.Fatalf("BaseURL = %q, want minimax v1 base", cfg.BaseURL)
	}
	if anthropicCatalogPath(cfg.BaseURL) != "/models" {
		t.Fatalf("anthropicCatalogPath(%q) = %q, want /models", cfg.BaseURL, anthropicCatalogPath(cfg.BaseURL))
	}
}

func TestResolveCatalogProtocolConfigAnthropicRejectsInvalidExtraHeaders(t *testing.T) {
	_, err := ResolveCatalogProtocolConfig(data.LlmCredential{
		Provider: "anthropic",
		AdvancedJSON: map[string]any{
			"extra_headers": map[string]any{
				"x-custom": "bad",
			},
		},
	}, "sk-ant")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "advanced_json.extra_headers only supports anthropic-beta" {
		t.Fatalf("unexpected error: %v", err)
	}
}
