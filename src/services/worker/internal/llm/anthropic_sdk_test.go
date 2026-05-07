package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

func anthropicSDKSSEBody(chunks []string) string {
	var sb strings.Builder
	for _, chunk := range chunks {
		var event struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(chunk), &event)
		sb.WriteString("event: ")
		sb.WriteString(event.Type)
		sb.WriteString("\n")
		sb.WriteString("data: ")
		sb.WriteString(chunk)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func TestAnthropicSDKGateway_RequestIncludesThinkingSignatureCacheAndAdvancedJSON(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("anthropic-beta") != "prompt-caching-2024-07-31" {
			t.Fatalf("missing beta header: %s", r.Header.Get("anthropic-beta"))
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSDKSSEBody([]string{
			`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}`,
			`{"type":"message_stop"}`,
		})))
	}))
	defer server.Close()

	gateway := NewAnthropicGatewaySDK(AnthropicGatewayConfig{
		Transport: TransportConfig{APIKey: "test-key", BaseURL: server.URL},
		Protocol: AnthropicProtocolConfig{
			Version: "2023-06-01",
			ExtraHeaders: map[string]string{
				"anthropic-beta": "prompt-caching-2024-07-31",
			},
			AdvancedPayloadJSON: map[string]any{"top_k": 4},
		},
	})
	cacheControl := "ephemeral"
	request := Request{
		Model:         "claude-test",
		ReasoningMode: "enabled",
		Messages: []Message{
			{Role: "system", Content: []ContentPart{{Text: "system", CacheControl: &cacheControl}}},
			{Role: "user", Content: []ContentPart{{Text: "hello"}}},
			{Role: "assistant", Content: []ContentPart{{Type: "thinking", Text: "reason", Signature: "sig_1"}, {Text: "answer"}}},
			{Role: "user", Content: []ContentPart{{Text: "next"}}},
		},
		PromptPlan: &PromptPlan{MessageCache: MessageCachePlan{NewCacheEdits: &PromptCacheEditsBlock{UserMessageIndex: 3, Edits: []PromptCacheEdit{{Type: CacheHintActionDelete, CacheReference: "cache_1"}}}}},
	}
	var events []StreamEvent
	if err := gateway.Stream(context.Background(), request, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected events")
	}
	if captured["top_k"] != float64(4) {
		t.Fatalf("advanced json missing: %#v", captured)
	}
	thinking := captured["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != float64(defaultAnthropicThinkingBudget) {
		t.Fatalf("unexpected thinking config: %#v", thinking)
	}
	system := captured["system"].([]any)[0].(map[string]any)
	if system["cache_control"].(map[string]any)["type"] != "ephemeral" {
		t.Fatalf("system cache_control missing: %#v", system)
	}
	messages := captured["messages"].([]any)
	assistantBlocks := messages[1].(map[string]any)["content"].([]any)
	thinkingBlock := assistantBlocks[0].(map[string]any)
	if thinkingBlock["type"] != "thinking" || thinkingBlock["signature"] != "sig_1" {
		t.Fatalf("thinking signature missing: %#v", thinkingBlock)
	}
	lastBlocks := messages[len(messages)-1].(map[string]any)["content"].([]any)
	cacheEdits := lastBlocks[len(lastBlocks)-1].(map[string]any)
	if cacheEdits["type"] != "cache_edits" {
		t.Fatalf("cache_edits missing: %#v", lastBlocks)
	}
}

func TestAnthropicSDKGateway_DeepSeekAutoDisablesThinking(t *testing.T) {
	gateway := NewAnthropicGatewaySDK(AnthropicGatewayConfig{
		Transport: TransportConfig{APIKey: "test-key", BaseURL: "https://api.deepseek.com/anthropic"},
		Protocol:  AnthropicProtocolConfig{Version: "2023-06-01"},
	})
	_, payload, _, err := gateway.(*anthropicSDKGateway).messageParams(Request{
		Model:         "deepseek-v4-flash",
		ReasoningMode: "auto",
		Messages: []Message{
			{Role: "user", Content: []ContentPart{{Text: "hello"}}},
			{Role: "assistant", Content: []ContentPart{{Text: "old answer"}}},
			{Role: "user", Content: []ContentPart{{Text: "next"}}},
		},
	})
	if err != nil {
		t.Fatalf("messageParams failed: %v", err)
	}
	thinking := payload["thinking"].(map[string]any)
	if thinking["type"] != "disabled" {
		t.Fatalf("expected thinking disabled, got %#v", thinking)
	}
}

