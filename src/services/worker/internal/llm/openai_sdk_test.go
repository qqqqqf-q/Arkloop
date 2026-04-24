package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func openAISDKSSE(events []string) string {
	var sb strings.Builder
	for _, event := range events {
		sb.WriteString("data: ")
		sb.WriteString(event)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func TestOpenAISDKGateway_ChatCompletionsStreamsToolAndCost(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(openAISDKSSE([]string{
			`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}`,
			`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"ok\"}"}}]},"finish_reason":"tool_calls"}]}`,
			`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7,"cost":0.0012}}`,
		})))
	}))
	defer server.Close()

	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIChatCompletions, AdvancedPayloadJSON: map[string]any{"seed": 7}}})
	var events []StreamEvent
	if err := gateway.Stream(context.Background(), Request{Model: "gpt", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}, ReasoningMode: "high"}, func(event StreamEvent) error { events = append(events, event); return nil }); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if captured["seed"] != float64(7) || captured["reasoning_effort"] != "high" {
		t.Fatalf("unexpected request: %#v", captured)
	}
	var completed *StreamRunCompleted
	var tool *ToolCall
	for _, event := range events {
		switch ev := event.(type) {
		case StreamRunCompleted:
			completed = &ev
		case ToolCall:
			tool = &ev
		}
	}
	if tool == nil || tool.ToolCallID != "call_1" || tool.ArgumentsJSON["text"] != "ok" {
		t.Fatalf("unexpected tool: %#v", tool)
	}
	if completed == nil || completed.Usage == nil || completed.Cost == nil || completed.Cost.AmountMicros != 1200 {
		t.Fatalf("unexpected completion: %#v", completed)
	}
}

func TestOpenAISDKGateway_ResponsesAutoFallback(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/responses" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"model_not_found","type":"invalid_request_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(openAISDKSSE([]string{`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`})))
	}))
	defer server.Close()
	fallback := ProtocolKindOpenAIChatCompletions
	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIResponses, FallbackKind: &fallback}})
	var sawFallback bool
	if err := gateway.Stream(context.Background(), Request{Model: "gpt", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}}, func(event StreamEvent) error {
		if _, ok := event.(StreamProviderFallback); ok {
			sawFallback = true
		}
		return nil
	}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if !sawFallback || len(paths) != 2 || paths[0] != "/responses" || paths[1] != "/chat/completions" {
		t.Fatalf("unexpected fallback paths=%v saw=%v", paths, sawFallback)
	}
}
