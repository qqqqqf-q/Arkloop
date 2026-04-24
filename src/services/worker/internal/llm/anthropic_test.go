package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func anthropicSSEBody(chunks []string) string {
	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString("data: ")
		sb.WriteString(c)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func TestToAnthropicMessages_ToolEnvelope(t *testing.T) {
	system, messages, err := toAnthropicMessages([]Message{
		{Role: "system", Content: []TextPart{{Text: "sys"}}},
		{
			Role:    "assistant",
			Content: []TextPart{{Text: ""}},
			ToolCalls: []ToolCall{
				{
					ToolCallID: "call_1",
					ToolName:   "web_search",
					ArgumentsJSON: map[string]any{
						"query": "hello",
					},
				},
			},
		},
		{
			Role: "tool",
			Content: []TextPart{{
				Text: `{"tool_call_id":"call_1","tool_name":"web_search","result":{"items":[{"title":"x"}]}}`,
			}},
		},
		{Role: "user", Content: []TextPart{{Text: "next"}}},
	})
	if err != nil {
		t.Fatalf("toAnthropicMessages failed: %v", err)
	}
	if len(system) != 1 || system[0]["text"] != "sys" {
		t.Fatalf("unexpected system: %#v", system)
	}
	if len(messages) != 3 {
		t.Fatalf("unexpected messages len: %d", len(messages))
	}

	assistant := messages[0]
	if assistant["role"] != "assistant" {
		t.Fatalf("unexpected assistant role: %#v", assistant["role"])
	}
	rawBlocks, ok := assistant["content"].([]map[string]any)
	if !ok || len(rawBlocks) != 1 {
		t.Fatalf("unexpected assistant content: %#v", assistant["content"])
	}
	if rawBlocks[0]["type"] != "tool_use" {
		t.Fatalf("unexpected tool_use block: %#v", rawBlocks[0])
	}
	if rawBlocks[0]["id"] != "call_1" || rawBlocks[0]["name"] != "web_search" {
		t.Fatalf("unexpected tool_use id/name: %#v", rawBlocks[0])
	}
	input, ok := rawBlocks[0]["input"].(map[string]any)
	if !ok || input["query"] != "hello" {
		t.Fatalf("unexpected tool_use input: %#v", rawBlocks[0]["input"])
	}

	toolResult := messages[1]
	if toolResult["role"] != "user" {
		t.Fatalf("unexpected tool_result wrapper role: %#v", toolResult["role"])
	}
	rawToolResults, ok := toolResult["content"].([]map[string]any)
	if !ok || len(rawToolResults) != 1 {
		t.Fatalf("unexpected tool_result wrapper content: %#v", toolResult["content"])
	}
	if rawToolResults[0]["type"] != "tool_result" {
		t.Fatalf("unexpected tool_result block: %#v", rawToolResults[0])
	}
	if rawToolResults[0]["tool_use_id"] != "call_1" {
		t.Fatalf("unexpected tool_use_id: %#v", rawToolResults[0]["tool_use_id"])
	}
	content, ok := rawToolResults[0]["content"].(string)
	if !ok {
		t.Fatalf("unexpected tool_result content: %#v", rawToolResults[0]["content"])
	}
	var parsedContent map[string]any
	if err := json.Unmarshal([]byte(content), &parsedContent); err != nil {
		t.Fatalf("tool_result content not json: %v", err)
	}
	if _, ok := parsedContent["items"]; !ok {
		t.Fatalf("expected items in tool_result content, got %#v", parsedContent)
	}

	user := messages[2]
	if user["role"] != "user" {
		t.Fatalf("unexpected user role: %#v", user["role"])
	}
}

func TestToAnthropicTools_SortsByCanonicalName(t *testing.T) {
	tools := toAnthropicTools([]ToolSpec{
		{Name: "web_search", JSONSchema: map[string]any{"type": "object"}},
		{Name: "read", JSONSchema: map[string]any{"type": "object"}},
		{Name: "browser", JSONSchema: map[string]any{"type": "object"}},
	})

	if len(tools) != 3 {
		t.Fatalf("unexpected tools len: %d", len(tools))
	}

	got := []string{
		tools[0]["name"].(string),
		tools[1]["name"].(string),
		tools[2]["name"].(string),
	}
	want := []string{"browser", "read", "web_search"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected tool order: got=%v want=%v", got, want)
		}
	}
}

func TestToAnthropicTools_EmitsCacheControlFromHint(t *testing.T) {
	tools := toAnthropicTools([]ToolSpec{
		{
			Name:       "web_search",
			JSONSchema: map[string]any{"type": "object"},
			CacheHint: &CacheHint{
				Action: CacheHintActionWrite,
				Scope:  "global",
			},
		},
	})
	if len(tools) != 1 {
		t.Fatalf("unexpected tools len: %d", len(tools))
	}
	cc, ok := tools[0]["cache_control"].(map[string]any)
	if !ok {
		t.Fatalf("expected cache_control, got %#v", tools[0])
	}
	if cc["type"] != "ephemeral" {
		t.Fatalf("unexpected cache_control type: %#v", cc)
	}
	if cc["scope"] != "global" {
		t.Fatalf("unexpected cache_control scope: %#v", cc)
	}
}

func TestToAnthropicTools_OnlyAnnotatedToolCarriesCacheControl(t *testing.T) {
	tools := toAnthropicTools([]ToolSpec{
		{Name: "read", JSONSchema: map[string]any{"type": "object"}},
		{
			Name:       "web_search",
			JSONSchema: map[string]any{"type": "object"},
			CacheHint: &CacheHint{
				Action: CacheHintActionWrite,
				Scope:  "global",
			},
		},
	})
	if len(tools) != 2 {
		t.Fatalf("unexpected tools len: %d", len(tools))
	}
	if _, ok := tools[0]["cache_control"]; ok {
		t.Fatalf("did not expect cache_control on first tool: %#v", tools[0])
	}
	if _, ok := tools[1]["cache_control"]; !ok {
		t.Fatalf("expected cache_control on annotated tool: %#v", tools[1])
	}
}

