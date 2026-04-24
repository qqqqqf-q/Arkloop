package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/genai"
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

func TestGeminiSDKGateway_StreamFunctionCallIDFallback(t *testing.T) {
	state := newGeminiSDKStreamState("llm_1", func(event StreamEvent) error { return nil })
	var tool ToolCall
	state.yield = func(event StreamEvent) error {
		if ev, ok := event.(ToolCall); ok {
			tool = ev
		}
		return nil
	}
	err := state.handle(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "echo", Args: map[string]any{"text": "hi"}}}}}, FinishReason: genai.FinishReasonStop}}})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if tool.ToolCallID != "llm_1:0" || tool.ToolName != "echo" || tool.ArgumentsJSON["text"] != "hi" {
		t.Fatalf("unexpected tool: %#v", tool)
	}
}

func TestGeminiSDKGenerateImageConfigUsesTypedGenerationConfig(t *testing.T) {
	config, err := geminiSDKGenerateImageConfig(map[string]any{
		"generationConfig": map[string]any{"temperature": 0.2, "maxOutputTokens": 64, "responseModalities": []any{"TEXT"}},
		"safetySettings":   []any{map[string]any{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "BLOCK_NONE"}},
	})
	if err != nil {
		t.Fatalf("geminiSDKGenerateImageConfig: %v", err)
	}
	if config.ResponseModalities == nil || len(config.ResponseModalities) != 1 || config.ResponseModalities[0] != "IMAGE" {
		t.Fatalf("response modalities not typed: %#v", config.ResponseModalities)
	}
	if config.Temperature == nil || *config.Temperature != float32(0.2) || config.MaxOutputTokens != 64 {
		t.Fatalf("generation config not typed: %#v", config)
	}
	if config.HTTPOptions == nil || config.HTTPOptions.ExtraBody["generationConfig"] != nil || config.HTTPOptions.ExtraBody["safetySettings"] == nil {
		t.Fatalf("unexpected extra body: %#v", config.HTTPOptions)
	}
}

func TestGeminiSDKGateway_NormalizesVersionedBaseURL(t *testing.T) {
	gateway := NewGeminiGatewaySDK(GeminiGatewayConfig{Transport: TransportConfig{APIKey: "key", BaseURL: "https://generativelanguage.googleapis.com/v1"}}).(*geminiSDKGateway)
	if gateway.transport.cfg.BaseURL != "https://generativelanguage.googleapis.com" {
		t.Fatalf("unexpected base url: %q", gateway.transport.cfg.BaseURL)
	}
	if gateway.protocol.APIVersion != "v1" {
		t.Fatalf("unexpected api version: %q", gateway.protocol.APIVersion)
	}
}

func TestGeminiSDKStreamState_PromptFeedbackFailsRun(t *testing.T) {
	var failed *StreamRunFailed
	state := newGeminiSDKStreamState("llm_1", func(event StreamEvent) error {
		if ev, ok := event.(StreamRunFailed); ok {
			failed = &ev
		}
		return nil
	})
	if err := state.handle(&genai.GenerateContentResponse{PromptFeedback: &genai.GenerateContentResponsePromptFeedback{BlockReason: genai.BlockedReasonSafety}}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if failed == nil || failed.Error.ErrorClass != ErrorClassPolicyDenied {
		t.Fatalf("unexpected failure: %#v", failed)
	}
}

func TestGeminiSDKStreamState_BuffersToolCallsUntilTerminalFinish(t *testing.T) {
	var tools []ToolCall
	state := newGeminiSDKStreamState("llm_1", func(event StreamEvent) error {
		if ev, ok := event.(ToolCall); ok {
			tools = append(tools, ev)
		}
		return nil
	})
	if err := state.handle(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "echo", Args: map[string]any{"text": "partial"}}}}}}}}); err != nil {
		t.Fatalf("handle partial: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("tool emitted before terminal finish: %#v", tools)
	}
	if err := state.handle(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "echo", Args: map[string]any{"text": "final"}}}}}, FinishReason: genai.FinishReasonStop}}}); err != nil {
		t.Fatalf("handle terminal: %v", err)
	}
	if len(tools) != 1 || tools[0].ToolCallID != "llm_1:0" || tools[0].ArgumentsJSON["text"] != "final" {
		t.Fatalf("unexpected buffered tool: %#v", tools)
	}
}

func TestGeminiSDKStreamState_UnfinishedToolCallFailsRun(t *testing.T) {
	var failed *StreamRunFailed
	state := newGeminiSDKStreamState("llm_1", func(event StreamEvent) error {
		if ev, ok := event.(StreamRunFailed); ok {
			failed = &ev
		}
		return nil
	})
	if err := state.handle(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "echo", Args: map[string]any{"text": "partial"}}}}}}}}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if err := state.complete(); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if failed == nil || failed.Error.ErrorClass != ErrorClassProviderRetryable {
		t.Fatalf("unexpected failure: %#v", failed)
	}
}

func TestGeminiSDKPart_FunctionResponseWrapsScalarResult(t *testing.T) {
	part, err := geminiSDKPart(map[string]any{"functionResponse": map[string]any{"id": "call_1", "name": "echo", "response": "ok"}})
	if err != nil {
		t.Fatalf("geminiSDKPart: %v", err)
	}
	if part.FunctionResponse == nil || part.FunctionResponse.ID != "call_1" || part.FunctionResponse.Response["output"] != "ok" {
		t.Fatalf("unexpected function response: %#v", part.FunctionResponse)
	}
}

func TestGeminiSDKPart_FunctionResponseKeepsObjectResult(t *testing.T) {
	part, err := geminiSDKPart(map[string]any{"functionResponse": map[string]any{"id": "call_1", "name": "echo", "response": map[string]any{"ok": true}}})
	if err != nil {
		t.Fatalf("geminiSDKPart: %v", err)
	}
	if part.FunctionResponse == nil || part.FunctionResponse.Response["ok"] != true || part.FunctionResponse.Response["output"] != nil {
		t.Fatalf("unexpected function response: %#v", part.FunctionResponse)
	}
}
