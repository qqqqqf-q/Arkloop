package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/genai"
)

type geminiSDKGateway struct {
	cfg       GeminiGatewayConfig
	transport protocolTransport
	protocol  GeminiProtocolConfig
	client    *genai.Client
	configErr error
}

func NewGeminiGatewaySDK(cfg GeminiGatewayConfig) Gateway {
	transport := cfg.Transport
	if strings.TrimSpace(transport.APIKey) == "" {
		transport.APIKey = cfg.APIKey
	}
	if strings.TrimSpace(transport.BaseURL) == "" {
		transport.BaseURL = cfg.BaseURL
	}
	if transport.TotalTimeout <= 0 {
		transport.TotalTimeout = cfg.TotalTimeout
	}
	if !transport.EmitDebugEvents {
		transport.EmitDebugEvents = cfg.Transport.EmitDebugEvents
	}

	protocol := cfg.Protocol
	var configErr error
	if protocol.APIVersion == "" && len(protocol.AdvancedPayloadJSON) == 0 {
		protocol, configErr = parseGeminiProtocolConfig(cfg.AdvancedJSON)
	}
	if inferredVersion := geminiAPIVersionFromBaseURL(transport.BaseURL); inferredVersion != "" {
		protocol.APIVersion = inferredVersion
	}
	if strings.TrimSpace(protocol.APIVersion) == "" {
		protocol.APIVersion = "v1beta"
	}

	normalizedTransport := newProtocolTransport(transport, "https://generativelanguage.googleapis.com", nil)
	cfg.Transport = normalizedTransport.cfg
	cfg.Protocol = protocol
	cfg.TotalTimeout = normalizedTransport.cfg.TotalTimeout
	cfg.BaseURL = normalizedTransport.cfg.BaseURL

	headers := http.Header{}
	for key, value := range normalizedTransport.cfg.DefaultHeaders {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			headers.Set(key, value)
		}
	}
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		Backend:    genai.BackendGeminiAPI,
		APIKey:     strings.TrimSpace(normalizedTransport.cfg.APIKey),
		HTTPClient: sdkHTTPClient(normalizedTransport),
		HTTPOptions: genai.HTTPOptions{
			BaseURL:    sdkBaseURL(normalizedTransport),
			APIVersion: protocol.APIVersion,
			Headers:    headers,
		},
	})
	if err != nil && configErr == nil {
		configErr = err
	}

	return &geminiSDKGateway{cfg: cfg, transport: normalizedTransport, protocol: protocol, client: client, configErr: configErr}
}

func (g *geminiSDKGateway) ProtocolKind() ProtocolKind { return ProtocolKindGeminiGenerateContent }

func (g *geminiSDKGateway) Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error {
	if g.configErr != nil {
		return yield(StreamRunFailed{Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: g.configErr.Error()}})
	}
	if g.transport.baseURLErr != nil {
		return yield(StreamRunFailed{Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: "Gemini base_url blocked", Details: map[string]any{"reason": g.transport.baseURLErr.Error()}}})
	}
	ctx, stopTimeout, _ := withStreamIdleTimeout(ctx, g.transport.cfg.TotalTimeout)
	defer stopTimeout()
	llmCallID := uuid.NewString()

	payload, err := toGeminiPayload(request, g.protocol.AdvancedPayloadJSON)
	if err != nil {
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: "Gemini payload construction failed", Details: map[string]any{"reason": err.Error()}}})
	}
	contents, config, err := geminiSDKRequest(payload)
	if err != nil {
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: "Gemini payload construction failed", Details: map[string]any{"reason": err.Error()}}})
	}

	path := geminiVersionedPath(g.transport.cfg.BaseURL, g.protocol.APIVersion, fmt.Sprintf("/models/%s:streamGenerateContent", request.Model))
	requestEvent, payloadBytes, err := g.requestEvent(request, llmCallID, path, payload)
	if err != nil {
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: "Gemini request serialization failed"}})
	}
	if RequestPayloadTooLarge(payloadBytes) {
		if err := yield(requestEvent); err != nil {
			return err
		}
		return yield(PreflightOversizeFailure(llmCallID, payloadBytes))
	}
	*requestEvent.NetworkAttempted = true
	if err := yield(requestEvent); err != nil {
		return err
	}

	state := newGeminiSDKStreamState(llmCallID, yield)
	for response, err := range g.client.Models.GenerateContentStream(ctx, request.Model, contents, config) {
		if err != nil {
			return state.fail(geminiSDKErrorToGateway(err, payloadBytes))
		}
		if err := state.handle(response); err != nil {
			return err
		}
	}
	return state.complete()
}

