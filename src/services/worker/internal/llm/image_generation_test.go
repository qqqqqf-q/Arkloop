package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"arkloop/services/shared/messagecontent"
)

const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+yF9kAAAAASUVORK5CYII="

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(status int, body map[string]any) *http.Response {
	raw, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(raw))),
	}
}

func TestOpenAIGatewayGenerateImageWithResponsesAPI(t *testing.T) {
	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		Transport: TransportConfig{
			APIKey:  "sk-test",
			BaseURL: "https://api.openai.test/v1",
		},
		Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIResponses},
	})
	gateway.transport.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["model"] != "gpt-5.4" {
			t.Fatalf("unexpected model: %#v", payload["model"])
		}
		input, ok := payload["input"].([]any)
		if !ok || len(input) != 1 {
			t.Fatalf("unexpected input payload: %#v", payload["input"])
		}
		message, ok := input[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected input[0]: %#v", input[0])
		}
		content, ok := message["content"].([]any)
		if !ok || len(content) != 3 {
			t.Fatalf("unexpected content: %#v", message["content"])
		}
		if content[0].(map[string]any)["text"] != "draw a cat" {
			t.Fatalf("unexpected text block: %#v", content[0])
		}
		if content[1].(map[string]any)["text"] != "[attachment_key:demo/source.png]" {
			t.Fatalf("unexpected attachment block: %#v", content[1])
		}
		if content[2].(map[string]any)["type"] != "input_image" {
			t.Fatalf("unexpected image block: %#v", content[2])
		}
		tools, ok := payload["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("unexpected tools: %#v", payload["tools"])
		}
		tool, ok := tools[0].(map[string]any)
		if !ok || tool["size"] != "1024x1024" || tool["quality"] != "high" {
			t.Fatalf("unexpected tool config: %#v", tools[0])
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"output": []map[string]any{
				{
					"type":           "image_generation_call",
					"result":         tinyPNGBase64,
					"revised_prompt": "draw a small cat",
				},
			},
		}), nil
	})}

	image, err := gateway.GenerateImage(context.Background(), "gpt-5.4", ImageGenerationRequest{
		Prompt:  "draw a cat",
		Size:    "1024x1024",
		Quality: "high",
		InputImages: []ContentPart{{
			Type: "image",
			Attachment: &messagecontent.AttachmentRef{
				Key:      "demo/source.png",
				MimeType: "image/png",
			},
			Data: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0},
		}},
	})
	if err != nil {
		t.Fatalf("GenerateImage returned error: %v", err)
	}
	if image.ProviderKind != "openai" || image.Model != "gpt-5.4" {
		t.Fatalf("unexpected image metadata: %#v", image)
	}
	if image.MimeType != "image/png" {
		t.Fatalf("unexpected mime type: %s", image.MimeType)
	}
	if image.RevisedPrompt != "draw a small cat" {
		t.Fatalf("unexpected revised prompt: %q", image.RevisedPrompt)
	}
}

func TestOpenAIGatewayGenerateImageWithImagesAPI(t *testing.T) {
	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		Transport: TransportConfig{
			APIKey:  "sk-test",
			BaseURL: "https://api.openai.test/v1",
		},
		Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIResponses},
	})
	gateway.transport.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["response_format"] != "b64_json" {
			t.Fatalf("unexpected response_format: %#v", payload["response_format"])
		}
		if payload["output_format"] != "png" {
			t.Fatalf("unexpected output_format: %#v", payload["output_format"])
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"data": []map[string]any{
				{
					"b64_json":       tinyPNGBase64,
					"revised_prompt": "draw a skyline at dusk",
				},
			},
		}), nil
	})}

	image, err := gateway.GenerateImage(context.Background(), "gpt-image-1", ImageGenerationRequest{
		Prompt:              "draw a skyline",
		OutputFormat:        "png",
		ForceOpenAIImageAPI: true,
	})
	if err != nil {
		t.Fatalf("GenerateImage returned error: %v", err)
	}
	if image.ProviderKind != "openai" || image.Model != "gpt-image-1" {
		t.Fatalf("unexpected image metadata: %#v", image)
	}
}

