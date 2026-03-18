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
)

func TestOpenAIToolMessage_IncludesErrorWhenResultAlsoPresent(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"tool_call_id": "call_1",
		"result": map[string]any{
			"dedup":            "same_args_as_previous",
			"ref_tool_call_id": "call_0",
		},
		"error": map[string]any{
			"error_class": "tool.memory_provider_error",
			"message":     "memory write failed",
		},
	})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	msg := toOpenAIToolMessage(string(raw))
	if msg["tool_call_id"] != "call_1" {
		t.Fatalf("unexpected tool_call_id: %v", msg["tool_call_id"])
	}
	content, _ := msg["content"].(string)
	if !strings.Contains(content, "tool.memory_provider_error") {
		t.Fatalf("expected error info in tool content, got %q", content)
	}
}

func TestOpenAIGateway_Stream_ToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "id":"chatcmpl_test",
  "object":"chat.completion",
  "choices":[
    {
      "index":0,
      "message":{
        "role":"assistant",
        "content":null,
        "tool_calls":[
          {
            "id":"call_1",
            "type":"function",
            "function":{
              "name":"web_search",
              "arguments":"{\"query\":\"hello\"}"
            }
          }
        ]
      },
      "finish_reason":"tool_calls"
    }
  ]
}`))
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		APIMode:         "chat_completions",
		EmitDebugEvents: false,
	})

	events := []StreamEvent{}
	err := gateway.Stream(context.Background(), Request{
		Model: "gpt-test",
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

	var gotCall *ToolCall
	for _, item := range events {
		call, ok := item.(ToolCall)
		if !ok {
			continue
		}
		gotCall = &call
		break
	}
	if gotCall == nil {
		t.Fatalf("expected tool call event, got %d events: %v", len(events), streamEventTypes(events))
	}
	if gotCall.ToolCallID != "call_1" {
		t.Fatalf("unexpected tool_call_id: %q", gotCall.ToolCallID)
	}
	if gotCall.ToolName != "web_search" {
		t.Fatalf("unexpected tool_name: %q", gotCall.ToolName)
	}
	if gotCall.ArgumentsJSON["query"] != "hello" {
		t.Fatalf("unexpected arguments_json: %#v", gotCall.ArgumentsJSON)
	}

	if _, ok := events[len(events)-1].(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
}

func TestOpenAIGateway_Stream_Responses_ToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "id":"resp_test",
  "object":"response",
  "output":[
    {
      "id":"call_1",
      "type":"function_call",
      "name":"web_search",
      "arguments":"{\"query\":\"hello\"}"
    },
    {
      "id":"msg_test",
      "type":"message",
      "role":"assistant",
      "content":[
        {"type":"output_text","text":"ok"}
      ]
    }
  ]
}`))
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		APIMode:         "responses",
		EmitDebugEvents: false,
	})

	events := []StreamEvent{}
	err := gateway.Stream(context.Background(), Request{
		Model: "gpt-test",
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

	var gotText *StreamMessageDelta
	var gotCall *ToolCall
	for _, item := range events {
		switch typed := item.(type) {
		case StreamMessageDelta:
			gotText = &typed
		case ToolCall:
			copy := typed
			gotCall = &copy
		}
	}
	if gotText == nil || gotText.ContentDelta != "ok" {
		t.Fatalf("expected message delta ok, got %#v", gotText)
	}
	if gotCall == nil {
		t.Fatalf("expected tool call event, got %d events: %v", len(events), streamEventTypes(events))
	}
	if gotCall.ToolCallID != "call_1" {
		t.Fatalf("unexpected tool_call_id: %q", gotCall.ToolCallID)
	}
	if gotCall.ToolName != "web_search" {
		t.Fatalf("unexpected tool_name: %q", gotCall.ToolName)
	}
	if gotCall.ArgumentsJSON["query"] != "hello" {
		t.Fatalf("unexpected arguments_json: %#v", gotCall.ArgumentsJSON)
	}

	if _, ok := events[len(events)-1].(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
}

func TestOpenAIGateway_Stream_Auto_Fallback_To_ChatCompletions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses":
			w.WriteHeader(http.StatusNotFound)
			return
		case "/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
  "id":"chatcmpl_test",
  "object":"chat.completion",
  "choices":[
    {
      "index":0,
      "message":{
        "role":"assistant",
        "content":null,
        "tool_calls":[
          {
            "id":"call_1",
            "type":"function",
            "function":{
              "name":"web_search",
              "arguments":"{\"query\":\"hello\"}"
            }
          }
        ]
      },
      "finish_reason":"tool_calls"
    }
  ]
}`))
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		APIMode:         "auto",
		EmitDebugEvents: false,
	})

	events := []StreamEvent{}
	err := gateway.Stream(context.Background(), Request{
		Model: "gpt-test",
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

	var gotFallback *StreamProviderFallback
	for _, item := range events {
		fb, ok := item.(StreamProviderFallback)
		if !ok {
			continue
		}
		gotFallback = &fb
		break
	}
	if gotFallback == nil {
		t.Fatalf("expected fallback event, got %d events", len(events))
	}
	if gotFallback.FromAPIMode != "responses" || gotFallback.ToAPIMode != "chat_completions" {
		t.Fatalf("unexpected fallback: %#v", gotFallback)
	}
	if gotFallback.StatusCode == nil || *gotFallback.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected fallback status: %#v", gotFallback.StatusCode)
	}
}

func TestToOpenAIResponsesInput_AssistantUsesOutputText(t *testing.T) {
	items, err := toOpenAIResponsesInput([]Message{
		{Role: "user", Content: []TextPart{{Text: "hi"}}},
		{Role: "assistant", Content: []TextPart{{Text: "ok"}}},
	})
	if err != nil {
		t.Fatalf("toOpenAIResponsesInput failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("unexpected items len: %d", len(items))
	}

	user := items[0]
	userContent, _ := user["content"].([]map[string]any)
	if role, _ := user["role"].(string); role != "user" {
		t.Fatalf("unexpected user role: %v", user["role"])
	}
	if len(userContent) != 1 || userContent[0]["type"] != "input_text" {
		t.Fatalf("unexpected user content: %#v", user["content"])
	}

	assistant := items[1]
	assistantContent, _ := assistant["content"].([]map[string]any)
	if role, _ := assistant["role"].(string); role != "assistant" {
		t.Fatalf("unexpected assistant role: %v", assistant["role"])
	}
	if len(assistantContent) != 1 || assistantContent[0]["type"] != "output_text" {
		t.Fatalf("unexpected assistant content: %#v", assistant["content"])
	}
}

func TestToOpenAIChatMessages_ToolEnvelope(t *testing.T) {
	messages := []Message{
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
	}

	out, err := toOpenAIChatMessages(messages)
	if err != nil {
		t.Fatalf("toOpenAIChatMessages failed: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}

	assistant := out[0]
	if assistant["role"] != "assistant" {
		t.Fatalf("unexpected assistant role: %#v", assistant["role"])
	}
	rawCalls, ok := assistant["tool_calls"].([]map[string]any)
	if !ok || len(rawCalls) != 1 {
		t.Fatalf("unexpected tool_calls: %#v", assistant["tool_calls"])
	}
	if rawCalls[0]["id"] != "call_1" {
		t.Fatalf("unexpected tool_call id: %#v", rawCalls[0]["id"])
	}
	fn, ok := rawCalls[0]["function"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected function payload: %#v", rawCalls[0]["function"])
	}
	if fn["name"] != "web_search" {
		t.Fatalf("unexpected function.name: %#v", fn["name"])
	}
	args, ok := fn["arguments"].(string)
	if !ok {
		t.Fatalf("unexpected function.arguments: %#v", fn["arguments"])
	}
	var parsedArgs map[string]any
	if err := json.Unmarshal([]byte(args), &parsedArgs); err != nil {
		t.Fatalf("arguments not valid json: %v", err)
	}
	if parsedArgs["query"] != "hello" {
		t.Fatalf("unexpected parsed args: %#v", parsedArgs)
	}

	tool := out[1]
	if tool["role"] != "tool" {
		t.Fatalf("unexpected tool role: %#v", tool["role"])
	}
	if tool["tool_call_id"] != "call_1" {
		t.Fatalf("unexpected tool_call_id: %#v", tool["tool_call_id"])
	}
	toolContent, ok := tool["content"].(string)
	if !ok {
		t.Fatalf("unexpected tool content: %#v", tool["content"])
	}
	var parsedContent map[string]any
	if err := json.Unmarshal([]byte(toolContent), &parsedContent); err != nil {
		t.Fatalf("tool content not valid json: %v", err)
	}
	if _, ok := parsedContent["items"]; !ok {
		t.Fatalf("expected items in tool content, got %#v", parsedContent)
	}
}

func TestOpenAIGateway_Stream_ChatCompletions_SSE_MessageDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		chunk1, _ := json.Marshal(map[string]any{
			"choices": []any{
				map[string]any{
					"delta": map[string]any{
						"role":    "assistant",
						"content": "he",
					},
				},
			},
		})
		_, _ = w.Write(append(append([]byte("data: "), chunk1...), []byte("\n\n")...))
		if flusher != nil {
			flusher.Flush()
		}
		chunk2, _ := json.Marshal(map[string]any{
			"choices": []any{
				map[string]any{
					"delta": map[string]any{
						"content": "llo",
					},
				},
			},
		})
		_, _ = w.Write(append(append([]byte("data: "), chunk2...), []byte("\n\n")...))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		APIMode:         "chat_completions",
		EmitDebugEvents: false,
	})

	events := []StreamEvent{}
	err := gateway.Stream(context.Background(), Request{
		Model: "gpt-test",
		Messages: []Message{
			{Role: "user", Content: []TextPart{{Text: "hi"}}},
		},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	deltas := []string{}
	for _, item := range events {
		delta, ok := item.(StreamMessageDelta)
		if !ok {
			continue
		}
		deltas = append(deltas, delta.ContentDelta)
	}
	if len(deltas) != 2 || deltas[0] != "he" || deltas[1] != "llo" {
		t.Fatalf("unexpected deltas: %#v", deltas)
	}

	if _, ok := events[len(events)-1].(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
}

func TestOpenAIGateway_Stream_ChatCompletions_SSE_ToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		chunk1, _ := json.Marshal(map[string]any{
			"choices": []any{
				map[string]any{
					"delta": map[string]any{
						"tool_calls": []any{
							map[string]any{
								"index": 0,
								"id":    "call_1",
								"type":  "function",
								"function": map[string]any{
									"name":      "web_search",
									"arguments": `{"query":`,
								},
							},
						},
					},
				},
			},
		})
		_, _ = w.Write(append(append([]byte("data: "), chunk1...), []byte("\n\n")...))
		if flusher != nil {
			flusher.Flush()
		}
		chunk2, _ := json.Marshal(map[string]any{
			"choices": []any{
				map[string]any{
					"finish_reason": "tool_calls",
					"delta": map[string]any{
						"tool_calls": []any{
							map[string]any{
								"index": 0,
								"function": map[string]any{
									"arguments": `"hello"}`,
								},
							},
						},
					},
				},
			},
		})
		_, _ = w.Write(append(append([]byte("data: "), chunk2...), []byte("\n\n")...))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		APIMode:         "chat_completions",
		EmitDebugEvents: false,
	})

	events := []StreamEvent{}
	err := gateway.Stream(context.Background(), Request{
		Model: "gpt-test",
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

	var gotCall *ToolCall
	var gotDeltas []ToolCallArgumentDelta
	for _, item := range events {
		switch typed := item.(type) {
		case ToolCallArgumentDelta:
			gotDeltas = append(gotDeltas, typed)
		case ToolCall:
			copied := typed
			gotCall = &copied
		}
	}
	if len(gotDeltas) != 2 {
		t.Fatalf("expected 2 tool call deltas, got %d events: %v", len(events), streamEventTypes(events))
	}
	if gotDeltas[0].ToolCallID != "call_1" || gotDeltas[0].ToolName != "web_search" || gotDeltas[0].ArgumentsDelta != `{"query":` {
		t.Fatalf("unexpected first tool call delta: %#v", gotDeltas[0])
	}
	if gotDeltas[1].ArgumentsDelta != `"hello"}` {
		t.Fatalf("unexpected second tool call delta: %#v", gotDeltas[1])
	}
	if gotCall == nil {
		t.Fatalf("expected tool call event, got %d events: %v", len(events), streamEventTypes(events))
	}
	if gotCall.ToolCallID != "call_1" || gotCall.ToolName != "web_search" || gotCall.ArgumentsJSON["query"] != "hello" {
		t.Fatalf("unexpected tool call: %#v", gotCall)
	}

	if _, ok := events[len(events)-1].(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
}

func TestOpenAIGateway_StreamChatCompletionsSSE_EOFWithoutDone_FinishReasonStop_Completes(t *testing.T) {
	chunk1, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{
					"role":    "assistant",
					"content": "hello",
				},
				"finish_reason": "stop",
			},
		},
	})
	reader := strings.NewReader("data: " + string(chunk1) + "\n\n")

	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamChatCompletionsSSE(context.Background(), reader, "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}
	if _, ok := events[len(events)-1].(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
}

func TestOpenAIGateway_StreamChatCompletionsSSE_EOFWithoutDone_ToolCalls_Completes(t *testing.T) {
	chunk1, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{
					"tool_calls": []any{
						map[string]any{
							"index": 0,
							"id":    "call_1",
							"type":  "function",
							"function": map[string]any{
								"name":      "web_search",
								"arguments": `{"query":"hello"}`,
							},
						},
					},
				},
			},
		},
	})
	reader := strings.NewReader("data: " + string(chunk1) + "\n\n")

	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamChatCompletionsSSE(context.Background(), reader, "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}
	var gotCall *ToolCall
	for _, item := range events {
		call, ok := item.(ToolCall)
		if !ok {
			continue
		}
		copied := call
		gotCall = &copied
		break
	}
	if gotCall == nil {
		t.Fatalf("expected ToolCall event, got %d events: %v", len(events), streamEventTypes(events))
	}
	if gotCall.ToolCallID != "call_1" || gotCall.ToolName != "web_search" || gotCall.ArgumentsJSON["query"] != "hello" {
		t.Fatalf("unexpected tool call: %#v", gotCall)
	}
	if _, ok := events[len(events)-1].(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
}

func TestOpenAIGateway_StreamChatCompletionsSSE_UsageOnlyChunkAfterFinishReason_IsCaptured(t *testing.T) {
	chunk1, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{
				"delta":         map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			},
		},
	})
	chunk2, _ := json.Marshal(map[string]any{
		"choices": []any{},
		"usage": map[string]any{
			"prompt_tokens":     12,
			"completion_tokens": 34,
		},
	})

	reader := strings.NewReader(
		"data: " + string(chunk1) + "\n\n" +
			"data: " + string(chunk2) + "\n\n" +
			"data: [DONE]\n\n",
	)

	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamChatCompletionsSSE(context.Background(), reader, "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}

	last, ok := events[len(events)-1].(StreamRunCompleted)
	if !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
	if last.Usage == nil || last.Usage.InputTokens == nil || last.Usage.OutputTokens == nil {
		t.Fatalf("expected usage in completion, got %#v", last.Usage)
	}
	if *last.Usage.InputTokens != 12 || *last.Usage.OutputTokens != 34 {
		t.Fatalf("unexpected usage: %#v", last.Usage)
	}
}

func TestOpenAIGateway_StreamChatCompletionsSSE_UsageCost_IsCaptured(t *testing.T) {
	chunk1, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{
				"delta":         map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			},
		},
	})
	chunk2, _ := json.Marshal(map[string]any{
		"choices": []any{},
		"usage": map[string]any{
			"prompt_tokens":     12,
			"completion_tokens": 34,
			"cost":              0.0012,
			"prompt_tokens_details": map[string]any{
				"cached_tokens": 5,
			},
		},
	})

	reader := strings.NewReader(
		"data: " + string(chunk1) + "\n\n" +
			"data: " + string(chunk2) + "\n\n" +
			"data: [DONE]\n\n",
	)

	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamChatCompletionsSSE(context.Background(), reader, "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}

	last, ok := events[len(events)-1].(StreamRunCompleted)
	if !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
	if last.Usage == nil || last.Usage.CachedTokens == nil || *last.Usage.CachedTokens != 5 {
		t.Fatalf("expected cached usage in completion, got %#v", last.Usage)
	}
	if last.Cost == nil || last.Cost.AmountMicros != 1200 || last.Cost.Currency != "USD" {
		t.Fatalf("expected cost in completion, got %#v", last.Cost)
	}
}

func TestOpenAIGateway_StreamChatCompletionsSSE_UsageOnlyWithoutContent_Fails(t *testing.T) {
	chunk1, _ := json.Marshal(map[string]any{
		"choices": []any{},
		"usage": map[string]any{
			"prompt_tokens":     12,
			"completion_tokens": 34,
		},
	})

	reader := strings.NewReader(
		"data: " + string(chunk1) + "\n\n" +
			"data: [DONE]\n\n",
	)

	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamChatCompletionsSSE(context.Background(), reader, "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}

	last, ok := events[len(events)-1].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed as last event, got %T", events[len(events)-1])
	}
	if last.Error.ErrorClass != ErrorClassInternalError {
		t.Fatalf("unexpected error_class: %#v", last.Error)
	}
	if last.Error.Message != "OpenAI stream completed without content" {
		t.Fatalf("unexpected error message: %#v", last.Error)
	}
}

func TestOpenAIGateway_StreamChatCompletionsSSE_RoleOnlyWithoutVisibleContent_Retryable(t *testing.T) {
	chunk1, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{
					"role": "assistant",
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     12,
			"completion_tokens": 15,
		},
	})

	reader := strings.NewReader(
		"data: " + string(chunk1) + "\n\n" +
			"data: [DONE]\n\n",
	)

	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamChatCompletionsSSE(context.Background(), reader, "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}

	last, ok := events[len(events)-1].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed as last event, got %T", events[len(events)-1])
	}
	if last.Error.ErrorClass != ErrorClassProviderRetryable {
		t.Fatalf("unexpected error_class: %#v", last.Error)
	}
	if last.Error.Message != "OpenAI stream emitted metadata without visible output" {
		t.Fatalf("unexpected error message: %#v", last.Error)
	}
}

func TestOpenAIGateway_StreamChatCompletionsSSE_ReasoningAliasWithoutVisibleContent_Retryable(t *testing.T) {
	chunk1, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{
					"role":      "assistant",
					"reasoning": "thinking",
				},
			},
		},
	})

	reader := strings.NewReader(
		"data: " + string(chunk1) + "\n\n" +
			"data: [DONE]\n\n",
	)

	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamChatCompletionsSSE(context.Background(), reader, "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}

	last, ok := events[len(events)-1].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed as last event, got %T", events[len(events)-1])
	}
	if last.Error.ErrorClass != ErrorClassProviderRetryable {
		t.Fatalf("unexpected error_class: %#v", last.Error)
	}
	if last.Error.Message != "LLM generated only internal reasoning without visible output" {
		t.Fatalf("unexpected error message: %#v", last.Error)
	}
}

func TestOpenAIGateway_StreamChatCompletionsSSE_StreamErrorChunkFailsWithDetails(t *testing.T) {
	chunk1, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": "provider disconnected",
			"type":    "provider_error",
			"code":    502,
		},
	})

	reader := strings.NewReader("data: " + string(chunk1) + "\n\n")

	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamChatCompletionsSSE(context.Background(), reader, "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}

	last, ok := events[len(events)-1].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed as last event, got %T", events[len(events)-1])
	}
	if last.Error.Message != "OpenAI stream returned error" {
		t.Fatalf("unexpected error message: %#v", last.Error)
	}
	if got := last.Error.Details["code"]; got != float64(502) {
		t.Fatalf("expected code=502, got %#v", got)
	}
}

func TestOpenAIGateway_StreamChatCompletionsSSE_RefusalDelta_Completes(t *testing.T) {
	chunk1, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{
					"role":    "assistant",
					"refusal": "no",
				},
			},
		},
	})
	chunk2, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{
					"refusal": "pe",
				},
			},
		},
	})

	reader := strings.NewReader(
		"data: " + string(chunk1) + "\n\n" +
			"data: " + string(chunk2) + "\n\n" +
			"data: [DONE]\n\n",
	)

	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamChatCompletionsSSE(context.Background(), reader, "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}

	deltas := []string{}
	for _, item := range events {
		delta, ok := item.(StreamMessageDelta)
		if !ok {
			continue
		}
		deltas = append(deltas, delta.ContentDelta)
	}
	if len(deltas) != 2 || deltas[0] != "no" || deltas[1] != "pe" {
		t.Fatalf("unexpected deltas: %#v", deltas)
	}

	if _, ok := events[len(events)-1].(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
}

func TestOpenAIGateway_Stream_ChatCompletions_SSE_InvalidJSONChunk_Fails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {bad json}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		APIMode:         "chat_completions",
		EmitDebugEvents: false,
	})

	var events []StreamEvent
	err := gateway.Stream(context.Background(), Request{
		Model: "gpt-test",
		Messages: []Message{
			{Role: "user", Content: []TextPart{{Text: "hi"}}},
		},
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

	if _, ok := events[len(events)-1].(StreamRunFailed); !ok {
		t.Fatalf("expected StreamRunFailed as last event, got %T", events[len(events)-1])
	}
	for _, item := range events {
		if _, ok := item.(StreamRunCompleted); ok {
			t.Fatalf("unexpected StreamRunCompleted event: %v", streamEventTypes(events))
		}
	}

	last := events[len(events)-1].(StreamRunFailed)
	if last.Error.ErrorClass != ErrorClassInternalError {
		t.Fatalf("unexpected error_class: %#v", last.Error)
	}
	if last.Error.Message != "OpenAI stream chunk parse failed" {
		t.Fatalf("unexpected error message: %#v", last.Error)
	}
}

func TestOpenAIGateway_Stream_Responses_SSE_ToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		chunk1, _ := json.Marshal(map[string]any{
			"type":  "response.output_text.delta",
			"delta": "ok",
		})
		_, _ = w.Write(append(append([]byte("data: "), chunk1...), []byte("\n\n")...))
		if flusher != nil {
			flusher.Flush()
		}
		chunk2, _ := json.Marshal(map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"output": []any{
					map[string]any{
						"id":        "call_1",
						"type":      "function_call",
						"name":      "web_search",
						"arguments": `{"query":"hello"}`,
					},
					map[string]any{
						"id":   "msg_1",
						"type": "message",
						"role": "assistant",
						"content": []any{
							map[string]any{"type": "output_text", "text": "ok"},
						},
					},
				},
			},
		})
		_, _ = w.Write(append(append([]byte("data: "), chunk2...), []byte("\n\n")...))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		APIMode:         "responses",
		EmitDebugEvents: false,
	})

	events := []StreamEvent{}
	err := gateway.Stream(context.Background(), Request{
		Model: "gpt-test",
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

	var gotText *StreamMessageDelta
	var gotCall *ToolCall
	for _, item := range events {
		switch typed := item.(type) {
		case StreamMessageDelta:
			gotText = &typed
		case ToolCall:
			copied := typed
			gotCall = &copied
		}
	}
	if gotText == nil || gotText.ContentDelta != "ok" {
		t.Fatalf("expected message delta ok, got %#v", gotText)
	}
	if gotCall == nil {
		t.Fatalf("expected tool call event, got %d events: %v", len(events), streamEventTypes(events))
	}
	if gotCall.ToolCallID != "call_1" || gotCall.ToolName != "web_search" || gotCall.ArgumentsJSON["query"] != "hello" {
		t.Fatalf("unexpected tool call: %#v", gotCall)
	}

	if _, ok := events[len(events)-1].(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
}

func TestOpenAIGateway_Stream_Responses_SSE_UsageCost_IsCaptured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		chunk1, _ := json.Marshal(map[string]any{
			"type":  "response.output_text.delta",
			"delta": "ok",
		})
		_, _ = w.Write(append(append([]byte("data: "), chunk1...), []byte("\n\n")...))
		if flusher != nil {
			flusher.Flush()
		}

		chunk2, _ := json.Marshal(map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"output": []any{
					map[string]any{
						"id":   "msg_1",
						"type": "message",
						"role": "assistant",
						"content": []any{
							map[string]any{"type": "output_text", "text": "ok"},
						},
					},
				},
				"usage": map[string]any{
					"input_tokens":  12,
					"output_tokens": 34,
					"cost":          0.0017,
					"input_tokens_details": map[string]any{
						"cached_tokens": 6,
					},
				},
			},
		})
		_, _ = w.Write(append(append([]byte("data: "), chunk2...), []byte("\n\n")...))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		APIMode:         "responses",
		EmitDebugEvents: false,
	})

	events := []StreamEvent{}
	err := gateway.Stream(context.Background(), Request{
		Model:    "gpt-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	last, ok := events[len(events)-1].(StreamRunCompleted)
	if !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
	if last.Usage == nil || last.Usage.CachedTokens == nil || *last.Usage.CachedTokens != 6 {
		t.Fatalf("expected cached usage in completion, got %#v", last.Usage)
	}
	if last.Cost == nil || last.Cost.AmountMicros != 1700 || last.Cost.Currency != "USD" {
		t.Fatalf("expected cost in completion, got %#v", last.Cost)
	}
}

func TestOpenAIGateway_Stream_Responses_SSE_InvalidJSONChunk_Fails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {bad json}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		APIMode:         "responses",
		EmitDebugEvents: false,
	})

	var events []StreamEvent
	err := gateway.Stream(context.Background(), Request{
		Model: "gpt-test",
		Messages: []Message{
			{Role: "user", Content: []TextPart{{Text: "hi"}}},
		},
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

	if _, ok := events[len(events)-1].(StreamRunFailed); !ok {
		t.Fatalf("expected StreamRunFailed as last event, got %T", events[len(events)-1])
	}
	for _, item := range events {
		if _, ok := item.(StreamRunCompleted); ok {
			t.Fatalf("unexpected StreamRunCompleted event: %v", streamEventTypes(events))
		}
	}

	last := events[len(events)-1].(StreamRunFailed)
	if last.Error.ErrorClass != ErrorClassInternalError {
		t.Fatalf("unexpected error_class: %#v", last.Error)
	}
	if last.Error.Message != "OpenAI responses stream chunk parse failed" {
		t.Fatalf("unexpected error message: %#v", last.Error)
	}
}

func TestOpenAIGateway_Stream_ErrorMessageAndDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{
  "error": {
    "message": "invalid api key",
    "type": "invalid_request_error",
    "code": "invalid_api_key",
    "param": null
  }
}`))
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		APIMode:         "chat_completions",
		EmitDebugEvents: false,
	})

	events := []StreamEvent{}
	err := gateway.Stream(context.Background(), Request{
		Model: "gpt-test",
		Messages: []Message{
			{Role: "user", Content: []TextPart{{Text: "hi"}}},
		},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	var gotFailed *StreamRunFailed
	for _, item := range events {
		failed, ok := item.(StreamRunFailed)
		if !ok {
			continue
		}
		copied := failed
		gotFailed = &copied
		break
	}
	if gotFailed == nil {
		t.Fatalf("expected StreamRunFailed, got %d events", len(events))
	}
	if gotFailed.Error.Message != "invalid api key" {
		t.Fatalf("unexpected error message: %#v", gotFailed.Error)
	}
	if gotFailed.Error.Details["status_code"] != int(http.StatusUnauthorized) {
		t.Fatalf("missing status_code detail: %#v", gotFailed.Error.Details)
	}
	if gotFailed.Error.Details["openai_error_type"] != "invalid_request_error" {
		t.Fatalf("missing openai_error_type: %#v", gotFailed.Error.Details)
	}
	if gotFailed.Error.Details["openai_error_code"] != "invalid_api_key" {
		t.Fatalf("missing openai_error_code: %#v", gotFailed.Error.Details)
	}
}

func TestOpenAIGateway_StreamChatCompletionsSSE_ReadError_YieldsRunFailed(t *testing.T) {
	// simulate network error: send one delta then disconnect, no [DONE] sent
	partial := "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"}}]}\n\n"
	reader := &sseErrorReader{data: []byte(partial), err: fmt.Errorf("connection reset by peer")}

	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamChatCompletionsSSE(context.Background(), reader, "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}
	if _, ok := events[len(events)-1].(StreamRunFailed); !ok {
		t.Fatalf("expected StreamRunFailed as last event, got %T", events[len(events)-1])
	}
	failed := events[len(events)-1].(StreamRunFailed)
	if failed.Error.ErrorClass != ErrorClassProviderRetryable {
		t.Fatalf("unexpected error class: %s", failed.Error.ErrorClass)
	}
}

