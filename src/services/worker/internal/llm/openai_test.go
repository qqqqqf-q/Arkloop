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

	"arkloop/services/shared/messagecontent"
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

func TestToOpenAIChatContentBlocks_PrependsAttachmentKeyForImages(t *testing.T) {
	blocks, hasStructured, err := toOpenAIChatContentBlocks([]ContentPart{
		{
			Type: messagecontent.PartTypeImage,
			Data: []byte("img"),
			Attachment: &messagecontent.AttachmentRef{
				Key:      "attachments/test/image.png",
				MimeType: "image/png",
			},
		},
	})
	if err != nil {
		t.Fatalf("toOpenAIChatContentBlocks failed: %v", err)
	}
	if !hasStructured {
		t.Fatalf("expected structured content")
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d: %#v", len(blocks), blocks)
	}
	if blocks[0]["type"] != "text" {
		t.Fatalf("expected text block first, got %#v", blocks[0])
	}
	if blocks[0]["text"] != "[attachment_key:attachments/test/image.png]" {
		t.Fatalf("unexpected attachment key text: %#v", blocks[0]["text"])
	}
	if blocks[1]["type"] != "image_url" {
		t.Fatalf("expected image block second, got %#v", blocks[1])
	}
}

func TestToOpenAIResponsesContentBlocks_PrependsAttachmentKeyForImages(t *testing.T) {
	blocks, err := toOpenAIResponsesContentBlocks([]ContentPart{
		{
			Type: messagecontent.PartTypeImage,
			Data: []byte("img"),
			Attachment: &messagecontent.AttachmentRef{
				Key:      "attachments/test/image.png",
				MimeType: "image/png",
			},
		},
	})
	if err != nil {
		t.Fatalf("toOpenAIResponsesContentBlocks failed: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d: %#v", len(blocks), blocks)
	}
	if blocks[0]["type"] != "input_text" {
		t.Fatalf("expected input_text block first, got %#v", blocks[0])
	}
	if blocks[0]["text"] != "[attachment_key:attachments/test/image.png]" {
		t.Fatalf("unexpected attachment key text: %#v", blocks[0]["text"])
	}
	if blocks[1]["type"] != "input_image" {
		t.Fatalf("expected input_image block second, got %#v", blocks[1])
	}
}

