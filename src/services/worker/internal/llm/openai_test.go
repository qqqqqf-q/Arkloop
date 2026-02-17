package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
		t.Fatalf("expected tool call event, got %d events", len(events))
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
		t.Fatalf("expected tool call event, got %d events", len(events))
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

	out := toOpenAIChatMessages(messages)
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
