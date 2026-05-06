package pipeline

import (
	"testing"

	"arkloop/services/shared/localproviders"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
)

func TestMergeAdvancedJSON_ModelOverridesProvider(t *testing.T) {
	merged := mergeAdvancedJSON(
		map[string]any{
			"metadata":    map[string]any{"source": "provider"},
			"temperature": 0.1,
		},
		map[string]any{
			"metadata": map[string]any{"source": "model"},
			"top_p":    0.9,
		},
	)

	metadata, ok := merged["metadata"].(map[string]any)
	if !ok || metadata["source"] != "model" {
		t.Fatalf("expected model metadata override, got %#v", merged)
	}
	if merged["temperature"] != 0.1 {
		t.Fatalf("expected provider key preserved, got %#v", merged)
	}
	if merged["top_p"] != 0.9 {
		t.Fatalf("expected model key merged, got %#v", merged)
	}
}

func TestMergeAdvancedJSON_EmptyInputs(t *testing.T) {
	merged := mergeAdvancedJSON(nil, nil)
	if len(merged) != 0 {
		t.Fatalf("expected empty map, got %#v", merged)
	}
}

func TestResolveGatewayConfigFromSelectedRoute_OpenAIAuto(t *testing.T) {
	apiMode := "auto"
	selected := routing.SelectedProviderRoute{
		Route: routing.ProviderRouteRule{
			ID:           "route-openai",
			Model:        "gpt-5.4",
			CredentialID: "cred-openai",
			AdvancedJSON: map[string]any{
				"metadata": map[string]any{"source": "route"},
				"openviking_extra_headers": map[string]any{
					"x-route":  "model",
					"x-shared": "model",
				},
				"available_catalog": map[string]any{
					"id":             "gpt-5.4",
					"context_length": float64(200000),
				},
			},
		},
		Credential: routing.ProviderCredential{
			ID:           "cred-openai",
			ProviderKind: routing.ProviderKindOpenAI,
			APIKeyValue:  routingAdvancedJSONStringPtr("sk-test"),
			OpenAIMode:   &apiMode,
			AdvancedJSON: map[string]any{
				"top_p":              0.9,
				"openviking_backend": "openai",
				"openviking_extra_headers": map[string]any{
					"x-provider": "credential",
					"x-shared":   "credential",
				},
			},
		},
	}

	resolved, err := ResolveGatewayConfigFromSelectedRoute(selected, true, 8192)
	if err != nil {
		t.Fatalf("ResolveGatewayConfigFromSelectedRoute returned error: %v", err)
	}
	if resolved.ProtocolKind != llm.ProtocolKindOpenAIResponses {
		t.Fatalf("unexpected protocol kind: %s", resolved.ProtocolKind)
	}
	if resolved.OpenAI == nil || resolved.OpenAI.FallbackKind == nil {
		t.Fatalf("expected openai fallback config, got %#v", resolved.OpenAI)
	}
	if *resolved.OpenAI.FallbackKind != llm.ProtocolKindOpenAIChatCompletions {
		t.Fatalf("unexpected fallback kind: %s", *resolved.OpenAI.FallbackKind)
	}
	if !resolved.Transport.EmitDebugEvents {
		t.Fatalf("expected transport debug flag to be preserved")
	}
	if resolved.OpenAI.AdvancedPayloadJSON["top_p"] != 0.9 {
		t.Fatalf("expected provider advanced_json merged, got %#v", resolved.OpenAI.AdvancedPayloadJSON)
	}
	if _, exists := resolved.OpenAI.AdvancedPayloadJSON["available_catalog"]; exists {
		t.Fatalf("available_catalog must stay internal, got %#v", resolved.OpenAI.AdvancedPayloadJSON)
	}
	if _, exists := resolved.OpenAI.AdvancedPayloadJSON["openviking_backend"]; exists {
		t.Fatalf("openviking_backend must stay internal, got %#v", resolved.OpenAI.AdvancedPayloadJSON)
	}
	if resolved.Transport.DefaultHeaders["x-route"] != "model" {
		t.Fatalf("expected model headers to be applied, got %#v", resolved.Transport.DefaultHeaders)
	}
	if resolved.Transport.DefaultHeaders["x-provider"] != "credential" {
		t.Fatalf("expected provider headers to be preserved, got %#v", resolved.Transport.DefaultHeaders)
	}
	if resolved.Transport.DefaultHeaders["x-shared"] != "model" {
		t.Fatalf("expected model headers to override matching provider headers, got %#v", resolved.Transport.DefaultHeaders)
	}
	if routing.RouteContextWindowTokens(selected.Route) != 200000 {
		t.Fatalf("expected route metadata to remain available locally")
	}
}

