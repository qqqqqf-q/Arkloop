package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"arkloop/services/worker/internal/stablejson"
	"github.com/google/uuid"
)

const (
	defaultGeminiBaseURL        = "https://generativelanguage.googleapis.com"
	defaultGeminiThinkingBudget = 8192
	geminiMaxErrorBodyBytes     = 4096
	geminiMaxDebugChunkBytes    = 8192
)

var geminiAdvancedJSONDenylist = map[string]struct{}{
	"contents":          {},
	"systemInstruction": {},
	"tools":             {},
	"toolConfig":        {},
}

type GeminiGatewayConfig struct {
	Transport    TransportConfig
	Protocol     GeminiProtocolConfig
	APIKey       string
	BaseURL      string
	AdvancedJSON map[string]any
	TotalTimeout time.Duration
}

type GeminiGateway struct {
	cfg       GeminiGatewayConfig
	transport protocolTransport
	protocol  GeminiProtocolConfig
	configErr error
}

func NewGeminiGateway(cfg GeminiGatewayConfig) *GeminiGateway {
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

	normalizedTransport := newProtocolTransport(transport, defaultGeminiBaseURL, normalizeGeminiBaseURL)
	cfg.Transport = normalizedTransport.cfg
	cfg.Protocol = protocol
	cfg.TotalTimeout = normalizedTransport.cfg.TotalTimeout
	cfg.BaseURL = normalizedTransport.cfg.BaseURL

	return &GeminiGateway{
		cfg:       cfg,
		transport: normalizedTransport,
		protocol:  protocol,
		configErr: configErr,
	}
}

func (g *GeminiGateway) ProtocolKind() ProtocolKind {
	return ProtocolKindGeminiGenerateContent
}

func (g *GeminiGateway) Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error {
	if g.configErr != nil {
		ge := GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    g.configErr.Error(),
		}
		var cfgErr protocolConfigError
		if errors.As(g.configErr, &cfgErr) && len(cfgErr.Details) > 0 {
			ge.Details = cfgErr.Details
		}
		return yield(StreamRunFailed{Error: ge})
	}
	if g.transport.baseURLErr != nil {
		return yield(StreamRunFailed{Error: GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "Gemini base_url blocked",
			Details:    map[string]any{"reason": g.transport.baseURLErr.Error()},
		}})
	}
	ctx, cancel := context.WithTimeout(ctx, g.transport.cfg.TotalTimeout)
	defer cancel()
	llmCallID := uuid.NewString()

	payload, err := toGeminiPayload(request, g.protocol.AdvancedPayloadJSON)
	if err != nil {
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "Gemini payload construction failed",
			Details:    map[string]any{"reason": err.Error()},
		}})
	}

	baseURL := g.transport.cfg.BaseURL
	path := geminiVersionedPath(g.transport.cfg.BaseURL, g.protocol.APIVersion, fmt.Sprintf("/models/%s:streamGenerateContent", request.Model))
	stats := ComputeRequestStats(request)
	debugPayload, redactedHints := sanitizeDebugPayloadJSON(payload)
	if err := yield(StreamLlmRequest{
		LlmCallID:          llmCallID,
		ProviderKind:       "gemini",
		APIMode:            "generate_content",
		BaseURL:            &baseURL,
		Path:               &path,
		PayloadJSON:        debugPayload,
		RedactedHints:      redactedHints,
		SystemBytes:        stats.SystemBytes,
		ToolsBytes:         stats.ToolsBytes,
		MessagesBytes:      stats.MessagesBytes,
		RoleBytes:          stats.RoleBytes,
		ToolSchemaBytesMap: stats.ToolSchemaBytesMap,
		StablePrefixHash:   stats.StablePrefixHash,
	}); err != nil {
		return err
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "Gemini request serialization failed",
		}})
	}

	resourcePath := geminiVersionedPath(g.transport.cfg.BaseURL, g.protocol.APIVersion, fmt.Sprintf("/models/%s:streamGenerateContent?alt=sse", request.Model))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.transport.endpoint(resourcePath), bytes.NewReader(encoded))
	if err != nil {
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "Gemini request construction failed",
			Details:    map[string]any{"reason": err.Error()},
		}})
	}
	req.Header.Set("x-goog-api-key", strings.TrimSpace(g.transport.cfg.APIKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	g.transport.applyDefaultHeaders(req)

	resp, err := g.transport.client.Do(req)
	if err != nil {
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{
			ErrorClass: ErrorClassProviderRetryable,
			Message:    "Gemini network error",
		}})
	}
	defer resp.Body.Close()

	status := resp.StatusCode
	if status < 200 || status >= 300 {
		body, bodyTruncated, _ := readAllWithLimit(resp.Body, geminiMaxErrorBodyBytes)
		if g.transport.cfg.EmitDebugEvents {
			raw, rawTruncated := truncateUTF8(string(body), geminiMaxDebugChunkBytes)
			_ = yield(StreamLlmResponseChunk{
				LlmCallID:    llmCallID,
				ProviderKind: "gemini",
				APIMode:      "generate_content",
				Raw:          raw,
				StatusCode:   &status,
				Truncated:    bodyTruncated || rawTruncated,
			})
		}
		message, details := geminiErrorMessageAndDetails(body, status)
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{
			ErrorClass: errorClassFromStatus(status),
			Message:    message,
			Details:    details,
		}})
	}

	return g.streamGeminiSSE(ctx, resp.Body, llmCallID, yield)
}

