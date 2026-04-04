package routing

import (
	"testing"
)

func TestRouteContextWindowTokens(t *testing.T) {
	rule := ProviderRouteRule{
		AdvancedJSON: map[string]any{
			"available_catalog": map[string]any{
				"id":             "gpt-4o",
				"name":           "GPT-4o",
				"context_length": float64(200000),
			},
		},
	}
	if n := RouteContextWindowTokens(rule); n != 200000 {
		t.Fatalf("got %d", n)
	}
	if RouteContextWindowTokens(ProviderRouteRule{}) != 0 {
		t.Fatal("expected 0 when empty")
	}
	if RouteContextWindowTokens(ProviderRouteRule{AdvancedJSON: map[string]any{}}) != 0 {
		t.Fatal("expected 0 when no catalog")
	}
	if n := RouteContextWindowTokens(ProviderRouteRule{
		AdvancedJSON: map[string]any{"context_window_tokens": float64(999)},
	}); n != 0 {
		t.Fatalf("top-level key ignored, got %d", n)
	}
}

func TestRouteModelCapabilities(t *testing.T) {
	rule := ProviderRouteRule{
		AdvancedJSON: map[string]any{
			"available_catalog": map[string]any{
				"context_length":    "131072",
				"max_output_tokens": float64(8192),
				"input_modalities":  []any{"text", " image ", "TEXT", ""},
				"output_modalities": []string{"text", "audio", "text"},
			},
		},
	}

	caps := RouteModelCapabilities(rule)
	if caps.ContextLength != 131072 {
		t.Fatalf("unexpected context_length: %d", caps.ContextLength)
	}
	if caps.MaxOutputTokens != 8192 {
		t.Fatalf("unexpected max_output_tokens: %d", caps.MaxOutputTokens)
	}
	if len(caps.InputModalities) != 2 || caps.InputModalities[0] != "text" || caps.InputModalities[1] != "image" {
		t.Fatalf("unexpected input_modalities: %#v", caps.InputModalities)
	}
	if len(caps.OutputModalities) != 2 || caps.OutputModalities[0] != "text" || caps.OutputModalities[1] != "audio" {
		t.Fatalf("unexpected output_modalities: %#v", caps.OutputModalities)
	}
	if !caps.SupportsInputModality("image") {
		t.Fatal("expected image input support")
	}
	if caps.SupportsInputModality("video") {
		t.Fatal("did not expect video input support")
	}
}

func TestRouteContextWindowTokensOverride(t *testing.T) {
	// override 优先于 context_length
	rule := ProviderRouteRule{
		AdvancedJSON: map[string]any{
			"available_catalog": map[string]any{
				"context_length":          float64(200000),
				"context_length_override": float64(128000),
			},
		},
	}
	if n := RouteContextWindowTokens(rule); n != 128000 {
		t.Fatalf("expected override 128000, got %d", n)
	}

	// 仅有 override，无 context_length
	ruleOnlyOverride := ProviderRouteRule{
		AdvancedJSON: map[string]any{
			"available_catalog": map[string]any{
				"context_length_override": float64(64000),
			},
		},
	}
	if n := RouteContextWindowTokens(ruleOnlyOverride); n != 64000 {
		t.Fatalf("expected override-only 64000, got %d", n)
	}

	// 仅有 context_length，无 override -> 回退到 context_length
	ruleNoOverride := ProviderRouteRule{
		AdvancedJSON: map[string]any{
			"available_catalog": map[string]any{
				"context_length": float64(200000),
			},
		},
	}
	if n := RouteContextWindowTokens(ruleNoOverride); n != 200000 {
		t.Fatalf("expected fallback to context_length 200000, got %d", n)
	}
}

func TestRouteModelCapabilitiesMissingCatalog(t *testing.T) {
	caps := RouteModelCapabilities(ProviderRouteRule{})
	if caps.ContextLength != 0 {
		t.Fatalf("expected empty context length, got %d", caps.ContextLength)
	}
	if caps.SupportsInputModality("image") {
		t.Fatal("did not expect image support when catalog is missing")
	}
}
