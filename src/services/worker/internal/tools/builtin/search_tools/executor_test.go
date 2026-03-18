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
		"visualize_read_me": {
			Name:        "visualize_read_me",
			Description: strPtr("load generative ui design rules"),
			JSONSchema:  map[string]any{"type": "object"},
		},
		"artifact_guidelines": {
			Name:        "artifact_guidelines",
			Description: strPtr("load artifact design rules"),
			JSONSchema:  map[string]any{"type": "object"},
		},
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
		"show_widget": {
			Name:        "show_widget",
			Description: strPtr("render a widget"),
			JSONSchema:  map[string]any{"type": "object"},
		},
	}
}

func strPtr(s string) *string { return &s }

func TestExactNameMatch(t *testing.T) {
	activator := &mockActivator{}
	pool := makeSearchable()
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return pool }, nil)

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
	if got := result.ResultJSON["activated_count"]; got != 1 {
		t.Fatalf("expected activated_count=1, got %v", got)
	}
	if got := result.ResultJSON["already_loaded_count"]; got != 0 {
		t.Fatalf("expected already_loaded_count=0, got %v", got)
	}
	if got := result.ResultJSON["already_active_count"]; got != 0 {
		t.Fatalf("expected already_active_count=0, got %v", got)
	}
	entries := matchedEntries(t, result.ResultJSON)
	if entries[0]["state"] != stateActivated {
		t.Fatalf("expected state=%s, got %v", stateActivated, entries[0]["state"])
	}
}

func TestBatchQuery(t *testing.T) {
	activator := &mockActivator{}
	pool := makeSearchable()
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return pool }, nil)

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

func TestDependencyToolAutoActivated(t *testing.T) {
	activator := &mockActivator{}
	pool := makeSearchable()
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return pool }, nil)

	result := exec.Execute(
		context.Background(),
		"search_tools",
		map[string]any{"queries": []any{"show_widget"}},
		tools.ExecutionContext{},
		"call_dep",
	)
	if result.Error != nil {
		t.Fatalf("unexpected error: %s", result.Error.Message)
	}
	if got := result.ResultJSON["count"]; got != 2 {
		t.Fatalf("expected count=2, got %v", got)
	}
	if len(activator.activated) != 2 {
		t.Fatalf("expected 2 activated, got %d", len(activator.activated))
	}
	entries := matchedEntries(t, result.ResultJSON)
	foundDependency := false
	for _, entry := range entries {
		if entry["name"] != "visualize_read_me" {
			continue
		}
		foundDependency = true
		if entry["auto_activated_by"] == nil {
			t.Fatalf("expected auto_activated_by metadata, got %#v", entry)
		}
	}
	if !foundDependency {
		t.Fatalf("expected visualize_read_me dependency in matched entries, got %#v", entries)
	}
}

func TestSubstringMatch(t *testing.T) {
	activator := &mockActivator{}
	pool := makeSearchable()
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return pool }, nil)

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

func TestSubstringMatchSkipsAlreadyActiveTools(t *testing.T) {
	activator := &mockActivator{}
	searchable := map[string]llm.ToolSpec{
		"web_fetch": {
			Name:        "web_fetch",
			Description: strPtr("fetch a web page"),
			JSONSchema:  map[string]any{"type": "object"},
		},
	}
	active := map[string]llm.ToolSpec{
		"web_search": {
			Name:        "web_search",
			Description: strPtr("search the web"),
			JSONSchema:  map[string]any{"type": "object"},
		},
	}
	exec := NewExecutor(
		activator,
		func() map[string]llm.ToolSpec { return searchable },
		func() map[string]llm.ToolSpec { return active },
	)

	result := exec.Execute(
		context.Background(),
		"search_tools",
		map[string]any{"queries": []any{"web"}},
		tools.ExecutionContext{},
		"call_substring",
	)
	if result.Error != nil {
		t.Fatalf("unexpected error: %s", result.Error.Message)
	}
	if got := result.ResultJSON["count"]; got != 1 {
		t.Fatalf("expected only searchable fuzzy matches, got %v", got)
	}
	entries := matchedEntries(t, result.ResultJSON)
	if entries[0]["name"] != "web_fetch" {
		t.Fatalf("expected web_fetch, got %v", entries[0]["name"])
	}
	if entries[0]["state"] != stateActivated {
		t.Fatalf("expected state=%s, got %v", stateActivated, entries[0]["state"])
	}
}

func TestNoMatch(t *testing.T) {
	activator := &mockActivator{}
	pool := makeSearchable()
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return pool }, nil)

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
	if got := result.ResultJSON["count"]; got != 0 {
		t.Fatalf("expected count=0, got %v", got)
	}
	if got := result.ResultJSON["activated_count"]; got != 0 {
		t.Fatalf("expected activated_count=0, got %v", got)
	}
	if got := result.ResultJSON["already_loaded_count"]; got != 0 {
		t.Fatalf("expected already_loaded_count=0, got %v", got)
	}
	if got := result.ResultJSON["already_active_count"]; got != 0 {
		t.Fatalf("expected already_active_count=0, got %v", got)
	}
}

