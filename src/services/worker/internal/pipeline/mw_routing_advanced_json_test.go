package pipeline

import (
	"testing"

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
			},
		},
		Credential: routing.ProviderCredential{
			ID:           "cred-openai",
			ProviderKind: routing.ProviderKindOpenAI,
			APIKeyValue:  routingAdvancedJSONStringPtr("sk-test"),
			OpenAIMode:   &apiMode,
			AdvancedJSON: map[string]any{
				"top_p": 0.9,
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
}

func routingAdvancedJSONStringPtr(v string) *string {
	return &v
}