func TestAnthropicSDKGateway_ThinkingAndToolUseAccumulators(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSDKSSEBody([]string{
			`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":2,"output_tokens":0}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"a"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"b"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_x"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"echo","input":{}}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"text\":"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"hi\"}"}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":2,"output_tokens":5}}`,
			`{"type":"message_stop"}`,
		})))
	}))
	defer server.Close()

	gateway := NewAnthropicGatewaySDK(AnthropicGatewayConfig{Transport: TransportConfig{APIKey: "test-key", BaseURL: server.URL}, Protocol: AnthropicProtocolConfig{Version: "2023-06-01"}})
	var events []StreamEvent
	if err := gateway.Stream(context.Background(), Request{Model: "claude-test", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}}, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	var toolCall *ToolCall
	var completed *StreamRunCompleted
	var thinkingDelta strings.Builder
	for _, event := range events {
		switch ev := event.(type) {
		case StreamMessageDelta:
			if ev.Channel != nil && *ev.Channel == "thinking" {
				thinkingDelta.WriteString(ev.ContentDelta)
			}
		case ToolCall:
			toolCall = &ev
		case StreamRunCompleted:
			completed = &ev
		}
	}
	if thinkingDelta.String() != "ab" {
		t.Fatalf("unexpected thinking deltas: %q", thinkingDelta.String())
	}
	if toolCall == nil || toolCall.ToolCallID != "toolu_1" || toolCall.ArgumentsJSON["text"] != "hi" {
		t.Fatalf("unexpected tool call: %#v", toolCall)
	}
	if completed == nil || completed.AssistantMessage == nil || len(completed.AssistantMessage.Content) != 1 {
		t.Fatalf("missing completed assistant message: %#v", completed)
	}
	part := completed.AssistantMessage.Content[0]
	if part.Kind() != "thinking" || part.Text != "ab" || part.Signature != "sig_x" {
		t.Fatalf("thinking part not preserved: %#v", part)
	}
}

func TestAnthropicSDKGateway_ReplaysRecoveredThinkingSignature(t *testing.T) {
	message := Message{Role: "assistant", Content: []ContentPart{{Type: "thinking", Text: "keep", Signature: "sig_keep"}, {Text: "done"}}}
	raw, err := BuildAssistantThreadContentJSON(message)
	if err != nil {
		t.Fatalf("BuildAssistantThreadContentJSON failed: %v", err)
	}
	restored, err := AssistantMessageFromThreadContentJSON(raw)
	if err != nil {
		t.Fatalf("AssistantMessageFromThreadContentJSON failed: %v", err)
	}
	system, messages, err := toAnthropicMessagesWithPlan([]Message{*restored}, nil)
	if err != nil {
		t.Fatalf("toAnthropicMessagesWithPlan failed: %v", err)
	}
	if len(system) != 0 || len(messages) != 1 {
		t.Fatalf("unexpected messages: system=%#v messages=%#v", system, messages)
	}
	params, err := anthropicSDKMessages(messages)
	if err != nil {
		t.Fatalf("anthropicSDKMessages failed: %v", err)
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if !strings.Contains(string(encoded), `"signature":"sig_keep"`) {
		t.Fatalf("signature not preserved: %s", string(encoded))
	}
}

func TestAnthropicHistoryPreservesDisplayDescriptionInToolArguments(t *testing.T) {
	_, messages, err := toAnthropicMessagesWithPlan([]Message{{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ToolCallID:         "call_1",
			ToolName:           "exec_command",
			ArgumentsJSON:      map[string]any{"command": "git status"},
			DisplayDescription: "Checking status",
		}},
	}, {
		Role:    "tool",
		Content: []ContentPart{{Text: `{"tool_call_id":"call_1","tool_name":"exec_command","result":{"ok":true}}`}},
	}}, nil)
	if err != nil {
		t.Fatalf("toAnthropicMessagesWithPlan: %v", err)
	}
	content := messages[0]["content"].([]map[string]any)
	input := content[0]["input"].(map[string]any)
	if input["display_description"] != "Checking status" {
		t.Fatalf("anthropic input lost display_description: %#v", input)
	}
}

