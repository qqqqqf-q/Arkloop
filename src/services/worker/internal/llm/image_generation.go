package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const imageGenerationMaxResponseBytes = 20 * 1024 * 1024

type GeneratedImage struct {
	Bytes         []byte
	MimeType      string
	ProviderKind  string
	Model         string
	RevisedPrompt string
}

type ImageGenerationRequest struct {
	Prompt              string
	InputImages         []ContentPart
	Size                string
	Quality             string
	Background          string
	OutputFormat        string
	ForceOpenAIImageAPI bool
}

func GenerateImageWithResolvedConfig(ctx context.Context, cfg ResolvedGatewayConfig, req ImageGenerationRequest) (GeneratedImage, error) {
	req.Prompt = strings.TrimSpace(req.Prompt)
	req.Size = strings.TrimSpace(req.Size)
	req.Quality = strings.TrimSpace(req.Quality)
	req.Background = strings.TrimSpace(req.Background)
	req.OutputFormat = strings.TrimSpace(req.OutputFormat)
	if req.Prompt == "" {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassConfigInvalid,
			Message:    "image generation prompt is required",
		}
	}

	switch cfg.ProtocolKind {
	case ProtocolKindOpenAIChatCompletions, ProtocolKindOpenAIResponses:
		if cfg.OpenAI == nil {
			return GeneratedImage{}, GatewayError{
				ErrorClass: ErrorClassConfigInvalid,
				Message:    "missing openai protocol config",
			}
		}
		gateway := NewOpenAIGateway(OpenAIGatewayConfig{
			Transport: cfg.Transport,
			Protocol:  *cfg.OpenAI,
		})
		return gateway.GenerateImage(ctx, cfg.Model, req)
	case ProtocolKindGeminiGenerateContent:
		if cfg.Gemini == nil {
			return GeneratedImage{}, GatewayError{
				ErrorClass: ErrorClassConfigInvalid,
				Message:    "missing gemini protocol config",
			}
		}
		gateway := NewGeminiGateway(GeminiGatewayConfig{
			Transport: cfg.Transport,
			Protocol:  *cfg.Gemini,
		})
		return gateway.GenerateImage(ctx, cfg.Model, req)
	default:
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassConfigInvalid,
			Message:    fmt.Sprintf("image generation unsupported for protocol: %s", cfg.ProtocolKind),
		}
	}
}

func (g *OpenAIGateway) GenerateImage(ctx context.Context, model string, req ImageGenerationRequest) (GeneratedImage, error) {
	if g == nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassConfigMissing,
			Message:    "openai gateway is not initialized",
		}
	}
	if g.configErr != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassConfigInvalid,
			Message:    g.configErr.Error(),
		}
	}
	if g.transport.baseURLErr != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassConfigInvalid,
			Message:    "OpenAI base_url blocked",
			Details:    map[string]any{"reason": g.transport.baseURLErr.Error()},
		}
	}

	if req.ForceOpenAIImageAPI || g.protocol.PrimaryKind == ProtocolKindOpenAIChatCompletions {
		return g.generateImageWithImageAPI(ctx, model, req)
	}
	return g.generateImageWithResponsesAPI(ctx, model, req)
}

func (g *OpenAIGateway) generateImageWithImageAPI(ctx context.Context, model string, req ImageGenerationRequest) (GeneratedImage, error) {
	if len(req.InputImages) > 0 {
		return g.generateImageWithEditsAPI(ctx, model, req)
	}
	return g.generateImageWithImagesAPI(ctx, model, req)
}

func (g *OpenAIGateway) generateImageWithResponsesAPI(ctx context.Context, model string, req ImageGenerationRequest) (GeneratedImage, error) {
	payload := copyAnyMap(g.protocol.AdvancedPayloadJSON)
	payload["model"] = strings.TrimSpace(model)
	blocks, err := imageGenerationOpenAIBlocks(req)
	if err != nil {
		return GeneratedImage{}, err
	}
	payload["input"] = []map[string]any{{
		"role":    "user",
		"content": blocks,
	}}
	payload["tools"] = []map[string]any{imageGenerationOpenAITool(req)}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "OpenAI image request serialization failed",
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.transport.endpoint("/responses"), bytes.NewReader(encoded))
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "OpenAI image request construction failed",
			Details:    map[string]any{"reason": err.Error()},
		}
	}
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(g.transport.cfg.APIKey))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	g.transport.applyDefaultHeaders(httpReq)

	resp, err := g.transport.client.Do(httpReq)
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderRetryable,
			Message:    "OpenAI image network error",
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _, _ := readAllWithLimit(resp.Body, openAIMaxErrorBodyBytes)
		message, details := openAIErrorMessageAndDetails(body, resp.StatusCode, "OpenAI image request failed")
		return GeneratedImage{}, GatewayError{
			ErrorClass: errorClassFromStatus(resp.StatusCode),
			Message:    message,
			Details:    details,
		}
	}

	body, _, err := readAllWithLimit(resp.Body, imageGenerationMaxResponseBytes)
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderRetryable,
			Message:    "OpenAI image response read failed",
			Details:    map[string]any{"reason": err.Error()},
		}
	}
	return parseOpenAIResponsesImage(body, model)
}

