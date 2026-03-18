package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"

	"arkloop/services/worker/internal/stablejson"
	"github.com/google/uuid"
)

const defaultAnthropicVersion = "2023-06-01"
const defaultAnthropicMaxResponseBytes = 16 * 1024
const anthropicMaxDebugChunkBytes = 8192

var errAnthropicToolUseInput = errors.New("anthropic_tool_use_input")

// critical fields denied in advanced_json; always reject regardless of existing payload keys
var anthropicAdvancedJSONDenylist = map[string]struct{}{
	"model":       {},
	"messages":    {},
	"max_tokens":  {},
	"stream":      {},
	"tools":       {},
	"tool_choice": {},
	"system":      {},
}

const (
	anthropicAdvancedKeyVersion      = "anthropic_version"
	anthropicAdvancedKeyExtraHeaders = "extra_headers"
	anthropicBetaHeader              = "anthropic-beta"
	defaultAnthropicThinkingBudget   = 8192
)

type anthropicAdvancedJSONError struct {
	Message string
	Details map[string]any
}

func (e anthropicAdvancedJSONError) Error() string { return e.Message }

type anthropicAdvancedConfig struct {
	Version      *string
	ExtraHeaders map[string]string
	Payload      map[string]any
}

type AnthropicGatewayConfig struct {
	APIKey           string
	BaseURL          string
	AnthropicVersion string
	EmitDebugEvents  bool
	TotalTimeout     time.Duration
	MaxResponseBytes int
	AdvancedJSON     map[string]any
}

type AnthropicGateway struct {
	cfg        AnthropicGatewayConfig
	client     *http.Client
	baseURLErr error
}

func NewAnthropicGateway(cfg AnthropicGatewayConfig) *AnthropicGateway {
	timeout := cfg.TotalTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	normalizedBaseURL, baseURLErr := sharedoutbound.DefaultPolicy().NormalizeBaseURL(baseURL)
	if baseURLErr == nil {
		baseURL = normalizedBaseURL
	}
	cfg.BaseURL = baseURL
	if strings.TrimSpace(cfg.AnthropicVersion) == "" {
		cfg.AnthropicVersion = defaultAnthropicVersion
	}
	cfg.TotalTimeout = timeout
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = defaultAnthropicMaxResponseBytes
	}
	if cfg.AdvancedJSON == nil {
		cfg.AdvancedJSON = map[string]any{}
	}
	return &AnthropicGateway{
		cfg:        cfg,
		client:     sharedoutbound.DefaultPolicy().NewHTTPClient(timeout),
		baseURLErr: baseURLErr,
	}
}