func (g *GeminiGateway) streamGeminiSSE(ctx context.Context, body interface{ Read([]byte) (int, error) }, llmCallID string, yield func(StreamEvent) error) error {
	var lastUsage *geminiUsageMetadata
	terminalEmitted := false
	toolCalls := map[int]*geminiStreamingToolCall{}

	var parseErr error
	sseErr := forEachSSEData(ctx, body, func(data string) error {
		data = strings.TrimSpace(data)
		if data == "" {
			return nil
		}

		if g.transport.cfg.EmitDebugEvents {
			raw, _ := truncateUTF8(data, geminiMaxDebugChunkBytes)
			_ = yield(StreamLlmResponseChunk{
				LlmCallID:    llmCallID,
				ProviderKind: "gemini",
				APIMode:      "generate_content",
				Raw:          raw,
			})
		}

		var chunk geminiGenerateContentResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			parseErr = fmt.Errorf("gemini SSE JSON parse failed: %w", err)
			return parseErr
		}

		if chunk.UsageMetadata != nil {
			lastUsage = chunk.UsageMetadata
		}

		if len(chunk.Candidates) == 0 {
			return nil
		}
		candidate := chunk.Candidates[0]

		for idx, part := range candidate.Content.Parts {
			if part.Thought {
				ch := "thinking"
				if err := yield(StreamMessageDelta{
					ContentDelta: part.Text,
					Role:         "assistant",
					Channel:      &ch,
				}); err != nil {
					return err
				}
				continue
			}
			if part.Text != "" {
				if err := yield(StreamMessageDelta{
					ContentDelta: part.Text,
					Role:         "assistant",
				}); err != nil {
					return err
				}
				continue
			}
			if part.FunctionCall != nil {
				args := mapOrEmpty(part.FunctionCall.Args)
				encodedArgs, err := stablejson.Encode(args)
				if err != nil {
					return err
				}
				call := toolCalls[idx]
				if call == nil {
					call = &geminiStreamingToolCall{
						ToolCallID: fmt.Sprintf("%s:%d", llmCallID, idx),
					}
					toolCalls[idx] = call
				}
				call.ToolName = part.FunctionCall.Name
				call.ArgumentsJSON = args
				if call.EncodedArgs == "" {
					call.EncodedArgs = encodedArgs
					if err := yield(ToolCallArgumentDelta{
						ToolCallIndex:  idx,
						ToolCallID:     call.ToolCallID,
						ToolName:       call.ToolName,
						ArgumentsDelta: encodedArgs,
					}); err != nil {
						return err
					}
					continue
				}
				if encodedArgs == call.EncodedArgs {
					continue
				}
				if strings.HasPrefix(encodedArgs, call.EncodedArgs) {
					if err := yield(ToolCallArgumentDelta{
						ToolCallIndex:  idx,
						ToolCallID:     call.ToolCallID,
						ToolName:       call.ToolName,
						ArgumentsDelta: encodedArgs[len(call.EncodedArgs):],
					}); err != nil {
						return err
					}
				}
				call.EncodedArgs = encodedArgs
			}
		}

		if failure := geminiFinishReasonFailure(candidate.FinishReason); failure != nil {
			terminalEmitted = true
			return yield(StreamRunFailed{LlmCallID: llmCallID, Error: *failure})
		}
		if candidate.FinishReason != "" {
			for idx := 0; idx < len(toolCalls); idx++ {
				call := toolCalls[idx]
				if call == nil {
					continue
				}
				if err := yield(ToolCall{
					ToolCallID:    call.ToolCallID,
					ToolName:      call.ToolName,
					ArgumentsJSON: call.ArgumentsJSON,
				}); err != nil {
					return err
				}
				delete(toolCalls, idx)
			}
		}

		return nil
	})

	if terminalEmitted {
		return sseErr
	}

	if sseErr != nil {
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{
			ErrorClass: ErrorClassProviderRetryable,
			Message:    "Gemini stream read error",
			Details:    map[string]any{"reason": sseErr.Error()},
		}})
	}

	if parseErr != nil {
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{
			ErrorClass: ErrorClassProviderNonRetryable,
			Message:    parseErr.Error(),
		}})
	}
	if len(toolCalls) > 0 {
		for idx := 0; idx < len(toolCalls); idx++ {
			call := toolCalls[idx]
			if call == nil {
				continue
			}
			if err := yield(ToolCall{
				ToolCallID:    call.ToolCallID,
				ToolName:      call.ToolName,
				ArgumentsJSON: call.ArgumentsJSON,
			}); err != nil {
				return err
			}
			delete(toolCalls, idx)
		}
	}

	return yield(StreamRunCompleted{LlmCallID: llmCallID, Usage: parseGeminiUsage(lastUsage)})
}

