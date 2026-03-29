package llm

import "testing"

func TestResolveOpenAIProtocolConfig_AutoAddsFallback(t *testing.T) {
	cfg, err := ResolveOpenAIProtocolConfig("auto", map[string]any{
		"metadata": map[string]any{"source": "test"},
	})
	if err != nil {
		t.Fatalf("ResolveOpenAIProtocolConfig returned error: %v", err)
	}
	if cfg.PrimaryKind != ProtocolKindOpenAIResponses {
		t.Fatalf("unexpected primary protocol kind: %s", cfg.PrimaryKind)
	}
	if cfg.FallbackKind == nil || *cfg.FallbackKind != ProtocolKindOpenAIChatCompletions {
		t.Fatalf("unexpected fallback protocol kind: %#v", cfg.FallbackKind)
	}
	if cfg.AdvancedPayloadJSON["metadata"] == nil {
		t.Fatalf("expected advanced payload to be preserved, got %#v", cfg.AdvancedPayloadJSON)
	}
}

func TestResolveAnthropicProtocolConfig_SeparatesHeadersFromPayload(t *testing.T) {
	cfg, err := ResolveAnthropicProtocolConfig(map[string]any{
		"anthropic_version": "2023-06-01",
		"extra_headers": map[string]any{
			"anthropic-beta": "tools-2024-04-04",
		},
		"thinking": map[string]any{
			"type":          "enabled",
			"budget_tokens": 1024,
			"signature":     "ignored-by-provider",
		},
	})
	if err != nil {
		t.Fatalf("ResolveAnthropicProtocolConfig returned error: %v", err)
	}
	if cfg.Version != "2023-06-01" {
		t.Fatalf("unexpected anthropic version: %q", cfg.Version)
	}
	if cfg.ExtraHeaders["anthropic-beta"] != "tools-2024-04-04" {
		t.Fatalf("unexpected anthropic headers: %#v", cfg.ExtraHeaders)
	}
	if cfg.AdvancedPayloadJSON["thinking"] == nil {
		t.Fatalf("expected thinking payload to remain in protocol payload: %#v", cfg.AdvancedPayloadJSON)
	}
}

func TestNewGatewayFromResolvedConfig_AnthropicUsesExplicitPathBase(t *testing.T) {
	gateway, err := NewGatewayFromResolvedConfig(ResolvedGatewayConfig{
		ProtocolKind: ProtocolKindAnthropicMessages,
		Model:        "MiniMax-M2.7",
		Transport: TransportConfig{
			APIKey:  "test",
			BaseURL: "https://api.minimaxi.com/anthropic/v1",
		},
		Anthropic: &AnthropicProtocolConfig{
			Version:             defaultAnthropicVersion,
			ExtraHeaders:        map[string]string{},
			AdvancedPayloadJSON: map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayFromResolvedConfig returned error: %v", err)
	}

	anthropicGateway, ok := gateway.(*AnthropicGateway)
	if !ok {
		t.Fatalf("expected AnthropicGateway, got %T", gateway)
	}
	if anthropicGateway.ProtocolKind() != ProtocolKindAnthropicMessages {
		t.Fatalf("unexpected protocol kind: %s", anthropicGateway.ProtocolKind())
	}
	if anthropicGateway.transport.cfg.BaseURL != "https://api.minimaxi.com/anthropic" {
		t.Fatalf("unexpected normalized base url: %q", anthropicGateway.transport.cfg.BaseURL)
	}
	if path := anthropicGateway.transport.endpoint("/v1/messages"); path != "https://api.minimaxi.com/anthropic/v1/messages" {
		t.Fatalf("unexpected anthropic endpoint: %q", path)
	}
}

func TestGeminiAPIVersionFromBaseURL(t *testing.T) {
	if got := geminiAPIVersionFromBaseURL("https://generativelanguage.googleapis.com/v1"); got != "v1" {
		t.Fatalf("unexpected version for v1 base: %q", got)
	}
	if got := geminiAPIVersionFromBaseURL("https://generativelanguage.googleapis.com/v1beta1"); got != "v1beta1" {
		t.Fatalf("unexpected version for v1beta1 base: %q", got)
	}
	if got := geminiAPIVersionFromBaseURL("https://generativelanguage.googleapis.com"); got != "" {
		t.Fatalf("unexpected version for unversioned base: %q", got)
	}
}