func (g *AnthropicGateway) Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error {
	if g.baseURLErr != nil {
		return yield(StreamRunFailed{Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: "Anthropic base_url blocked", Details: map[string]any{"reason": g.baseURLErr.Error()}}})
	}
	ctx, cancel := context.WithTimeout(ctx, g.cfg.TotalTimeout)
	defer cancel()
	llmCallID := uuid.NewString()

	system, messages, err := toAnthropicMessages(request.Messages)
	if err != nil {
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "Anthropic messages construction failed",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}
	maxTokens := 1024
	if request.MaxOutputTokens != nil && *request.MaxOutputTokens > 0 {
		maxTokens = *request.MaxOutputTokens
	}

	payload := map[string]any{
		"model":      request.Model,
		"messages":   messages,
		"max_tokens": maxTokens,
	}
	if len(system) > 0 {
		payload["system"] = system
	}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	if len(request.Tools) > 0 {
		payload["tools"] = toAnthropicTools(request.Tools)
	}
	payload["stream"] = true

	advancedCfg, err := parseAnthropicAdvancedJSON(g.cfg.AdvancedJSON)
	if err != nil {
		ge := GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    err.Error(),
		}
		if typed, ok := err.(anthropicAdvancedJSONError); ok && len(typed.Details) > 0 {
			ge.Details = typed.Details
		}
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: ge})
	}

	// merge keys not already present in payload
	for k, v := range advancedCfg.Payload {
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}
	// reasoning_mode 控制是否发送 thinking 参数
	switch request.ReasoningMode {
	case "enabled":
		if tObj, ok := payload["thinking"].(map[string]any); ok {
			tObj["type"] = "enabled"
			if _, has := tObj["budget_tokens"]; !has {
				tObj["budget_tokens"] = defaultAnthropicThinkingBudget
			}
		} else {
			payload["thinking"] = map[string]any{
				"type":          "enabled",
				"budget_tokens": defaultAnthropicThinkingBudget,
			}
		}
		ensureAnthropicMaxTokensForThinking(payload)
	case "disabled":
		delete(payload, "thinking")
	default: // "auto", "none", ""
		if tObj, ok := payload["thinking"].(map[string]any); ok {
			if _, has := tObj["budget_tokens"]; !has {
				tObj["budget_tokens"] = defaultAnthropicThinkingBudget
			}
			ensureAnthropicMaxTokensForThinking(payload)
		}
	}
	anthropicVersion := g.cfg.AnthropicVersion
	if advancedCfg.Version != nil {
		anthropicVersion = *advancedCfg.Version
	}

	baseURL := g.cfg.BaseURL
	path := "/messages"
	stats := ComputeRequestStats(request)
	if err := yield(StreamLlmRequest{
		LlmCallID:          llmCallID,
		ProviderKind:       "anthropic",
		APIMode:            "messages",
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
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "Anthropic request serialization failed",
			},
		})
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.BaseURL+"/messages", bytes.NewReader(encoded))
	if err != nil {
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "Anthropic request construction failed",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}
	req.Header.Set("x-api-key", strings.TrimSpace(g.cfg.APIKey))
	req.Header.Set("anthropic-version", anthropicVersion)
	for k, v := range advancedCfg.ExtraHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		var denied sharedoutbound.DeniedError
		if errors.As(err, &denied) {
			return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: "Anthropic base_url blocked", Details: map[string]any{"reason": denied.Error()}}})
		}
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassProviderRetryable,
				Message:    "Anthropic network error",
			},
		})
	}
	defer resp.Body.Close()

	status := resp.StatusCode

	if status < 200 || status >= 300 {
		body, bodyTruncated, _ := readAllWithLimit(resp.Body, g.cfg.MaxResponseBytes)
		if g.cfg.EmitDebugEvents {
			raw, rawTruncated := truncateUTF8(string(body), anthropicMaxDebugChunkBytes)
			chunk := StreamLlmResponseChunk{
				LlmCallID:    llmCallID,
				ProviderKind: "anthropic",
				APIMode:      "messages",
				Raw:          raw,
				StatusCode:   &status,
				Truncated:    bodyTruncated || rawTruncated,
			}
			_ = yield(chunk)
		}
		message, details := anthropicErrorMessageAndDetails(body, status)
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: errorClassFromStatus(status),
				Message:    message,
				Details:    details,
			},
		})
	}

	return g.streamAnthropicSSE(ctx, resp.Body, llmCallID, yield)
}

func parseAnthropicAdvancedJSON(raw map[string]any) (anthropicAdvancedConfig, error) {
	cfg := anthropicAdvancedConfig{
		ExtraHeaders: map[string]string{},
		Payload:      map[string]any{},
	}
	if raw == nil {
		return cfg, nil
	}

	for key, value := range raw {
		switch key {
		case anthropicAdvancedKeyVersion:
			version, ok := value.(string)
			if !ok || strings.TrimSpace(version) == "" {
				return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
					Message: "advanced_json.anthropic_version must be a non-empty string",
				}
			}
			v := strings.TrimSpace(version)
			cfg.Version = &v
		case anthropicAdvancedKeyExtraHeaders:
			headers, ok := value.(map[string]any)
			if !ok {
				return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
					Message: "advanced_json.extra_headers must be an object",
				}
			}
			for hk, hv := range headers {
				headerName := strings.ToLower(strings.TrimSpace(hk))
				if headerName != anthropicBetaHeader {
					return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
						Message: "advanced_json.extra_headers only supports anthropic-beta",
						Details: map[string]any{"invalid_header": hk},
					}
				}
				headerValue, ok := hv.(string)
				if !ok || strings.TrimSpace(headerValue) == "" {
					return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
						Message: "advanced_json.extra_headers.anthropic-beta must be a non-empty string",
					}
				}
				cfg.ExtraHeaders[anthropicBetaHeader] = strings.TrimSpace(headerValue)
			}
		default:
			if _, denied := anthropicAdvancedJSONDenylist[key]; denied {
				return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
					Message: fmt.Sprintf("advanced_json must not set critical field: %s", key),
					Details: map[string]any{"denied_key": key},
				}
			}
			cfg.Payload[key] = value
		}
	}

	return cfg, nil
}