func TestToAnthropicMessagesWithPlan_MessageCacheAndReferences(t *testing.T) {
	system, messages, err := toAnthropicMessagesWithPlan([]Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{
					ToolCallID:    "call_1",
					ToolName:      "web_search",
					ArgumentsJSON: map[string]any{"query": "hello"},
				},
			},
		},
		{
			Role: "tool",
			Content: []TextPart{{
				Text: `{"tool_call_id":"call_1","tool_name":"web_search","result":{"ok":true}}`,
			}},
		},
		{
			Role:    "user",
			Content: []TextPart{{Text: "next turn"}},
		},
	}, &PromptPlan{
		SystemBlocks: []PromptPlanBlock{
			{
				Name:          "persona",
				Target:        PromptTargetSystemPrefix,
				Role:          "system",
				Text:          "stable system",
				Stability:     CacheStabilityStablePrefix,
				CacheEligible: true,
			},
		},
		MessageCache: MessageCachePlan{
			Enabled:                   true,
			MarkerMessageIndex:        2,
			ToolResultCacheCutIndex:   2,
			ToolResultCacheReferences: true,
			NewCacheEdits: &PromptCacheEditsBlock{
				UserMessageIndex: 2,
				Edits: []PromptCacheEdit{
					{Type: CacheHintActionDelete, CacheReference: "call_legacy"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("toAnthropicMessagesWithPlan failed: %v", err)
	}
	if len(system) != 1 {
		t.Fatalf("unexpected system blocks: %#v", system)
	}
	if _, ok := system[0]["cache_control"].(map[string]any); !ok {
		t.Fatalf("expected system cache_control: %#v", system[0])
	}
	if len(messages) != 3 {
		t.Fatalf("unexpected messages len: %d", len(messages))
	}

	toolWrapper := messages[1]
	toolBlocks, ok := toolWrapper["content"].([]map[string]any)
	if !ok || len(toolBlocks) == 0 {
		t.Fatalf("unexpected tool wrapper content: %#v", toolWrapper["content"])
	}
	toolResult := toolBlocks[0]
	if toolResult["type"] != "tool_result" {
		t.Fatalf("expected tool_result block: %#v", toolResult)
	}
	if toolResult["cache_reference"] != "call_1" {
		t.Fatalf("expected cache_reference on tool_result, got %#v", toolResult)
	}

	lastUser := messages[2]
	lastBlocks, ok := lastUser["content"].([]map[string]any)
	if !ok || len(lastBlocks) < 2 {
		t.Fatalf("unexpected last user content: %#v", lastUser["content"])
	}
	textBlock := lastBlocks[0]
	cc, ok := textBlock["cache_control"].(map[string]any)
	if !ok || cc["type"] != "ephemeral" {
		t.Fatalf("expected message cache marker on text block, got %#v", textBlock)
	}
	cacheEdits := lastBlocks[len(lastBlocks)-1]
	if cacheEdits["type"] != "cache_edits" {
		t.Fatalf("expected cache_edits block, got %#v", cacheEdits)
	}
	rawEdits, ok := cacheEdits["edits"].([]map[string]any)
	if !ok || len(rawEdits) != 1 {
		t.Fatalf("unexpected cache_edits payload: %#v", cacheEdits["edits"])
	}
	if rawEdits[0]["cache_reference"] != "call_legacy" {
		t.Fatalf("unexpected cache edit reference: %#v", rawEdits[0])
	}
}

func TestToAnthropicMessagesWithPlan_MessageCacheMarkerOnTrailingToolResult(t *testing.T) {
	_, messages, err := toAnthropicMessagesWithPlan([]Message{
		{
			Role: "assistant",
			Content: []TextPart{{
				Text: "正在调用工具",
			}},
			ToolCalls: []ToolCall{
				{
					ToolCallID:    "call_1",
					ToolName:      "web_search",
					ArgumentsJSON: map[string]any{"query": "hello"},
				},
			},
		},
		{
			Role: "tool",
			Content: []TextPart{{
				Text: `{"tool_call_id":"call_1","tool_name":"web_search","result":{"ok":true}}`,
			}},
		},
	}, &PromptPlan{
		MessageCache: MessageCachePlan{
			Enabled:                 true,
			MarkerMessageIndex:      1,
			ToolResultCacheCutIndex: 1,
		},
	})
	if err != nil {
		t.Fatalf("toAnthropicMessagesWithPlan failed: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("unexpected messages len: %d", len(messages))
	}

	lastUser := messages[1]
	lastBlocks, ok := lastUser["content"].([]map[string]any)
	if !ok || len(lastBlocks) != 1 {
		t.Fatalf("unexpected trailing tool_result wrapper: %#v", lastUser["content"])
	}
	cc, ok := lastBlocks[0]["cache_control"].(map[string]any)
	if !ok || cc["type"] != "ephemeral" {
		t.Fatalf("expected message cache marker on trailing tool_result, got %#v", lastBlocks[0])
	}
}

func TestAnthropicSystemBlocksFromPlan_MergesContiguousCacheScopes(t *testing.T) {
	blocks := anthropicSystemBlocksFromPlan(&PromptPlan{
		SystemBlocks: []PromptPlanBlock{
			{
				Name:          "persona",
				Target:        PromptTargetSystemPrefix,
				Role:          "system",
				Text:          "stable one",
				Stability:     CacheStabilityStablePrefix,
				CacheEligible: true,
			},
			{
				Name:          "security",
				Target:        PromptTargetSystemPrefix,
				Role:          "system",
				Text:          "stable two",
				Stability:     CacheStabilityStablePrefix,
				CacheEligible: true,
			},
			{
				Name:          "skills",
				Target:        PromptTargetSystemPrefix,
				Role:          "system",
				Text:          "session one",
				Stability:     CacheStabilitySessionPrefix,
				CacheEligible: true,
			},
			{
				Name:          "notebook",
				Target:        PromptTargetSystemPrefix,
				Role:          "system",
				Text:          "session two",
				Stability:     CacheStabilitySessionPrefix,
				CacheEligible: true,
			},
		},
	})
	if len(blocks) != 2 {
		t.Fatalf("expected 2 merged system blocks, got %#v", blocks)
	}
	if blocks[0]["text"] != "stable one\n\nstable two" {
		t.Fatalf("unexpected merged stable text: %#v", blocks[0])
	}
	cc0, ok := blocks[0]["cache_control"].(map[string]any)
	if !ok || cc0["scope"] != "global" {
		t.Fatalf("unexpected stable cache_control: %#v", blocks[0])
	}
	if blocks[1]["text"] != "session one\n\nsession two" {
		t.Fatalf("unexpected merged session text: %#v", blocks[1])
	}
	cc1, ok := blocks[1]["cache_control"].(map[string]any)
	if !ok || cc1["scope"] != "org" {
		t.Fatalf("unexpected session cache_control: %#v", blocks[1])
	}
}

func TestAnthropicGateway_Stream_PreflightOversizeSkipsHTTP(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})

	var got []StreamEvent
	err := gateway.Stream(context.Background(), Request{
		Model: "claude-test",
		Messages: []Message{{
			Role:    "user",
			Content: []TextPart{{Text: strings.Repeat("x", RequestPayloadLimitBytes+1024)}},
		}},
	}, func(ev StreamEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected no HTTP request, got %d", calls)
	}
	failed, ok := got[len(got)-1].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed, got %T", got[len(got)-1])
	}
	if failed.Error.Details["status_code"] != http.StatusRequestEntityTooLarge {
		t.Fatalf("unexpected details: %#v", failed.Error.Details)
	}
	if failed.Error.Details["oversize_phase"] != OversizePhasePreflight {
		t.Fatalf("unexpected phase: %#v", failed.Error.Details)
	}
	if failed.Error.Details["network_attempted"] != false {
		t.Fatalf("unexpected network_attempted: %#v", failed.Error.Details)
	}
}

func TestAnthropicGateway_Stream_Provider413AddsOversizeDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"type":"error","error":{"type":"request_too_large","message":"too large"}}`, http.StatusRequestEntityTooLarge)
	}))
	defer server.Close()

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})

	var got []StreamEvent
	err := gateway.Stream(context.Background(), Request{
		Model: "claude-test",
		Messages: []Message{{
			Role:    "user",
			Content: []TextPart{{Text: "hello"}},
		}},
	}, func(ev StreamEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	failed, ok := got[len(got)-1].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed, got %T", got[len(got)-1])
	}
	if failed.Error.Details["status_code"] != http.StatusRequestEntityTooLarge {
		t.Fatalf("unexpected details: %#v", failed.Error.Details)
	}
	if failed.Error.Details["oversize_phase"] != OversizePhaseProvider {
		t.Fatalf("unexpected phase: %#v", failed.Error.Details)
	}
	if failed.Error.Details["network_attempted"] != true {
		t.Fatalf("unexpected network_attempted: %#v", failed.Error.Details)
	}
}

func TestToAnthropicMessages_PartialToolResultsStripUnmatchedToolUse(t *testing.T) {
	_, messages, err := toAnthropicMessages([]Message{
		{
			Role:    "assistant",
			Content: []TextPart{{Text: "working"}},
			ToolCalls: []ToolCall{
				{
					ToolCallID:    "call_1",
					ToolName:      "telegram_react",
					ArgumentsJSON: map[string]any{"emoji": "❤️"},
				},
				{
					ToolCallID:    "call_2",
					ToolName:      "telegram_reply",
					ArgumentsJSON: map[string]any{"reply_to_message_id": "42"},
				},
			},
		},
		{
			Role: "tool",
			Content: []TextPart{{
				Text: `{"tool_call_id":"call_2","tool_name":"telegram_reply","result":{"ok":true}}`,
			}},
		},
	})
	if err != nil {
		t.Fatalf("toAnthropicMessages failed: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("unexpected messages len: %d", len(messages))
	}
	assistant := messages[0]
	blocks, ok := assistant["content"].([]map[string]any)
	if !ok {
		t.Fatalf("unexpected assistant content: %#v", assistant["content"])
	}
	if len(blocks) != 2 {
		t.Fatalf("expected text + surviving tool_use blocks, got %#v", blocks)
	}
	if blocks[0]["type"] != "text" || blocks[1]["type"] != "tool_use" {
		t.Fatalf("unexpected assistant blocks: %#v", blocks)
	}
	if blocks[1]["id"] != "call_2" {
		t.Fatalf("expected only matched tool_use to survive, got %#v", blocks[1])
	}

	toolResult := messages[1]
	rawToolResults, ok := toolResult["content"].([]map[string]any)
	if !ok || len(rawToolResults) != 1 {
		t.Fatalf("unexpected tool_result wrapper content: %#v", toolResult["content"])
	}
	if rawToolResults[0]["tool_use_id"] != "call_2" {
		t.Fatalf("expected matched tool result, got %#v", rawToolResults[0])
	}
}

func TestToAnthropicMessages_AssistantThinkingBlocksPreserved(t *testing.T) {
	system, messages, err := toAnthropicMessages([]Message{
		{
			Role: "assistant",
			Content: []ContentPart{
				{Type: "thinking", Text: "deliberating", Signature: "sig_1"},
				{Type: "text", Text: "done"},
			},
		},
	})
	if err != nil {
		t.Fatalf("toAnthropicMessages failed: %v", err)
	}
	if len(system) != 0 {
		t.Fatalf("unexpected system blocks: %#v", system)
	}
	if len(messages) != 1 {
		t.Fatalf("unexpected messages len: %d", len(messages))
	}
	blocks, ok := messages[0]["content"].([]map[string]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("unexpected assistant blocks: %#v", messages[0]["content"])
	}
	if blocks[0]["type"] != "thinking" || blocks[0]["thinking"] != "deliberating" || blocks[0]["signature"] != "sig_1" {
		t.Fatalf("unexpected thinking block: %#v", blocks[0])
	}
	if blocks[1]["type"] != "text" || blocks[1]["text"] != "done" {
		t.Fatalf("unexpected text block: %#v", blocks[1])
	}
}

func TestToAnthropicMessages_AssistantUnsignedThinkingDropped(t *testing.T) {
	system, messages, err := toAnthropicMessages([]Message{
		{
			Role: "assistant",
			Content: []ContentPart{
				{Type: "thinking", Text: "deliberating"},
			},
			ToolCalls: []ToolCall{
				{
					ToolCallID:    "call_1",
					ToolName:      "web_search",
					ArgumentsJSON: map[string]any{"query": "hello"},
				},
			},
		},
		{
			Role: "tool",
			Content: []TextPart{{
				Text: `{"tool_call_id":"call_1","tool_name":"web_search","result":{"ok":true}}`,
			}},
		},
	})
	if err != nil {
		t.Fatalf("toAnthropicMessages failed: %v", err)
	}
	if len(system) != 0 {
		t.Fatalf("unexpected system blocks: %#v", system)
	}
	if len(messages) != 2 {
		t.Fatalf("unexpected messages len: %d", len(messages))
	}
	blocks, ok := messages[0]["content"].([]map[string]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("unexpected assistant blocks: %#v", messages[0]["content"])
	}
	if blocks[0]["type"] != "tool_use" {
		t.Fatalf("expected unsigned thinking to be dropped, got %#v", blocks[0])
	}
	if _, ok := blocks[0]["signature"]; ok {
		t.Fatalf("tool_use block should not carry signature: %#v", blocks[0])
	}
}

func TestParseAnthropicMessage_ToolUse(t *testing.T) {
	body := []byte(`{
  "id":"msg_test",
  "type":"message",
  "role":"assistant",
  "content":[
    {"type":"text","text":"ok"},
    {"type":"tool_use","id":"call_1","name":"web_search","input":{"query":"hello"}}
  ]
}`)

	content, _, toolCalls, err := parseAnthropicMessage(body)
	if err != nil {
		t.Fatalf("parseAnthropicMessage failed: %v", err)
	}
	if content != "ok" {
		t.Fatalf("unexpected content: %q", content)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].ToolCallID != "call_1" || toolCalls[0].ToolName != "web_search" {
		t.Fatalf("unexpected tool call: %#v", toolCalls[0])
	}
	if toolCalls[0].ArgumentsJSON["query"] != "hello" {
		t.Fatalf("unexpected tool call args: %#v", toolCalls[0].ArgumentsJSON)
	}
}

func TestAnthropicGateway_Stream_ToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSSEBody([]string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"web_search","input":{}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"hello\"}"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_stop"}`,
		})))
	}))
	t.Cleanup(server.Close)

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		EmitDebugEvents: false,
	})

	events := []StreamEvent{}
	err := gateway.Stream(context.Background(), Request{
		Model: "claude-test",
		Messages: []Message{
			{Role: "user", Content: []TextPart{{Text: "hi"}}},
		},
		Tools: []ToolSpec{
			{Name: "web_search", JSONSchema: map[string]any{"type": "object"}},
		},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	var gotDelta *ToolCallArgumentDelta
	var gotCall *ToolCall
	for _, item := range events {
		switch typed := item.(type) {
		case ToolCallArgumentDelta:
			copy := typed
			gotDelta = &copy
		case ToolCall:
			copy := typed
			gotCall = &copy
		}
	}
	if gotDelta == nil {
		t.Fatalf("expected tool call delta event, got %d events", len(events))
	}
	if gotDelta.ToolCallID != "call_1" || gotDelta.ToolName != "web_search" || gotDelta.ArgumentsDelta != `{"query":"hello"}` {
		t.Fatalf("unexpected tool call delta: %#v", gotDelta)
	}
	if gotCall == nil {
		t.Fatalf("expected tool call event, got %d events", len(events))
	}
	if gotCall.ToolCallID != "call_1" || gotCall.ToolName != "web_search" || gotCall.ArgumentsJSON["query"] != "hello" {
		t.Fatalf("unexpected tool call: %#v", gotCall)
	}

	if _, ok := events[len(events)-1].(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
}

func TestAnthropicGateway_Stream_ThinkingSignaturePreservedOnCompletion(t *testing.T) {
	reader := strings.NewReader(anthropicSSEBody([]string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"plan"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" more"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_1"}}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":"done"}}`,
		`{"type":"message_stop"}`,
	}))

	gateway := &AnthropicGateway{}
	var events []StreamEvent
	err := gateway.streamAnthropicSSE(context.Background(), reader, "test", func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("streamAnthropicSSE failed: %v", err)
	}

	last, ok := events[len(events)-1].(StreamRunCompleted)
	if !ok {
		t.Fatalf("expected StreamRunCompleted, got %T", events[len(events)-1])
	}
	if last.AssistantMessage == nil || len(last.AssistantMessage.Content) != 2 {
		t.Fatalf("unexpected assistant message: %#v", last.AssistantMessage)
	}
	if last.AssistantMessage.Content[0].Kind() != "thinking" || last.AssistantMessage.Content[0].Text != "plan more" || last.AssistantMessage.Content[0].Signature != "sig_1" {
		t.Fatalf("unexpected thinking part: %#v", last.AssistantMessage.Content[0])
	}
	if last.AssistantMessage.Content[1].Text != "done" {
		t.Fatalf("unexpected text part: %#v", last.AssistantMessage.Content[1])
	}
}

func TestNewAnthropicGateway_NormalizesMiniMaxBaseURL(t *testing.T) {
	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test",
		BaseURL: "https://api.minimaxi.com/anthropic",
	})

	if gateway.cfg.BaseURL != "https://api.minimaxi.com/anthropic" {
		t.Fatalf("unexpected base url: %q", gateway.cfg.BaseURL)
	}
}

func TestAnthropicGateway_Stream_DebugChunk_NotTruncated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSSEBody([]string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"ok"}}`,
			`{"type":"message_stop"}`,
		})))
	}))
	t.Cleanup(server.Close)

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		EmitDebugEvents: true,
	})

	var chunks []StreamLlmResponseChunk
	_ = gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		if c, ok := ev.(StreamLlmResponseChunk); ok {
			chunks = append(chunks, c)
		}
		return nil
	})

	if len(chunks) == 0 {
		t.Fatal("expected at least one debug chunk")
	}
	// body is well under MaxResponseBytes, should not be marked as truncated
	if chunks[0].Truncated {
		t.Fatalf("expected truncated=false for small body, got true")
	}
}

