package subagentctl

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRepairUnclosedToolCalls_Empty(t *testing.T) {
	result := repairUnclosedToolCalls(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d", len(result))
	}
}

func TestRepairUnclosedToolCalls_NoToolUse(t *testing.T) {
	messages := []ContextSnapshotMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	result := repairUnclosedToolCalls(messages)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
}

func TestRepairUnclosedToolCalls_ClosedSpawn(t *testing.T) {
	assistantBlocks, _ := json.Marshal([]map[string]any{
		{"type": "text", "text": "spawning"},
		{"type": "tool_use", "id": "call_1", "name": "spawn_agent", "input": map[string]any{}},
	})
	toolBlocks, _ := json.Marshal([]map[string]any{
		{"type": "tool_result", "tool_use_id": "call_1", "content": "ok"},
	})
	messages := []ContextSnapshotMessage{
		{Role: "assistant", Content: "spawning", ContentJSON: assistantBlocks, CreatedAt: time.Now()},
		{Role: "tool", Content: "ok", ContentJSON: toolBlocks, CreatedAt: time.Now()},
	}
	result := repairUnclosedToolCalls(messages)
	if len(result) != 2 {
		t.Fatalf("expected 2 (already closed), got %d", len(result))
	}
}

func TestRepairUnclosedToolCalls_UnclosedSpawn(t *testing.T) {
	assistantBlocks, _ := json.Marshal([]map[string]any{
		{"type": "tool_use", "id": "call_2", "name": "spawn_agent", "input": map[string]any{}},
	})
	messages := []ContextSnapshotMessage{
		{Role: "user", Content: "do something"},
		{Role: "assistant", Content: "", ContentJSON: assistantBlocks, CreatedAt: time.Now()},
	}
	result := repairUnclosedToolCalls(messages)
	if len(result) != 3 {
		t.Fatalf("expected 3 (1 closure added), got %d", len(result))
	}
	closure := result[2]
	if closure.Role != "tool" {
		t.Errorf("closure role = %q, want tool", closure.Role)
	}
	var blocks []map[string]any
	if err := json.Unmarshal(closure.ContentJSON, &blocks); err != nil {
		t.Fatalf("parse closure content_json: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0]["tool_use_id"] != "call_2" {
		t.Errorf("tool_use_id = %v, want call_2", blocks[0]["tool_use_id"])
	}
}

func TestRepairUnclosedToolCalls_MultipleUnclosed(t *testing.T) {
	blocks, _ := json.Marshal([]map[string]any{
		{"type": "tool_use", "id": "c1", "name": "spawn_agent", "input": map[string]any{}},
		{"type": "tool_use", "id": "c2", "name": "send_input", "input": map[string]any{}},
	})
	messages := []ContextSnapshotMessage{
		{Role: "assistant", Content: "", ContentJSON: blocks, CreatedAt: time.Now()},
	}
	result := repairUnclosedToolCalls(messages)
	if len(result) != 3 {
		t.Fatalf("expected 3 (2 closures), got %d", len(result))
	}
	closedIDs := map[string]bool{}
	for _, msg := range result[1:] {
		var parsed []map[string]any
		if err := json.Unmarshal(msg.ContentJSON, &parsed); err != nil {
			t.Fatal(err)
		}
		if id, ok := parsed[0]["tool_use_id"].(string); ok {
			closedIDs[id] = true
		}
	}
	if !closedIDs["c1"] || !closedIDs["c2"] {
		t.Errorf("not all unclosed calls repaired: %v", closedIDs)
	}
}

func TestRepairUnclosedToolCalls_RepairsNonSpawnTools(t *testing.T) {
	blocks, _ := json.Marshal([]map[string]any{
		{"type": "tool_use", "id": "c1", "name": "web_search", "input": map[string]any{}},
	})
	messages := []ContextSnapshotMessage{
		{Role: "assistant", Content: "", ContentJSON: blocks, CreatedAt: time.Now()},
	}
	result := repairUnclosedToolCalls(messages)
	if len(result) != 2 {
		t.Fatalf("expected 2 (non-spawn also repaired), got %d", len(result))
	}
	if result[1].Role != "tool" {
		t.Errorf("closure role = %q, want tool", result[1].Role)
	}
}

func TestRepairUnclosedToolCalls_ToolRoleResult(t *testing.T) {
	assistantBlocks, _ := json.Marshal([]map[string]any{
		{"type": "tool_use", "id": "c1", "name": "wait_agent", "input": map[string]any{}},
	})
	toolResult, _ := json.Marshal(map[string]any{
		"tool_use_id": "c1",
		"content":     "done",
	})
	messages := []ContextSnapshotMessage{
		{Role: "assistant", Content: "", ContentJSON: assistantBlocks, CreatedAt: time.Now()},
		{Role: "tool", Content: "done", ContentJSON: toolResult, CreatedAt: time.Now()},
	}
	result := repairUnclosedToolCalls(messages)
	if len(result) != 2 {
		t.Fatalf("expected 2 (tool role result closes it), got %d", len(result))
	}
}

func TestContextSnapshotEffectiveRoutingFallsBackToRouting(t *testing.T) {
	snapshot := ContextSnapshot{
		Routing: &ContextSnapshotRouting{
			RouteID: "route-parent",
			Model:   "anthropic^claude-sonnet-4-5",
		},
	}

	routing := snapshot.EffectiveRouting()
	if routing.RouteID != "route-parent" {
		t.Fatalf("unexpected route_id: %#v", routing)
	}
	if routing.Model != "anthropic^claude-sonnet-4-5" {
		t.Fatalf("unexpected model: %#v", routing)
	}
}

func TestContextSnapshotEffectiveRoutingPrefersRuntime(t *testing.T) {
	snapshot := ContextSnapshot{
		Routing: &ContextSnapshotRouting{
			RouteID: "route-parent",
			Model:   "anthropic^claude-sonnet-4-5",
		},
		Runtime: ContextSnapshotRuntime{
			RouteID: "route-override",
			Model:   "openai^gpt-5",
		},
	}

	routing := snapshot.EffectiveRouting()
	if routing.RouteID != "route-override" {
		t.Fatalf("unexpected route_id: %#v", routing)
	}
	if routing.Model != "openai^gpt-5" {
		t.Fatalf("unexpected model: %#v", routing)
	}
}