func toAnthropicMessages(messages []Message) ([]map[string]any, []map[string]any, error) {
	systemBlocks := []map[string]any{}
	out := []map[string]any{}
	pendingToolResults := []map[string]any{}

	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		out = append(out, map[string]any{
			"role":    "user",
			"content": pendingToolResults,
		})
		pendingToolResults = []map[string]any{}
	}

	for _, message := range messages {
		text := joinParts(message.Content)
		if message.Role == "system" {
			if strings.TrimSpace(text) != "" {
				block := map[string]any{"type": "text", "text": text}
				// 若任意 system TextPart 带 cache_control，取第一个非空值
				for _, part := range message.Content {
					if part.CacheControl != nil && strings.TrimSpace(*part.CacheControl) != "" {
						block["cache_control"] = map[string]any{"type": *part.CacheControl}
						break
					}
				}
				systemBlocks = append(systemBlocks, block)
			}
			continue
		}

		if message.Role == "tool" {
			block, err := anthropicToolResultBlock(text)
			if err != nil {
				return nil, nil, err
			}
			pendingToolResults = append(pendingToolResults, block)
			continue
		}

		flushToolResults()

		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			blocks := []map[string]any{}
			if strings.TrimSpace(text) != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": text})
			}
			for _, call := range message.ToolCalls {
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    call.ToolCallID,
					"name":  call.ToolName,
					"input": mapOrEmpty(call.ArgumentsJSON),
				})
			}
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": blocks,
			})
			continue
		}

		blocks, err := anthropicContentBlocks(message.Content)
		if err != nil {
			return nil, nil, err
		}
		if len(blocks) == 0 {
			blocks = []map[string]any{{"type": "text", "text": text}}
		}
		out = append(out, map[string]any{
			"role":    message.Role,
			"content": blocks,
		})
	}

	flushToolResults()
	return systemBlocks, out, nil
}

func anthropicContentBlocks(parts []ContentPart) ([]map[string]any, error) {
	blocks := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part.Kind() {
		case "text":
			block := map[string]any{"type": "text", "text": part.Text}
			if part.CacheControl != nil && strings.TrimSpace(*part.CacheControl) != "" {
				block["cache_control"] = map[string]any{"type": *part.CacheControl}
			}
			blocks = append(blocks, block)
		case "file":
			text := PartPromptText(part)
			if strings.TrimSpace(text) == "" {
				continue
			}
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		case "image":
			if part.Attachment == nil {
				return nil, fmt.Errorf("image attachment is required")
			}
			if len(part.Data) == 0 {
				return nil, fmt.Errorf("image attachment data is required")
			}
			mimeType := strings.TrimSpace(part.Attachment.MimeType)
			if mimeType == "" {
				mimeType = "application/octet-stream"
			}
			blocks = append(blocks, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": mimeType,
					"data":       base64.StdEncoding.EncodeToString(part.Data),
				},
			})
		}
	}
	return blocks, nil
}

func anthropicToolResultBlock(text string) (map[string]any, error) {
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("tool message is not valid JSON")
	}
	envelope, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool message is not valid JSON")
	}

	toolCallID, _ := envelope["tool_call_id"].(string)
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return nil, fmt.Errorf("tool message missing tool_call_id")
	}

	isError := false
	var contentSource any
	if errObj, ok := envelope["error"]; ok && errObj != nil {
		isError = true
		contentSource = map[string]any{"error": errObj}
	} else {
		contentSource = envelope["result"]
	}

	content, err := stablejson.Encode(contentSource)
	if err != nil {
		content = "{}"
	}

	block := map[string]any{
		"type":        "tool_result",
		"tool_use_id": toolCallID,
		"content":     content,
	}
	if isError {
		block["is_error"] = true
	}
	return block, nil
}