func TestAnthropicGateway_Stream_DebugChunk_Truncated(t *testing.T) {
	largeText := strings.Repeat("x", anthropicMaxDebugChunkBytes+128)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		payload, _ := json.Marshal(map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "text",
				"text": largeText,
			},
		})
		_, _ = w.Write([]byte(anthropicSSEBody([]string{
			string(payload),
			`{"type":"message_stop"}`,
		})))
	}))
	t.Cleanup(server.Close)

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		EmitDebugEvents: true,
	})

	var chunks []StreamLlmResponseChunk
	_ = gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		if c, ok := ev.(StreamLlmResponseChunk); ok {
			chunks = append(chunks, c)
		}
		return nil
	})

	if len(chunks) == 0 {
		t.Fatal("expected at least one debug chunk")
	}
	if !chunks[0].Truncated {
		t.Fatalf("expected truncated=true for oversized debug chunk, got false")
	}
}

func TestAnthropicGateway_Stream_TextStartWithoutDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSSEBody([]string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_stop"}`,
		})))
	}))
	t.Cleanup(server.Close)

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
	})

	var deltas []string
	err := gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		if delta, ok := ev.(StreamMessageDelta); ok {
			deltas = append(deltas, delta.ContentDelta)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}
	if strings.Join(deltas, "") != "hello" {
		t.Fatalf("unexpected text deltas: %#v", deltas)
	}
}

