package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func geminiSDKSSE(chunks []string) string {
	out := ""
	for _, chunk := range chunks {
		out += "data: " + chunk + "\n\n"
	}
	return out
}

func TestGeminiSDKGateway_StreamRequestAndEvents(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-test:streamGenerateContent" {
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(geminiSDKSSE([]string{
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"think","thought":true},{"text":"hello"},{"functionCall":{"id":"call_1","name":"echo","args":{"text":"ok"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5}}`,
		})))
	}))
	defer server.Close()

	gateway := NewGeminiGatewaySDK(GeminiGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Protocol: GeminiProtocolConfig{APIVersion: "v1beta", AdvancedPayloadJSON: map[string]any{"generationConfig": map[string]any{"topK": 3}}}})
	var events []StreamEvent
	if err := gateway.Stream(context.Background(), Request{Model: "gemini-test", Messages: []Message{{Role: "system", Content: []ContentPart{{Text: "sys"}}}, {Role: "user", Content: []ContentPart{{Text: "hi"}}}}, Tools: []ToolSpec{{Name: "echo", JSONSchema: map[string]any{"type": "object"}}}, ToolChoice: &ToolChoice{Mode: "required"}, ReasoningMode: "enabled"}, func(event StreamEvent) error { events = append(events, event); return nil }); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if captured["systemInstruction"] == nil || captured["tools"] == nil || captured["toolConfig"] == nil {
		t.Fatalf("missing request fields: %#v", captured)
	}
	gen := captured["generationConfig"].(map[string]any)
	if gen["topK"] != float64(3) {
		t.Fatalf("missing advanced generation config: %#v", gen)
	}
	if _, leaked := captured["generationConfig"].(map[string]any)["tools"]; leaked {
		t.Fatalf("typed fields leaked into generation config: %#v", gen)
	}
	var thinking, text bool
	var tool *ToolCall
	var completed *StreamRunCompleted
	for _, event := range events {
		switch ev := event.(type) {
		case StreamMessageDelta:
			if ev.Channel != nil {
				thinking = true
			} else if ev.ContentDelta == "hello" {
				text = true
			}
		case ToolCall:
			tool = &ev
		case StreamRunCompleted:
			completed = &ev
		}
	}
	if !thinking || !text || tool == nil || tool.ArgumentsJSON["text"] != "ok" {
		t.Fatalf("unexpected events: %#v", events)
	}
	if completed == nil || completed.Usage == nil || *completed.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected completion: %#v", completed)
	}
}

func TestGeminiSDKGateway_ReplaysToolCallID(t *testing.T) {
	call := ToolCall{ToolCallID: "call_123", ToolName: "echo", ArgumentsJSON: map[string]any{"text": "hi"}}
	result := Message{Role: "tool", Content: []ContentPart{{Text: `{"tool_call_id":"call_123","tool_name":"echo","result":{"ok":true}}`}}}
	_, contents, err := toGeminiContents([]Message{{Role: "assistant", ToolCalls: []ToolCall{call}}, result})
	if err != nil {
		t.Fatalf("toGeminiContents: %v", err)
	}
	if len(contents) != 2 {
		t.Fatalf("unexpected contents: %#v", contents)
	}
	assistant, err := geminiSDKContent(contents[0])
	if err != nil {
		t.Fatalf("assistant content: %v", err)
	}
	toolResult, err := geminiSDKContent(contents[1])
	if err != nil {
		t.Fatalf("tool result content: %v", err)
	}
	if got := assistant.Parts[0].FunctionCall.ID; got != "call_123" {
		t.Fatalf("function call id lost: %q", got)
	}
	if got := toolResult.Parts[0].FunctionResponse.ID; got != "call_123" {
		t.Fatalf("function response id lost: %q", got)
	}
}

func TestGeminiSDKGateway_GenerateImageUsesSDKGatePath(t *testing.T) {
	t.Setenv("ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP", "true")
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nimage"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-image:generateContent" {
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"` + png + `"}}]}}]}`))
	}))
	defer server.Close()
	cfg := ResolvedGatewayConfig{ProtocolKind: ProtocolKindGeminiGenerateContent, Model: "gemini-image", Transport: TransportConfig{APIKey: "key", BaseURL: server.URL}, Gemini: &GeminiProtocolConfig{APIVersion: "v1beta"}}
	image, err := GenerateImageWithResolvedConfig(context.Background(), cfg, ImageGenerationRequest{Prompt: "draw"})
	if err != nil {
		t.Fatalf("GenerateImageWithResolvedConfig: %v", err)
	}
	if image.ProviderKind != "gemini" || image.MimeType != "image/png" || len(image.Bytes) == 0 {
		t.Fatalf("unexpected image: %#v", image)
	}
}