func TestAnthropicMessagesDeduplicatesRepeatedToolResultBlocks(t *testing.T) {
	_, messages, err := toAnthropicMessages([]Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ToolCallID:    "call_1",
				ToolName:      "read",
				ArgumentsJSON: map[string]any{"path": "a.txt"},
			}},
		},
		{
			Role:    "tool",
			Content: []ContentPart{{Type: "text", Text: `{"tool_call_id":"call_1","tool_name":"read","result":{"value":"stale"}}`}},
		},
		{
			Role:    "tool",
			Content: []ContentPart{{Type: "text", Text: `{"tool_call_id":"call_1","tool_name":"read","result":{"value":"fresh"}}`}},
		},
	})
	if err != nil {
		t.Fatalf("toAnthropicMessages failed: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("unexpected messages: %#v", messages)
	}
	blocks, ok := messages[1]["content"].([]map[string]any)
	if !ok {
		t.Fatalf("tool result content was not block list: %#v", messages[1]["content"])
	}
	var results []map[string]any
	for _, block := range blocks {
		if block["type"] == "tool_result" {
			results = append(results, block)
		}
	}
	if len(results) != 1 {
		t.Fatalf("expected one tool_result block, got %#v", results)
	}
	if got := results[0]["content"]; !strings.Contains(fmt.Sprint(got), "fresh") || strings.Contains(fmt.Sprint(got), "stale") {
		t.Fatalf("expected latest tool_result content, got %#v", got)
	}
}

func TestAnthropicSDKGateway_DropsUnsignedThinking(t *testing.T) {
	_, messages, err := toAnthropicMessagesWithPlan([]Message{{
		Role: "assistant",
		Content: []ContentPart{
			{Type: "thinking", Text: "drop"},
			{Text: "done"},
		},
	}, {
		Role:    "assistant",
		Content: []ContentPart{{Type: "thinking", Text: "drop-only"}},
	}, {
		Role: "assistant",
		Content: []ContentPart{
			{Type: "thinking", Text: "keep", Signature: "sig_keep"},
			{Text: "answer"},
		},
	}}, nil)
	if err != nil {
		t.Fatalf("toAnthropicMessagesWithPlan failed: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("unexpected messages: %#v", messages)
	}
	firstContent := messages[0]["content"].([]map[string]any)
	if len(firstContent) != 1 || firstContent[0]["type"] != "text" || firstContent[0]["text"] != "done" {
		t.Fatalf("unsigned thinking not dropped: %#v", messages[0])
	}

	secondContent := messages[1]["content"].([]map[string]any)
	if len(secondContent) != 2 || secondContent[0]["type"] != "thinking" || secondContent[0]["thinking"] != "keep" || secondContent[0]["signature"] != "sig_keep" {
		t.Fatalf("signed thinking not preserved: %#v", messages[1])
	}
	payload := map[string]any{"messages": messages}
	if anthropicSDKMessagesRequireRawJSON(payload) {
		t.Fatalf("signed thinking should use SDK params: %#v", payload)
	}
}

func TestAnthropicSDKGateway_ErrorClassification(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		typeName  string
		wantClass string
	}{
		{name: "rate_limit", status: http.StatusTooManyRequests, typeName: "rate_limit_error", wantClass: ErrorClassProviderRetryable},
		{name: "server", status: http.StatusInternalServerError, typeName: "api_error", wantClass: ErrorClassProviderRetryable},
		{name: "auth", status: http.StatusUnauthorized, typeName: "authentication_error", wantClass: ErrorClassProviderNonRetryable},
		{name: "context_length", status: http.StatusBadRequest, typeName: "context_length_exceeded", wantClass: ErrorClassProviderNonRetryable},
		{name: "invalid_value", status: http.StatusBadRequest, typeName: "invalid_value", wantClass: ErrorClassProviderNonRetryable},
		{name: "bad_request_nil_type", status: http.StatusBadRequest, typeName: "<nil>", wantClass: ErrorClassProviderNonRetryable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":{"type":"` + tc.typeName + `","message":"failed"}}`))
			}))
			defer server.Close()
			gateway := NewAnthropicGatewaySDK(AnthropicGatewayConfig{Transport: TransportConfig{APIKey: "test-key", BaseURL: server.URL}, Protocol: AnthropicProtocolConfig{Version: "2023-06-01"}})
			var failed *StreamRunFailed
			err := gateway.Stream(context.Background(), Request{Model: "claude-test", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}}, func(event StreamEvent) error {
				if ev, ok := event.(StreamRunFailed); ok {
					failed = &ev
				}
				return nil
			})
			if err != nil {
				t.Fatalf("Stream returned unexpected error: %v", err)
			}
			if failed == nil || failed.Error.ErrorClass != tc.wantClass {
				t.Fatalf("unexpected failure: %#v", failed)
			}
		})
	}
}

