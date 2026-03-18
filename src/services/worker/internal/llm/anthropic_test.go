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
		if r.URL.Path != "/messages" {
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
			"type": "content_block_start",
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
