package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"arkloop/services/worker/internal/stablejson"
	"github.com/google/uuid"
)

const defaultAnthropicVersion = "2023-06-01"
const maxAnthropicResponseBytes = 4096 * 4
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

type AnthropicGatewayConfig struct {
	APIKey           string
	BaseURL          string
	AnthropicVersion string
	EmitDebugEvents  bool
	TotalTimeout     time.Duration
	AdvancedJSON     map[string]any
}

type AnthropicGateway struct {
	cfg    AnthropicGatewayConfig
	client *http.Client
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
	cfg.BaseURL = baseURL
	if strings.TrimSpace(cfg.AnthropicVersion) == "" {
		cfg.AnthropicVersion = defaultAnthropicVersion
	}
	cfg.TotalTimeout = timeout
	if cfg.AdvancedJSON == nil {
		cfg.AdvancedJSON = map[string]any{}
	}
	return &AnthropicGateway{
		cfg: cfg,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (g *AnthropicGateway) Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error {
	llmCallID := uuid.NewString()

	system, messages, err := toAnthropicMessages(request.Messages)
	if err != nil {
		return yield(StreamRunFailed{
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
	// reject advanced_json if it contains denied critical fields
	for k := range g.cfg.AdvancedJSON {
		if _, denied := anthropicAdvancedJSONDenylist[k]; denied {
			return yield(StreamRunFailed{
				Error: GatewayError{
					ErrorClass: ErrorClassInternalError,
					Message:    fmt.Sprintf("advanced_json must not set critical field: %s", k),
					Details:    map[string]any{"denied_key": k},
				},
			})
		}
	}
	// merge keys not already present in payload
	for k, v := range g.cfg.AdvancedJSON {
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}

	if g.cfg.EmitDebugEvents {
		baseURL := g.cfg.BaseURL
		path := "/messages"
		if err := yield(StreamLlmRequest{
			LlmCallID:    llmCallID,
			ProviderKind: "anthropic",
			APIMode:      "messages",
			BaseURL:      &baseURL,
			Path:         &path,
			PayloadJSON:  payload,
		}); err != nil {
			return err
		}
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "Anthropic request serialization failed",
			},
		})
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.BaseURL+"/messages", bytes.NewReader(encoded))
	if err != nil {
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "Anthropic request construction failed",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}
	req.Header.Set("x-api-key", strings.TrimSpace(g.cfg.APIKey))
	req.Header.Set("anthropic-version", g.cfg.AnthropicVersion)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassProviderRetryable,
				Message:    "Anthropic network error",
			},
		})
	}
	defer resp.Body.Close()

	body, bodyTruncated, _ := readAllWithLimit(resp.Body, maxAnthropicResponseBytes)
	status := resp.StatusCode

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

	if status < 200 || status >= 300 {
		message, details := anthropicErrorMessageAndDetails(body, status)
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: errorClassFromStatus(status),
				Message:    message,
				Details:    details,
			},
		})
	}

	content, toolCalls, err := parseAnthropicMessage(body)
	if err != nil {
		if errors.Is(err, errAnthropicToolUseInput) {
			return yield(StreamRunFailed{
				Error: GatewayError{
					ErrorClass: ErrorClassProviderNonRetryable,
					Message:    "Anthropic tool_use input parse failed",
					Details:    map[string]any{"reason": err.Error()},
				},
			})
		}
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "Anthropic response parse failed",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}

	if content != "" {
		if err := yield(StreamMessageDelta{ContentDelta: content, Role: "assistant"}); err != nil {
			return err
		}
	}

	for _, call := range toolCalls {
		if err := yield(call); err != nil {
			return err
		}
	}

	return yield(StreamRunCompleted{Usage: parseAnthropicUsage(body)})
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

		blocks := []map[string]any{}
		for _, part := range message.Content {
			block := map[string]any{"type": "text", "text": part.Text}
			if part.CacheControl != nil && strings.TrimSpace(*part.CacheControl) != "" {
				block["cache_control"] = map[string]any{"type": *part.CacheControl}
			}
			blocks = append(blocks, block)
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

func parseAnthropicMessage(body []byte) (string, []ToolCall, error) {
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", nil, err
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return "", nil, fmt.Errorf("response root is not an object")
	}
	content, ok := root["content"].([]any)
	if !ok || len(content) == 0 {
		return "", nil, fmt.Errorf("response missing content")
	}

	var b strings.Builder
	toolCalls := []ToolCall{}

	for idx, rawItem := range content {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := item["type"].(string)
		if typ == "text" {
			text, _ := item["text"].(string)
			b.WriteString(text)
			continue
		}
		if typ != "tool_use" {
			continue
		}

		toolCallID, _ := item["id"].(string)
		toolCallID = strings.TrimSpace(toolCallID)
		if toolCallID == "" {
			return "", nil, fmt.Errorf("content[%d] missing tool_use.id", idx)
		}
		toolName, _ := item["name"].(string)
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			return "", nil, fmt.Errorf("content[%d] missing tool_use.name", idx)
		}

		argumentsJSON := map[string]any{}
		if rawInput, ok := item["input"]; ok && rawInput != nil {
			obj, ok := rawInput.(map[string]any)
			if !ok {
				return "", nil, fmt.Errorf("%w: content[%d].input must be a JSON object", errAnthropicToolUseInput, idx)
			}
			argumentsJSON = obj
		}

		toolCalls = append(toolCalls, ToolCall{
			ToolCallID:    toolCallID,
			ToolName:      toolName,
			ArgumentsJSON: argumentsJSON,
		})
	}

	return b.String(), toolCalls, nil
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