func TestAnthropicSDKGateway_ProviderOversizeDetails(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"too large"}}`))
	}))
	defer server.Close()

	gateway := NewAnthropicGatewaySDK(AnthropicGatewayConfig{Transport: TransportConfig{APIKey: "test-key", BaseURL: server.URL}, Protocol: AnthropicProtocolConfig{Version: "2023-06-01"}})
	var failed *StreamRunFailed
	err := gateway.Stream(context.Background(), Request{Model: "claude-test", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}}, func(event StreamEvent) error {
		if ev, ok := event.(StreamRunFailed); ok {
			failed = &ev
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Stream returned unexpected error: %v", err)
	}
	if failed == nil {
		t.Fatalf("missing failure")
	}
	if failed.Error.Details["status_code"] != http.StatusRequestEntityTooLarge || failed.Error.Details["network_attempted"] != true || failed.Error.Details["oversize_phase"] != OversizePhaseProvider {
		t.Fatalf("missing oversize details: %#v", failed.Error.Details)
	}
}

func TestAnthropicSDKGateway_APIErrorHasDiagnosticDetails(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "req_anthropic_1")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"context_length_exceeded","message":"too many tokens"}}`))
	}))
	defer server.Close()

	gateway := NewAnthropicGatewaySDK(AnthropicGatewayConfig{Transport: TransportConfig{APIKey: "test-key", BaseURL: server.URL}, Protocol: AnthropicProtocolConfig{Version: "2023-06-01"}})
	var failed *StreamRunFailed
	err := gateway.Stream(context.Background(), Request{Model: "claude-test", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}}, func(event StreamEvent) error {
		if ev, ok := event.(StreamRunFailed); ok {
			failed = &ev
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Stream returned unexpected error: %v", err)
	}
	if failed == nil {
		t.Fatalf("missing failure")
	}
	if failed.Error.ErrorClass != ErrorClassProviderNonRetryable || failed.Error.Message != "too many tokens" {
		t.Fatalf("unexpected failure: %#v", failed.Error)
	}
	details := failed.Error.Details
	if details["provider_kind"] != "anthropic" || details["api_mode"] != "messages" || details["network_attempted"] != true || details["streaming"] != true {
		t.Fatalf("missing provider diagnostics: %#v", details)
	}
	if details["anthropic_error_type"] != "context_length_exceeded" || details["provider_request_id"] != "req_anthropic_1" {
		t.Fatalf("missing Anthropic error details: %#v", details)
	}
	if raw, _ := details["provider_error_body"].(string); !strings.Contains(raw, "too many tokens") || strings.Contains(raw, "HTTP/1.1") {
		t.Fatalf("unexpected provider error body: %#v", details)
	}
	if tail, _ := details["provider_response_tail"].(string); !strings.Contains(tail, "too many tokens") || strings.Contains(tail, "HTTP/1.1") {
		t.Fatalf("unexpected provider response tail: %#v", details)
	}
}

