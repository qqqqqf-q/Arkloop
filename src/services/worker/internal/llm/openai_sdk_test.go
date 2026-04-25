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
	state := newOpenAISDKResponsesState("llm_1", func(event StreamEvent) error {
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
	state := newOpenAISDKResponsesState("llm_1", func(event StreamEvent) error {
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
	state := newOpenAISDKResponsesState("llm_1", func(event StreamEvent) error {
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

func TestOpenAIResponsesInputCarriesEmptyAssistantThinkingAsReasoningContent(t *testing.T) {
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
	if len(input) != 2 {
		t.Fatalf("expected reasoning message plus function call, got %#v", input)
	}
	if input[0]["type"] != "message" || input[0]["reasoning_content"] != "" {
		t.Fatalf("missing empty reasoning_content: %#v", input[0])
	}
	if content, ok := input[0]["content"].([]map[string]any); !ok || len(content) != 0 {
		t.Fatalf("thinking must not leak into response content: %#v", input[0])
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
