package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- 消息转换测试 ---

func TestToGeminiContents_SystemSeparated(t *testing.T) {
	sys, contents, err := toGeminiContents([]Message{
		{Role: "system", Content: []TextPart{{Text: "be helpful"}}},
		{Role: "user", Content: []TextPart{{Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sys == nil {
		t.Fatal("expected systemInstruction, got nil")
	}
	parts, ok := sys["parts"].([]map[string]any)
	if !ok || len(parts) != 1 || parts[0]["text"] != "be helpful" {
		t.Fatalf("unexpected systemInstruction: %#v", sys)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	if contents[0]["role"] != "user" {
		t.Fatalf("expected user role, got %v", contents[0]["role"])
	}
}

func TestToGeminiContents_ToolEnvelope(t *testing.T) {
	_, contents, err := toGeminiContents([]Message{
		{Role: "user", Content: []TextPart{{Text: "search"}}},
		{
			Role:    "assistant",
			Content: []TextPart{{Text: ""}},
			ToolCalls: []ToolCall{{
				ToolCallID:    "call_1",
				ToolName:      "web_search",
				ArgumentsJSON: map[string]any{"query": "go lang"},
			}},
		},
		{
			Role: "tool",
			Content: []TextPart{{
				Text: `{"tool_call_id":"call_1","tool_name":"web_search","result":{"items":["a","b"]}}`,
			}},
		},
		{Role: "user", Content: []TextPart{{Text: "continue"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contents) != 4 {
		t.Fatalf("expected 4 contents, got %d: %#v", len(contents), contents)
	}
	// index 0: user
	if contents[0]["role"] != "user" {
		t.Fatalf("expected user, got %v", contents[0]["role"])
	}
	// index 1: model with functionCall
	if contents[1]["role"] != "model" {
		t.Fatalf("expected model, got %v", contents[1]["role"])
	}
	modelParts := contents[1]["parts"].([]map[string]any)
	if len(modelParts) != 1 {
		t.Fatalf("expected 1 model part, got %d", len(modelParts))
	}
	fc, ok := modelParts[0]["functionCall"].(map[string]any)
	if !ok {
		t.Fatalf("expected functionCall, got %#v", modelParts[0])
	}
	if fc["name"] != "web_search" {
		t.Fatalf("unexpected function name: %v", fc["name"])
	}
	// index 2: user with functionResponse
	if contents[2]["role"] != "user" {
		t.Fatalf("expected user for tool response, got %v", contents[2]["role"])
	}
	toolParts := contents[2]["parts"].([]map[string]any)
	if len(toolParts) != 1 {
		t.Fatalf("expected 1 tool part, got %d", len(toolParts))
	}
	fr, ok := toolParts[0]["functionResponse"].(map[string]any)
	if !ok {
		t.Fatalf("expected functionResponse, got %#v", toolParts[0])
	}
	if fr["name"] != "web_search" {
		t.Fatalf("unexpected function response name: %v", fr["name"])
	}
	// index 3: user continue
	if contents[3]["role"] != "user" {
		t.Fatalf("expected user, got %v", contents[3]["role"])
	}
}

func TestToGeminiContents_ConsecutiveToolResponses(t *testing.T) {
	_, contents, err := toGeminiContents([]Message{
		{Role: "user", Content: []TextPart{{Text: "go"}}},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ToolCallID: "c1", ToolName: "tool_a", ArgumentsJSON: map[string]any{}},
				{ToolCallID: "c2", ToolName: "tool_b", ArgumentsJSON: map[string]any{}},
			},
		},
		{
			Role:    "tool",
			Content: []TextPart{{Text: `{"tool_call_id":"c1","tool_name":"tool_a","result":{"x":1}}`}},
		},
		{
			Role:    "tool",
			Content: []TextPart{{Text: `{"tool_call_id":"c2","tool_name":"tool_b","result":{"y":2}}`}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 两个连续 tool 应合并到同一个 user content
	if len(contents) != 3 {
		t.Fatalf("expected 3 contents (user, model, user[tool results]), got %d", len(contents))
	}
	toolContent := contents[2]
	if toolContent["role"] != "user" {
		t.Fatalf("expected user for merged tool responses, got %v", toolContent["role"])
	}
	parts := toolContent["parts"].([]map[string]any)
	if len(parts) != 2 {
		t.Fatalf("expected 2 tool response parts, got %d", len(parts))
	}
}

// --- HTTP 端到端测试 ---

func sseBody(chunks []string) string {
	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString("data: ")
		sb.WriteString(c)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func TestGeminiGateway_Stream_TextResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":streamGenerateContent") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("x-goog-api-key") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		body := sseBody([]string{
			`{"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"},"finishReason":""}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`,
			`{"candidates":[{"content":{"parts":[{"text":" world"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`,
		})
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	gw := NewGeminiGateway(GeminiGatewayConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})

	var events []StreamEvent
	err := gw.Stream(context.Background(), Request{
		Model:    "gemini-2.0-flash",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	var text strings.Builder
	for _, ev := range events {
		if d, ok := ev.(StreamMessageDelta); ok {
			text.WriteString(d.ContentDelta)
		}
	}
	if got := text.String(); got != "Hello world" {
		t.Fatalf("unexpected text: %q", got)
	}

	last := events[len(events)-1]
	completed, ok := last.(StreamRunCompleted)
	if !ok {
		t.Fatalf("expected StreamRunCompleted, got %T", last)
	}
	if completed.Usage == nil {
		t.Fatal("expected usage, got nil")
	}
	if *completed.Usage.TotalTokens != 15 {
		t.Fatalf("unexpected total_tokens: %d", *completed.Usage.TotalTokens)
	}
}

func TestNewGeminiGateway_PreservesVersionFromBaseURL(t *testing.T) {
	gw := NewGeminiGateway(GeminiGatewayConfig{
		APIKey:  "test-key",
		BaseURL: "https://generativelanguage.googleapis.com/v1",
	})
	if gw.transport.cfg.BaseURL != "https://generativelanguage.googleapis.com" {
		t.Fatalf("unexpected normalized base url: %q", gw.transport.cfg.BaseURL)
	}
	if gw.protocol.APIVersion != "v1" {
		t.Fatalf("unexpected api version: %q", gw.protocol.APIVersion)
	}
	path := geminiVersionedPath(gw.transport.cfg.BaseURL, gw.protocol.APIVersion, "/models/gemini-2.0-flash:streamGenerateContent?alt=sse")
	if path != "/v1/models/gemini-2.0-flash:streamGenerateContent?alt=sse" {
		t.Fatalf("unexpected request path: %q", path)
	}
}

func TestGeminiGateway_Stream_ToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		body := sseBody([]string{
			`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"web_search","args":{"query":"cats"}}}],"role":"model"},"finishReason":"STOP"}]}`,
		})
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	gw := NewGeminiGateway(GeminiGatewayConfig{APIKey: "k", BaseURL: server.URL})

	var events []StreamEvent
	_ = gw.Stream(context.Background(), Request{
		Model:    "gemini-2.0-flash",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "search cats"}}}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})

	var delta *ToolCallArgumentDelta
	var call *ToolCall
	for _, ev := range events {
		switch typed := ev.(type) {
		case ToolCallArgumentDelta:
			copy := typed
			delta = &copy
		case ToolCall:
			copied := typed
			call = &copied
		}
	}
	if delta == nil {
		t.Fatal("expected ToolCallArgumentDelta event")
	}
	if delta.ToolName != "web_search" || delta.ArgumentsDelta != `{"query":"cats"}` {
		t.Fatalf("unexpected tool call delta: %#v", delta)
	}
	if call == nil {
		t.Fatal("expected ToolCall event")
	}
	if call.ToolName != "web_search" {
		t.Fatalf("unexpected tool name: %q", call.ToolName)
	}
	if call.ArgumentsJSON["query"] != "cats" {
		t.Fatalf("unexpected args: %#v", call.ArgumentsJSON)
	}
	if call.ToolCallID == "" {
		t.Fatal("expected non-empty ToolCallID")
	}
}

func TestGeminiGateway_Stream_ThinkingContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		body := sseBody([]string{
			`{"candidates":[{"content":{"parts":[{"text":"thinking...","thought":true},{"text":"answer"}],"role":"model"},"finishReason":"STOP"}]}`,
		})
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	gw := NewGeminiGateway(GeminiGatewayConfig{APIKey: "k", BaseURL: server.URL})

	var thinkingDeltas, textDeltas []StreamMessageDelta
	_ = gw.Stream(context.Background(), Request{
		Model:    "gemini-2.0-flash",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "think"}}}},
	}, func(ev StreamEvent) error {
		if d, ok := ev.(StreamMessageDelta); ok {
			if d.Channel != nil && *d.Channel == "thinking" {
				thinkingDeltas = append(thinkingDeltas, d)
			} else {
				textDeltas = append(textDeltas, d)
			}
		}
		return nil
	})

	if len(thinkingDeltas) != 1 || thinkingDeltas[0].ContentDelta != "thinking..." {
		t.Fatalf("unexpected thinking deltas: %#v", thinkingDeltas)
	}
	if len(textDeltas) != 1 || textDeltas[0].ContentDelta != "answer" {
		t.Fatalf("unexpected text deltas: %#v", textDeltas)
	}
}

func TestGeminiGateway_Stream_ErrorResponse(t *testing.T) {
	for _, code := range []int{401, 500} {
		t.Run(fmt.Sprintf("HTTP%d", code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				_, _ = w.Write([]byte(`{"error":{"code":` + fmt.Sprint(code) + `,"message":"error msg","status":"UNAUTHENTICATED"}}`))
			}))
			t.Cleanup(server.Close)

			gw := NewGeminiGateway(GeminiGatewayConfig{APIKey: "k", BaseURL: server.URL})
			var events []StreamEvent
			_ = gw.Stream(context.Background(), Request{
				Model:    "gemini-2.0-flash",
				Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
			}, func(ev StreamEvent) error {
				events = append(events, ev)
				return nil
			})

			if len(events) == 0 {
				t.Fatal("expected events")
			}
			last := events[len(events)-1]
			failed, ok := last.(StreamRunFailed)
			if !ok {
				t.Fatalf("expected StreamRunFailed, got %T", last)
			}
			if failed.Error.Message != "error msg" {
				t.Fatalf("unexpected message: %q", failed.Error.Message)
			}
		})
	}
}