func toAnthropicTools(specs []ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		payload := map[string]any{
			"name":         spec.Name,
			"input_schema": mapOrEmpty(spec.JSONSchema),
		}
		if spec.Description != nil {
			payload["description"] = *spec.Description
		}
		out = append(out, payload)
	}
	return out
}

func parseAnthropicMessage(body []byte) (content string, thinkingText string, toolCalls []ToolCall, err error) {
	var parsed any
	if err = json.Unmarshal(body, &parsed); err != nil {
		return
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		err = fmt.Errorf("response root is not an object")
		return
	}
	rawContent, ok := root["content"].([]any)
	if !ok || len(rawContent) == 0 {
		err = fmt.Errorf("response missing content")
		return
	}

	var textBuilder strings.Builder
	var thinkingBuilder strings.Builder
	toolCalls = []ToolCall{}

	for idx, rawItem := range rawContent {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := item["type"].(string)
		if typ == "thinking" {
			text, _ := item["thinking"].(string)
			thinkingBuilder.WriteString(text)
			continue
		}
		if typ == "text" {
			text, _ := item["text"].(string)
			textBuilder.WriteString(text)
			continue
		}
		if typ != "tool_use" {
			continue
		}

		toolCallID, _ := item["id"].(string)
		toolCallID = strings.TrimSpace(toolCallID)
		if toolCallID == "" {
			err = fmt.Errorf("content[%d] missing tool_use.id", idx)
			return
		}
		toolName, _ := item["name"].(string)
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			err = fmt.Errorf("content[%d] missing tool_use.name", idx)
			return
		}

		argumentsJSON := map[string]any{}
		if rawInput, ok := item["input"]; ok && rawInput != nil {
			obj, ok := rawInput.(map[string]any)
			if !ok {
				err = fmt.Errorf("%w: content[%d].input must be a JSON object", errAnthropicToolUseInput, idx)
				return
			}
			argumentsJSON = obj
		}

		toolCalls = append(toolCalls, ToolCall{
			ToolCallID:    toolCallID,
			ToolName:      toolName,
			ArgumentsJSON: argumentsJSON,
		})
	}

	content = textBuilder.String()
	thinkingText = thinkingBuilder.String()
	return
}

// ensureAnthropicMaxTokensForThinking 确保 max_tokens > budget_tokens，
// 当 thinking 已注入但 max_tokens 不足时自动上调。
func ensureAnthropicMaxTokensForThinking(payload map[string]any) {
	tObj, ok := payload["thinking"].(map[string]any)
	if !ok {
		return
	}
	budget := anyToInt(tObj["budget_tokens"])
	if budget <= 0 {
		return
	}
	maxTokens := anyToInt(payload["max_tokens"])
	if maxTokens <= budget {
		payload["max_tokens"] = budget + maxTokens
	}
}

func anyToInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}

func anthropicErrorMessageAndDetails(body []byte, status int) (string, map[string]any) {
	details := map[string]any{"status_code": status}

	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "Anthropic request failed", details
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return "Anthropic request failed", details
	}
	errObj, ok := root["error"].(map[string]any)
	if !ok {
		return "Anthropic request failed", details
	}
	if errType, ok := errObj["type"].(string); ok && strings.TrimSpace(errType) != "" {
		details["anthropic_error_type"] = strings.TrimSpace(errType)
	}
	if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg), details
	}
	return "Anthropic request failed", details
}

