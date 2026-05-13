package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arkloop/services/shared/messagecontent"
	"github.com/openai/openai-go/v3/responses"
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

func TestOpenAISDKGateway_ChatCompletionsIgnoresSSECommentKeepalive(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": PROCESSING\n\n" + openAISDKSSE([]string{
			`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`,
		})))
	}))
	defer server.Close()

	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIChatCompletions}})
	var completed *StreamRunCompleted
	var failed *StreamRunFailed
	if err := gateway.Stream(context.Background(), Request{Model: "gpt", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}}, func(event StreamEvent) error {
		switch ev := event.(type) {
		case StreamRunCompleted:
			completed = &ev
		case StreamRunFailed:
			failed = &ev
		}
		return nil
	}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if failed != nil || completed == nil || completed.AssistantMessage == nil || VisibleMessageText(*completed.AssistantMessage) != "hi" {
		t.Fatalf("unexpected terminal events failed=%#v completed=%#v", failed, completed)
	}
}

func TestOpenAISDKGateway_ChatCompletionsCompletedMessageCarriesThinking(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(openAISDKSSE([]string{
			`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"role":"assistant","content":"<think>plan"},"finish_reason":null}]}`,
			`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"content":"</think>visible "},"finish_reason":null}]}`,
			`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"reasoning_content":"deep "},"finish_reason":null}]}`,
			`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"reasoning":"trace"},"finish_reason":null}]}`,
			`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`,
		})))
	}))
	defer server.Close()

	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIChatCompletions}})
	var thinkingDeltas []string
	var visibleDeltas []string
	var completed *StreamRunCompleted
	if err := gateway.Stream(context.Background(), Request{Model: "gpt", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}}, func(event StreamEvent) error {
		switch ev := event.(type) {
		case StreamMessageDelta:
			if ev.Channel != nil && *ev.Channel == "thinking" {
				thinkingDeltas = append(thinkingDeltas, ev.ContentDelta)
			} else {
				visibleDeltas = append(visibleDeltas, ev.ContentDelta)
			}
		case StreamRunCompleted:
			completed = &ev
		}
		return nil
	}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if strings.Join(thinkingDeltas, "") != "plandeep trace" {
		t.Fatalf("unexpected thinking deltas: %#v", thinkingDeltas)
	}
	if strings.Join(visibleDeltas, "") != "visible answer" {
		t.Fatalf("unexpected visible deltas: %#v", visibleDeltas)
	}
	if completed == nil || completed.AssistantMessage == nil {
		t.Fatalf("missing assistant message: %#v", completed)
	}
	parts := completed.AssistantMessage.Content
	if len(parts) != 2 || parts[0].Kind() != "thinking" || parts[0].Text != "plandeep trace" || parts[1].Text != "visible answer" {
		t.Fatalf("unexpected assistant parts: %#v", parts)
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

func TestOpenAISDKGateway_ChatCompletionsPartialStreamFails(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(openAISDKSSE([]string{`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"role":"assistant","content":"partial"},"finish_reason":null}]}`})))
	}))
	defer server.Close()

	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIChatCompletions}})
	var failed *StreamRunFailed
	var completed *StreamRunCompleted
	if err := gateway.Stream(context.Background(), Request{Model: "gpt", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}}, func(event StreamEvent) error {
		switch ev := event.(type) {
		case StreamRunFailed:
			failed = &ev
		case StreamRunCompleted:
			completed = &ev
		}
		return nil
	}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if completed != nil || failed == nil || failed.Error.ErrorClass != ErrorClassProviderRetryable {
		t.Fatalf("unexpected terminal events failed=%#v completed=%#v", failed, completed)
	}
}

func TestOpenAISDKGateway_ChatCompletionsTruncatedJSONStreamHasDiagnosticDetails(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Request-Id", "req_openai_tail_1")
		_, _ = w.Write([]byte(openAISDKSSE([]string{`{"id":"chatcmpl_1","object":"chat.completion.chunk"`})))
	}))
	defer server.Close()

	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIChatCompletions}})
	var failed *StreamRunFailed
	if err := gateway.Stream(context.Background(), Request{Model: "gpt", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}}, func(event StreamEvent) error {
		if ev, ok := event.(StreamRunFailed); ok {
			failed = &ev
		}
		return nil
	}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if failed == nil {
		t.Fatalf("missing failure")
	}
	if failed.Error.ErrorClass != ErrorClassProviderRetryable || failed.Error.Message != "OpenAI network error" {
		t.Fatalf("unexpected failure: %#v", failed.Error)
	}
	details := failed.Error.Details
	reason, _ := details["reason"].(string)
	errorType, _ := details["error_type"].(string)
	if !strings.Contains(reason, "unexpected end of JSON input") || errorType == "" {
		t.Fatalf("missing diagnostic reason/type: %#v", details)
	}
	if details["streaming"] != true || details["network_attempted"] != true || details["provider_kind"] != "openai" || details["api_mode"] != "chat_completions" || details["provider_response_parse_error"] != true {
		t.Fatalf("missing stream diagnostic details: %#v", details)
	}
	if details["status_code"] != http.StatusOK || details["content_type"] != "text/event-stream" || details["provider_request_id"] != "req_openai_tail_1" {
		t.Fatalf("missing response capture metadata: %#v", details)
	}
	tail, _ := details["provider_response_tail"].(string)
	if !strings.Contains(tail, "chatcmpl_1") || !strings.Contains(tail, "chat.completion.chunk") || strings.Contains(tail, "HTTP/1.1") {
		t.Fatalf("unexpected response tail: %#v", details)
	}
}