func TestAnthropicSDKGateway_TruncatedJSONStreamHasDiagnosticDetails(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Anthropic-Request-Id", "req_anthropic_tail_1")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":\n\n"))
	}))
	defer server.Close()

	gateway := NewAnthropicGatewaySDK(AnthropicGatewayConfig{Transport: TransportConfig{APIKey: "test-key", BaseURL: server.URL}, Protocol: AnthropicProtocolConfig{Version: "2023-06-01"}})
	var failed *StreamRunFailed
	err := gateway.Stream(context.Background(), Request{Model: "claude-test", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}}, func(event StreamEvent) error {
		if ev, ok := event.(StreamRunFailed); ok {
			failed = &ev
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Stream returned unexpected error: %v", err)
	}
	if failed == nil {
		t.Fatalf("missing failure")
	}
	if failed.Error.ErrorClass != ErrorClassProviderRetryable || failed.Error.Message != "Anthropic network error" {
		t.Fatalf("unexpected failure: %#v", failed.Error)
	}
	details := failed.Error.Details
	reason, _ := details["reason"].(string)
	errorType, _ := details["error_type"].(string)
	if !strings.Contains(reason, "unexpected end of JSON input") || errorType == "" {
		t.Fatalf("missing diagnostic reason/type: %#v", details)
	}
	if details["streaming"] != true || details["network_attempted"] != true || details["provider_kind"] != "anthropic" || details["api_mode"] != "messages" || details["provider_response_parse_error"] != true {
		t.Fatalf("missing stream diagnostic details: %#v", details)
	}
	if details["status_code"] != http.StatusOK || details["content_type"] != "text/event-stream" || details["provider_request_id"] != "req_anthropic_tail_1" {
		t.Fatalf("missing response capture metadata: %#v", details)
	}
	tail, _ := details["provider_response_tail"].(string)
	if !strings.Contains(tail, "event: message_start") || !strings.Contains(tail, `"type":"message_start"`) || strings.Contains(tail, "HTTP/1.1") {
		t.Fatalf("unexpected response tail: %#v", details)
	}
}

func TestAnthropicSDKGateway_RequestOmitsToolResultCacheReferences(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSDKSSEBody([]string{
			`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}`,
			`{"type":"message_stop"}`,
		})))
	}))
	defer server.Close()

	gateway := NewAnthropicGatewaySDK(AnthropicGatewayConfig{Transport: TransportConfig{APIKey: "test-key", BaseURL: server.URL}, Protocol: AnthropicProtocolConfig{Version: "2023-06-01"}})
	request := Request{
		Model: "claude-test",
		Messages: []Message{
			{Role: "user", Content: []ContentPart{{Text: "run tool"}}},
			{Role: "assistant", ToolCalls: []ToolCall{{ToolCallID: "toolu_1", ToolName: "echo", ArgumentsJSON: map[string]any{"text": "hi"}}}},
			{Role: "tool", Content: []ContentPart{{Text: `{"tool_call_id":"toolu_1","tool_name":"echo","result":"ok"}`}}},
			{Role: "user", Content: []ContentPart{{Text: "continue"}}},
		},
		PromptPlan: &PromptPlan{MessageCache: MessageCachePlan{Enabled: true, MarkerMessageIndex: 3, ToolResultCacheReferences: true, ToolResultCacheCutIndex: 3}},
	}
	if err := gateway.Stream(context.Background(), request, func(event StreamEvent) error { return nil }); err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	messages := captured["messages"].([]any)
	var found bool
	for _, item := range messages {
		blocks, _ := item.(map[string]any)["content"].([]any)
		for _, rawBlock := range blocks {
			block, _ := rawBlock.(map[string]any)
			if block["type"] == "tool_result" && block["cache_reference"] == "toolu_1" {
				found = true
			}
		}
	}
	if found {
		t.Fatalf("tool_result cache_reference must not be sent to Anthropic: %#v", captured)
	}
}

