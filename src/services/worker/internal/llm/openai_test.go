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