func TestOpenAIGateway_StreamChatCompletionsSSE_CtxCanceled_YieldsRunFailed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pr, pw := io.Pipe()
	defer pw.Close()

	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamChatCompletionsSSE(ctx, pr, "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}
	if _, ok := events[len(events)-1].(StreamRunFailed); !ok {
		t.Fatalf("expected StreamRunFailed as last event, got %T", events[len(events)-1])
	}
}

func TestOpenAIGateway_StreamResponsesSSE_ReadError_YieldsRunFailed(t *testing.T) {
	partial := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"
	reader := &sseErrorReader{data: []byte(partial), err: fmt.Errorf("connection reset by peer")}

	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamResponsesSSE(context.Background(), reader, "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}
	if _, ok := events[len(events)-1].(StreamRunFailed); !ok {
		t.Fatalf("expected StreamRunFailed as last event, got %T", events[len(events)-1])
	}
	failed := events[len(events)-1].(StreamRunFailed)
	if failed.Error.ErrorClass != ErrorClassProviderRetryable {
		t.Fatalf("unexpected error class: %s", failed.Error.ErrorClass)
	}
}

// sseErrorReader returns the specified error after sending data, simulating a network read interruption
type sseErrorReader struct {
	data []byte
	pos  int
	err  error
}

func (r *sseErrorReader) Read(p []byte) (int, error) {
	if r.pos < len(r.data) {
		n := copy(p, r.data[r.pos:])
		r.pos += n
		return n, nil
	}
	return 0, r.err
}

func streamEventTypes(events []StreamEvent) []string {
	out := make([]string, 0, len(events))
	for _, item := range events {
		out = append(out, fmt.Sprintf("%T", item))
	}
	return out
}