func TestGeminiGateway_Stream_SafetyBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		body := sseBody([]string{
			`{"candidates":[{"content":{"parts":[],"role":"model"},"finishReason":"SAFETY"}]}`,
		})
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	gw := NewGeminiGateway(GeminiGatewayConfig{APIKey: "k", BaseURL: server.URL})
	var events []StreamEvent
	_ = gw.Stream(context.Background(), Request{
		Model:    "gemini-2.0-flash",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "bad prompt"}}}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})

	last := events[len(events)-1]
	failed, ok := last.(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed, got %T", last)
	}
	if failed.Error.ErrorClass != ErrorClassPolicyDenied {
		t.Fatalf("expected PolicyDenied, got %q", failed.Error.ErrorClass)
	}
}

func TestGeminiGateway_Stream_FinishReasonClassification(t *testing.T) {
	testCases := []struct {
		name      string
		reason    string
		errClass  string
		msgPrefix string
	}{
		{name: "policy blocklist", reason: "BLOCKLIST", errClass: ErrorClassPolicyDenied, msgPrefix: "Gemini content blocked:"},
		{name: "policy spii", reason: "SPII", errClass: ErrorClassPolicyDenied, msgPrefix: "Gemini content blocked:"},
		{name: "tool malformed function", reason: "MALFORMED_FUNCTION_CALL", errClass: ErrorClassProviderNonRetryable, msgPrefix: "Gemini invalid response:"},
		{name: "tool unexpected call", reason: "UNEXPECTED_TOOL_CALL", errClass: ErrorClassProviderNonRetryable, msgPrefix: "Gemini invalid response:"},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				body := sseBody([]string{
					fmt.Sprintf(`{"candidates":[{"content":{"parts":[],"role":"model"},"finishReason":"%s"}]}`, tc.reason),
				})
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(server.Close)

			gw := NewGeminiGateway(GeminiGatewayConfig{APIKey: "k", BaseURL: server.URL})
			var events []StreamEvent
			_ = gw.Stream(context.Background(), Request{
				Model:    "gemini-2.0-flash",
				Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
			}, func(ev StreamEvent) error {
				events = append(events, ev)
				return nil
			})

			last := events[len(events)-1]
			failed, ok := last.(StreamRunFailed)
			if !ok {
				t.Fatalf("expected StreamRunFailed, got %T", last)
			}
			if failed.Error.ErrorClass != tc.errClass {
				t.Fatalf("unexpected error class: %q", failed.Error.ErrorClass)
			}
			if !strings.HasPrefix(failed.Error.Message, tc.msgPrefix) {
				t.Fatalf("unexpected error message: %q", failed.Error.Message)
			}
			if failed.Error.Details["finish_reason"] != tc.reason {
				t.Fatalf("unexpected finish_reason: %#v", failed.Error.Details)
			}
		})
	}
}