type geminiStreamingToolCall struct {
	ToolCallID    string
	ToolName      string
	EncodedArgs   string
	ArgumentsJSON map[string]any
}

func normalizeGeminiThinkingConfig(genConfig map[string]any, reasoningMode string) {
	rawThinkingConfig, hasThinkingConfig := genConfig["thinkingConfig"].(map[string]any)
	thinkingConfig := map[string]any{}
	if hasThinkingConfig {
		for key, value := range rawThinkingConfig {
			thinkingConfig[key] = value
		}
	}

	switch reasoningMode {
	case "enabled":
		if anyToInt(thinkingConfig["thinkingBudget"]) <= 0 {
			thinkingConfig["thinkingBudget"] = defaultGeminiThinkingBudget
		}
		thinkingConfig["includeThoughts"] = true
		genConfig["thinkingConfig"] = thinkingConfig
	case "disabled":
		thinkingConfig["thinkingBudget"] = 0
		thinkingConfig["includeThoughts"] = false
		genConfig["thinkingConfig"] = thinkingConfig
	default:
		if !hasThinkingConfig {
			return
		}
		if _, has := thinkingConfig["includeThoughts"]; !has {
			thinkingConfig["includeThoughts"] = anyToInt(thinkingConfig["thinkingBudget"]) > 0
		}
		genConfig["thinkingConfig"] = thinkingConfig
	}
}

