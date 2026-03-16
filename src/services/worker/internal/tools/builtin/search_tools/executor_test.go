package searchtools

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

type mockActivator struct {
	activated []llm.ToolSpec
}

func (m *mockActivator) Activate(specs ...llm.ToolSpec) {
	m.activated = append(m.activated, specs...)
}

func (m *mockActivator) DrainActivated() []llm.ToolSpec {
	out := m.activated
	m.activated = nil
	return out
}

func makeSearchable() map[string]llm.ToolSpec {
	return map[string]llm.ToolSpec{
		"web_search": {
			Name:        "web_search",
			Description: strPtr("search the web"),
			JSONSchema:  map[string]any{"type": "object"},
		},
		"web_fetch": {
			Name:        "web_fetch",
			Description: strPtr("fetch a web page"),
			JSONSchema:  map[string]any{"type": "object"},
		},
		"exec_command": {
			Name:        "exec_command",
			Description: strPtr("run a shell command"),
			JSONSchema:  map[string]any{"type": "object"},
		},
	}
}

func strPtr(s string) *string { return &s }

func TestExactNameMatch(t *testing.T) {
	activator := &mockActivator{}
	pool := makeSearchable()
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return pool })

	result := exec.Execute(
		context.Background(),
		"search_tools",
		map[string]any{"queries": []any{"web_search"}},
		tools.ExecutionContext{},
		"call_1",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %s", result.Error.Message)
	}
	matched, ok := result.ResultJSON["count"]
	if !ok || matched.(int) != 1 {
		t.Fatalf("expected 1 match, got %v", result.ResultJSON)
	}
	if len(activator.activated) != 1 {
		t.Fatalf("expected 1 activated, got %d", len(activator.activated))
	}
	if activator.activated[0].Name != "web_search" {
		t.Fatalf("expected web_search activated, got %s", activator.activated[0].Name)
	}
}

func TestBatchQuery(t *testing.T) {
	activator := &mockActivator{}
	pool := makeSearchable()
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return pool })

	result := exec.Execute(
		context.Background(),
		"search_tools",
		map[string]any{"queries": []any{"web_search", "exec_command"}},
		tools.ExecutionContext{},
		"call_2",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %s", result.Error.Message)
	}
	if count := result.ResultJSON["count"].(int); count != 2 {
		t.Fatalf("expected 2 matches, got %d", count)
	}
	if len(activator.activated) != 2 {
		t.Fatalf("expected 2 activated, got %d", len(activator.activated))
	}
}

func TestSubstringMatch(t *testing.T) {
	activator := &mockActivator{}
	pool := makeSearchable()
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return pool })

	result := exec.Execute(
		context.Background(),
		"search_tools",
		map[string]any{"queries": []any{"web"}},
		tools.ExecutionContext{},
		"call_3",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %s", result.Error.Message)
	}
	count := result.ResultJSON["count"].(int)
	if count != 2 {
		t.Fatalf("expected 2 matches (web_search, web_fetch), got %d", count)
	}
}

func TestNoMatch(t *testing.T) {
	activator := &mockActivator{}
	pool := makeSearchable()
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return pool })

	result := exec.Execute(
		context.Background(),
		"search_tools",
		map[string]any{"queries": []any{"nonexistent_tool"}},
		tools.ExecutionContext{},
		"call_4",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %s", result.Error.Message)
	}
	matched, _ := result.ResultJSON["matched"].([]any)
	if len(matched) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matched))
	}
}

func TestInvalidArgs(t *testing.T) {
	activator := &mockActivator{}
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return nil })

	result := exec.Execute(
		context.Background(),
		"search_tools",
		map[string]any{},
		tools.ExecutionContext{},
		"call_5",
	)

	if result.Error == nil {
		t.Fatal("expected error for missing queries")
	}
	if result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected %s, got %s", errorArgsInvalid, result.Error.ErrorClass)
	}
}

func TestWildcardAll(t *testing.T) {
	activator := &mockActivator{}
	pool := makeSearchable()
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return pool })

	result := exec.Execute(
		context.Background(),
		"search_tools",
		map[string]any{"queries": []any{"*"}},
		tools.ExecutionContext{},
		"call_wildcard",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %s", result.Error.Message)
	}
	if count := result.ResultJSON["count"].(int); count != 3 {
		t.Fatalf("expected 3 matches (all), got %d", count)
	}
	if len(activator.activated) != 3 {
		t.Fatalf("expected 3 activated, got %d", len(activator.activated))
	}
}

func TestDuplicateSearchDoesNotReactivate(t *testing.T) {
	activator := &mockActivator{}
	pool := makeSearchable()
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return pool })

	// First search activates web_search
	exec.Execute(
		context.Background(),
		"search_tools",
		map[string]any{"queries": []any{"web_search"}},
		tools.ExecutionContext{},
		"call_a",
	)
	if len(activator.activated) != 1 {
		t.Fatalf("expected 1 activated after first search, got %d", len(activator.activated))
	}

	// Drain to simulate agent loop consuming
	activator.DrainActivated()

	// Second search for same tool should return it (schema info) but NOT re-activate
	result := exec.Execute(
		context.Background(),
		"search_tools",
		map[string]any{"queries": []any{"web_search"}},
		tools.ExecutionContext{},
		"call_b",
	)
	if result.Error != nil {
		t.Fatalf("unexpected error: %s", result.Error.Message)
	}
	if count := result.ResultJSON["count"].(int); count != 1 {
		t.Fatalf("expected 1 match in result, got %d", count)
	}
	if len(activator.activated) != 0 {
		t.Fatalf("expected 0 new activations, got %d", len(activator.activated))
	}
}

func TestSearchableMapNotMutated(t *testing.T) {
	activator := &mockActivator{}
	pool := makeSearchable()
	originalLen := len(pool)
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return pool })

	exec.Execute(
		context.Background(),
		"search_tools",
		map[string]any{"queries": []any{"*"}},
		tools.ExecutionContext{},
		"call_mut",
	)

	if len(pool) != originalLen {
		t.Fatalf("searchable pool was mutated: expected %d entries, got %d", originalLen, len(pool))
	}
}

func TestBuildCatalogPrompt(t *testing.T) {
	pool := makeSearchable()
	catalog := BuildCatalogPrompt(pool)

	if catalog == "" {
		t.Fatal("expected non-empty catalog")
	}
	if !contains(catalog, "web_search") || !contains(catalog, "exec_command") {
		t.Fatalf("catalog missing expected tools: %s", catalog)
	}
	if !contains(catalog, "<available_tools>") {
		t.Fatalf("catalog missing XML tags: %s", catalog)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