func TestAnthropicGateway_Stream_DoesNotDuplicateTextStartAndDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSSEBody([]string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_stop"}`,
		})))
	}))
	t.Cleanup(server.Close)

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
	})

	var deltas []string
	var completed *Message
	err := gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		if delta, ok := ev.(StreamMessageDelta); ok {
			deltas = append(deltas, delta.ContentDelta)
		}
		if done, ok := ev.(StreamRunCompleted); ok {
			completed = done.AssistantMessage
		}
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}
	if strings.Join(deltas, "") != "hello" {
		t.Fatalf("unexpected text deltas: %#v", deltas)
	}
	if completed == nil || len(completed.Content) != 1 || completed.Content[0].Text != "hello" {
		t.Fatalf("unexpected completed assistant message: %#v", completed)
	}
}

func TestAnthropicGateway_Stream_PreservesNonDuplicateTextDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSSEBody([]string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"he"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"llo"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_stop"}`,
		})))
	}))
	t.Cleanup(server.Close)

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
	})

	var deltas []string
	var completed *Message
	err := gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		if delta, ok := ev.(StreamMessageDelta); ok {
			deltas = append(deltas, delta.ContentDelta)
		}
		if done, ok := ev.(StreamRunCompleted); ok {
			completed = done.AssistantMessage
		}
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}
	if strings.Join(deltas, "") != "hello" {
		t.Fatalf("unexpected text deltas: %#v", deltas)
	}
	if completed == nil || len(completed.Content) != 1 || completed.Content[0].Text != "hello" {
		t.Fatalf("unexpected completed assistant message: %#v", completed)
	}
}