func TestAnthropicSDKGateway_LimitsCacheControlBlocks(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSDKSSEBody([]string{
			`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}`,
			`{"type":"message_stop"}`,
		})))
	}))
	defer server.Close()

	gateway := NewAnthropicGatewaySDK(AnthropicGatewayConfig{Transport: TransportConfig{APIKey: "test-key", BaseURL: server.URL}, Protocol: AnthropicProtocolConfig{Version: "2023-06-01"}})
	cacheControl := "ephemeral"
	desc := "cached tool"
	request := Request{
		Model: "claude-test",
		Messages: []Message{
			{Role: "system", Content: []ContentPart{{Text: "legacy system", CacheControl: &cacheControl}}},
			{Role: "user", Content: []ContentPart{{Text: "hello"}}},
			{Role: "user", Content: []ContentPart{{Text: "continue"}}},
		},
		Tools: []ToolSpec{{
			Name:        "echo",
			Description: &desc,
			JSONSchema:  map[string]any{"type": "object"},
			CacheHint:   &CacheHint{Action: CacheHintActionWrite},
		}},
		PromptPlan: &PromptPlan{
			SystemBlocks: []PromptPlanBlock{
				{Text: "stable one", Stability: CacheStabilityStablePrefix, CacheEligible: true},
				{Text: "session two", Stability: CacheStabilitySessionPrefix, CacheEligible: true},
				{Text: "stable three", Stability: CacheStabilityStablePrefix, CacheEligible: true},
				{Text: "session four", Stability: CacheStabilitySessionPrefix, CacheEligible: true},
			},
			MessageCache: MessageCachePlan{Enabled: true, MarkerMessageIndex: 2},
		},
	}
	if err := gateway.Stream(context.Background(), request, func(event StreamEvent) error { return nil }); err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	if count := countAnthropicCacheControlBlocks(captured); count != anthropicMaxCacheControlBlocks {
		t.Fatalf("expected %d cache_control blocks, got %d in %#v", anthropicMaxCacheControlBlocks, count, captured)
	}
	if count := countAnthropicMessageCacheControlBlocks(captured); count != 1 {
		t.Fatalf("expected message cache_control to be preserved, got %d in %#v", count, captured)
	}
	if count := countAnthropicSystemCacheControlBlocks(captured); count != anthropicMaxCacheControlBlocks-1 {
		t.Fatalf("expected one system cache_control to be removed, got %d in %#v", count, captured)
	}
}

func TestAnthropicMessageCacheMarkerMissingSourceDoesNotFallbackToTail(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": []map[string]any{{"type": "text", "text": "stable"}}},
		{"role": "user", "content": []map[string]any{{"type": "text", "text": "volatile tail"}}},
	}

	applyAnthropicMessageCachePlan(messages, map[int]int{0: 0}, nil, MessageCachePlan{
		Enabled:            true,
		MarkerMessageIndex: 7,
	})

	if count := countAnthropicMessageCacheControlBlocks(map[string]any{"messages": messages}); count != 0 {
		t.Fatalf("missing source marker must not fall back to tail: %#v", messages)
	}
}

func countAnthropicCacheControlBlocks(payload map[string]any) int {
	count := countAnthropicSystemCacheControlBlocks(payload)
	count += countAnthropicMessageCacheControlBlocks(payload)
	count += countAnthropicToolCacheControlBlocks(payload)
	return count
}

func countAnthropicSystemCacheControlBlocks(payload map[string]any) int {
	count := 0
	if system, _ := payload["system"].([]any); len(system) > 0 {
		for _, raw := range system {
			block, _ := raw.(map[string]any)
			if _, ok := block["cache_control"]; ok {
				count++
			}
		}
	}
	return count
}

func countAnthropicMessageCacheControlBlocks(payload map[string]any) int {
	count := 0
	if messages, _ := payload["messages"].([]any); len(messages) > 0 {
		for _, rawMessage := range messages {
			message, _ := rawMessage.(map[string]any)
			content, _ := message["content"].([]any)
			for _, rawBlock := range content {
				block, _ := rawBlock.(map[string]any)
				if _, ok := block["cache_control"]; ok {
					count++
				}
			}
		}
	}
	return count
}

func countAnthropicToolCacheControlBlocks(payload map[string]any) int {
	count := 0
	if tools, _ := payload["tools"].([]any); len(tools) > 0 {
		for _, raw := range tools {
			tool, _ := raw.(map[string]any)
			if _, ok := tool["cache_control"]; ok {
				count++
			}
		}
	}
	return count
}