func TestOpenAIGatewayGenerateImageWithEditsAPI(t *testing.T) {
	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		Transport: TransportConfig{
			APIKey:  "sk-test",
			BaseURL: "https://api.openai.test/v1",
		},
		Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIResponses},
	})
	gateway.transport.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/images/edits" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		images, ok := payload["images"].([]any)
		if !ok || len(images) != 1 {
			t.Fatalf("unexpected images payload: %#v", payload["images"])
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"data": []map[string]any{
				{
					"b64_json": tinyPNGBase64,
				},
			},
		}), nil
	})}

	image, err := gateway.GenerateImage(context.Background(), "gpt-image-2", ImageGenerationRequest{
		Prompt:              "edit this image",
		ForceOpenAIImageAPI: true,
		InputImages: []ContentPart{{
			Type: "image",
			Attachment: &messagecontent.AttachmentRef{
				Key:      "demo/source.png",
				MimeType: "image/png",
			},
			Data: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0},
		}},
	})
	if err != nil {
		t.Fatalf("GenerateImage returned error: %v", err)
	}
	if image.ProviderKind != "openai" || image.Model != "gpt-image-2" {
		t.Fatalf("unexpected image metadata: %#v", image)
	}
}

func TestParseOpenAIResponsesImageReturnsExcerptWhenMissingImage(t *testing.T) {
	_, err := parseOpenAIResponsesImage([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}]}`), "gpt-image-2")
	if err == nil {
		t.Fatal("expected error")
	}
	gatewayErr, ok := err.(GatewayError)
	if !ok {
		t.Fatalf("unexpected error type: %T", err)
	}
	if gatewayErr.ErrorClass != ErrorClassProviderNonRetryable {
		t.Fatalf("unexpected error class: %#v", gatewayErr)
	}
	if gatewayErr.Details["response_excerpt"] == "" {
		t.Fatalf("expected response excerpt in details: %#v", gatewayErr.Details)
	}
}

func TestGeminiGatewayGenerateImage(t *testing.T) {
	gateway := NewGeminiGateway(GeminiGatewayConfig{
		Transport: TransportConfig{
			APIKey:  "gemini-test",
			BaseURL: "https://gemini.test/v1beta",
		},
		Protocol: GeminiProtocolConfig{APIVersion: "v1beta"},
	})
	gateway.transport.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1beta/models/gemini-2.5-flash-image:generateContent" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		generationConfig, ok := payload["generationConfig"].(map[string]any)
		if !ok {
			t.Fatalf("missing generationConfig: %#v", payload)
		}
		modalities, ok := generationConfig["responseModalities"].([]any)
		if !ok || len(modalities) != 1 || modalities[0] != "IMAGE" {
			t.Fatalf("unexpected responseModalities: %#v", generationConfig["responseModalities"])
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "ignored"},
							{
								"inlineData": map[string]any{
									"mimeType": "image/png",
									"data":     tinyPNGBase64,
								},
							},
						},
					},
				},
			},
		}), nil
	})}

	image, err := gateway.GenerateImage(context.Background(), "gemini-2.5-flash-image", ImageGenerationRequest{Prompt: "draw a poster"})
	if err != nil {
		t.Fatalf("GenerateImage returned error: %v", err)
	}
	if image.ProviderKind != "gemini" || image.Model != "gemini-2.5-flash-image" {
		t.Fatalf("unexpected image metadata: %#v", image)
	}
	if image.MimeType != "image/png" {
		t.Fatalf("unexpected mime type: %s", image.MimeType)
	}
	decoded, _ := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if string(image.Bytes) != string(decoded) {
		t.Fatal("unexpected image bytes")
	}
}

func TestOpenAIGatewayGenerateImageWithImagesAPIDisallowsInputImages(t *testing.T) {
	gateway := NewOpenAIGateway(OpenAIGatewayConfig{
		Transport: TransportConfig{
			APIKey:  "sk-test",
			BaseURL: "https://api.openai.test/v1",
		},
		Protocol: OpenAIProtocolConfig{PrimaryKind: ProtocolKindOpenAIChatCompletions},
	})
	gateway.transport.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/images/edits" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"data": []map[string]any{
				{
					"b64_json": tinyPNGBase64,
				},
			},
		}), nil
	})}

	_, err := gateway.GenerateImage(context.Background(), "gpt-image-1", ImageGenerationRequest{
		Prompt: "draw a skyline",
		InputImages: []ContentPart{{
			Type: "image",
			Attachment: &messagecontent.AttachmentRef{
				Key:      "demo/source.png",
				MimeType: "image/png",
			},
			Data: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0},
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