func TestOpenAISDKGateway_ImageEditUsesMultipartSDKPath(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Fatalf("expected multipart request, got %s", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `name="image"`) || !strings.Contains(string(body), `name="prompt"`) {
			t.Fatalf("missing multipart fields: %s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"iVBORw0KGgppbWFnZQ=="}]}`))
	}))
	defer server.Close()

	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIResponses}}).(interface {
		GenerateImage(context.Context, string, ImageGenerationRequest) (GeneratedImage, error)
	})
	image, err := gateway.GenerateImage(context.Background(), "gpt-image-1", ImageGenerationRequest{Prompt: "edit", InputImages: []ContentPart{{Type: "image", Attachment: &messagecontent.AttachmentRef{MimeType: "image/png"}, Data: []byte("\x89PNG\r\n\x1a\nimage")}}, ForceOpenAIImageAPI: true})
	if err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}
	if image.ProviderKind != "openai" || image.MimeType != "image/png" || len(image.Bytes) == 0 {
		t.Fatalf("unexpected image: %#v", image)
	}
}

func TestOpenAISDKGateway_ResponsesPayloadUsesInstructionsAndResponsesTools(t *testing.T) {
	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key"}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIResponses}}).(*openAISDKGateway)
	desc := "Echo text"
	payload, _, _, err := gateway.responsesPayload(Request{
		Model: "gpt",
		Messages: []Message{
			{Role: "system", Content: []ContentPart{{Text: "system rules"}}},
			{Role: "user", Content: []ContentPart{{Text: "hello"}}},
		},
		Tools: []ToolSpec{{Name: "echo", Description: &desc, JSONSchema: map[string]any{"type": "object"}}},
	}, "call")
	if err != nil {
		t.Fatalf("responsesPayload: %v", err)
	}
	if payload["instructions"] != "system rules" {
		t.Fatalf("missing instructions: %#v", payload)
	}
	input := payload["input"].([]map[string]any)
	if len(input) != 1 || input[0]["role"] != "user" {
		t.Fatalf("system message leaked into input: %#v", input)
	}
	tools := payload["tools"].([]map[string]any)
	if len(tools) != 1 || tools[0]["name"] != "echo" || tools[0]["function"] != nil {
		t.Fatalf("unexpected responses tools shape: %#v", tools)
	}
}

func TestOpenAISDKGateway_DebugEventsEmitResponseChunks(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(openAISDKSSE([]string{`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`})))
	}))
	defer server.Close()

	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL, EmitDebugEvents: true}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIChatCompletions}})
	var debug *StreamLlmResponseChunk
	if err := gateway.Stream(context.Background(), Request{Model: "gpt", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}}, func(event StreamEvent) error {
		if ev, ok := event.(StreamLlmResponseChunk); ok {
			debug = &ev
		}
		return nil
	}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if debug == nil || debug.ProviderKind != "openai" || debug.APIMode != "chat_completions" || !strings.Contains(debug.Raw, "chatcmpl_1") {
		t.Fatalf("missing debug chunk: %#v", debug)
	}
}

func TestOpenAISDKGateway_ChatCompletionsPartialToolStreamDoesNotEmitFinalToolCall(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(openAISDKSSE([]string{`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"text\":"}}]},"finish_reason":null}]}`})))
	}))
	defer server.Close()

	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIChatCompletions}})
	var failed *StreamRunFailed
	var finalTool *ToolCall
	if err := gateway.Stream(context.Background(), Request{Model: "gpt", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}}, func(event StreamEvent) error {
		switch ev := event.(type) {
		case StreamRunFailed:
			failed = &ev
		case ToolCall:
			finalTool = &ev
		}
		return nil
	}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if failed == nil || failed.Error.ErrorClass != ErrorClassProviderRetryable || finalTool != nil {
		t.Fatalf("unexpected terminal events failed=%#v tool=%#v", failed, finalTool)
	}
}