func TestAnthropicGateway_Stream_ErrorMessageExtracted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	t.Cleanup(server.Close)

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
	})

	var events []StreamEvent
	err := gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	var gotFailed *StreamRunFailed
	for _, ev := range events {
		if f, ok := ev.(StreamRunFailed); ok {
			copied := f
			gotFailed = &copied
			break
		}
	}
	if gotFailed == nil {
		t.Fatal("expected StreamRunFailed")
	}
	if gotFailed.Error.Message != "invalid x-api-key" {
		t.Fatalf("unexpected error message: %q", gotFailed.Error.Message)
	}
	if gotFailed.Error.Details["anthropic_error_type"] != "authentication_error" {
		t.Fatalf("unexpected anthropic_error_type: %v", gotFailed.Error.Details)
	}
	if gotFailed.Error.Details["status_code"] != http.StatusUnauthorized {
		t.Fatalf("unexpected status_code: %v", gotFailed.Error.Details)
	}
}

func TestAnthropicGateway_Stream_AdvancedJSON_Merged(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`))
	}))
	t.Cleanup(server.Close)

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
		AdvancedJSON: map[string]any{
			"stop_sequences": []any{"STOP"},
			"metadata":       map[string]any{"user_id": "u1"},
		},
	})

	_ = gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error { return nil })

	var body map[string]any
	if err := json.Unmarshal(capturedBody, &body); err != nil {
		t.Fatalf("request body not valid json: %v", err)
	}
	if body["stop_sequences"] == nil {
		t.Fatalf("expected stop_sequences in request, got: %v", body)
	}
	if body["metadata"] == nil {
		t.Fatalf("expected metadata in request, got: %v", body)
	}
}

func TestAnthropicGateway_Stream_AdvancedJSON_VersionAndHeaderApplied(t *testing.T) {
	var capturedBody []byte
	var capturedVersion string
	var capturedBeta string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedVersion = r.Header.Get("anthropic-version")
		capturedBeta = r.Header.Get("anthropic-beta")
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`))
	}))
	t.Cleanup(server.Close)

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
		AdvancedJSON: map[string]any{
			"anthropic_version": "2024-01-01",
			"extra_headers": map[string]any{
				"anthropic-beta": "prompt-caching-2024-07-31",
			},
			"metadata": map[string]any{"user_id": "u1"},
		},
	})

	_ = gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error { return nil })

	if capturedVersion != "2024-01-01" {
		t.Fatalf("expected anthropic-version overridden, got %q", capturedVersion)
	}
	if capturedBeta != "prompt-caching-2024-07-31" {
		t.Fatalf("expected anthropic-beta header, got %q", capturedBeta)
	}

	var body map[string]any
	if err := json.Unmarshal(capturedBody, &body); err != nil {
		t.Fatalf("request body not valid json: %v", err)
	}
	if _, ok := body["anthropic_version"]; ok {
		t.Fatalf("anthropic_version should not be merged into body: %v", body)
	}
	if _, ok := body["extra_headers"]; ok {
		t.Fatalf("extra_headers should not be merged into body: %v", body)
	}
	if body["metadata"] == nil {
		t.Fatalf("expected metadata in body, got: %v", body)
	}
}