func TestToOpenAIResponsesInput_ToolImageReplayUsesResponsesBlocks(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"tool_call_id": "call_1",
		"tool_name":    "read",
		"result": map[string]any{
			"attachment_key": "attachments/test/image.png",
		},
	})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	items, err := toOpenAIResponsesInput([]Message{
		{
			Role: "tool",
			Content: []ContentPart{
				{Type: messagecontent.PartTypeText, Text: string(raw)},
				{
					Type: messagecontent.PartTypeImage,
					Data: []byte("img"),
					Attachment: &messagecontent.AttachmentRef{
						Key:      "attachments/test/image.png",
						MimeType: "image/png",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("toOpenAIResponsesInput failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d: %#v", len(items), items)
	}
	msg := items[1]
	if msg["type"] != "message" || msg["role"] != "user" {
		t.Fatalf("unexpected replay message: %#v", msg)
	}
	content, ok := msg["content"].([]map[string]any)
	if !ok {
		t.Fatalf("expected content blocks, got %#v", msg["content"])
	}
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d: %#v", len(content), content)
	}
	if content[0]["type"] != "input_text" {
		t.Fatalf("expected input_text first, got %#v", content[0])
	}
	if content[1]["type"] != "input_image" {
		t.Fatalf("expected input_image second, got %#v", content[1])
	}
}

func TestOpenAIGateway_Stream_PreflightOversizeSkipsHTTP(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		APIMode: "chat_completions",
	})

	var events []StreamEvent
	err := gateway.Stream(context.Background(), Request{
		Model: "gpt-test",
		Messages: []Message{{
			Role:    "user",
			Content: []TextPart{{Text: strings.Repeat("x", RequestPayloadLimitBytes+1024)}},
		}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected no HTTP request, got %d", calls)
	}
	failed, ok := events[len(events)-1].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed, got %T", events[len(events)-1])
	}
	if failed.Error.Details["status_code"] != http.StatusRequestEntityTooLarge {
		t.Fatalf("unexpected status_code: %#v", failed.Error.Details)
	}
	if failed.Error.Details["oversize_phase"] != OversizePhasePreflight {
		t.Fatalf("unexpected oversize phase: %#v", failed.Error.Details)
	}
	if failed.Error.Details["network_attempted"] != false {
		t.Fatalf("unexpected network_attempted: %#v", failed.Error.Details)
	}
}

func TestOpenAIGateway_Stream_Provider413AddsOversizeDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"too large","type":"invalid_request_error"}}`, http.StatusRequestEntityTooLarge)
	}))
	defer server.Close()

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		APIMode: "chat_completions",
	})

	var events []StreamEvent
	err := gateway.Stream(context.Background(), Request{
		Model: "gpt-test",
		Messages: []Message{{
			Role:    "user",
			Content: []TextPart{{Text: "hello"}},
		}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	failed, ok := events[len(events)-1].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed, got %T", events[len(events)-1])
	}
	if failed.Error.Details["status_code"] != http.StatusRequestEntityTooLarge {
		t.Fatalf("unexpected status_code: %#v", failed.Error.Details)
	}
	if failed.Error.Details["oversize_phase"] != OversizePhaseProvider {
		t.Fatalf("unexpected oversize phase: %#v", failed.Error.Details)
	}
	if failed.Error.Details["network_attempted"] != true {
		t.Fatalf("unexpected network_attempted: %#v", failed.Error.Details)
	}
	if _, ok := failed.Error.Details["payload_bytes"]; !ok {
		t.Fatalf("expected payload_bytes in details: %#v", failed.Error.Details)
	}
}

func TestOpenAIGateway_Stream_Responses_ReasoningEnabledSendsEffortAndSummary(t *testing.T) {
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body failed: %v", err)
		}
		receivedBody = append([]byte(nil), body...)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
		APIMode: "responses",
	})

	err := gateway.Stream(context.Background(), Request{
		Model:         "gpt-test",
		Messages:      []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
		ReasoningMode: "enabled",
	}, func(StreamEvent) error {
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("unmarshal request failed: %v", err)
	}
	reasoning, ok := payload["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("expected reasoning payload, got %#v", payload["reasoning"])
	}
	if reasoning["effort"] != "medium" {
		t.Fatalf("expected reasoning.effort=medium, got %#v", reasoning["effort"])
	}
	if reasoning["summary"] != "auto" {
		t.Fatalf("expected reasoning.summary=auto, got %#v", reasoning["summary"])
	}
}

func TestOpenAIGateway_Stream_Responses_ReasoningHighPreservesExplicitLevel(t *testing.T) {
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body failed: %v", err)
		}
		receivedBody = append([]byte(nil), body...)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
		APIMode: "responses",
	})

	err := gateway.Stream(context.Background(), Request{
		Model:         "gpt-test",
		Messages:      []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
		ReasoningMode: "high",
	}, func(StreamEvent) error {
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("unmarshal request failed: %v", err)
	}
	reasoning := payload["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" {
		t.Fatalf("expected reasoning.effort=high, got %#v", reasoning["effort"])
	}
}

func TestOpenAIGateway_Stream_ChatCompletions_ReasoningNoneUsesExplicitEffort(t *testing.T) {
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body failed: %v", err)
		}
		receivedBody = append([]byte(nil), body...)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
		APIMode: "chat_completions",
	})

	err := gateway.Stream(context.Background(), Request{
		Model:         "gpt-test",
		Messages:      []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
		ReasoningMode: "none",
	}, func(StreamEvent) error {
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("unmarshal request failed: %v", err)
	}
	if payload["reasoning_effort"] != "none" {
		t.Fatalf("expected reasoning_effort=none, got %#v", payload["reasoning_effort"])
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

func TestOpenAIGateway_Stream_Responses_Refusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "id":"resp_refusal",
  "object":"response",
  "output":[
    {
      "id":"msg_refusal",
      "type":"message",
      "role":"assistant",
      "content":[
        {"type":"refusal","refusal":"cannot help with that"}
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

	var events []StreamEvent
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

	var gotText *StreamMessageDelta
	for _, item := range events {
		delta, ok := item.(StreamMessageDelta)
		if !ok {
			continue
		}
		copy := delta
		gotText = &copy
		break
	}
	if gotText == nil || gotText.ContentDelta != "cannot help with that" {
		t.Fatalf("expected refusal delta, got %#v", gotText)
	}
	if _, ok := events[len(events)-1].(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
}

func TestOpenAIGateway_Stream_Responses_RequestBody_MultiTurnWithToolHistory(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"id":"msg_2","type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`))
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		APIMode:         "responses",
		EmitDebugEvents: false,
	})

	err := gateway.Stream(context.Background(), Request{
		Model: "gpt-test",
		Messages: []Message{
			{Role: "system", Content: []TextPart{{Text: "be helpful"}}},
			{Role: "user", Content: []TextPart{{Text: "天气怎么样"}}},
			{
				Role:    "assistant",
				Content: []TextPart{{Text: "我来查一下"}},
				ToolCalls: []ToolCall{{
					ToolCallID:    "call_1",
					ToolName:      "web_search",
					ArgumentsJSON: map[string]any{"query": "weather"},
				}},
			},
			{Role: "tool", Content: []TextPart{{Text: `{"tool_call_id":"call_1","result":{"summary":"sunny"}}`}}},
			{Role: "assistant", Content: []TextPart{{Text: "查到了，今天晴朗。"}}},
			{Role: "user", Content: []TextPart{{Text: "那明天呢"}}},
		},
	}, func(ev StreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	if gotBody["model"] != "gpt-test" {
		t.Fatalf("unexpected model: %#v", gotBody)
	}
	if gotBody["instructions"] != "be helpful" {
		t.Fatalf("expected instructions to carry system prompt, got %#v", gotBody["instructions"])
	}
	input, ok := gotBody["input"].([]any)
	if !ok {
		t.Fatalf("missing input: %#v", gotBody)
	}
	if len(input) != 6 {
		t.Fatalf("unexpected input len: %d %#v", len(input), input)
	}

	firstUser, _ := input[0].(map[string]any)
	if firstUser["type"] != "message" || firstUser["role"] != "user" {
		t.Fatalf("unexpected first user item: %#v", firstUser)
	}

	assistantMsg, _ := input[1].(map[string]any)
	if assistantMsg["type"] != "message" || assistantMsg["role"] != "assistant" || assistantMsg["status"] != "completed" {
		t.Fatalf("unexpected assistant history item: %#v", assistantMsg)
	}
	assistantContent, _ := assistantMsg["content"].([]any)
	firstAssistantBlock, _ := assistantContent[0].(map[string]any)
	if firstAssistantBlock["type"] != "output_text" || firstAssistantBlock["text"] != "我来查一下" {
		t.Fatalf("unexpected assistant content block: %#v", assistantContent)
	}

	functionCall, _ := input[2].(map[string]any)
	if functionCall["type"] != "function_call" || functionCall["call_id"] != "call_1" || functionCall["name"] != "web_search" {
		t.Fatalf("unexpected function_call item: %#v", functionCall)
	}

	functionOutput, _ := input[3].(map[string]any)
	if functionOutput["type"] != "function_call_output" || functionOutput["call_id"] != "call_1" {
		t.Fatalf("unexpected function_call_output item: %#v", functionOutput)
	}
	if output, ok := functionOutput["output"].(string); !ok || output != `{"summary":"sunny"}` {
		t.Fatalf("unexpected function_call_output payload: %#v", functionOutput["output"])
	}

	secondAssistant, _ := input[4].(map[string]any)
	if secondAssistant["type"] != "message" || secondAssistant["role"] != "assistant" || secondAssistant["status"] != "completed" {
		t.Fatalf("unexpected second assistant item: %#v", secondAssistant)
	}

	lastUser, _ := input[5].(map[string]any)
	if lastUser["type"] != "message" || lastUser["role"] != "user" {
		t.Fatalf("unexpected last user item: %#v", lastUser)
	}
}

func TestSplitOpenAIResponsesInstructions_RemovesSystemMessages(t *testing.T) {
	instructions, filtered := splitOpenAIResponsesInstructions([]Message{
		{Role: "system", Content: []TextPart{{Text: "base rules"}}},
		{Role: "user", Content: []TextPart{{Text: "hi"}}},
		{Role: "system", Content: []TextPart{{Text: "more rules"}}},
		{Role: "assistant", Content: []TextPart{{Text: "hello"}}},
	})

	if instructions != "base rules\n\nmore rules" {
		t.Fatalf("unexpected instructions: %q", instructions)
	}
	if len(filtered) != 2 {
		t.Fatalf("unexpected filtered messages: %#v", filtered)
	}
	if filtered[0].Role != "user" || filtered[1].Role != "assistant" {
		t.Fatalf("unexpected filtered roles: %#v", filtered)
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

func TestOpenAIGateway_Stream_Auto_FallbacksOnResponsesBadRequestUnknownEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{
  "error": {
    "message": "Unknown endpoint: POST /v1/responses",
    "type": "invalid_request_error"
  }
}`))
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
        "content":"ok"
      },
      "finish_reason":"stop"
    }
  ]
}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		APIMode:         "auto",
		EmitDebugEvents: false,
	})

	var events []StreamEvent
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

	var gotFallback *StreamProviderFallback
	var gotText *StreamMessageDelta
	for _, item := range events {
		switch typed := item.(type) {
		case StreamProviderFallback:
			copy := typed
			gotFallback = &copy
		case StreamMessageDelta:
			copy := typed
			gotText = &copy
		}
	}
	if gotFallback == nil {
		t.Fatalf("expected fallback event, got %v", streamEventTypes(events))
	}
	if gotFallback.StatusCode == nil || *gotFallback.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected fallback status: %#v", gotFallback)
	}
	if gotText == nil || gotText.ContentDelta != "ok" {
		t.Fatalf("expected fallback chat response, got %#v", gotText)
	}
}

func TestOpenAIGateway_Stream_Auto_DoesNotFallbackOnBadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{
  "error": {
    "message": "responses input is invalid",
    "type": "invalid_request_error"
  }
}`))
		case "/chat/completions":
			t.Fatal("chat/completions should not be called on 400 responses error")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		APIMode:         "auto",
		EmitDebugEvents: false,
	})

	var events []StreamEvent
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

	for _, item := range events {
		if _, ok := item.(StreamProviderFallback); ok {
			t.Fatalf("unexpected fallback event: %v", streamEventTypes(events))
		}
	}

	last, ok := events[len(events)-1].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed as last event, got %T", events[len(events)-1])
	}
	if last.Error.Message != "responses input is invalid" {
		t.Fatalf("unexpected error: %#v", last.Error)
	}
}