func (g *OpenAIGateway) generateImageWithImagesAPI(ctx context.Context, model string, req ImageGenerationRequest) (GeneratedImage, error) {
	payload := map[string]any{
		"model":           strings.TrimSpace(model),
		"prompt":          req.Prompt,
		"response_format": "b64_json",
	}
	applyOpenAIImageOptions(payload, req)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "OpenAI image request serialization failed",
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.transport.endpoint("/images/generations"), bytes.NewReader(encoded))
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "OpenAI image request construction failed",
			Details:    map[string]any{"reason": err.Error()},
		}
	}
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(g.transport.cfg.APIKey))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	g.transport.applyDefaultHeaders(httpReq)

	resp, err := g.transport.client.Do(httpReq)
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderRetryable,
			Message:    "OpenAI image network error",
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _, _ := readAllWithLimit(resp.Body, openAIMaxErrorBodyBytes)
		message, details := openAIErrorMessageAndDetails(body, resp.StatusCode, "OpenAI image request failed")
		return GeneratedImage{}, GatewayError{
			ErrorClass: errorClassFromStatus(resp.StatusCode),
			Message:    message,
			Details:    details,
		}
	}

	body, _, err := readAllWithLimit(resp.Body, imageGenerationMaxResponseBytes)
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderRetryable,
			Message:    "OpenAI image response read failed",
			Details:    map[string]any{"reason": err.Error()},
		}
	}
	return parseOpenAIImagesAPIResponse(body, model)
}

func (g *OpenAIGateway) generateImageWithEditsAPI(ctx context.Context, model string, req ImageGenerationRequest) (GeneratedImage, error) {
	images := make([]map[string]any, 0, len(req.InputImages))
	for idx, image := range req.InputImages {
		dataURL, err := partDataURL(image)
		if err != nil {
			return GeneratedImage{}, GatewayError{
				ErrorClass: ErrorClassConfigInvalid,
				Message:    "OpenAI image input encoding failed",
				Details:    map[string]any{"index": idx, "reason": err.Error()},
			}
		}
		images = append(images, map[string]any{"image_url": dataURL})
	}
	payload := map[string]any{
		"model":           strings.TrimSpace(model),
		"prompt":          req.Prompt,
		"images":          images,
		"response_format": "b64_json",
	}
	applyOpenAIImageOptions(payload, req)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "OpenAI image edit request serialization failed",
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.transport.endpoint("/images/edits"), bytes.NewReader(encoded))
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "OpenAI image edit request construction failed",
			Details:    map[string]any{"reason": err.Error()},
		}
	}
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(g.transport.cfg.APIKey))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	g.transport.applyDefaultHeaders(httpReq)

	resp, err := g.transport.client.Do(httpReq)
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderRetryable,
			Message:    "OpenAI image network error",
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _, _ := readAllWithLimit(resp.Body, openAIMaxErrorBodyBytes)
		message, details := openAIErrorMessageAndDetails(body, resp.StatusCode, "OpenAI image request failed")
		return GeneratedImage{}, GatewayError{
			ErrorClass: errorClassFromStatus(resp.StatusCode),
			Message:    message,
			Details:    details,
		}
	}

	body, _, err := readAllWithLimit(resp.Body, imageGenerationMaxResponseBytes)
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderRetryable,
			Message:    "OpenAI image response read failed",
			Details:    map[string]any{"reason": err.Error()},
		}
	}
	return parseOpenAIImagesAPIResponse(body, model)
}