func TestAnthropicGateway_Stream_DefaultMaxTokensApplied(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`))
	}))
	t.Cleanup(server.Close)

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
	})

	_ = gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error { return nil })

	var body map[string]any
	if err := json.Unmarshal(capturedBody, &body); err != nil {
		t.Fatalf("request body not valid json: %v", err)
	}
	if got, _ := body["max_tokens"].(float64); got != float64(defaultAnthropicMaxTokens) {
		t.Fatalf("expected max_tokens=%d, got %#v", defaultAnthropicMaxTokens, body["max_tokens"])
	}
}

func TestAnthropicGateway_Stream_AdvancedJSON_RejectsInvalidHeaderKey(t *testing.T) {
	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test",
		BaseURL: "http://127.0.0.1:0",
		AdvancedJSON: map[string]any{
			"extra_headers": map[string]any{
				"x-custom": "v",
			},
		},
	})

	var events []StreamEvent
	err := gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected StreamRunFailed event, got none")
	}
	failed, ok := events[0].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed, got %T", events[0])
	}
	if failed.Error.Message != "advanced_json.extra_headers only supports anthropic-beta" {
		t.Fatalf("unexpected error message: %q", failed.Error.Message)
	}
	if failed.Error.Details["invalid_header"] != "x-custom" {
		t.Fatalf("expected invalid_header=x-custom, got: %v", failed.Error.Details)
	}
}

func TestAnthropicGateway_Stream_AdvancedJSON_CannotEnableStream(t *testing.T) {
	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:       "test",
		BaseURL:      "http://127.0.0.1:0", // no real connection needed
		AdvancedJSON: map[string]any{"stream": true},
	})

	var events []StreamEvent
	err := gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected StreamRunFailed event, got none")
	}
	failed, ok := events[0].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed, got %T", events[0])
	}
	if failed.Error.ErrorClass != ErrorClassInternalError {
		t.Fatalf("unexpected error class: %s", failed.Error.ErrorClass)
	}
	if failed.Error.Details["denied_key"] != "stream" {
		t.Fatalf("expected denied_key=stream, got: %v", failed.Error.Details)
	}
}

func TestAnthropicGateway_Stream_AdvancedJSON_CannotInjectToolsWhenRequestHasNone(t *testing.T) {
	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:       "test",
		BaseURL:      "http://127.0.0.1:0",
		AdvancedJSON: map[string]any{"tools": []any{map[string]any{"name": "evil"}}},
	})

	var events []StreamEvent
	err := gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
		// Tools is empty, expect advanced_json cannot inject
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected StreamRunFailed event, got none")
	}
	failed, ok := events[0].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed, got %T", events[0])
	}
	if failed.Error.Details["denied_key"] != "tools" {
		t.Fatalf("expected denied_key=tools, got: %v", failed.Error.Details)
	}
}

func TestAnthropicThinkingBudget(t *testing.T) {
	cases := map[string]int{
		"enabled":      defaultAnthropicThinkingBudget,
		"minimal":      anthropicMinThinkingBudget,
		"low":          anthropicLowThinkingBudget,
		"medium":       defaultAnthropicThinkingBudget,
		"high":         anthropicHighThinkingBudget,
		"max":          anthropicMaxThinkingBudget,
		"xhigh":        anthropicMaxThinkingBudget,
		"extra-high":   anthropicMaxThinkingBudget,
		" EXTRA HIGH ": anthropicMaxThinkingBudget,
	}

	for input, want := range cases {
		got, ok := anthropicThinkingBudget(input)
		if !ok || got != want {
			t.Fatalf("mode %q => (%d, %v), want (%d, true)", input, got, ok, want)
		}
	}

	if _, ok := anthropicThinkingBudget("auto"); ok {
		t.Fatal("auto should not force a thinking budget")
	}
}

func TestAnthropicThinkingDisabled(t *testing.T) {
	for _, input := range []string{"disabled", "none", "off"} {
		if !anthropicThinkingDisabled(input) {
			t.Fatalf("%q should disable thinking", input)
		}
	}
	if anthropicThinkingDisabled("enabled") {
		t.Fatal("enabled should not disable thinking")
	}
}

func TestApplyAnthropicReasoningModeUsesMappedBudget(t *testing.T) {
	payload := map[string]any{"max_tokens": 2048}
	applyAnthropicReasoningMode(payload, "high")

	thinking, ok := payload["thinking"].(map[string]any)
	if !ok {
		t.Fatal("expected thinking payload")
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("thinking.type = %#v, want enabled", thinking["type"])
	}
	if thinking["budget_tokens"] != anthropicHighThinkingBudget {
		t.Fatalf("thinking.budget_tokens = %#v, want %d", thinking["budget_tokens"], anthropicHighThinkingBudget)
	}
	if got := anyToInt(payload["max_tokens"]); got <= anthropicHighThinkingBudget {
		t.Fatalf("max_tokens should be raised above budget, got %d", got)
	}
}

func TestApplyAnthropicReasoningModeDisablesThinking(t *testing.T) {
	payload := map[string]any{
		"thinking": map[string]any{
			"type":          "enabled",
			"budget_tokens": defaultAnthropicThinkingBudget,
		},
	}
	applyAnthropicReasoningMode(payload, "off")
	if _, ok := payload["thinking"]; ok {
		t.Fatalf("thinking should be removed, got %#v", payload["thinking"])
	}
}

func TestAnthropicGateway_Stream_AdvancedJSON_DeniedKeyReturnsError(t *testing.T) {
	// denylist keys (model/max_tokens/stream/tools/system etc.) should all fail immediately
	deniedKeys := []string{"model", "max_tokens", "system"}
	for _, key := range deniedKeys {
		key := key
		t.Run(key, func(t *testing.T) {
			gateway := NewAnthropicGateway(AnthropicGatewayConfig{
				APIKey:       "test",
				BaseURL:      "http://127.0.0.1:0",
				AdvancedJSON: map[string]any{key: "anything"},
			})

			var events []StreamEvent
			err := gateway.Stream(context.Background(), Request{
				Model:    "claude-real",
				Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
			}, func(ev StreamEvent) error {
				events = append(events, ev)
				return nil
			})
			if err != nil {
				t.Fatalf("expected nil error, got: %v", err)
			}
			if len(events) == 0 {
				t.Fatal("expected StreamRunFailed, got no events")
			}
			failed, ok := events[0].(StreamRunFailed)
			if !ok {
				t.Fatalf("expected StreamRunFailed, got %T", events[0])
			}
			if failed.Error.Details["denied_key"] != key {
				t.Fatalf("expected denied_key=%s, got: %v", key, failed.Error.Details)
			}
		})
	}
}

func TestAnthropicGateway_Stream_ErrorEventStopsTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSSEBody([]string{
			`{"type":"error","error":{"type":"overloaded_error","message":"upstream busy"}}`,
			`{"type":"message_stop"}`,
		})))
	}))
	t.Cleanup(server.Close)

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
	})

	var events []StreamEvent
	err := gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	var failedCount, completedCount int
	for _, ev := range events {
		switch ev.(type) {
		case StreamRunFailed:
			failedCount++
		case StreamRunCompleted:
			completedCount++
		}
	}
	if failedCount != 1 {
		t.Fatalf("expected exactly one StreamRunFailed, got %d", failedCount)
	}
	if completedCount != 0 {
		t.Fatalf("expected no StreamRunCompleted after stream error, got %d", completedCount)
	}

	last := events[len(events)-1]
	failed, ok := last.(StreamRunFailed)
	if !ok {
		t.Fatalf("expected terminal StreamRunFailed, got %T", last)
	}
	if failed.Error.ErrorClass != ErrorClassProviderRetryable {
		t.Fatalf("unexpected error class: %q", failed.Error.ErrorClass)
	}
	if failed.Error.Message != "upstream busy" {
		t.Fatalf("unexpected error message: %q", failed.Error.Message)
	}
	if failed.Error.Details["anthropic_error_type"] != "overloaded_error" {
		t.Fatalf("unexpected error details: %#v", failed.Error.Details)
	}
}

func TestAnthropicGateway_Stream_MissingMessageStopAfterTextIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSSEBody([]string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"partial"}}`,
		})))
	}))
	t.Cleanup(server.Close)

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
	})

	var events []StreamEvent
	err := gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}
	failed, ok := events[len(events)-1].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected terminal StreamRunFailed, got %T", events[len(events)-1])
	}
	if failed.Error.ErrorClass != ErrorClassProviderRetryable {
		t.Fatalf("unexpected error class: %q", failed.Error.ErrorClass)
	}
	if failed.Error.Message != "upstream stream ended prematurely without completion" {
		t.Fatalf("unexpected error message: %q", failed.Error.Message)
	}
}