func TestGeminiGateway_Stream_UsageMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		body := sseBody([]string{
			`{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":10,"totalTokenCount":30,"cachedContentTokenCount":5}}`,
		})
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	gw := NewGeminiGateway(GeminiGatewayConfig{APIKey: "k", BaseURL: server.URL})
	var events []StreamEvent
	_ = gw.Stream(context.Background(), Request{
		Model:    "gemini-2.0-flash",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})

	last := events[len(events)-1]
	completed, ok := last.(StreamRunCompleted)
	if !ok {
		t.Fatalf("expected StreamRunCompleted, got %T", last)
	}
	u := completed.Usage
	if u == nil {
		t.Fatal("expected usage")
	}
	if *u.InputTokens != 20 {
		t.Fatalf("expected input=20, got %d", *u.InputTokens)
	}
	if *u.OutputTokens != 10 {
		t.Fatalf("expected output=10, got %d", *u.OutputTokens)
	}
	if *u.TotalTokens != 30 {
		t.Fatalf("expected total=30, got %d", *u.TotalTokens)
	}
	if *u.CachedTokens != 5 {
		t.Fatalf("expected cached=5, got %d", *u.CachedTokens)
	}
}

func TestGeminiGateway_Stream_AdvancedJSON_Merged(t *testing.T) {
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]
		w.Header().Set("Content-Type", "text/event-stream")
		body := sseBody([]string{
			`{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},"finishReason":"STOP"}]}`,
		})
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	gw := NewGeminiGateway(GeminiGatewayConfig{
		APIKey:  "k",
		BaseURL: server.URL,
		AdvancedJSON: map[string]any{
			"generationConfig": map[string]any{
				"stopSequences": []string{"END"},
			},
		},
	})
	_ = gw.Stream(context.Background(), Request{
		Model:    "gemini-2.0-flash",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error { return nil })

	if !strings.Contains(string(receivedBody), "stopSequences") {
		t.Fatalf("expected stopSequences in payload, got: %s", receivedBody)
	}
}