func (g *GeminiGateway) GenerateImage(ctx context.Context, model string, req ImageGenerationRequest) (GeneratedImage, error) {
	if g == nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassConfigMissing,
			Message:    "gemini gateway is not initialized",
		}
	}
	if g.configErr != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassConfigInvalid,
			Message:    g.configErr.Error(),
		}
	}
	if g.transport.baseURLErr != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassConfigInvalid,
			Message:    "Gemini base_url blocked",
			Details:    map[string]any{"reason": g.transport.baseURLErr.Error()},
		}
	}

	payload := copyAnyMap(g.protocol.AdvancedPayloadJSON)
	parts, err := imageGenerationGeminiParts(req)
	if err != nil {
		return GeneratedImage{}, err
	}
	payload["contents"] = []map[string]any{{
		"role":  "user",
		"parts": parts,
	}}

	generationConfig := map[string]any{}
	if rawGenerationConfig, ok := payload["generationConfig"].(map[string]any); ok {
		generationConfig = copyAnyMap(rawGenerationConfig)
	}
	generationConfig["responseModalities"] = []string{"IMAGE"}
	payload["generationConfig"] = generationConfig

	encoded, err := json.Marshal(payload)
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "Gemini image request serialization failed",
		}
	}

	path := geminiVersionedPath(g.transport.cfg.BaseURL, g.protocol.APIVersion, fmt.Sprintf("/models/%s:generateContent", strings.TrimSpace(model)))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.transport.endpoint(path), bytes.NewReader(encoded))
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "Gemini image request construction failed",
			Details:    map[string]any{"reason": err.Error()},
		}
	}
	httpReq.Header.Set("x-goog-api-key", strings.TrimSpace(g.transport.cfg.APIKey))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	g.transport.applyDefaultHeaders(httpReq)

	resp, err := g.transport.client.Do(httpReq)
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderRetryable,
			Message:    "Gemini image network error",
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, bodyTruncated, _ := readAllWithLimit(resp.Body, geminiMaxErrorBodyBytes)
		message, details := geminiErrorMessageAndDetails(body, resp.StatusCode, bodyTruncated)
		return GeneratedImage{}, GatewayError{
			ErrorClass: errorClassFromStatus(resp.StatusCode),
			Message:    message,
			Details:    details,
		}
	}

	body, _, err := readAllWithLimit(resp.Body, imageGenerationMaxResponseBytes)
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderRetryable,
			Message:    "Gemini image response read failed",
			Details:    map[string]any{"reason": err.Error()},
		}
	}
	return parseGeminiImageResponse(body, model)
}

func parseOpenAIResponsesImage(body []byte, model string) (GeneratedImage, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderNonRetryable,
			Message:    "OpenAI image response parse failed",
			Details:    map[string]any{"reason": err.Error()},
		}
	}

	rawOutput, ok := root["output"].([]any)
	if !ok {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderNonRetryable,
			Message:    "OpenAI image response missing output",
		}
	}

	for _, rawItem := range rawOutput {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(stringValueFromAny(item["type"])) != "image_generation_call" {
			continue
		}
		imageBase64 := strings.TrimSpace(stringValueFromAny(item["result"]))
		if imageBase64 == "" {
			continue
		}
		return buildGeneratedImage(imageBase64, stringValueFromAny(item["revised_prompt"]), "openai", model)
	}

	return GeneratedImage{}, GatewayError{
		ErrorClass: ErrorClassProviderNonRetryable,
		Message:    "OpenAI image response contained no generated image",
		Details:    map[string]any{"response_excerpt": compactResponseExcerpt(body)},
	}
}

func parseOpenAIImagesAPIResponse(body []byte, model string) (GeneratedImage, error) {
	var root struct {
		Data []struct {
			B64JSON       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderNonRetryable,
			Message:    "OpenAI image response parse failed",
			Details:    map[string]any{"reason": err.Error()},
		}
	}
	if len(root.Data) == 0 || strings.TrimSpace(root.Data[0].B64JSON) == "" {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderNonRetryable,
			Message:    "OpenAI image response contained no generated image",
			Details:    map[string]any{"response_excerpt": compactResponseExcerpt(body)},
		}
	}
	return buildGeneratedImage(root.Data[0].B64JSON, root.Data[0].RevisedPrompt, "openai", model)
}