func TestAnthropicHTTPErrorClassInvalidRequestIsNonRetryable(t *testing.T) {
	details := map[string]any{
		"anthropic_error_type": "invalid_request_error",
		"status_code":          400,
	}
	if got := anthropicHTTPErrorClass(400, details); got != ErrorClassProviderNonRetryable {
		t.Fatalf("anthropicHTTPErrorClass(400, invalid_request_error) = %q, want %q", got, ErrorClassProviderNonRetryable)
	}
}

func TestAnthropicGateway_Stream_RefusalStopsWithoutSuccessTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSSEBody([]string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"cannot comply"}}`,
			`{"type":"message_delta","delta":{"stop_reason":"refusal"}}`,
			`{"type":"message_stop"}`,
		})))
	}))
	t.Cleanup(server.Close)

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
	})

	var events []StreamEvent
	err := gateway.Stream(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	var completedCount int
	for _, ev := range events {
		if _, ok := ev.(StreamRunCompleted); ok {
			completedCount++
		}
	}
	if completedCount != 0 {
		t.Fatalf("expected no StreamRunCompleted for refusal, got %d", completedCount)
	}

	last := events[len(events)-1]
	failed, ok := last.(StreamRunFailed)
	if !ok {
		t.Fatalf("expected terminal StreamRunFailed, got %T", last)
	}
	if failed.Error.ErrorClass != ErrorClassPolicyDenied {
		t.Fatalf("unexpected error class: %q", failed.Error.ErrorClass)
	}
	if failed.Error.Details["stop_reason"] != "refusal" {
		t.Fatalf("unexpected stop_reason: %#v", failed.Error.Details)
	}
}

func TestAnthropicGateway_Stream_RequestBodyDoesNotLeakProviderToolName(t *testing.T) {
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body failed: %v", err)
		}
		receivedBody = append([]byte(nil), body...)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSSEBody([]string{`{"type":"message_stop"}`})))
	}))
	t.Cleanup(server.Close)

	gateway := NewAnthropicGateway(AnthropicGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
	})

	err := gateway.Stream(context.Background(), Request{
		Model: "claude-test",
		Messages: []Message{
			{Role: "user", Content: []TextPart{{Text: "hi"}}},
			{
				Role:    "assistant",
				Content: []TextPart{{Text: "searching"}},
				ToolCalls: []ToolCall{{
					ToolCallID:    "call_1",
					ToolName:      "web_search.tavily",
					ArgumentsJSON: map[string]any{"query": "hello"},
				}},
			},
			{
				Role: "tool",
				Content: []TextPart{{
					Text: `{"tool_call_id":"call_1","tool_name":"web_search.tavily","result":{"items":[{"title":"x"}]}}`,
				}},
			},
		},
	}, func(StreamEvent) error {
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	bodyText := string(receivedBody)
	if strings.Contains(bodyText, "web_search.tavily") {
		t.Fatalf("expected anthropic request body to hide provider tool name, got %s", bodyText)
	}
	if !strings.Contains(bodyText, `"name":"web_search"`) {
		t.Fatalf("expected anthropic request body to keep canonical tool name, got %s", bodyText)
	}
}

func TestResolveStableMarkerMessageIndex(t *testing.T) {
	sourceToOut := map[int]int{
		0: 0,
		1: 1,
		5: 3,
	}

	tests := []struct {
		name        string
		sourceIndex int
		want        int
	}{
		{"sentinel passthrough", -1, -1},
		{"found in map", 1, 1},
		{"found with different output index", 5, 3},
		{"not found in map", 10, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveStableMarkerMessageIndex(tt.sourceIndex, sourceToOut)
			if got != tt.want {
				t.Errorf("resolveStableMarkerMessageIndex(%d) = %d, want %d", tt.sourceIndex, got, tt.want)
			}
		})
	}
}

func TestApplyAnthropicMessageCachePlan_StableMarker(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": []map[string]any{{"type": "text", "text": "msg0"}}},
		{"role": "assistant", "content": []map[string]any{{"type": "text", "text": "msg1"}}},
		{"role": "user", "content": []map[string]any{{"type": "text", "text": "msg2"}}},
		{"role": "assistant", "content": []map[string]any{{"type": "text", "text": "msg3"}}},
	}

	sourceToOut := map[int]int{0: 0, 1: 1, 2: 2, 3: 3}
	userSourceToOut := map[int]int{0: 0, 2: 2}

	plan := MessageCachePlan{
		Enabled:                  true,
		MarkerMessageIndex:       3,
		StableMarkerEnabled:      true,
		StableMarkerMessageIndex: 1,
	}

	applyAnthropicMessageCachePlan(messages, sourceToOut, userSourceToOut, plan)

	content1 := messages[1]["content"].([]map[string]any)
	if len(content1) == 0 {
		t.Fatal("messages[1] has no content blocks")
	}
	cc1, ok := content1[0]["cache_control"].(map[string]any)
	if !ok {
		t.Errorf("messages[1] content[0] missing cache_control")
	} else if cc1["type"] != "ephemeral" {
		t.Errorf("messages[1] cache_control type = %v, want ephemeral", cc1["type"])
	}

	content3 := messages[3]["content"].([]map[string]any)
	if len(content3) == 0 {
		t.Fatal("messages[3] has no content blocks")
	}
	cc3, ok := content3[0]["cache_control"].(map[string]any)
	if !ok {
		t.Errorf("messages[3] content[0] missing cache_control")
	} else if cc3["type"] != "ephemeral" {
		t.Errorf("messages[3] cache_control type = %v, want ephemeral", cc3["type"])
	}
}