func TestOpenAISDKGateway_ResponsesSpecificToolChoiceShape(t *testing.T) {
	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key"}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIResponses}}).(*openAISDKGateway)
	payload, _, _, err := gateway.responsesPayload(Request{
		Model:      "gpt",
		Messages:   []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}},
		Tools:      []ToolSpec{{Name: "echo", JSONSchema: map[string]any{"type": "object"}}},
		ToolChoice: &ToolChoice{Mode: "specific", ToolName: "echo"},
	}, "call")
	if err != nil {
		t.Fatalf("responsesPayload: %v", err)
	}
	choice := payload["tool_choice"].(map[string]any)
	if choice["type"] != "function" || choice["name"] != "echo" || choice["function"] != nil {
		t.Fatalf("unexpected responses tool_choice: %#v", choice)
	}
}

func TestOpenAISDKResponsesState_ToolDeltaAndCompletedFallback(t *testing.T) {
	var events []StreamEvent
	state := newOpenAISDKResponsesState(context.Background(), "llm_1", func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	chunks := []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"echo","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"text\":\"hi\"}"}`,
		`{"type":"response.completed","response":{"id":"resp_1","output":[],"usage":{"input_tokens":1,"output_tokens":2}}}`,
	}
	for _, chunk := range chunks {
		var event responses.ResponseStreamEventUnion
		if err := json.Unmarshal([]byte(chunk), &event); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if err := state.handle(event); err != nil {
			t.Fatalf("handle: %v", err)
		}
	}
	var delta *ToolCallArgumentDelta
	var tool *ToolCall
	var completed *StreamRunCompleted
	for _, event := range events {
		switch ev := event.(type) {
		case ToolCallArgumentDelta:
			delta = &ev
		case ToolCall:
			tool = &ev
		case StreamRunCompleted:
			completed = &ev
		}
	}
	if delta == nil || delta.ToolCallID != "call_1" || delta.ToolName != "echo" || delta.ArgumentsDelta != `{"text":"hi"}` {
		t.Fatalf("unexpected delta: %#v", delta)
	}
	if tool == nil || tool.ToolCallID != "call_1" || tool.ToolName != "echo" || tool.ArgumentsJSON["text"] != "hi" {
		t.Fatalf("unexpected tool fallback: %#v", tool)
	}
	if completed == nil || completed.Usage == nil {
		t.Fatalf("missing completion: %#v", completed)
	}
}

func TestOpenAISDKResponsesState_ErrorEvent(t *testing.T) {
	var failed *StreamRunFailed
	state := newOpenAISDKResponsesState(context.Background(), "llm_1", func(event StreamEvent) error {
		if ev, ok := event.(StreamRunFailed); ok {
			failed = &ev
		}
		return nil
	})
	var event responses.ResponseStreamEventUnion
	if err := json.Unmarshal([]byte(`{"type":"error","message":"bad request"}`), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := state.handle(event); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if failed == nil || failed.Error.Message != "bad request" {
		t.Fatalf("unexpected failure: %#v", failed)
	}
}

func TestOpenAISDKResponsesState_CompletedOnlyEmitsVisibleTextDelta(t *testing.T) {
	var deltas []string
	var completed *StreamRunCompleted
	state := newOpenAISDKResponsesState(context.Background(), "llm_1", func(event StreamEvent) error {
		switch ev := event.(type) {
		case StreamMessageDelta:
			if ev.Channel == nil {
				deltas = append(deltas, ev.ContentDelta)
			}
		case StreamRunCompleted:
			completed = &ev
		}
		return nil
	})
	var event responses.ResponseStreamEventUnion
	chunk := `{"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"final answer"}]}],"usage":{"input_tokens":1,"output_tokens":2}}}`
	if err := json.Unmarshal([]byte(chunk), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := state.handle(event); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(deltas) != 1 || deltas[0] != "final answer" {
		t.Fatalf("unexpected deltas: %#v", deltas)
	}
	if completed == nil || completed.AssistantMessage == nil || VisibleMessageText(*completed.AssistantMessage) != "final answer" {
		t.Fatalf("unexpected completion: %#v", completed)
	}
}

func TestOpenAISDKResponsesState_CompletedMessageCarriesStreamedVisibleDelta(t *testing.T) {
	var deltas []string
	var completed *StreamRunCompleted
	state := newOpenAISDKResponsesState(context.Background(), "llm_1", func(event StreamEvent) error {
		switch ev := event.(type) {
		case StreamMessageDelta:
			if ev.Channel == nil {
				deltas = append(deltas, ev.ContentDelta)
			}
		case StreamRunCompleted:
			completed = &ev
		}
		return nil
	})
	chunks := []string{
		`{"type":"response.output_text.delta","delta":"hello "}`,
		`{"type":"response.output_text.delta","delta":"world"}`,
		`{"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","content":null}],"usage":{"input_tokens":1,"output_tokens":2}}}`,
	}
	for _, chunk := range chunks {
		var event responses.ResponseStreamEventUnion
		if err := json.Unmarshal([]byte(chunk), &event); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if err := state.handle(event); err != nil {
			t.Fatalf("handle: %v", err)
		}
	}

	if strings.Join(deltas, "") != "hello world" {
		t.Fatalf("unexpected deltas: %#v", deltas)
	}
	if completed == nil || completed.AssistantMessage == nil {
		t.Fatalf("missing completion: %#v", completed)
	}
	if got := VisibleMessageText(*completed.AssistantMessage); got != "hello world" {
		t.Fatalf("expected completed assistant message to carry streamed text, got %q", got)
	}
}

func TestOpenAISDKResponsesState_CompletedMessageCarriesReasoningDelta(t *testing.T) {
	var thinkingDeltas []string
	var completed *StreamRunCompleted
	var toolCalls []ToolCall
	state := newOpenAISDKResponsesState(context.Background(), "llm_1", func(event StreamEvent) error {
		switch ev := event.(type) {
		case StreamMessageDelta:
			if ev.Channel != nil && *ev.Channel == "thinking" {
				thinkingDeltas = append(thinkingDeltas, ev.ContentDelta)
			}
		case ToolCall:
			toolCalls = append(toolCalls, ev)
		case StreamRunCompleted:
			completed = &ev
		}
		return nil
	})
	chunks := []string{
		`{"type":"response.reasoning_summary_text.delta","delta":"plan"}`,
		`{"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"final answer"}]},{"type":"function_call","call_id":"call_1","name":"echo","arguments":"{\"text\":\"hi\"}"}],"usage":{"input_tokens":1,"output_tokens":2}}}`,
	}
	for _, chunk := range chunks {
		var event responses.ResponseStreamEventUnion
		if err := json.Unmarshal([]byte(chunk), &event); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if err := state.handle(event); err != nil {
			t.Fatalf("handle: %v", err)
		}
	}
	if strings.Join(thinkingDeltas, "") != "plan" {
		t.Fatalf("unexpected thinking deltas: %#v", thinkingDeltas)
	}
	if len(toolCalls) != 1 || toolCalls[0].ToolCallID != "call_1" {
		t.Fatalf("unexpected tool calls: %#v", toolCalls)
	}
	if completed == nil || completed.AssistantMessage == nil {
		t.Fatalf("missing completion: %#v", completed)
	}
	parts := completed.AssistantMessage.Content
	if len(parts) != 2 || parts[0].Kind() != "thinking" || parts[0].Text != "plan" || parts[1].Text != "final answer" {
		t.Fatalf("unexpected assistant parts: %#v", parts)
	}
}

func TestParseOpenAIResponsesAssistantResponseCarriesReasoningItem(t *testing.T) {
	message, _, _, _, _, err := parseOpenAIResponsesAssistantResponse(map[string]any{
		"output": []any{
			map[string]any{
				"type": "reasoning",
				"summary": []any{
					map[string]any{"type": "summary_text", "text": "plan"},
				},
			},
			map[string]any{
				"type":    "message",
				"role":    "assistant",
				"content": []any{map[string]any{"type": "output_text", "text": "final answer"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("parseOpenAIResponsesAssistantResponse: %v", err)
	}
	if len(message.Content) != 2 || message.Content[0].Kind() != "thinking" || message.Content[0].Text != "plan" || message.Content[1].Text != "final answer" {
		t.Fatalf("unexpected assistant message: %#v", message)
	}
}

func TestOpenAISDKGateway_ProviderOversizeDetails(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(`{"error":{"message":"too large","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIChatCompletions}})
	var failed *StreamRunFailed
	if err := gateway.Stream(context.Background(), Request{Model: "gpt", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}}, func(event StreamEvent) error {
		if ev, ok := event.(StreamRunFailed); ok {
			failed = &ev
		}
		return nil
	}); err != nil {
		t.Fatalf("Stream returned unexpected error: %v", err)
	}
	if failed == nil {
		t.Fatalf("missing failure")
	}
	if failed.Error.Details["status_code"] != http.StatusRequestEntityTooLarge || failed.Error.Details["network_attempted"] != true || failed.Error.Details["oversize_phase"] != OversizePhaseProvider {
		t.Fatalf("missing oversize details: %#v", failed.Error.Details)
	}
}

func TestClassifyOpenAIStatusBadRequest(t *testing.T) {
	cases := []struct {
		name    string
		details map[string]any
		want    string
	}{
		{
			name: "context_length_exceeded",
			details: map[string]any{
				"openai_error_code": "context_length_exceeded",
			},
			want: ErrorClassProviderNonRetryable,
		},
		{
			name: "invalid_request_error",
			details: map[string]any{
				"openai_error_type": "invalid_request_error",
			},
			want: ErrorClassProviderNonRetryable,
		},
		{
			name: "rate_limit_code",
			details: map[string]any{
				"openai_error_code": "rate_limit_exceeded",
			},
			want: ErrorClassProviderRetryable,
		},
		{
			name: "rate_limit_type",
			details: map[string]any{
				"openai_error_type": "rate_limit_error",
			},
			want: ErrorClassProviderRetryable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyOpenAIStatus(http.StatusBadRequest, tc.details)
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestOpenAISDKGateway_BadRequestInvalidRequestIsNonRetryable(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req_openai_1")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Error from provider (DeepSeek): The reasoning_content in the thinking mode must be passed back to the API.","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIChatCompletions}})
	var failed *StreamRunFailed
	if err := gateway.Stream(context.Background(), Request{Model: "deepseek", Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}}}, func(event StreamEvent) error {
		if ev, ok := event.(StreamRunFailed); ok {
			failed = &ev
		}
		return nil
	}); err != nil {
		t.Fatalf("Stream returned unexpected error: %v", err)
	}
	if failed == nil {
		t.Fatalf("missing failure")
	}
	if failed.Error.ErrorClass != ErrorClassProviderNonRetryable {
		t.Fatalf("expected non-retryable error, got %q", failed.Error.ErrorClass)
	}
	if !strings.Contains(failed.Error.Message, "reasoning_content") {
		t.Fatalf("expected provider message, got %q", failed.Error.Message)
	}
	details := failed.Error.Details
	if details["provider_kind"] != "openai" || details["api_mode"] != "chat_completions" || details["network_attempted"] != true || details["streaming"] != true {
		t.Fatalf("missing provider diagnostics: %#v", details)
	}
	if details["openai_error_type"] != "invalid_request_error" || details["provider_request_id"] != "req_openai_1" {
		t.Fatalf("missing OpenAI error details: %#v", details)
	}
	if raw, _ := details["provider_error_body"].(string); !strings.Contains(raw, "reasoning_content") || strings.Contains(raw, "HTTP/1.1") {
		t.Fatalf("unexpected provider error body: %#v", details)
	}
}

func TestOpenAIResponsesInputUsesResponsesFunctionCallIDForProviderAgnosticToolCallID(t *testing.T) {
	input, err := toOpenAIResponsesInput([]Message{{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ToolCallID:    "toolu_0139RxnwMxUiL4oU5fgtiVqh",
			ToolName:      "echo",
			ArgumentsJSON: map[string]any{"text": "hi"},
		}},
	}})
	if err != nil {
		t.Fatalf("toOpenAIResponsesInput: %v", err)
	}
	if len(input) != 1 {
		t.Fatalf("expected one input item, got %#v", input)
	}
	item := input[0]
	if item["type"] != "function_call" || item["call_id"] != "toolu_0139RxnwMxUiL4oU5fgtiVqh" {
		t.Fatalf("unexpected function call item: %#v", item)
	}
	id, _ := item["id"].(string)
	if !strings.HasPrefix(id, "fc_hist_") {
		t.Fatalf("responses function_call id must be provider-local fc id, got %#v", item)
	}
}

func TestOpenAIHistoryPreservesDisplayDescriptionInToolArguments(t *testing.T) {
	call := ToolCall{
		ToolCallID:         "call_1",
		ToolName:           "exec_command",
		ArgumentsJSON:      map[string]any{"command": "git status"},
		DisplayDescription: "Checking status",
	}
	messages, err := toOpenAIChatMessages([]Message{{Role: "assistant", ToolCalls: []ToolCall{call}}})
	if err != nil {
		t.Fatalf("toOpenAIChatMessages: %v", err)
	}
	toolCalls := messages[0]["tool_calls"].([]map[string]any)
	function := toolCalls[0]["function"].(map[string]any)
	var chatArgs map[string]any
	if err := json.Unmarshal([]byte(function["arguments"].(string)), &chatArgs); err != nil {
		t.Fatalf("chat arguments json: %v", err)
	}
	if chatArgs["display_description"] != "Checking status" {
		t.Fatalf("chat arguments lost display_description: %#v", chatArgs)
	}

	input, err := toOpenAIResponsesInput([]Message{{Role: "assistant", ToolCalls: []ToolCall{call}}})
	if err != nil {
		t.Fatalf("toOpenAIResponsesInput: %v", err)
	}
	var responsesArgs map[string]any
	if err := json.Unmarshal([]byte(input[0]["arguments"].(string)), &responsesArgs); err != nil {
		t.Fatalf("responses arguments json: %v", err)
	}
	if responsesArgs["display_description"] != "Checking status" {
		t.Fatalf("responses arguments lost display_description: %#v", responsesArgs)
	}
}

func TestOpenAIChatMessagesCarryAssistantThinkingAsReasoningContent(t *testing.T) {
	messages, err := toOpenAIChatMessages([]Message{{
		Role: "assistant",
		Content: []ContentPart{
			{Type: "thinking", Text: "first"},
			{Type: "thinking", Text: " second"},
			{Text: "answer"},
		},
		ToolCalls: []ToolCall{{
			ToolCallID:    "call_1",
			ToolName:      "echo",
			ArgumentsJSON: map[string]any{"text": "hi"},
		}},
	}})
	if err != nil {
		t.Fatalf("toOpenAIChatMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one message, got %#v", messages)
	}
	if messages[0]["content"] != "answer" || messages[0]["reasoning_content"] != "first second" {
		t.Fatalf("unexpected assistant message: %#v", messages[0])
	}
}

func TestOpenAIChatMessagesAddEmptyReasoningContentForAssistantToolCalls(t *testing.T) {
	messages, err := toOpenAIChatMessages([]Message{{
		Role:    "assistant",
		Content: []ContentPart{{Text: "answer"}},
		ToolCalls: []ToolCall{{
			ToolCallID:    "call_1",
			ToolName:      "echo",
			ArgumentsJSON: map[string]any{"text": "hi"},
		}},
	}})
	if err != nil {
		t.Fatalf("toOpenAIChatMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one message, got %#v", messages)
	}
	if messages[0]["content"] != "answer" || messages[0]["reasoning_content"] != "" {
		t.Fatalf("unexpected assistant message: %#v", messages[0])
	}
}

func TestOpenAIResponsesInputDropsAssistantThinkingWireField(t *testing.T) {
	input, err := toOpenAIResponsesInput([]Message{{
		Role:    "assistant",
		Content: []ContentPart{{Type: "thinking", Text: ""}},
		ToolCalls: []ToolCall{{
			ToolCallID:    "call_1",
			ToolName:      "echo",
			ArgumentsJSON: map[string]any{"text": "hi"},
		}},
	}})
	if err != nil {
		t.Fatalf("toOpenAIResponsesInput: %v", err)
	}
	if len(input) != 1 {
		t.Fatalf("expected only function call, got %#v", input)
	}
	if input[0]["type"] != "function_call" || input[0]["call_id"] != "call_1" {
		t.Fatalf("unexpected responses assistant item: %#v", input[0])
	}
	if _, ok := input[0]["reasoning_content"]; ok {
		t.Fatalf("responses input must not include reasoning_content: %#v", input[0])
	}
}

func TestOpenAIToolsNormalizeEmptyParameters(t *testing.T) {
	chatTools := toOpenAITools([]ToolSpec{{Name: "memory_status"}})
	chatFunction := chatTools[0]["function"].(map[string]any)
	chatParams := chatFunction["parameters"].(map[string]any)
	if chatParams["type"] != "object" || chatParams["properties"] == nil {
		t.Fatalf("chat tool parameters must be object schema: %#v", chatParams)
	}

	responsesTools := toOpenAIResponsesTools([]ToolSpec{{Name: "memory_status", JSONSchema: map[string]any{}}})
	responsesParams := responsesTools[0]["parameters"].(map[string]any)
	if responsesParams["type"] != "object" || responsesParams["properties"] == nil {
		t.Fatalf("responses tool parameters must be object schema: %#v", responsesParams)
	}
}

func TestOpenAISDKGateway_RequestBodyKeepsEmptyToolSchemaObjects(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	rawByPath := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		rawByPath[r.URL.Path] = string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		switch r.URL.Path {
		case "/chat/completions":
			_, _ = w.Write([]byte(openAISDKSSE([]string{
				`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`,
			})))
		case "/responses":
			_, _ = w.Write([]byte(openAISDKSSE([]string{
				`{"type":"response.completed","response":{"id":"resp_1","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`,
			})))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	var nilProperties map[string]any
	var nilRequired []string
	request := Request{
		Model:    "gpt",
		Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}},
		Tools: []ToolSpec{{
			Name: "enter_plan_mode",
			JSONSchema: map[string]any{
				"type":       "object",
				"properties": nilProperties,
				"required":   nilRequired,
			},
		}},
	}

	for _, kind := range []ProtocolKind{ProtocolKindOpenAIChatCompletions, ProtocolKindOpenAIResponses} {
		gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{
			Transport: TransportConfig{APIKey: "key", BaseURL: server.URL},
			Protocol:  OpenAIProtocolConfig{PrimaryKind: kind},
		})
		if err := gateway.Stream(context.Background(), request, func(StreamEvent) error { return nil }); err != nil {
			t.Fatalf("%s stream: %v", kind, err)
		}
	}

	assertOpenAISDKToolSchemaBody(t, rawByPath["/chat/completions"], true)
	assertOpenAISDKToolSchemaBody(t, rawByPath["/responses"], false)
}

func assertOpenAISDKToolSchemaBody(t *testing.T, raw string, chat bool) {
	t.Helper()
	if raw == "" {
		t.Fatal("missing captured request body")
	}
	for _, fragment := range []string{`"parameters":null`, `"properties":null`, `"required":null`} {
		if strings.Contains(raw, fragment) {
			t.Fatalf("request body contains %s: %s", fragment, raw)
		}
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("unexpected tools payload: %#v", payload["tools"])
	}
	tool := tools[0].(map[string]any)
	var parameters any
	if chat {
		function := tool["function"].(map[string]any)
		parameters = function["parameters"]
	} else {
		parameters = tool["parameters"]
	}
	params, ok := parameters.(map[string]any)
	if !ok {
		t.Fatalf("parameters must be an object: %#v", parameters)
	}
	if properties, ok := params["properties"].(map[string]any); !ok || properties == nil {
		t.Fatalf("properties must be an object: %#v", params["properties"])
	}
	if required, ok := params["required"].([]any); !ok || required == nil {
		t.Fatalf("required must be an array: %#v", params["required"])
	}
}

func TestOpenAISDKGateway_QuirkRetryEchoReasoningContent(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"error":{"message":"Error from provider (DeepSeek): The reasoning_content in the thinking mode must be passed back to the API.","type":"invalid_request_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(openAISDKSSE([]string{
			`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`,
		})))
	}))
	defer server.Close()

	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIChatCompletions}})
	var completed *StreamRunCompleted
	var learned []StreamQuirkLearned
	if err := gateway.Stream(context.Background(), Request{
		Model: "deepseek",
		Messages: []Message{
			{Role: "user", Content: []ContentPart{{Text: "hello"}}},
			{Role: "assistant", Content: []ContentPart{{Text: "earlier"}}},
			{Role: "user", Content: []ContentPart{{Text: "again"}}},
		},
	}, func(event StreamEvent) error {
		switch ev := event.(type) {
		case StreamRunCompleted:
			completed = &ev
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
	if completed == nil {
		t.Fatalf("expected completion after retry")
	}
	if !gateway.(*openAISDKGateway).quirks.Has(QuirkEchoReasoningContent) {
		t.Fatalf("quirk not stored")
	}
	if len(learned) != 1 || learned[0].ProviderKind != "openai" || learned[0].QuirkID != string(QuirkEchoReasoningContent) {
		t.Fatalf("expected one StreamQuirkLearned event, got %#v", learned)
	}
	secondMsgs := bodies[1]["messages"].([]any)
	sawAssistantWithReasoning := false
	for _, raw := range secondMsgs {
		m := raw.(map[string]any)
		if m["role"] != "assistant" {
			continue
		}
		if _, ok := m["reasoning_content"]; ok {
			sawAssistantWithReasoning = true
		}
	}
	if !sawAssistantWithReasoning {
		t.Fatalf("retry payload missing reasoning_content on assistant message: %#v", secondMsgs)
	}
}

func TestOpenAISDKGateway_QuirkRetryDowngradeXHighReasoning(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"code":"400","message":"Error from provider (Xiaomi): [{'type': 'literal_error', 'loc': ('body', 'reasoning_effort'), 'msg': \"Input should be 'low', 'medium' or 'high'\", 'input': 'xhigh', 'ctx': {'expected': \"'low', 'medium' or 'high'\"}}]","param":"","type":"Bad Request"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(openAISDKSSE([]string{
			`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"gpt","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`,
		})))
	}))
	defer server.Close()

	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIChatCompletions}})
	var completed *StreamRunCompleted
	var learned []StreamQuirkLearned
	if err := gateway.Stream(context.Background(), Request{
		Model:         "xiaomi",
		Messages:      []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}},
		ReasoningMode: "xhigh",
	}, func(event StreamEvent) error {
		switch ev := event.(type) {
		case StreamRunCompleted:
			completed = &ev
		case StreamQuirkLearned:
			learned = append(learned, ev)
		}
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if attempts != 2 || len(bodies) != 2 {
		t.Fatalf("expected retry, got attempts=%d bodies=%#v", attempts, bodies)
	}
	if completed == nil {
		t.Fatalf("expected completion after retry")
	}
	if bodies[0]["reasoning_effort"] != "xhigh" || bodies[1]["reasoning_effort"] != "high" {
		t.Fatalf("expected xhigh then high, got %#v", bodies)
	}
	if !gateway.(*openAISDKGateway).quirks.Has(QuirkDowngradeXHighReasoning) {
		t.Fatalf("quirk not stored")
	}
	if len(learned) != 1 || learned[0].ProviderKind != "openai" || learned[0].QuirkID != string(QuirkDowngradeXHighReasoning) {
		t.Fatalf("expected one StreamQuirkLearned event, got %#v", learned)
	}
}