func parseGeminiImageResponse(body []byte, model string) (GeneratedImage, error) {
	type geminiInlineData struct {
		MimeType string `json:"mimeType"`
		Data     string `json:"data"`
	}
	type geminiImagePart struct {
		Text       string            `json:"text"`
		InlineData *geminiInlineData `json:"inlineData"`
		Thought    bool              `json:"thought"`
	}
	type geminiImageResponse struct {
		Candidates []struct {
			Content struct {
				Parts []geminiImagePart `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	var root geminiImageResponse
	if err := json.Unmarshal(body, &root); err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderNonRetryable,
			Message:    "Gemini image response parse failed",
			Details:    map[string]any{"reason": err.Error()},
		}
	}

	var lastInline *geminiInlineData
	for _, candidate := range root.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.InlineData == nil || strings.TrimSpace(part.InlineData.Data) == "" {
				continue
			}
			lastInline = part.InlineData
		}
	}
	if lastInline == nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderNonRetryable,
			Message:    "Gemini image response contained no generated image",
		}
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lastInline.Data))
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderNonRetryable,
			Message:    "Gemini image payload is not valid base64",
			Details:    map[string]any{"reason": err.Error()},
		}
	}
	mimeType := strings.TrimSpace(lastInline.MimeType)
	if mimeType == "" {
		mimeType = detectGeneratedImageMime(decoded)
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderNonRetryable,
			Message:    "Gemini returned non-image content for image generation",
			Details:    map[string]any{"mime_type": mimeType},
		}
	}
	return GeneratedImage{
		Bytes:        decoded,
		MimeType:     mimeType,
		ProviderKind: "gemini",
		Model:        model,
	}, nil
}

func buildGeneratedImage(imageBase64 string, revisedPrompt string, provider string, model string) (GeneratedImage, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(imageBase64))
	if err != nil {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderNonRetryable,
			Message:    "generated image payload is not valid base64",
			Details:    map[string]any{"reason": err.Error()},
		}
	}
	mimeType := detectGeneratedImageMime(decoded)
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return GeneratedImage{}, GatewayError{
			ErrorClass: ErrorClassProviderNonRetryable,
			Message:    "image generation returned non-image content",
			Details:    map[string]any{"mime_type": mimeType},
		}
	}
	return GeneratedImage{
		Bytes:         decoded,
		MimeType:      mimeType,
		ProviderKind:  provider,
		Model:         model,
		RevisedPrompt: strings.TrimSpace(revisedPrompt),
	}, nil
}

func detectGeneratedImageMime(data []byte) string {
	mimeType := strings.TrimSpace(http.DetectContentType(data))
	if strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return mimeType
	}
	return "application/octet-stream"
}

func compactResponseExcerpt(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	text := strings.Join(strings.Fields(strings.TrimSpace(string(body))), " ")
	if len(text) > 320 {
		return text[:320]
	}
	return text
}

func imageGenerationOpenAIBlocks(req ImageGenerationRequest) ([]map[string]any, error) {
	content := make([]ContentPart, 0, len(req.InputImages)+1)
	content = append(content, ContentPart{Type: "text", Text: req.Prompt})
	content = append(content, req.InputImages...)
	blocks, err := toOpenAIResponsesContentBlocks(content)
	if err != nil {
		return nil, GatewayError{
			ErrorClass: ErrorClassConfigInvalid,
			Message:    "image input encoding failed",
			Details:    map[string]any{"reason": err.Error()},
		}
	}
	return blocks, nil
}

func imageGenerationOpenAITool(req ImageGenerationRequest) map[string]any {
	tool := map[string]any{"type": "image_generation"}
	applyOpenAIImageOptions(tool, req)
	return tool
}

func applyOpenAIImageOptions(payload map[string]any, req ImageGenerationRequest) {
	if req.Size != "" {
		payload["size"] = req.Size
	}
	if req.Quality != "" {
		payload["quality"] = req.Quality
	}
	if req.Background != "" {
		payload["background"] = req.Background
	}
	if req.OutputFormat != "" {
		payload["output_format"] = req.OutputFormat
	}
}

func imageGenerationGeminiParts(req ImageGenerationRequest) ([]map[string]any, error) {
	content := make([]ContentPart, 0, len(req.InputImages)+1)
	content = append(content, ContentPart{Type: "text", Text: req.Prompt})
	content = append(content, req.InputImages...)
	parts, err := geminiUserParts(content)
	if err != nil {
		return nil, GatewayError{
			ErrorClass: ErrorClassConfigInvalid,
			Message:    "image input encoding failed",
			Details:    map[string]any{"reason": err.Error()},
		}
	}
	return parts, nil
}
