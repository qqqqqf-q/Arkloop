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

	sharedoutbound "arkloop/services/shared/outboundurl"

	"github.com/google/uuid"
)

const (
	defaultGeminiBaseURL          = "https://generativelanguage.googleapis.com/v1beta"
	defaultGeminiThinkingBudget   = 8192
	geminiMaxErrorBodyBytes        = 4096
	geminiMaxDebugChunkBytes       = 8192
)

var geminiAdvancedJSONDenylist = map[string]struct{}{
	"contents":          {},
	"systemInstruction": {},
	"tools":             {},
	"toolConfig":        {},
}

type GeminiGatewayConfig struct {
	APIKey          string
	BaseURL         string
	AdvancedJSON    map[string]any
	EmitDebugEvents bool
	TotalTimeout    time.Duration
}

type GeminiGateway struct {
	cfg        GeminiGatewayConfig
	client     *http.Client
	baseURLErr error
}

func NewGeminiGateway(cfg GeminiGatewayConfig) *GeminiGateway {
	timeout := cfg.TotalTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultGeminiBaseURL
	}
	normalizedBaseURL, baseURLErr := sharedoutbound.DefaultPolicy().NormalizeBaseURL(baseURL)
	if baseURLErr == nil {
		baseURL = normalizedBaseURL
	}
	cfg.BaseURL = baseURL
	cfg.TotalTimeout = timeout
	if cfg.AdvancedJSON == nil {
		cfg.AdvancedJSON = map[string]any{}
	}
	return &GeminiGateway{
		cfg:        cfg,
		client:     sharedoutbound.DefaultPolicy().NewHTTPClient(timeout),
		baseURLErr: baseURLErr,
	}
}

func (g *GeminiGateway) Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error {
	if g.baseURLErr != nil {
		return yield(StreamRunFailed{Error: GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "Gemini base_url blocked",
			Details:    map[string]any{"reason": g.baseURLErr.Error()},
		}})
	}
	ctx, cancel := context.WithTimeout(ctx, g.cfg.TotalTimeout)
	defer cancel()
	llmCallID := uuid.NewString()

	for k := range g.cfg.AdvancedJSON {
		if _, denied := geminiAdvancedJSONDenylist[k]; denied {
			return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    fmt.Sprintf("advanced_json must not set critical field: %s", k),
				Details:    map[string]any{"denied_key": k},
			}})
		}
	}

	payload, err := toGeminiPayload(request, g.cfg.AdvancedJSON)
	if err != nil {
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "Gemini payload construction failed",
			Details:    map[string]any{"reason": err.Error()},
		}})
	}

	baseURL := g.cfg.BaseURL
	path := fmt.Sprintf("/models/%s:streamGenerateContent", request.Model)
	stats := ComputeRequestStats(request)
	if err := yield(StreamLlmRequest{
		LlmCallID:          llmCallID,
		ProviderKind:       "gemini",
		APIMode:            "generate_content",
		BaseURL:            &baseURL,
		Path:               &path,
		PayloadJSON:        payload,
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

	endpoint := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", g.cfg.BaseURL, request.Model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    "Gemini request construction failed",
			Details:    map[string]any{"reason": err.Error()},
		}})
	}
	req.Header.Set("x-goog-api-key", strings.TrimSpace(g.cfg.APIKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := g.client.Do(req)
	if err != nil {
		var denied sharedoutbound.DeniedError
		if errors.As(err, &denied) {
			return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "Gemini base_url blocked",
				Details:    map[string]any{"reason": denied.Error()},
			}})
		}
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{
			ErrorClass: ErrorClassProviderRetryable,
			Message:    "Gemini network error",
		}})
	}
	defer resp.Body.Close()

	status := resp.StatusCode
	if status < 200 || status >= 300 {
		body, bodyTruncated, _ := readAllWithLimit(resp.Body, geminiMaxErrorBodyBytes)
		if g.cfg.EmitDebugEvents {
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

	var parseErr error
	sseErr := forEachSSEData(ctx, body, func(data string) error {
		data = strings.TrimSpace(data)
		if data == "" {
			return nil
		}

		if g.cfg.EmitDebugEvents {
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
			parseErr = fmt.Errorf("Gemini SSE JSON parse failed: %w", err)
			return parseErr
		}

		if chunk.UsageMetadata != nil {
			lastUsage = chunk.UsageMetadata
		}

		if len(chunk.Candidates) == 0 {
			return nil
		}
		candidate := chunk.Candidates[0]

		for _, part := range candidate.Content.Parts {
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
				if err := yield(ToolCall{
					ToolCallID:    uuid.NewString(),
					ToolName:      part.FunctionCall.Name,
					ArgumentsJSON: args,
				}); err != nil {
					return err
				}
			}
		}

		switch candidate.FinishReason {
		case "STOP", "MAX_TOKENS", "":
			// 继续或正常结束
		case "SAFETY", "RECITATION":
			terminalEmitted = true
			return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{
				ErrorClass: ErrorClassPolicyDenied,
				Message:    fmt.Sprintf("Gemini content blocked: %s", candidate.FinishReason),
				Details:    map[string]any{"finish_reason": candidate.FinishReason},
			}})
		default:
			terminalEmitted = true
			return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{
				ErrorClass: ErrorClassProviderRetryable,
				Message:    fmt.Sprintf("Gemini unexpected finish: %s", candidate.FinishReason),
				Details:    map[string]any{"finish_reason": candidate.FinishReason},
			}})
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

	return yield(StreamRunCompleted{LlmCallID: llmCallID, Usage: parseGeminiUsage(lastUsage)})
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

	// thinkingConfig
	if _, hasThinking := genConfig["thinkingConfig"]; !hasThinking {
		switch request.ReasoningMode {
		case "enabled":
			genConfig["thinkingConfig"] = map[string]any{"thinkingBudget": defaultGeminiThinkingBudget}
		case "disabled":
			genConfig["thinkingConfig"] = map[string]any{"thinkingBudget": 0}
		}
	}

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
	PromptTokenCount      int `json:"promptTokenCount"`
	CandidatesTokenCount  int `json:"candidatesTokenCount"`
	TotalTokenCount       int `json:"totalTokenCount"`
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