func TestOpenAISDKGateway_QuirkRetryStripToolChoice(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"message":"deepseek-reasoner does not support this tool_choice","type":"invalid_request_error","code":"invalid_request_error"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(openAISDKSSE([]string{
			`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"deepseek","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`,
		})))
	}))
	defer server.Close()

	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIChatCompletions}})
	var completed *StreamRunCompleted
	var learned []StreamQuirkLearned
	if err := gateway.Stream(context.Background(), Request{
		Model:    "deepseek-v4-flash",
		Messages: []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}},
		Tools: []ToolSpec{{
			Name:       "heartbeat_decision",
			JSONSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		}},
		ToolChoice: &ToolChoice{Mode: "specific", ToolName: "heartbeat_decision"},
	}, func(event StreamEvent) error {
		switch ev := event.(type) {
		case StreamRunCompleted:
			completed = &ev
		case StreamQuirkLearned:
			learned = append(learned, ev)
		}
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if attempts != 2 || len(bodies) != 2 {
		t.Fatalf("expected retry, got attempts=%d bodies=%#v", attempts, bodies)
	}
	if completed == nil {
		t.Fatalf("expected completion after retry")
	}
	if _, ok := bodies[0]["tool_choice"]; !ok {
		t.Fatalf("first payload should include tool_choice: %#v", bodies[0])
	}
	if _, ok := bodies[1]["tool_choice"]; ok {
		t.Fatalf("retry payload should strip tool_choice: %#v", bodies[1])
	}
	if _, ok := bodies[1]["tools"]; !ok {
		t.Fatalf("retry payload should preserve tools: %#v", bodies[1])
	}
	if !gateway.(*openAISDKGateway).quirks.Has(QuirkStripToolChoice) {
		t.Fatalf("quirk not stored")
	}
	if len(learned) != 1 || learned[0].ProviderKind != "openai" || learned[0].QuirkID != string(QuirkStripToolChoice) {
		t.Fatalf("expected one StreamQuirkLearned event, got %#v", learned)
	}
}