func TestResolveLocalProviderGatewayConfigClaudeOAuth(t *testing.T) {
	selected := routing.SelectedProviderRoute{
		Route: routing.ProviderRouteRule{ID: "local-claude", Model: "claude-sonnet-4-6"},
		Credential: routing.ProviderCredential{
			ID:           localproviders.ClaudeCodeProviderID,
			ProviderKind: routing.ProviderKindClaudeLocal,
		},
	}
	resolved, err := resolveLocalProviderGatewayConfig(selected, localproviders.Credential{
		ProviderID:  localproviders.ClaudeCodeProviderID,
		AuthMode:    localproviders.AuthModeOAuth,
		BaseURL:     "https://gateway.local/anthropic",
		AccessToken: "oauth-access",
	}, map[string]any{}, true, 8192)
	if err != nil {
		t.Fatalf("resolveLocalProviderGatewayConfig: %v", err)
	}
	if resolved.ProtocolKind != llm.ProtocolKindAnthropicMessages || resolved.Transport.AuthScheme != "bearer" {
		t.Fatalf("unexpected resolved config: %#v", resolved)
	}
	if resolved.Transport.APIKey != "oauth-access" || resolved.Transport.DefaultHeaders["x-app"] != "cli" {
		t.Fatalf("unexpected transport: %#v", resolved.Transport)
	}
	if resolved.Transport.BaseURL != "https://gateway.local/anthropic" {
		t.Fatalf("expected local Claude Code base url, got %q", resolved.Transport.BaseURL)
	}
	if resolved.Transport.DefaultHeaders["anthropic-beta"] == "" {
		t.Fatalf("expected anthropic beta header")
	}
}

func TestResolveLocalProviderGatewayConfigCodexOAuth(t *testing.T) {
	selected := routing.SelectedProviderRoute{
		Route: routing.ProviderRouteRule{ID: "local-codex", Model: "gpt-5.3-codex"},
		Credential: routing.ProviderCredential{
			ID:           localproviders.CodexProviderID,
			ProviderKind: routing.ProviderKindCodexLocal,
		},
	}
	resolved, err := resolveLocalProviderGatewayConfig(selected, localproviders.Credential{
		ProviderID:  localproviders.CodexProviderID,
		AuthMode:    localproviders.AuthModeOAuth,
		AccessToken: "oauth-access",
		AccountID:   "acc_123",
	}, map[string]any{}, true, 8192)
	if err != nil {
		t.Fatalf("resolveLocalProviderGatewayConfig: %v", err)
	}
	if resolved.ProtocolKind != llm.ProtocolKindOpenAICodexResponses {
		t.Fatalf("unexpected protocol kind: %s", resolved.ProtocolKind)
	}
	if resolved.Transport.BaseURL != "https://chatgpt.com/backend-api" || resolved.Transport.APIKey != "oauth-access" {
		t.Fatalf("unexpected transport: %#v", resolved.Transport)
	}
	if resolved.Transport.DefaultHeaders["chatgpt-account-id"] != "acc_123" {
		t.Fatalf("unexpected headers: %#v", resolved.Transport.DefaultHeaders)
	}
}

func TestResolveLocalProviderGatewayConfigStripsLocalMetadata(t *testing.T) {
	selected := routing.SelectedProviderRoute{
		Route: routing.ProviderRouteRule{ID: "local-codex", Model: "gpt-5.3-codex"},
		Credential: routing.ProviderCredential{
			ID:           localproviders.CodexProviderID,
			ProviderKind: routing.ProviderKindCodexLocal,
			AdvancedJSON: map[string]any{
				"source":            localproviders.SourceLocal,
				"local_provider_id": localproviders.CodexProviderID,
				"auth_mode":         localproviders.AuthModeOAuth,
				"read_only":         true,
			},
		},
	}
	advancedJSON := providerPayloadAdvancedJSON(mergeAdvancedJSON(selected.Credential.AdvancedJSON, selected.Route.AdvancedJSON))
	resolved, err := resolveLocalProviderGatewayConfig(selected, localproviders.Credential{
		ProviderID:  localproviders.CodexProviderID,
		AuthMode:    localproviders.AuthModeOAuth,
		AccessToken: "oauth-access",
		AccountID:   "acc_123",
	}, advancedJSON, true, 8192)
	if err != nil {
		t.Fatalf("resolveLocalProviderGatewayConfig: %v", err)
	}
	for _, key := range []string{"source", "local_provider_id", "auth_mode", "read_only"} {
		if _, exists := resolved.OpenAI.AdvancedPayloadJSON[key]; exists {
			t.Fatalf("%s must stay internal, got %#v", key, resolved.OpenAI.AdvancedPayloadJSON)
		}
	}
}

func TestProviderPayloadAdvancedJSON_StripsInternalRouteMetadata(t *testing.T) {
	filtered := providerPayloadAdvancedJSON(map[string]any{
		"available_catalog":        map[string]any{"id": "gpt-5.4"},
		"openviking_backend":       "openai",
		"openviking_extra_headers": map[string]any{"x-test": "1"},
		"top_p":                    0.9,
	})

	if _, exists := filtered["available_catalog"]; exists {
		t.Fatalf("unexpected available_catalog in provider payload: %#v", filtered)
	}
	if _, exists := filtered["openviking_backend"]; exists {
		t.Fatalf("unexpected openviking_backend in provider payload: %#v", filtered)
	}
	if _, exists := filtered["openviking_extra_headers"]; exists {
		t.Fatalf("unexpected openviking_extra_headers in provider payload: %#v", filtered)
	}
	if filtered["top_p"] != 0.9 {
		t.Fatalf("expected top_p preserved, got %#v", filtered)
	}
}

func routingAdvancedJSONStringPtr(v string) *string {
	return &v
}