func TestInvalidArgs(t *testing.T) {
	activator := &mockActivator{}
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return nil }, nil)

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
	active := map[string]llm.ToolSpec{
		"timeline_title": {
			Name:        "timeline_title",
			Description: strPtr("set timeline title"),
			JSONSchema:  map[string]any{"type": "object"},
		},
	}
	exec := NewExecutor(
		activator,
		func() map[string]llm.ToolSpec { return pool },
		func() map[string]llm.ToolSpec { return active },
	)

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
	if count := result.ResultJSON["count"].(int); count != 6 {
		t.Fatalf("expected 6 matches (all searchable), got %d", count)
	}
	if len(activator.activated) != 6 {
		t.Fatalf("expected 6 activated, got %d", len(activator.activated))
	}
	if got := result.ResultJSON["already_active_count"]; got != 0 {
		t.Fatalf("expected already_active_count=0, got %v", got)
	}
	entries := matchedEntries(t, result.ResultJSON)
	for _, entry := range entries {
		if entry["state"] != stateActivated {
			t.Fatalf("expected wildcard entries to be %s, got %v", stateActivated, entry["state"])
		}
	}
}

func TestActiveToolMatchReturnsAlreadyActive(t *testing.T) {
	activator := &mockActivator{}
	active := map[string]llm.ToolSpec{
		"web_search": {
			Name:        "web_search",
			Description: strPtr("search the web"),
			JSONSchema:  map[string]any{"type": "object"},
		},
	}
	exec := NewExecutor(
		activator,
		func() map[string]llm.ToolSpec { return map[string]llm.ToolSpec{} },
		func() map[string]llm.ToolSpec { return active },
	)

	result := exec.Execute(
		context.Background(),
		"search_tools",
		map[string]any{"queries": []any{"web_search"}},
		tools.ExecutionContext{},
		"call_active",
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %s", result.Error.Message)
	}
	if len(activator.activated) != 0 {
		t.Fatalf("expected no new activations, got %d", len(activator.activated))
	}
	entries := matchedEntries(t, result.ResultJSON)
	if len(entries) != 1 {
		t.Fatalf("expected 1 matched entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry["already_active"] != true {
		t.Fatalf("expected already_active flag, got %v", entry["already_active"])
	}
	if entry["state"] != stateAlreadyActive {
		t.Fatalf("expected state=%s, got %v", stateAlreadyActive, entry["state"])
	}
	if got := result.ResultJSON["already_active_count"]; got != 1 {
		t.Fatalf("expected already_active_count=1, got %v", got)
	}
	if got := result.ResultJSON["activated_count"]; got != 0 {
		t.Fatalf("expected activated_count=0, got %v", got)
	}
}

func TestDuplicateSearchDoesNotReactivate(t *testing.T) {
	activator := &mockActivator{}
	pool := makeSearchable()
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return pool }, nil)

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
	if got := result.ResultJSON["already_loaded_count"]; got != 1 {
		t.Fatalf("expected already_loaded_count=1, got %v", got)
	}
	entries := matchedEntries(t, result.ResultJSON)
	if entries[0]["state"] != stateAlreadyLoaded {
		t.Fatalf("expected state=%s, got %v", stateAlreadyLoaded, entries[0]["state"])
	}
	if entries[0]["already_loaded"] != true {
		t.Fatalf("expected already_loaded=true, got %v", entries[0]["already_loaded"])
	}
}

func TestSearchableMapNotMutated(t *testing.T) {
	activator := &mockActivator{}
	pool := makeSearchable()
	originalLen := len(pool)
	exec := NewExecutor(activator, func() map[string]llm.ToolSpec { return pool }, nil)

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
	if !contains(catalog, "same reasoning loop") {
		t.Fatalf("catalog should explain same-loop activation timing: %s", catalog)
	}
}

func TestStatusBreakdownWithMixedMatches(t *testing.T) {
	activator := &mockActivator{}
	searchable := map[string]llm.ToolSpec{
		"web_fetch": {
			Name:        "web_fetch",
			Description: strPtr("fetch a web page"),
			JSONSchema:  map[string]any{"type": "object"},
		},
	}
	active := map[string]llm.ToolSpec{
		"web_search": {
			Name:        "web_search",
			Description: strPtr("search the web"),
			JSONSchema:  map[string]any{"type": "object"},
		},
	}
	exec := NewExecutor(
		activator,
		func() map[string]llm.ToolSpec { return searchable },
		func() map[string]llm.ToolSpec { return active },
	)

	result := exec.Execute(
		context.Background(),
		"search_tools",
		map[string]any{"queries": []any{"web_search", "web_fetch"}},
		tools.ExecutionContext{},
		"call_mixed",
	)
	if result.Error != nil {
		t.Fatalf("unexpected error: %s", result.Error.Message)
	}
	if got := result.ResultJSON["count"]; got != 2 {
		t.Fatalf("expected count=2, got %v", got)
	}
	if got := result.ResultJSON["activated_count"]; got != 1 {
		t.Fatalf("expected activated_count=1, got %v", got)
	}
	if got := result.ResultJSON["already_active_count"]; got != 1 {
		t.Fatalf("expected already_active_count=1, got %v", got)
	}
	if got := result.ResultJSON["already_loaded_count"]; got != 0 {
		t.Fatalf("expected already_loaded_count=0, got %v", got)
	}

	entries := matchedEntries(t, result.ResultJSON)
	states := map[string]string{}
	for _, entry := range entries {
		name, _ := entry["name"].(string)
		state, _ := entry["state"].(string)
		states[name] = state
	}
	if states["web_fetch"] != stateActivated {
		t.Fatalf("expected web_fetch=%s, got %q", stateActivated, states["web_fetch"])
	}
	if states["web_search"] != stateAlreadyActive {
		t.Fatalf("expected web_search=%s, got %q", stateAlreadyActive, states["web_search"])
	}
}

func matchedEntries(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	if direct, ok := payload["matched"].([]map[string]any); ok {
		return direct
	}
	raw, ok := payload["matched"].([]any)
	if !ok {
		t.Fatalf("unexpected matched payload: %#v", payload["matched"])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("unexpected matched entry payload: %#v", item)
		}
		out = append(out, entry)
	}
	return out
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