func TestGeminiGateway_Stream_AdvancedJSON_DeniedKey(t *testing.T) {
	gw := NewGeminiGateway(GeminiGatewayConfig{
		APIKey: "k",
		AdvancedJSON: map[string]any{
			"contents": []any{},
		},
	})

	var events []StreamEvent
	_ = gw.Stream(context.Background(), Request{
		Model:    "gemini-2.0-flash",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})

	if len(events) == 0 {
		t.Fatal("expected event")
	}
	failed, ok := events[0].(StreamRunFailed)
	if !ok {
		t.Fatalf("expected StreamRunFailed, got %T", events[0])
	}
	if failed.Error.ErrorClass != ErrorClassInternalError {
		t.Fatalf("unexpected error class: %q", failed.Error.ErrorClass)
	}
	if failed.Error.Details["denied_key"] != "contents" {
		t.Fatalf("unexpected denied_key: %v", failed.Error.Details["denied_key"])
	}
}

func TestGeminiGateway_StreamSSE_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {not valid json}\n\n"))
	}))
	t.Cleanup(server.Close)

	gw := NewGeminiGateway(GeminiGatewayConfig{APIKey: "k", BaseURL: server.URL})
	var events []StreamEvent
	_ = gw.Stream(context.Background(), Request{
		Model:    "gemini-2.0-flash",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})

	last := events[len(events)-1]
	if _, ok := last.(StreamRunFailed); !ok {
		t.Fatalf("expected StreamRunFailed for invalid JSON, got %T", last)
	}
}

