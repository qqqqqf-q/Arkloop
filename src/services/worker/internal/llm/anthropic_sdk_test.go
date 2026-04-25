package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
			{Role: "assistant", ToolCalls: []ToolCall{{ToolCallID: "toolu_1", ToolName: "echo", ArgumentsJSON: map[string]any{"text": "hi"}}}},
			{Role: "tool", Content: []ContentPart{{Text: `{"tool_call_id":"toolu_1","tool_name":"echo","result":"ok"}`}}},
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
				{Text: "stable five", Stability: CacheStabilityStablePrefix, CacheEligible: true},
			},
			MessageCache: MessageCachePlan{Enabled: true, MarkerMessageIndex: 4},
		},
	}
	if err := gateway.Stream(context.Background(), request, func(event StreamEvent) error { return nil }); err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	if count := countAnthropicCacheControlBlocks(captured); count != anthropicMaxCacheControlBlocks {
		t.Fatalf("expected %d cache_control blocks, got %d in %#v", anthropicMaxCacheControlBlocks, count, captured)
	}
}

func countAnthropicCacheControlBlocks(payload map[string]any) int {
	count := 0
	if system, _ := payload["system"].([]any); len(system) > 0 {
		for _, raw := range system {
			block, _ := raw.(map[string]any)
			if _, ok := block["cache_control"]; ok {
				count++
			}
		}
	}
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
