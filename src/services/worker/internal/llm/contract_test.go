package llm

import (
	"encoding/json"
	"testing"

	"arkloop/services/shared/messagecontent"
)

func TestToolSpecToJSONWithAnnotations(t *testing.T) {
	desc := "A read-only tool"
	destructive := false
	spec := ToolSpec{
		Name:        "my_tool",
		Description: &desc,
		JSONSchema:  map[string]any{"type": "object"},
		Annotations: &ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: &destructive,
			Title:           "My Tool",
		},
	}
	result := spec.ToJSON()

	if result["name"] != "my_tool" {
		t.Fatalf("name: expected 'my_tool', got %v", result["name"])
	}
	annotations, ok := result["annotations"].(*ToolAnnotations)
	if !ok || annotations == nil {
		t.Fatal("expected annotations in result")
	}
	if annotations.ReadOnlyHint != true {
		t.Fatalf("ReadOnlyHint: expected true, got %v", annotations.ReadOnlyHint)
	}
	if annotations.Title != "My Tool" {
		t.Fatalf("Title: expected 'My Tool', got %q", annotations.Title)
	}
}

func TestToolSpecToJSONWithoutAnnotations(t *testing.T) {
	desc := "plain tool"
	spec := ToolSpec{
		Name:        "plain",
		Description: &desc,
		JSONSchema:  map[string]any{},
	}
	result := spec.ToJSON()
	if _, ok := result["annotations"]; ok {
		t.Fatal("expected no annotations key when nil")
	}
}

func TestBuildAssistantThreadContentJSON_RoundTripPreservesContinuityState(t *testing.T) {
	phase := "commentary"
	message := Message{
		Role:  "assistant",
		Phase: &phase,
		Content: []ContentPart{
			{Type: "thinking", Text: "deliberating", Signature: "sig_1"},
			{Type: messagecontent.PartTypeText, Text: "done"},
		},
	}

	raw, err := BuildAssistantThreadContentJSON(message)
	if err != nil {
		t.Fatalf("BuildAssistantThreadContentJSON failed: %v", err)
	}

	parsed, err := messagecontent.Parse(raw)
	if err != nil {
		t.Fatalf("messagecontent.Parse failed: %v", err)
	}
	if len(parsed.Parts) != 1 || parsed.Parts[0].Type != messagecontent.PartTypeText || parsed.Parts[0].Text != "done" {
		t.Fatalf("unexpected visible parts: %#v", parsed.Parts)
	}

	restored, err := AssistantMessageFromThreadContentJSON(raw)
	if err != nil {
		t.Fatalf("AssistantMessageFromThreadContentJSON failed: %v", err)
	}
	if restored == nil || restored.Phase == nil || *restored.Phase != "commentary" {
		t.Fatalf("unexpected restored phase: %#v", restored)
	}
	if len(restored.Content) != 2 {
		t.Fatalf("unexpected restored content len: %#v", restored.Content)
	}
	if restored.Content[0].Kind() != "thinking" || restored.Content[0].Signature != "sig_1" || restored.Content[0].Text != "deliberating" {
		t.Fatalf("unexpected restored thinking part: %#v", restored.Content[0])
	}
	if restored.Content[1].Text != "done" {
		t.Fatalf("unexpected restored visible part: %#v", restored.Content[1])
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if _, ok := payload["assistant_state"]; !ok {
		t.Fatalf("expected assistant_state envelope in %s", string(raw))
	}
}

func TestBuildIntermediateAssistantContentJSON_RoundTripPreservesContinuityState(t *testing.T) {
	message := Message{
		Role: "assistant",
		Content: []ContentPart{
			{Type: "thinking", Text: "tool plan", Signature: "sig_tool"},
			{Type: messagecontent.PartTypeText, Text: "calling tool"},
		},
	}
	raw, err := BuildIntermediateAssistantContentJSON(message, []ToolCall{{
		ToolCallID:    "call_1",
		ToolName:      "web_search",
		ArgumentsJSON: map[string]any{"query": "arkloop"},
	}})
	if err != nil {
		t.Fatalf("BuildIntermediateAssistantContentJSON failed: %v", err)
	}

	parsed, err := messagecontent.Parse(raw)
	if err != nil {
		t.Fatalf("messagecontent.Parse failed: %v", err)
	}
	if len(parsed.Parts) != 1 || parsed.Parts[0].Type != messagecontent.PartTypeText || parsed.Parts[0].Text != "calling tool" {
		t.Fatalf("unexpected visible parts: %#v", parsed.Parts)
	}

	restored, err := AssistantMessageFromThreadContentJSON(raw)
	if err != nil {
		t.Fatalf("AssistantMessageFromThreadContentJSON failed: %v", err)
	}
	if restored == nil || len(restored.Content) != 2 {
		t.Fatalf("unexpected restored content: %#v", restored)
	}
	if restored.Content[0].Kind() != "thinking" || restored.Content[0].Signature != "sig_tool" || restored.Content[0].Text != "tool plan" {
		t.Fatalf("unexpected restored thinking part: %#v", restored.Content[0])
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if _, ok := payload["assistant_state"]; !ok {
		t.Fatalf("expected assistant_state envelope in %s", string(raw))
	}
	if _, ok := payload["tool_calls"]; !ok {
		t.Fatalf("expected tool_calls in %s", string(raw))
	}
}

func TestMessageFromJSONMapAcceptsTypedContentParts(t *testing.T) {
	message := Message{Role: "assistant", Content: []ContentPart{
		{Type: "thinking", Text: "deliberating", Signature: "sig_1"},
		{Type: messagecontent.PartTypeText, Text: "done"},
	}}

	restored, err := MessageFromJSONMap(message.ToJSON())
	if err != nil {
		t.Fatalf("MessageFromJSONMap failed: %v", err)
	}
	if len(restored.Content) != 2 {
		t.Fatalf("unexpected content len: %#v", restored.Content)
	}
	if restored.Content[0].Kind() != "thinking" || restored.Content[0].Signature != "sig_1" || restored.Content[0].Text != "deliberating" {
		t.Fatalf("unexpected thinking part: %#v", restored.Content[0])
	}
}