func TestToOpenAIResponsesInput_AssistantUsesOutputMessageItem(t *testing.T) {
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
	if typ, _ := assistant["type"].(string); typ != "message" {
		t.Fatalf("unexpected assistant type: %#v", assistant)
	}
	assistantContent, _ := assistant["content"].([]map[string]any)
	if role, _ := assistant["role"].(string); role != "assistant" {
		t.Fatalf("unexpected assistant role: %v", assistant["role"])
	}
	if status, _ := assistant["status"].(string); status != "completed" {
		t.Fatalf("unexpected assistant status: %#v", assistant)
	}
	if len(assistantContent) != 1 || assistantContent[0]["type"] != "output_text" {
		t.Fatalf("unexpected assistant content: %#v", assistant["content"])
	}
	if assistantContent[0]["text"] != "ok" {
		t.Fatalf("unexpected assistant content text: %#v", assistantContent[0])
	}
}

func TestToOpenAIResponsesInput_AssistantPreservesPhase(t *testing.T) {
	phase := "commentary"
	items, err := toOpenAIResponsesInput([]Message{
		{Role: "assistant", Phase: &phase, Content: []TextPart{{Text: "working"}}},
	})
	if err != nil {
		t.Fatalf("toOpenAIResponsesInput failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected items len: %d", len(items))
	}
	if items[0]["phase"] != "commentary" {
		t.Fatalf("expected assistant phase commentary, got %#v", items[0])
	}
}