func TestGeminiGateway_StreamSSE_EmptyStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// 空响应
	}))
	t.Cleanup(server.Close)

	gw := NewGeminiGateway(GeminiGatewayConfig{APIKey: "k", BaseURL: server.URL})
	var events []StreamEvent
	_ = gw.Stream(context.Background(), Request{
		Model:    "gemini-2.0-flash",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error {
		events = append(events, ev)
		return nil
	})

	// 空流应该正常结束为 StreamRunCompleted（无 usage）
	last := events[len(events)-1]
	if _, ok := last.(StreamRunCompleted); !ok {
		t.Fatalf("expected StreamRunCompleted for empty stream, got %T", last)
	}
}

func TestGeminiGateway_Stream_ReasoningMode_Enabled(t *testing.T) {
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 8192)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]
		w.Header().Set("Content-Type", "text/event-stream")
		body := sseBody([]string{
			`{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},"finishReason":"STOP"}]}`,
		})
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	gw := NewGeminiGateway(GeminiGatewayConfig{APIKey: "k", BaseURL: server.URL})
	_ = gw.Stream(context.Background(), Request{
		Model:         "gemini-2.0-flash",
		Messages:      []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
		ReasoningMode: "enabled",
	}, func(ev StreamEvent) error { return nil })

	if !strings.Contains(string(receivedBody), "thinkingBudget") {
		t.Fatalf("expected thinkingBudget in payload, got: %s", receivedBody)
	}
	if !strings.Contains(string(receivedBody), "8192") {
		t.Fatalf("expected budget=8192 in payload, got: %s", receivedBody)
	}
	if !strings.Contains(string(receivedBody), `"includeThoughts":true`) {
		t.Fatalf("expected includeThoughts:true in payload, got: %s", receivedBody)
	}
}

func TestGeminiGateway_Stream_ReasoningMode_Disabled(t *testing.T) {
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 8192)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]
		w.Header().Set("Content-Type", "text/event-stream")
		body := sseBody([]string{
			`{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},"finishReason":"STOP"}]}`,
		})
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	gw := NewGeminiGateway(GeminiGatewayConfig{APIKey: "k", BaseURL: server.URL})
	_ = gw.Stream(context.Background(), Request{
		Model:         "gemini-2.0-flash",
		Messages:      []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
		ReasoningMode: "disabled",
	}, func(ev StreamEvent) error { return nil })

	if !strings.Contains(string(receivedBody), `"thinkingBudget":0`) {
		t.Fatalf("expected thinkingBudget:0 in payload, got: %s", receivedBody)
	}
	if !strings.Contains(string(receivedBody), `"includeThoughts":false`) {
		t.Fatalf("expected includeThoughts:false in payload, got: %s", receivedBody)
	}
}

func TestGeminiGateway_Stream_ReasoningConfigAutoAddsIncludeThoughts(t *testing.T) {
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 8192)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]
		w.Header().Set("Content-Type", "text/event-stream")
		body := sseBody([]string{
			`{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},"finishReason":"STOP"}]}`,
		})
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	gw := NewGeminiGateway(GeminiGatewayConfig{
		APIKey:  "k",
		BaseURL: server.URL,
		AdvancedJSON: map[string]any{
			"generationConfig": map[string]any{
				"thinkingConfig": map[string]any{
					"thinkingBudget": 2048,
				},
			},
		},
	})

	_ = gw.Stream(context.Background(), Request{
		Model:    "gemini-2.0-flash",
		Messages: []Message{{Role: "user", Content: []TextPart{{Text: "hi"}}}},
	}, func(ev StreamEvent) error { return nil })

	if !strings.Contains(string(receivedBody), `"thinkingBudget":2048`) {
		t.Fatalf("expected thinkingBudget:2048 in payload, got: %s", receivedBody)
	}
	if !strings.Contains(string(receivedBody), `"includeThoughts":true`) {
		t.Fatalf("expected includeThoughts:true in payload, got: %s", receivedBody)
	}
}