func parseAnthropicUsage(body []byte) *Usage {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil
	}
	usageObj, ok := root["usage"].(map[string]any)
	if !ok {
		return nil
	}
	input, hasInput := usageObj["input_tokens"].(float64)
	output, hasOutput := usageObj["output_tokens"].(float64)
	cacheCreate, hasCacheCreate := usageObj["cache_creation_input_tokens"].(float64)
	cacheRead, hasCacheRead := usageObj["cache_read_input_tokens"].(float64)

	if !hasInput && !hasOutput && !hasCacheCreate && !hasCacheRead {
		return nil
	}
	u := &Usage{}
	if hasInput {
		iv := int(input)
		u.InputTokens = &iv
	}
	if hasOutput {
		ov := int(output)
		u.OutputTokens = &ov
	}
	if hasCacheCreate {
		cv := int(cacheCreate)
		u.CacheCreationInputTokens = &cv
	}
	if hasCacheRead {
		rv := int(cacheRead)
		u.CacheReadInputTokens = &rv
	}
	return u
}

type anthropicStreamEvent struct {
	Type         string                     `json:"type"`
	Index        *int                       `json:"index"`
	ContentBlock *anthropicStreamBlock      `json:"content_block"`
	Delta        *anthropicStreamDelta      `json:"delta"`
	Message      *anthropicStreamMessage    `json:"message"`
	Usage        map[string]any             `json:"usage"`
}

type anthropicStreamMessage struct {
	Usage map[string]any `json:"usage"`
}

type anthropicStreamBlock struct {
	Type     string         `json:"type"`
	Text     string         `json:"text"`
	Thinking string         `json:"thinking"`
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Input    map[string]any `json:"input"`
}

type anthropicStreamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Thinking    string `json:"thinking"`
	PartialJSON string `json:"partial_json"`
}

type anthropicToolUseBuffer struct {
	ID   string
	Name string
	JSON strings.Builder
}

