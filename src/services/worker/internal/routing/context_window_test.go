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