func TestToOpenAIResponsesInput_AssistantToolCallsBecomeFunctionCallItems(t *testing.T) {
	items, err := toOpenAIResponsesInput([]Message{
		{Role: "assistant", Content: []TextPart{{Text: "let me check"}}, ToolCalls: []ToolCall{{
			ToolCallID:    "call_1",
			ToolName:      "web_search",
			ArgumentsJSON: map[string]any{"query": "hello"},
		}}},
		{Role: "tool", Content: []TextPart{{Text: `{"tool_call_id":"call_1","result":{"answer":"world"}}`}}},
	})
	if err != nil {
		t.Fatalf("toOpenAIResponsesInput failed: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("unexpected items len: %d %#v", len(items), items)
	}
	if items[0]["type"] != "message" || items[0]["role"] != "assistant" {
		t.Fatalf("unexpected assistant message item: %#v", items[0])
	}
	if items[1]["type"] != "function_call" || items[1]["call_id"] != "call_1" {
		t.Fatalf("unexpected function_call item: %#v", items[1])
	}
	if items[2]["type"] != "function_call_output" || items[2]["call_id"] != "call_1" {
		t.Fatalf("unexpected function_call_output item: %#v", items[2])
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

func TestToOpenAIChatMessages_UserImageIncludesAttachmentKeyText(t *testing.T) {
	out, err := toOpenAIChatMessages([]Message{{
		Role: "user",
		Content: []ContentPart{
			{Type: "text", Text: "看这张图"},
			{
				Type: "image",
				Attachment: &messagecontent.AttachmentRef{
					Key:      "attachments/acc/thread/image.png",
					Filename: "image.png",
					MimeType: "image/png",
				},
				Data: makeVisionTestPNG(t, 64, 64),
			},
		},
	}})
	if err != nil {
		t.Fatalf("toOpenAIChatMessages failed: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("unexpected messages len: %d", len(out))
	}

	content, ok := out[0]["content"].([]map[string]any)
	if !ok {
		t.Fatalf("unexpected content payload: %#v", out[0]["content"])
	}
	if len(content) != 3 {
		t.Fatalf("unexpected content blocks: %#v", content)
	}
	if content[1]["type"] != "text" || content[1]["text"] != "[attachment_key:attachments/acc/thread/image.png]" {
		t.Fatalf("missing attachment key text block: %#v", content)
	}
	if content[2]["type"] != "image_url" {
		t.Fatalf("missing image block: %#v", content)
	}
}

func TestToOpenAIResponsesInput_UserImageIncludesAttachmentKeyText(t *testing.T) {
	items, err := toOpenAIResponsesInput([]Message{{
		Role: "user",
		Content: []ContentPart{
			{Type: "text", Text: "看这张图"},
			{
				Type: "image",
				Attachment: &messagecontent.AttachmentRef{
					Key:      "attachments/acc/thread/image.png",
					Filename: "image.png",
					MimeType: "image/png",
				},
				Data: makeVisionTestPNG(t, 64, 64),
			},
		},
	}})
	if err != nil {
		t.Fatalf("toOpenAIResponsesInput failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected items len: %d", len(items))
	}

	content, ok := items[0]["content"].([]map[string]any)
	if !ok {
		t.Fatalf("unexpected content payload: %#v", items[0]["content"])
	}
	if len(content) != 3 {
		t.Fatalf("unexpected content blocks: %#v", content)
	}
	if content[1]["type"] != "input_text" || content[1]["text"] != "[attachment_key:attachments/acc/thread/image.png]" {
		t.Fatalf("missing attachment key text block: %#v", content)
	}
	if content[2]["type"] != "input_image" {
		t.Fatalf("missing input_image block: %#v", content)
	}
}

func TestOpenAIGateway_Stream_Responses_RequestBodyDoesNotLeakProviderToolName(t *testing.T) {
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body failed: %v", err)
		}
		receivedBody = append([]byte(nil), body...)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
		APIMode: "responses",
	})

	err := gateway.Stream(context.Background(), Request{
		Model: "gpt-test",
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
		t.Fatalf("expected responses request body to hide provider tool name, got %s", bodyText)
	}
	if !strings.Contains(bodyText, `"name":"web_search"`) {
		t.Fatalf("expected responses request body to keep canonical tool name, got %s", bodyText)
	}
}

func TestOpenAIGateway_Stream_ChatCompletions_RequestBodyDoesNotLeakProviderToolName(t *testing.T) {
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body failed: %v", err)
		}
		receivedBody = append([]byte(nil), body...)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:  "test",
		BaseURL: server.URL,
		APIMode: "chat_completions",
	})

	err := gateway.Stream(context.Background(), Request{
		Model: "gpt-test",
		Messages: []Message{
			{Role: "user", Content: []TextPart{{Text: "hi"}}},
			{
				Role:    "assistant",
				Content: []TextPart{{Text: "fetching"}},
				ToolCalls: []ToolCall{{
					ToolCallID:    "call_1",
					ToolName:      "web_fetch.jina",
					ArgumentsJSON: map[string]any{"url": "https://example.com"},
				}},
			},
			{
				Role: "tool",
				Content: []TextPart{{
					Text: `{"tool_call_id":"call_1","tool_name":"web_fetch.jina","result":{"title":"Example"}}`,
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
	if strings.Contains(bodyText, "web_fetch.jina") {
		t.Fatalf("expected chat_completions request body to hide provider tool name, got %s", bodyText)
	}
	if !strings.Contains(bodyText, `"name":"web_fetch"`) {
		t.Fatalf("expected chat_completions request body to keep canonical tool name, got %s", bodyText)
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
			"type":         "response.output_item.added",
			"output_index": 0,
			"item": map[string]any{
				"id":        "fc_1",
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "web_search",
				"arguments": "",
			},
		})
		_, _ = w.Write(append(append([]byte("data: "), chunk2...), []byte("\n\n")...))
		if flusher != nil {
			flusher.Flush()
		}
		chunk3, _ := json.Marshal(map[string]any{
			"type":         "response.function_call_arguments.delta",
			"item_id":      "fc_1",
			"output_index": 0,
			"delta":        `{"query":`,
		})
		_, _ = w.Write(append(append([]byte("data: "), chunk3...), []byte("\n\n")...))
		if flusher != nil {
			flusher.Flush()
		}
		chunk4, _ := json.Marshal(map[string]any{
			"type":         "response.function_call_arguments.delta",
			"item_id":      "fc_1",
			"output_index": 0,
			"delta":        `"hello"}`,
		})
		_, _ = w.Write(append(append([]byte("data: "), chunk4...), []byte("\n\n")...))
		if flusher != nil {
			flusher.Flush()
		}
		chunk5, _ := json.Marshal(map[string]any{
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
		_, _ = w.Write(append(append([]byte("data: "), chunk5...), []byte("\n\n")...))
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
	var gotTextCount int
	var gotCall *ToolCall
	var gotDeltas []ToolCallArgumentDelta
	for _, item := range events {
		switch typed := item.(type) {
		case StreamMessageDelta:
			copied := typed
			gotTextCount++
			if copied.ContentDelta == "ok" {
				gotText = &copied
			}
		case ToolCall:
			copied := typed
			gotCall = &copied
		case ToolCallArgumentDelta:
			gotDeltas = append(gotDeltas, typed)
		}
	}
	if gotText == nil || gotText.ContentDelta != "ok" {
		t.Fatalf("expected message delta ok, got %#v", gotText)
	}
	if gotTextCount != 1 {
		t.Fatalf("expected only one text delta event, got %d events: %v", gotTextCount, streamEventTypes(events))
	}
	if len(gotDeltas) != 2 {
		t.Fatalf("expected 2 tool call deltas, got %d events: %v", len(gotDeltas), streamEventTypes(events))
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
	for _, item := range events {
		msg, ok := item.(StreamMessageDelta)
		if !ok {
			continue
		}
		if strings.Contains(msg.ContentDelta, `{"query":`) || strings.Contains(msg.ContentDelta, `"hello"}`) {
			t.Fatalf("unexpected tool args in assistant text stream: %#v", events)
		}
	}

	if _, ok := events[len(events)-1].(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
}

func TestOpenAIGateway_Stream_Responses_SSE_RefusalDelta_Completes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		writeChunk := func(payload map[string]any) {
			chunk, _ := json.Marshal(payload)
			_, _ = w.Write(append(append([]byte("data: "), chunk...), []byte("\n\n")...))
			if flusher != nil {
				flusher.Flush()
			}
		}

		writeChunk(map[string]any{
			"type":  "response.refusal.delta",
			"delta": "no",
		})
		writeChunk(map[string]any{
			"type":  "response.refusal.delta",
			"delta": "pe",
		})
		writeChunk(map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"output": []any{
					map[string]any{
						"id":   "msg_1",
						"type": "message",
						"role": "assistant",
						"content": []any{
							map[string]any{"type": "refusal", "refusal": "nope"},
						},
					},
				},
			},
		})
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

	var events []StreamEvent
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

	var deltas []string
	for _, item := range events {
		delta, ok := item.(StreamMessageDelta)
		if !ok {
			continue
		}
		deltas = append(deltas, delta.ContentDelta)
	}
	if len(deltas) != 2 || deltas[0] != "no" || deltas[1] != "pe" {
		t.Fatalf("unexpected refusal deltas: %#v", deltas)
	}
	if _, ok := events[len(events)-1].(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
}

func TestOpenAIGateway_Stream_Responses_SSE_CompletedOnlyRefusal_EmitsVisibleOutput(t *testing.T) {
	reader := strings.NewReader(
		"data: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"refusal\",\"refusal\":\"cannot comply\"}]}]}}\n\n" +
			"data: [DONE]\n\n",
	)

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

	var gotText *StreamMessageDelta
	for _, item := range events {
		delta, ok := item.(StreamMessageDelta)
		if !ok {
			continue
		}
		copy := delta
		gotText = &copy
		break
	}
	if gotText == nil || gotText.ContentDelta != "cannot comply" {
		t.Fatalf("expected refusal from completed event, got %#v", gotText)
	}
	if _, ok := events[len(events)-1].(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
}

func TestOpenAIGateway_Stream_Responses_SSE_CompletedPreservesAssistantPhase(t *testing.T) {
	reader := strings.NewReader(
		"data: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"commentary\",\"content\":[{\"type\":\"output_text\",\"text\":\"working\"}]}]}}\n\n" +
			"data: [DONE]\n\n",
	)

	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamResponsesSSE(context.Background(), reader, "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}

	last, ok := events[len(events)-1].(StreamRunCompleted)
	if !ok {
		t.Fatalf("expected StreamRunCompleted, got %T", events[len(events)-1])
	}
	if last.AssistantMessage == nil || last.AssistantMessage.Phase == nil || *last.AssistantMessage.Phase != "commentary" {
		t.Fatalf("expected completed assistant phase commentary, got %#v", last.AssistantMessage)
	}
}

func TestOpenAIGateway_Stream_Responses_SSE_FunctionArgumentsDelta_StaysOutOfText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		writeChunk := func(payload map[string]any) {
			chunk, _ := json.Marshal(payload)
			_, _ = w.Write(append(append([]byte("data: "), chunk...), []byte("\n\n")...))
			if flusher != nil {
				flusher.Flush()
			}
		}

		writeChunk(map[string]any{
			"type":         "response.output_item.added",
			"output_index": 0,
			"item": map[string]any{
				"id":      "item_fc_1",
				"type":    "function_call",
				"call_id": "call_1",
				"name":    "web_search",
			},
		})
		writeChunk(map[string]any{
			"type":         "response.function_call_arguments.delta",
			"output_index": 0,
			"item_id":      "item_fc_1",
			"delta":        `{"query":"hel`,
		})
		writeChunk(map[string]any{
			"type":         "response.function_call_arguments.delta",
			"output_index": 0,
			"item_id":      "item_fc_1",
			"delta":        `lo"}`,
		})
		writeChunk(map[string]any{
			"type":  "response.output_text.delta",
			"delta": "done",
		})
		writeChunk(map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"output": []any{
					map[string]any{
						"id":        "item_fc_1",
						"type":      "function_call",
						"call_id":   "call_1",
						"name":      "web_search",
						"arguments": `{"query":"hello"}`,
					},
					map[string]any{
						"id":   "msg_1",
						"type": "message",
						"role": "assistant",
						"content": []any{
							map[string]any{"type": "output_text", "text": "done"},
						},
					},
				},
			},
		})
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

	var (
		textDeltas []string
		argDeltas  []ToolCallArgumentDelta
	)
	err := gateway.Stream(context.Background(), Request{
		Model:    "gpt-test",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		switch typed := ev.(type) {
		case StreamMessageDelta:
			textDeltas = append(textDeltas, typed.ContentDelta)
		case ToolCallArgumentDelta:
			argDeltas = append(argDeltas, typed)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	if len(textDeltas) != 1 || textDeltas[0] != "done" {
		t.Fatalf("unexpected text deltas: %#v", textDeltas)
	}
	if len(argDeltas) != 2 {
		t.Fatalf("unexpected tool arg deltas: %#v", argDeltas)
	}
	if argDeltas[0].ToolCallID != "call_1" || argDeltas[0].ToolName != "web_search" || argDeltas[0].ArgumentsDelta != `{"query":"hel` {
		t.Fatalf("unexpected first tool arg delta: %#v", argDeltas[0])
	}
	if argDeltas[1].ArgumentsDelta != `lo"}` {
		t.Fatalf("unexpected second tool arg delta: %#v", argDeltas[1])
	}
}

func TestOpenAIGateway_Stream_AutoDoesNotFallbackOnResponsesBadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"responses invalid because field foo is invalid","type":"invalid_request_error"}}`))
	}))
	t.Cleanup(server.Close)

	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		APIKey:          "test",
		BaseURL:         server.URL,
		APIMode:         "auto",
		EmitDebugEvents: false,
	})

	var events []StreamEvent
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
	for _, item := range events {
		if _, ok := item.(StreamProviderFallback); ok {
			t.Fatalf("unexpected fallback event: %v", streamEventTypes(events))
		}
	}
	last, ok := events[len(events)-1].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed as last event, got %T", events[len(events)-1])
	}
	if last.Error.Message == "" {
		t.Fatalf("expected error message, got %#v", last)
	}
}

func TestOpenAIGateway_StreamResponsesSSE_DoneWithoutCompleted_AfterText_Completes(t *testing.T) {
	reader := strings.NewReader(
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
			"data: [DONE]\n\n",
	)

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
	if _, ok := events[len(events)-1].(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted as last event, got %T", events[len(events)-1])
	}
}

func TestOpenAIGateway_StreamResponsesSSE_DoneWithoutCompleted_AfterToolDelta_Completes(t *testing.T) {
	reader := strings.NewReader(
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"fc_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"web_search\"}}\n\n" +
			"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"fc_1\",\"delta\":\"{\\\"query\\\":\\\"hello\\\"}\"}\n\n" +
			"data: [DONE]\n\n",
	)

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
	if gotDelta == nil || gotDelta.ToolCallID != "call_1" || gotDelta.ToolName != "web_search" {
		t.Fatalf("unexpected tool arg delta: %#v", gotDelta)
	}
	if gotCall == nil || gotCall.ToolCallID != "call_1" || gotCall.ToolName != "web_search" || gotCall.ArgumentsJSON["query"] != "hello" {
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
	defer func() { _ = pw.Close() }()

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

func TestOpenAIGateway_StreamChatCompletionsSSE_EarlyEOFIsRetryable(t *testing.T) {
	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamChatCompletionsSSE(context.Background(), strings.NewReader(""), "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}
	failed, ok := events[len(events)-1].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed as last event, got %T", events[len(events)-1])
	}
	if failed.Error.ErrorClass != ErrorClassProviderRetryable {
		t.Fatalf("unexpected error class: %s", failed.Error.ErrorClass)
	}
	if failed.Error.Message != "upstream stream ended prematurely without completion" {
		t.Fatalf("unexpected error message: %q", failed.Error.Message)
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

func TestOpenAIGateway_StreamResponsesSSE_EarlyEOFIsRetryable(t *testing.T) {
	gateway := &OpenAIGateway{cfg: OpenAIGatewayConfig{}}
	var events []StreamEvent
	err := gateway.streamResponsesSSE(context.Background(), strings.NewReader(""), "test", 200, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error from gateway, got: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}
	failed, ok := events[len(events)-1].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed as last event, got %T", events[len(events)-1])
	}
	if failed.Error.ErrorClass != ErrorClassProviderRetryable {
		t.Fatalf("unexpected error class: %s", failed.Error.ErrorClass)
	}
	if failed.Error.Message != "upstream stream ended prematurely without completion" {
		t.Fatalf("unexpected error message: %q", failed.Error.Message)
	}
}

func TestOpenAIErrorMessageAndDetails(t *testing.T) {
	body := []byte(`{"error":{"type":"invalid_request_error","code":400,"param":"tools[0]","message":""}}`)
	msg, details := openAIErrorMessageAndDetails(body, 400, "fallback")
	if msg != "invalid_request_error: 400, param=tools[0]" {
		t.Fatalf("synthetic message: got %q", msg)
	}
	if details["provider_error_body"] != string(body) {
		t.Fatalf("expected provider_error_body to mirror body")
	}
	if details["openai_error_type"] != "invalid_request_error" {
		t.Fatalf("type: %#v", details["openai_error_type"])
	}

	flat := []byte(`{"message":"flat provider message"}`)
	msg2, d2 := openAIErrorMessageAndDetails(flat, 502, "f")
	if msg2 != "flat provider message" {
		t.Fatalf("flat message: %q", msg2)
	}
	if d2["provider_error_body"] != string(flat) {
		t.Fatalf("missing provider_error_body")
	}
}

func TestOpenAIReasoningEffort(t *testing.T) {
	cases := map[string]string{
		"enabled":      "medium",
		"off":          "none",
		"minimal":      "minimal",
		"low":          "low",
		"medium":       "medium",
		"high":         "high",
		"max":          "xhigh",
		"xhigh":        "xhigh",
		"none":         "none",
		"extra_high":   "xhigh",
		"extra-high":   "xhigh",
		"extra high":   "xhigh",
		" EXTRA-HIGH ": "xhigh",
	}
	for input, want := range cases {
		got, ok := openAIReasoningEffort(input)
		if !ok || got != want {
			t.Fatalf("mode %q => (%q, %v), want (%q, true)", input, got, ok, want)
		}
	}

	if _, ok := openAIReasoningEffort("auto"); ok {
		t.Fatal("auto should not force a reasoning effort")
	}
}

func TestOpenAIReasoningDisabled(t *testing.T) {
	if !openAIReasoningDisabled("disabled") {
		t.Fatal("disabled should remove reasoning")
	}
	if openAIReasoningDisabled("enabled") {
		t.Fatal("enabled should not remove reasoning")
	}
}

func TestOpenAIResponsesReasoningPayloadUsesEffort(t *testing.T) {
	payload := map[string]any{}
	applyOpenAIResponsesReasoningMode(payload, "enabled")
	reasoning, ok := payload["reasoning"].(map[string]any)
	if !ok {
		t.Fatal("expected reasoning payload")
	}
	if reasoning["effort"] != "medium" {
		t.Fatalf("reasoning.effort = %#v, want medium", reasoning["effort"])
	}
	if reasoning["summary"] != "auto" {
		t.Fatalf("reasoning.summary = %#v, want auto", reasoning["summary"])
	}
}

func TestOpenAIResponsesReasoningPayloadKeepsAdvancedSummary(t *testing.T) {
	payload := map[string]any{
		"reasoning": map[string]any{"summary": "detailed"},
	}
	applyOpenAIResponsesReasoningMode(payload, "high")
	reasoning := payload["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" {
		t.Fatalf("reasoning.effort = %#v, want high", reasoning["effort"])
	}
	if reasoning["summary"] != "detailed" {
		t.Fatalf("reasoning.summary = %#v, want detailed", reasoning["summary"])
	}
}

func TestOpenAIResponsesReasoningPayloadSupportsNoneEffort(t *testing.T) {
	payload := map[string]any{}
	applyOpenAIResponsesReasoningMode(payload, "none")
	reasoning, ok := payload["reasoning"].(map[string]any)
	if !ok {
		t.Fatal("expected reasoning payload")
	}
	if reasoning["effort"] != "none" {
		t.Fatalf("reasoning.effort = %#v, want none", reasoning["effort"])
	}
}

func TestOpenAIChatReasoningPayloadRemovesEffortWhenDisabled(t *testing.T) {
	payload := map[string]any{"reasoning_effort": "high"}
	applyOpenAIChatReasoningMode(payload, "disabled")
	if _, ok := payload["reasoning_effort"]; ok {
		t.Fatal("reasoning_effort should be removed when disabled")
	}
}

func TestOpenAIResponsesReasoningPayloadNoneRemovesSummary(t *testing.T) {
	payload := map[string]any{
		"reasoning": map[string]any{"summary": "auto"},
	}
	applyOpenAIResponsesReasoningMode(payload, "none")
	reasoning := payload["reasoning"].(map[string]any)
	if reasoning["effort"] != "none" {
		t.Fatalf("reasoning.effort = %#v, want none", reasoning["effort"])
	}
	if _, ok := reasoning["summary"]; ok {
		t.Fatalf("reasoning.summary should be removed, got %#v", reasoning["summary"])
	}
}

func TestOpenAIChatReasoningPayloadOverridesAdvancedJSON(t *testing.T) {
	payload := map[string]any{"reasoning_effort": "low"}
	applyOpenAIChatReasoningMode(payload, "high")
	if payload["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", payload["reasoning_effort"])
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