func TestOpenAISDKGateway_ResponsesQuirkRetryDowngradeXHighReasoning(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	var attempts int
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		bodies = append(bodies, body)
		attempts++
		if attempts == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"400","message":"Error from provider (Xiaomi): [{'type': 'literal_error', 'loc': ('body', 'reasoning_effort'), 'msg': \"Input should be 'low', 'medium' or 'high'\", 'input': 'xhigh', 'ctx': {'expected': \"'low', 'medium' or 'high'\"}}]","param":"","type":"Bad Request"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(openAISDKSSE([]string{
			`{"type":"response.completed","response":{"id":"resp_1","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1}}}`,
		})))
	}))
	defer server.Close()

	gateway := NewOpenAIGatewaySDK(OpenAIGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIResponses}})
	var completed *StreamRunCompleted
	var learned []StreamQuirkLearned
	if err := gateway.Stream(context.Background(), Request{
		Model:         "xiaomi",
		Messages:      []Message{{Role: "user", Content: []ContentPart{{Text: "hello"}}}},
		ReasoningMode: "xhigh",
	}, func(event StreamEvent) error {
		switch ev := event.(type) {
		case StreamRunCompleted:
			completed = &ev
		case StreamQuirkLearned:
			learned = append(learned, ev)
		}
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if attempts != 2 || len(bodies) != 2 {
		t.Fatalf("expected retry, got attempts=%d bodies=%#v", attempts, bodies)
	}
	if completed == nil {
		t.Fatalf("expected completion after retry")
	}
	firstReasoning := bodies[0]["reasoning"].(map[string]any)
	secondReasoning := bodies[1]["reasoning"].(map[string]any)
	if firstReasoning["effort"] != "xhigh" || secondReasoning["effort"] != "high" {
		t.Fatalf("expected xhigh then high, got %#v", bodies)
	}
	if !gateway.(*openAISDKGateway).quirks.Has(QuirkDowngradeXHighReasoning) {
		t.Fatalf("quirk not stored")
	}
	if len(learned) != 1 || learned[0].ProviderKind != "openai" || learned[0].QuirkID != string(QuirkDowngradeXHighReasoning) {
		t.Fatalf("expected one StreamQuirkLearned event, got %#v", learned)
	}
}