func geminiFinishReasonFailure(finishReason string) *GatewayError {
	reason := strings.TrimSpace(finishReason)
	switch reason {
	case "", "STOP", "MAX_TOKENS":
		return nil
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "LANGUAGE", "IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "IMAGE_RECITATION":
		return &GatewayError{
			ErrorClass: ErrorClassPolicyDenied,
			Message:    fmt.Sprintf("Gemini content blocked: %s", reason),
			Details:    map[string]any{"finish_reason": reason},
		}
	case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL", "TOO_MANY_TOOL_CALLS", "MALFORMED_RESPONSE", "MISSING_THOUGHT_SIGNATURE":
		return &GatewayError{
			ErrorClass: ErrorClassProviderNonRetryable,
			Message:    fmt.Sprintf("Gemini invalid response: %s", reason),
			Details:    map[string]any{"finish_reason": reason},
		}
	default:
		return &GatewayError{
			ErrorClass: ErrorClassProviderRetryable,
			Message:    fmt.Sprintf("Gemini unexpected finish: %s", reason),
			Details:    map[string]any{"finish_reason": reason},
		}
	}
}

// toGeminiPayload 构建完整请求体，合并 advancedJSON。
func toGeminiPayload(request Request, advancedJSON map[string]any) (map[string]any, error) {
	systemInstruction, contents, err := toGeminiContents(request.Messages)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"contents": contents,
	}
	if systemInstruction != nil {
		payload["systemInstruction"] = systemInstruction
	}
	if len(request.Tools) > 0 {
		payload["tools"] = toGeminiTools(request.Tools)
	}

	// generationConfig 合并：advancedJSON 优先
	genConfig := map[string]any{}
	if existing, ok := advancedJSON["generationConfig"].(map[string]any); ok {
		for k, v := range existing {
			genConfig[k] = v
		}
	}
	if request.Temperature != nil {
		if _, has := genConfig["temperature"]; !has {
			genConfig["temperature"] = *request.Temperature
		}
	}
	if request.MaxOutputTokens != nil && *request.MaxOutputTokens > 0 {
		if _, has := genConfig["maxOutputTokens"]; !has {
			genConfig["maxOutputTokens"] = *request.MaxOutputTokens
		}
	}

	normalizeGeminiThinkingConfig(genConfig, request.ReasoningMode)

	if len(genConfig) > 0 {
		payload["generationConfig"] = genConfig
	}

	// 合并 advancedJSON 中不在 denylist 且不是 generationConfig 的字段
	for k, v := range advancedJSON {
		if _, denied := geminiAdvancedJSONDenylist[k]; denied {
			continue
		}
		if k == "generationConfig" {
			continue // 已处理
		}
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}

	return payload, nil
}

// toGeminiContents 将消息列表转换为 Gemini contents 格式。
// system 消息提取为 systemInstruction，连续 tool 响应合并到同一 user content。
func toGeminiContents(messages []Message) (systemInstruction map[string]any, contents []map[string]any, err error) {
	contents = []map[string]any{}
	var pendingToolParts []map[string]any

	flushToolParts := func() {
		if len(pendingToolParts) == 0 {
			return
		}
		contents = append(contents, map[string]any{
			"role":  "user",
			"parts": pendingToolParts,
		})
		pendingToolParts = nil
	}

	for _, msg := range messages {
		text := joinParts(msg.Content)

		switch msg.Role {
		case "system":
			if strings.TrimSpace(text) != "" {
				systemInstruction = map[string]any{
					"parts": []map[string]any{{"text": text}},
				}
			}
			continue

		case "tool":
			part, parseErr := geminiToolResponsePart(text)
			if parseErr != nil {
				err = parseErr
				return
			}
			pendingToolParts = append(pendingToolParts, part)
			continue

		default:
			flushToolParts()
		}

		switch msg.Role {
		case "assistant":
			parts := []map[string]any{}
			if strings.TrimSpace(text) != "" {
				parts = append(parts, map[string]any{"text": text})
			}
			for _, call := range msg.ToolCalls {
				parts = append(parts, map[string]any{
					"functionCall": map[string]any{
						"name": call.ToolName,
						"args": mapOrEmpty(call.ArgumentsJSON),
					},
				})
			}
			if len(parts) == 0 {
				parts = []map[string]any{{"text": ""}}
			}
			contents = append(contents, map[string]any{
				"role":  "model",
				"parts": parts,
			})

		case "user":
			parts, buildErr := geminiUserParts(msg.Content)
			if buildErr != nil {
				err = buildErr
				return
			}
			contents = append(contents, map[string]any{
				"role":  "user",
				"parts": parts,
			})
		}
	}

	flushToolParts()
	return
}