func (g *geminiSDKGateway) requestEvent(request Request, llmCallID string, path string, payload map[string]any) (StreamLlmRequest, int, error) {
	debugPayload, redactedHints := sanitizeDebugPayloadJSON(payload)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return StreamLlmRequest{}, 0, err
	}
	baseURL := g.transport.cfg.BaseURL
	stats := ComputeRequestStats(request)
	networkAttempted := false
	return StreamLlmRequest{LlmCallID: llmCallID, ProviderKind: "gemini", APIMode: "generate_content", BaseURL: &baseURL, Path: &path, InputJSON: request.ToJSON(), PayloadJSON: debugPayload, RedactedHints: redactedHints, SystemBytes: stats.SystemBytes, ToolsBytes: stats.ToolsBytes, MessagesBytes: stats.MessagesBytes, AbstractRequestBytes: stats.AbstractRequestBytes, ProviderPayloadBytes: len(encoded), ImagePartCount: stats.ImagePartCount, Base64ImageBytes: stats.Base64ImageBytes, NetworkAttempted: &networkAttempted, RoleBytes: stats.RoleBytes, ToolSchemaBytesMap: stats.ToolSchemaBytesMap, StablePrefixHash: stats.StablePrefixHash, SessionPrefixHash: stats.SessionPrefixHash, VolatileTailHash: stats.VolatileTailHash, ToolSchemaHash: stats.ToolSchemaHash, StablePrefixBytes: stats.StablePrefixBytes, SessionPrefixBytes: stats.SessionPrefixBytes, VolatileTailBytes: stats.VolatileTailBytes, CacheCandidateBytes: stats.CacheCandidateBytes}, len(encoded), nil
}

func geminiSDKRequest(payload map[string]any) ([]*genai.Content, *genai.GenerateContentConfig, error) {
	contents, err := geminiSDKContents(payload["contents"])
	if err != nil {
		return nil, nil, err
	}
	config, err := geminiSDKGenerateContentConfig(payload)
	if err != nil {
		return nil, nil, err
	}
	if system, ok := payload["systemInstruction"].(map[string]any); ok {
		config.SystemInstruction, _ = geminiSDKContent(system)
	}
	return contents, config, nil
}

func geminiSDKGenerateContentConfig(payload map[string]any) (*genai.GenerateContentConfig, error) {
	config := &genai.GenerateContentConfig{}
	extraBody := copyAnyMap(payload)
	for _, key := range []string{"contents", "systemInstruction", "generationConfig", "tools", "toolConfig"} {
		delete(extraBody, key)
	}
	if len(extraBody) > 0 {
		config.HTTPOptions = &genai.HTTPOptions{ExtraBody: extraBody}
	}
	if generationConfig, ok := payload["generationConfig"].(map[string]any); ok {
		if err := genai.InternalMapToStruct(generationConfig, config); err != nil {
			return nil, err
		}
	}
	if rawTools, ok := payload["tools"].([]map[string]any); ok {
		tools, err := geminiSDKTools(rawTools)
		if err != nil {
			return nil, err
		}
		config.Tools = tools
	}
	if rawToolConfig, ok := payload["toolConfig"].(map[string]any); ok {
		toolConfig, err := geminiSDKToolConfig(rawToolConfig)
		if err != nil {
			return nil, err
		}
		config.ToolConfig = toolConfig
	}
	return config, nil
}

