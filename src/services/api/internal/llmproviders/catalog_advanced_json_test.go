package llmproviders

import (
	"testing"
)

func TestRouteAdvancedJSONFromAvailableModel_full(t *testing.T) {
	cl := 128000
	mo := 4096
	am := AvailableModel{
		ID:               "gpt-4o",
		Name:             "GPT-4o",
		Type:             "chat",
		ContextLength:    &cl,
		MaxOutputTokens:  &mo,
		InputModalities:  []string{"text", "image"},
		OutputModalities: []string{"text"},
	}
	out := RouteAdvancedJSONFromAvailableModel(am)
	if _, ok := out["context_window_tokens"]; ok {
		t.Fatal("unexpected top-level context_window_tokens")
	}
	raw, ok := out[AvailableCatalogAdvancedKey].(map[string]any)
	if !ok || raw["id"] != "gpt-4o" || raw["name"] != "GPT-4o" {
		t.Fatalf("catalog: %#v", raw)
	}
	if cl, ok := raw["context_length"].(int); !ok || cl != 128000 {
		t.Fatalf("context_length: %#v", raw["context_length"])
	}
}

func TestRouteAdvancedJSONFromAvailableModel_minimal(t *testing.T) {
	am := AvailableModel{ID: "x", Name: "X"}
	out := RouteAdvancedJSONFromAvailableModel(am)
	if _, ok := out["context_window_tokens"]; ok {
		t.Fatal("unexpected top-level context_window_tokens")
	}
	raw, ok := out[AvailableCatalogAdvancedKey].(map[string]any)
	if !ok || raw["id"] != "x" {
		t.Fatalf("catalog: %#v", raw)
	}
}