func geminiUserParts(content []ContentPart) ([]map[string]any, error) {
	parts := make([]map[string]any, 0, len(content))
	for _, p := range content {
		switch p.Kind() {
		case "text":
			parts = append(parts, map[string]any{"text": p.Text})
		case "file":
			t := PartPromptText(p)
			if strings.TrimSpace(t) != "" {
				parts = append(parts, map[string]any{"text": t})
			}
		case "image":
			if p.Attachment == nil || len(p.Data) == 0 {
				return nil, fmt.Errorf("image part missing data")
			}
			mimeType := strings.TrimSpace(p.Attachment.MimeType)
			if mimeType == "" {
				mimeType = "application/octet-stream"
			}
			parts = append(parts, map[string]any{
				"inlineData": map[string]any{
					"mimeType": mimeType,
					"data":     base64.StdEncoding.EncodeToString(p.Data),
				},
			})
		}
	}
	if len(parts) == 0 {
		parts = []map[string]any{{"text": ""}}
	}
	return parts, nil
}

func geminiToolResponsePart(text string) (map[string]any, error) {
	var envelope map[string]any
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		return nil, fmt.Errorf("tool message is not valid JSON")
	}
	toolName, _ := envelope["tool_name"].(string)
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		// 降级：从 tool_call_id 中能读到名字的情况
		toolName = "unknown"
	}

	var response any
	if errObj, ok := envelope["error"]; ok && errObj != nil {
		response = map[string]any{"error": errObj}
	} else {
		response = envelope["result"]
	}
	if response == nil {
		response = map[string]any{}
	}

	return map[string]any{
		"functionResponse": map[string]any{
			"name":     toolName,
			"response": response,
		},
	}, nil
}

func toGeminiTools(specs []ToolSpec) []map[string]any {
	decls := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		decl := map[string]any{
			"name":       spec.Name,
			"parameters": mapOrEmpty(spec.JSONSchema),
		}
		if spec.Description != nil {
			decl["description"] = *spec.Description
		}
		decls = append(decls, decl)
	}
	return []map[string]any{
		{"functionDeclarations": decls},
	}
}

func geminiErrorMessageAndDetails(body []byte, status int) (string, map[string]any) {
	details := map[string]any{"status_code": status}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return "Gemini request failed", details
	}
	errObj, ok := root["error"].(map[string]any)
	if !ok {
		return "Gemini request failed", details
	}
	if code, ok := errObj["code"].(float64); ok {
		details["gemini_error_code"] = int(code)
	}
	if status, ok := errObj["status"].(string); ok && strings.TrimSpace(status) != "" {
		details["gemini_error_status"] = strings.TrimSpace(status)
	}
	if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg), details
	}
	return "Gemini request failed", details
}

type geminiGenerateContentResponse struct {
	Candidates    []geminiCandidate    `json:"candidates"`
	UsageMetadata *geminiUsageMetadata `json:"usageMetadata"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role"`
}

type geminiPart struct {
	Text         string              `json:"text"`
	Thought      bool                `json:"thought"`
	FunctionCall *geminiFunctionCall `json:"functionCall"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type geminiUsageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount"`
}

func parseGeminiUsage(meta *geminiUsageMetadata) *Usage {
	if meta == nil {
		return nil
	}
	u := &Usage{}
	if meta.PromptTokenCount > 0 {
		v := meta.PromptTokenCount
		u.InputTokens = &v
	}
	if meta.CandidatesTokenCount > 0 {
		v := meta.CandidatesTokenCount
		u.OutputTokens = &v
	}
	if meta.TotalTokenCount > 0 {
		v := meta.TotalTokenCount
		u.TotalTokens = &v
	}
	if meta.CachedContentTokenCount > 0 {
		v := meta.CachedContentTokenCount
		u.CachedTokens = &v
	}
	return u
}