func TestAnthropicSDKGateway_QuirkRetryStripUnsignedThinking(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	var attempts int
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		bodies = append(bodies, body)
		attempts++
		if attempts == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"Invalid signature in thinking block"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSDKSSEBody([]string{
			`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}`,
			`{"type":"message_stop"}`,
		})))
	}))
	defer server.Close()

	gateway := NewAnthropicGatewaySDK(AnthropicGatewayConfig{
		Transport: TransportConfig{APIKey: "key", BaseURL: server.URL},
		Protocol:  AnthropicProtocolConfig{Version: "2023-06-01"},
	})
	request := Request{
		Model: "claude-test",
		Messages: []Message{
			{Role: "user", Content: []ContentPart{{Text: "hi"}}},
			{Role: "assistant", Content: []ContentPart{{Type: "thinking", Text: "unsigned"}, {Text: "answer"}}},
			{Role: "user", Content: []ContentPart{{Text: "again"}}},
		},
	}
	var completed bool
	var learned []StreamQuirkLearned
	if err := gateway.Stream(context.Background(), request, func(event StreamEvent) error {
		switch ev := event.(type) {
		case StreamRunCompleted:
			completed = true
		case StreamQuirkLearned:
			learned = append(learned, ev)
		}
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected retry, got attempts=%d", attempts)
	}
	if !completed {
		t.Fatalf("missing completion after retry")
	}
	if !gateway.(*anthropicSDKGateway).quirks.Has(QuirkStripUnsignedThinking) {
		t.Fatalf("quirk not stored")
	}
	if len(learned) != 1 || learned[0].ProviderKind != "anthropic" || learned[0].QuirkID != string(QuirkStripUnsignedThinking) {
		t.Fatalf("expected one StreamQuirkLearned event, got %#v", learned)
	}
	secondMsgs, _ := bodies[1]["messages"].([]any)
	for _, raw := range secondMsgs {
		m := raw.(map[string]any)
		content, _ := m["content"].([]any)
		for _, blockRaw := range content {
			block, ok := blockRaw.(map[string]any)
			if !ok {
				continue
			}
			if typ, _ := block["type"].(string); typ != "thinking" {
				continue
			}
			sig, _ := block["signature"].(string)
			if strings.TrimSpace(sig) == "" {
				t.Fatalf("retry must drop unsigned thinking, found %#v", block)
			}
		}
	}
}

func TestAnthropicSDKDetectQuirk_UsesCapturedResponseTail(t *testing.T) {
	capture := newProviderResponseCapture()
	capture.setResponse(&http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"text/event-stream"}}})
	capture.appendBody([]byte(`{"error":{"message":"Extra inputs are not permitted, field: 'messages[1].content[0].cache_control'"}}`))

	apiErr := &anthropic.Error{
		StatusCode: http.StatusBadRequest,
		Response: &http.Response{
			StatusCode: http.StatusBadRequest,
			Status:     "400 Bad Request",
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Anthropic request failed: status=400"}}`)),
		},
	}
	id, ok := anthropicSDKDetectQuirk(apiErr, capture)
	if !ok || id != QuirkStripCacheControl {
		t.Fatalf("expected strip cache_control quirk from response tail, got id=%s ok=%v", id, ok)
	}
	id, ok = anthropicSDKDetectQuirk(fmt.Errorf("anthropic stream failed"), capture)
	if !ok || id != QuirkStripCacheControl {
		t.Fatalf("expected strip cache_control quirk without api error, got id=%s ok=%v", id, ok)
	}
}

func TestAnthropicSDKGateway_QuirkRetryStripCacheControl(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	var attempts int
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		bodies = append(bodies, body)
		attempts++
		if attempts == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Extra inputs are not permitted, field: 'messages[1].content[0].cache_control'"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSDKSSEBody([]string{
			`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}`,
			`{"type":"message_stop"}`,
		})))
	}))
	defer server.Close()

	cacheControl := "ephemeral"
	gateway := NewAnthropicGatewaySDK(AnthropicGatewayConfig{
		Transport: TransportConfig{APIKey: "key", BaseURL: server.URL},
		Protocol:  AnthropicProtocolConfig{Version: "2023-06-01"},
	})
	request := Request{
		Model: "claude-test",
		Messages: []Message{
			{Role: "system", Content: []ContentPart{{Text: "system", CacheControl: &cacheControl}}},
			{Role: "user", Content: []ContentPart{{Text: "hi", CacheControl: &cacheControl}}},
		},
	}
	var completed bool
	var learned []StreamQuirkLearned
	if err := gateway.Stream(context.Background(), request, func(event StreamEvent) error {
		switch ev := event.(type) {
		case StreamRunCompleted:
			completed = true
		case StreamQuirkLearned:
			learned = append(learned, ev)
		}
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected retry, got attempts=%d", attempts)
	}
	if !completed {
		t.Fatalf("missing completion after retry")
	}
	if len(learned) != 1 || learned[0].QuirkID != string(QuirkStripCacheControl) {
		t.Fatalf("expected strip cache_control quirk, got %#v", learned)
	}
	if count := countAnthropicCacheControlBlocks(bodies[1]); count != 0 {
		t.Fatalf("retry payload must strip cache_control, got %d in %#v", count, bodies[1])
	}
}