func (g *AnthropicGateway) streamAnthropicSSE(ctx context.Context, body io.Reader, llmCallID string, yield func(StreamEvent) error) error {
	var usage *Usage
	toolBuffers := map[int]*anthropicToolUseBuffer{}
	completed := false

	err := forEachSSEData(ctx, body, func(data string) error {
		data = strings.TrimSpace(data)
		if data == "" {
			return nil
		}
		if g.cfg.EmitDebugEvents {
			raw, rawTruncated := truncateUTF8(data, anthropicMaxDebugChunkBytes)
			if err := yield(StreamLlmResponseChunk{
				LlmCallID:    llmCallID,
				ProviderKind: "anthropic",
				APIMode:      "messages",
				Raw:          raw,
				Truncated:    rawTruncated,
			}); err != nil {
				return err
			}
		}

		var event anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return yield(StreamRunFailed{
				LlmCallID: llmCallID,
				Error: GatewayError{
					ErrorClass: ErrorClassInternalError,
					Message:    "Anthropic response parse failed",
					Details:    map[string]any{"reason": err.Error()},
				},
			})
		}

		if parsed := parseAnthropicUsageMap(event.Usage); parsed != nil {
			usage = parsed
		}
		if event.Message != nil {
			if parsed := parseAnthropicUsageMap(event.Message.Usage); parsed != nil {
				usage = parsed
			}
		}

		idx := 0
		if event.Index != nil {
			idx = *event.Index
		}

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock == nil {
				return nil
			}
			switch event.ContentBlock.Type {
			case "text":
				if strings.TrimSpace(event.ContentBlock.Text) == "" {
					return nil
				}
				return yield(StreamMessageDelta{ContentDelta: event.ContentBlock.Text, Role: "assistant"})
			case "thinking":
				if strings.TrimSpace(event.ContentBlock.Thinking) == "" {
					return nil
				}
				channel := "thinking"
				return yield(StreamMessageDelta{ContentDelta: event.ContentBlock.Thinking, Role: "assistant", Channel: &channel})
			case "tool_use":
				buffer := &anthropicToolUseBuffer{
					ID:   strings.TrimSpace(event.ContentBlock.ID),
					Name: strings.TrimSpace(event.ContentBlock.Name),
				}
				toolBuffers[idx] = buffer
				if len(event.ContentBlock.Input) == 0 {
					return nil
				}
				encoded, err := stablejson.Encode(event.ContentBlock.Input)
				if err != nil {
					return err
				}
				buffer.JSON.WriteString(encoded)
				return yield(ToolCallArgumentDelta{
					ToolCallIndex:  idx,
					ToolCallID:     buffer.ID,
					ToolName:       buffer.Name,
					ArgumentsDelta: encoded,
				})
			}
		case "content_block_delta":
			if event.Delta == nil {
				return nil
			}
			switch event.Delta.Type {
			case "text_delta":
				if event.Delta.Text == "" {
					return nil
				}
				return yield(StreamMessageDelta{ContentDelta: event.Delta.Text, Role: "assistant"})
			case "thinking_delta":
				if event.Delta.Thinking == "" {
					return nil
				}
				channel := "thinking"
				return yield(StreamMessageDelta{ContentDelta: event.Delta.Thinking, Role: "assistant", Channel: &channel})
			case "input_json_delta":
				buffer := toolBuffers[idx]
				if buffer == nil {
					return nil
				}
				buffer.JSON.WriteString(event.Delta.PartialJSON)
				return yield(ToolCallArgumentDelta{
					ToolCallIndex:  idx,
					ToolCallID:     buffer.ID,
					ToolName:       buffer.Name,
					ArgumentsDelta: event.Delta.PartialJSON,
				})
			}
		case "content_block_stop":
			buffer := toolBuffers[idx]
			if buffer == nil {
				return nil
			}
			delete(toolBuffers, idx)
			if strings.TrimSpace(buffer.ID) == "" || strings.TrimSpace(buffer.Name) == "" {
				return yield(StreamRunFailed{
					LlmCallID: llmCallID,
					Error: GatewayError{
						ErrorClass: ErrorClassProviderNonRetryable,
						Message:    "Anthropic tool_use input parse failed",
						Details:    map[string]any{"reason": "content block missing tool_use id or name"},
					},
				})
			}
			argumentsJSON := map[string]any{}
			rawArgs := strings.TrimSpace(buffer.JSON.String())
			if rawArgs != "" {
				var parsed any
				if err := json.Unmarshal([]byte(rawArgs), &parsed); err != nil {
					return yield(StreamRunFailed{
						LlmCallID: llmCallID,
						Error: GatewayError{
							ErrorClass: ErrorClassProviderNonRetryable,
							Message:    "Anthropic tool_use input parse failed",
							Details:    map[string]any{"reason": err.Error()},
						},
					})
				}
				obj, ok := parsed.(map[string]any)
				if !ok {
					return yield(StreamRunFailed{
						LlmCallID: llmCallID,
						Error: GatewayError{
							ErrorClass: ErrorClassProviderNonRetryable,
							Message:    "Anthropic tool_use input parse failed",
							Details:    map[string]any{"reason": "tool_use input must be a JSON object"},
						},
					})
				}
				argumentsJSON = obj
			}
			return yield(ToolCall{
				ToolCallID:    buffer.ID,
				ToolName:      buffer.Name,
				ArgumentsJSON: argumentsJSON,
			})
		case "message_stop":
			completed = true
			return nil
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !completed {
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: InternalStreamEndedError()})
	}
	return yield(StreamRunCompleted{LlmCallID: llmCallID, Usage: usage})
}

func parseAnthropicUsageMap(usageObj map[string]any) *Usage {
	if len(usageObj) == 0 {
		return nil
	}
	input, hasInput := usageObj["input_tokens"].(float64)
	output, hasOutput := usageObj["output_tokens"].(float64)
	cacheCreate, hasCacheCreate := usageObj["cache_creation_input_tokens"].(float64)
	cacheRead, hasCacheRead := usageObj["cache_read_input_tokens"].(float64)

	if !hasInput && !hasOutput && !hasCacheCreate && !hasCacheRead {
		return nil
	}
	u := &Usage{}
	if hasInput {
		iv := int(input)
		u.InputTokens = &iv
	}
	if hasOutput {
		ov := int(output)
		u.OutputTokens = &ov
	}
	if hasCacheCreate {
		cv := int(cacheCreate)
		u.CacheCreationInputTokens = &cv
	}
	if hasCacheRead {
		rv := int(cacheRead)
		u.CacheReadInputTokens = &rv
	}
	return u
}