func geminiSDKContents(raw any) ([]*genai.Content, error) {
	arr, ok := raw.([]map[string]any)
	if !ok {
		return nil, fmt.Errorf("contents must be array")
	}
	out := make([]*genai.Content, 0, len(arr))
	for _, item := range arr {
		content, err := geminiSDKContent(item)
		if err != nil {
			return nil, err
		}
		out = append(out, content)
	}
	return out, nil
}
func geminiSDKContent(item map[string]any) (*genai.Content, error) {
	role, _ := item["role"].(string)
	rawParts, _ := item["parts"].([]map[string]any)
	parts := make([]*genai.Part, 0, len(rawParts))
	for _, raw := range rawParts {
		part, err := geminiSDKPart(raw)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return &genai.Content{Role: role, Parts: parts}, nil
}
func geminiSDKPart(raw map[string]any) (*genai.Part, error) {
	if text, ok := raw["text"].(string); ok {
		return &genai.Part{Text: text}, nil
	}
	if inlineData, ok := raw["inlineData"].(map[string]any); ok {
		data, _ := inlineData["data"].(string)
		mime, _ := inlineData["mimeType"].(string)
		decoded, err := decodeBase64String(data)
		if err != nil {
			return nil, err
		}
		return &genai.Part{InlineData: &genai.Blob{MIMEType: mime, Data: decoded}}, nil
	}
	if fc, ok := raw["functionCall"].(map[string]any); ok {
		id, _ := fc["id"].(string)
		name, _ := fc["name"].(string)
		args, _ := fc["args"].(map[string]any)
		return &genai.Part{FunctionCall: &genai.FunctionCall{ID: strings.TrimSpace(id), Name: name, Args: mapOrEmpty(args)}}, nil
	}
	if fr, ok := raw["functionResponse"].(map[string]any); ok {
		id, _ := fr["id"].(string)
		name, _ := fr["name"].(string)
		response, _ := fr["response"].(map[string]any)
		return &genai.Part{FunctionResponse: &genai.FunctionResponse{ID: strings.TrimSpace(id), Name: name, Response: mapOrEmpty(response)}}, nil
	}
	return &genai.Part{Text: ""}, nil
}

func geminiSDKTools(rawTools []map[string]any) ([]*genai.Tool, error) {
	tools := make([]*genai.Tool, 0, len(rawTools))
	for _, rawTool := range rawTools {
		rawDecls, _ := rawTool["functionDeclarations"].([]map[string]any)
		if len(rawDecls) == 0 {
			continue
		}
		tool := &genai.Tool{FunctionDeclarations: make([]*genai.FunctionDeclaration, 0, len(rawDecls))}
		for _, rawDecl := range rawDecls {
			decl := &genai.FunctionDeclaration{}
			if name, _ := rawDecl["name"].(string); strings.TrimSpace(name) != "" {
				decl.Name = strings.TrimSpace(name)
			}
			if description, _ := rawDecl["description"].(string); strings.TrimSpace(description) != "" {
				decl.Description = description
			}
			if schema, ok := rawDecl["parametersJsonSchema"].(map[string]any); ok && len(schema) > 0 {
				decl.ParametersJsonSchema = schema
			}
			tool.FunctionDeclarations = append(tool.FunctionDeclarations, decl)
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func geminiSDKToolConfig(raw map[string]any) (*genai.ToolConfig, error) {
	config := &genai.ToolConfig{}
	if err := genai.InternalMapToStruct(raw, config); err != nil {
		return nil, err
	}
	return config, nil
}

func decodeBase64String(data string) ([]byte, error) {
	var decoded []byte
	if err := json.Unmarshal([]byte(`"`+strings.TrimSpace(data)+`"`), &decoded); err == nil {
		return decoded, nil
	}
	return base64StdDecode(data)
}
func base64StdDecode(data string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(data))
}

type geminiSDKStreamState struct {
	llmCallID string
	yield     func(StreamEvent) error
	usage     *Usage
	emitted   bool
	completed bool
}

func newGeminiSDKStreamState(id string, yield func(StreamEvent) error) *geminiSDKStreamState {
	return &geminiSDKStreamState{llmCallID: id, yield: yield}
}
func (s *geminiSDKStreamState) handle(response *genai.GenerateContentResponse) error {
	if response == nil {
		return nil
	}
	s.usage = geminiSDKUsage(response.UsageMetadata)
	for _, candidate := range response.Candidates {
		if failure := geminiFinishReasonFailure(string(candidate.FinishReason)); failure != nil {
			return s.fail(*failure)
		}
		if candidate.Content == nil {
			continue
		}
		for _, part := range candidate.Content.Parts {
			if part == nil {
				continue
			}
			if part.Text != "" {
				if part.Thought {
					ch := "thinking"
					if err := s.yield(StreamMessageDelta{ContentDelta: part.Text, Role: "assistant", Channel: &ch}); err != nil {
						return err
					}
				} else {
					s.emitted = true
					if err := s.yield(StreamMessageDelta{ContentDelta: part.Text, Role: "assistant"}); err != nil {
						return err
					}
				}
			}
			if part.FunctionCall != nil {
				s.emitted = true
				if err := s.yield(ToolCall{ToolCallID: strings.TrimSpace(part.FunctionCall.ID), ToolName: CanonicalToolName(part.FunctionCall.Name), ArgumentsJSON: mapOrEmpty(part.FunctionCall.Args)}); err != nil {
					return err
				}
			}
		}
	}
	s.completed = true
	return nil
}
func (s *geminiSDKStreamState) fail(g GatewayError) error {
	return s.yield(StreamRunFailed{LlmCallID: s.llmCallID, Error: g, Usage: s.usage})
}
func (s *geminiSDKStreamState) complete() error {
	if s.completed || s.emitted || s.usage != nil {
		return s.yield(StreamRunCompleted{LlmCallID: s.llmCallID, Usage: s.usage})
	}
	return s.yield(StreamRunFailed{LlmCallID: s.llmCallID, Error: RetryableStreamEndedError()})
}
func geminiSDKUsage(meta *genai.GenerateContentResponseUsageMetadata) *Usage {
	if meta == nil {
		return nil
	}
	return parseGeminiUsage(&geminiUsageMetadata{PromptTokenCount: int(meta.PromptTokenCount), CandidatesTokenCount: int(meta.CandidatesTokenCount), TotalTokenCount: int(meta.TotalTokenCount), CachedContentTokenCount: int(meta.CachedContentTokenCount)})
}
func geminiSDKErrorToGateway(err error, payloadBytes int) GatewayError {
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		details := map[string]any{"status_code": apiErr.Code}
		if apiErr.Status != "" {
			details["gemini_error_status"] = apiErr.Status
		}
		if apiErr.Code == http.StatusRequestEntityTooLarge {
			details["network_attempted"] = true
			details = OversizeFailureDetails(payloadBytes, OversizePhaseProvider, details)
		}
		message := strings.TrimSpace(apiErr.Message)
		if message == "" {
			message = "Gemini request failed"
		}
		return GatewayError{ErrorClass: classifyHTTPStatus(apiErr.Code), Message: message, Details: details}
	}
	return GatewayError{ErrorClass: ErrorClassProviderRetryable, Message: "Gemini network error", Details: map[string]any{"reason": err.Error()}}
}

func (g *geminiSDKGateway) GenerateImage(ctx context.Context, model string, req ImageGenerationRequest) (GeneratedImage, error) {
	if g == nil {
		return GeneratedImage{}, GatewayError{ErrorClass: ErrorClassConfigMissing, Message: "gemini gateway is not initialized"}
	}
	if g.configErr != nil {
		return GeneratedImage{}, GatewayError{ErrorClass: ErrorClassConfigInvalid, Message: g.configErr.Error()}
	}
	if g.transport.baseURLErr != nil {
		return GeneratedImage{}, GatewayError{ErrorClass: ErrorClassConfigInvalid, Message: "Gemini base_url blocked", Details: map[string]any{"reason": g.transport.baseURLErr.Error()}}
	}
	parts, err := imageGenerationGeminiParts(req)
	if err != nil {
		return GeneratedImage{}, err
	}
	content, err := geminiSDKContent(map[string]any{"role": "user", "parts": parts})
	if err != nil {
		return GeneratedImage{}, GatewayError{ErrorClass: ErrorClassConfigInvalid, Message: "image input encoding failed", Details: map[string]any{"reason": err.Error()}}
	}
	config := &genai.GenerateContentConfig{ResponseModalities: []string{"IMAGE"}, HTTPOptions: &genai.HTTPOptions{ExtraBody: copyAnyMap(g.protocol.AdvancedPayloadJSON)}}
	if generationConfig, ok := config.HTTPOptions.ExtraBody["generationConfig"].(map[string]any); ok {
		for key, value := range generationConfig {
			if key == "responseModalities" {
				continue
			}
			config.HTTPOptions.ExtraBody[key] = value
		}
		delete(config.HTTPOptions.ExtraBody, "generationConfig")
	}
	response, err := g.client.Models.GenerateContent(ctx, strings.TrimSpace(model), []*genai.Content{content}, config)
	if err != nil {
		return GeneratedImage{}, geminiSDKErrorToGateway(err, 0)
	}
	for _, candidate := range response.Candidates {
		if candidate.Content == nil {
			continue
		}
		for _, part := range candidate.Content.Parts {
			if part != nil && part.InlineData != nil && len(part.InlineData.Data) > 0 {
				mimeType := strings.TrimSpace(part.InlineData.MIMEType)
				if mimeType == "" {
					mimeType = detectGeneratedImageMime(part.InlineData.Data)
				}
				if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
					return GeneratedImage{}, GatewayError{ErrorClass: ErrorClassProviderNonRetryable, Message: "Gemini returned non-image content for image generation", Details: map[string]any{"mime_type": mimeType}}
				}
				return GeneratedImage{Bytes: part.InlineData.Data, MimeType: mimeType, ProviderKind: "gemini", Model: model}, nil
			}
		}
	}
	return GeneratedImage{}, GatewayError{ErrorClass: ErrorClassProviderNonRetryable, Message: "Gemini image response contained no generated image"}
}