func TestAnthropicSDKGateway_QuirkRetryEchoEmptyTextOnThinking(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	var attempts int
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		bodies = append(bodies, body)
		attempts++
		if attempts == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"The content in the thinking mode must be passed back to the API."}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSDKSSEBody([]string{
			`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}`,
			`{"type":"message_stop"}`,
		})))
	}))
	defer server.Close()

	gateway := NewAnthropicGatewaySDK(AnthropicGatewayConfig{
		Transport: TransportConfig{APIKey: "key", BaseURL: server.URL},
		Protocol:  AnthropicProtocolConfig{Version: "2023-06-01"},
	})
	request := Request{
		Model: "claude-test",
		Messages: []Message{
			{Role: "user", Content: []ContentPart{{Text: "hi"}}},
			{
				Role:    "assistant",
				Content: []ContentPart{{Type: "thinking", Text: "signed", Signature: "sig_1"}},
				ToolCalls: []ToolCall{{
					ToolCallID:    "toolu_1",
					ToolName:      "echo",
					ArgumentsJSON: map[string]any{"text": "hi"},
				}},
			},
			{Role: "tool", Content: []ContentPart{{Text: `{"tool_call_id":"toolu_1","tool_name":"echo","result":"ok"}`}}},
			{Role: "user", Content: []ContentPart{{Text: "again"}}},
		},
	}
	var completed bool
	var learned []StreamQuirkLearned
	if err := gateway.Stream(context.Background(), request, func(event StreamEvent) error {
		switch ev := event.(type) {
		case StreamRunCompleted:
			completed = true
		case StreamQuirkLearned:
			learned = append(learned, ev)
		}
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected retry, got attempts=%d", attempts)
	}
	if !completed {
		t.Fatalf("missing completion after retry")
	}
	if !gateway.(*anthropicSDKGateway).quirks.Has(QuirkEchoEmptyTextOnThink) {
		t.Fatalf("quirk not stored")
	}
	if len(learned) != 1 || learned[0].ProviderKind != "anthropic" || learned[0].QuirkID != string(QuirkEchoEmptyTextOnThink) {
		t.Fatalf("expected one StreamQuirkLearned event, got %#v", learned)
	}
	secondMsgs, _ := bodies[1]["messages"].([]any)
	var assistant map[string]any
	assistantIndex := -1
	for i, raw := range secondMsgs {
		msg, _ := raw.(map[string]any)
		if role, _ := msg["role"].(string); role == "assistant" {
			assistant = msg
			assistantIndex = i
			break
		}
	}
	if assistant == nil {
		t.Fatalf("retry payload missing assistant message: %#v", secondMsgs)
	}
	content, _ := assistant["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("retry assistant content must include thinking, empty text, and tool_use: %#v", content)
	}
	textBlock, _ := content[1].(map[string]any)
	if textBlock["type"] != "text" || textBlock["text"] != "" {
		t.Fatalf("retry payload missing empty text block: %#v", content)
	}
	toolUse, _ := content[2].(map[string]any)
	if toolUse["type"] != "tool_use" || toolUse["id"] != "toolu_1" {
		t.Fatalf("retry payload must keep tool_use at assistant tail: %#v", content)
	}
	if assistantIndex+1 >= len(secondMsgs) {
		t.Fatalf("retry payload missing user tool_result after assistant: %#v", secondMsgs)
	}
	nextUser, _ := secondMsgs[assistantIndex+1].(map[string]any)
	if nextUser["role"] != "user" {
		t.Fatalf("retry payload next message must be user tool_result: %#v", secondMsgs)
	}
	nextContent, _ := nextUser["content"].([]any)
	if len(nextContent) != 1 {
		t.Fatalf("retry payload next user must only contain tool_result: %#v", nextUser)
	}
	toolResult, _ := nextContent[0].(map[string]any)
	if toolResult["type"] != "tool_result" || toolResult["tool_use_id"] != "toolu_1" {
		t.Fatalf("retry payload next user must keep matching tool_result: %#v", nextUser)
	}
}
