package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"arkloop/services/worker/internal/stablejson"
	"github.com/google/uuid"
)

const defaultAnthropicVersion = "2023-06-01"
const maxAnthropicResponseBytes = 4096 * 4

var errAnthropicToolUseInput = errors.New("anthropic_tool_use_input")

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
				Message:    "Anthropic messages 构造失败",
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
	if system != "" {
		payload["system"] = system
	}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	if len(request.Tools) > 0 {
		payload["tools"] = toAnthropicTools(request.Tools)
	}
	// advanced_json 补充未被显式字段占用的 key，已存在的 key 不覆盖
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
				Message:    "Anthropic 请求序列化失败",
			},
		})
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.BaseURL+"/messages", bytes.NewReader(encoded))
	if err != nil {
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "Anthropic 请求构造失败",
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
				Message:    "Anthropic 网络错误",
			},
		})
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxAnthropicResponseBytes))
	status := resp.StatusCode

	if g.cfg.EmitDebugEvents {
		raw := string(body)
		chunk := StreamLlmResponseChunk{
			LlmCallID:    llmCallID,
			ProviderKind: "anthropic",
			APIMode:      "messages",
			Raw:          raw,
			StatusCode:   &status,
			Truncated:    true,
		}
		_ = yield(chunk)
	}

	if status < 200 || status >= 300 {
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: errorClassFromStatus(status),
				Message:    "Anthropic 请求失败",
				Details:    map[string]any{"status_code": status},
			},
		})
	}

	content, toolCalls, err := parseAnthropicMessage(body)
	if err != nil {
		if errors.Is(err, errAnthropicToolUseInput) {
			return yield(StreamRunFailed{
				Error: GatewayError{
					ErrorClass: ErrorClassProviderNonRetryable,
					Message:    "Anthropic tool_use 输入解析失败",
					Details:    map[string]any{"reason": err.Error()},
				},
			})
		}
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "Anthropic 响应解析失败",
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

	return yield(StreamRunCompleted{})
}

func toAnthropicMessages(messages []Message) (string, []map[string]any, error) {
	systemParts := []string{}
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
				systemParts = append(systemParts, text)
			}
			continue
		}

		if message.Role == "tool" {
			block, err := anthropicToolResultBlock(text)
			if err != nil {
				return "", nil, err
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

		out = append(out, map[string]any{
			"role": message.Role,
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
		})
	}

	flushToolResults()
	return strings.TrimSpace(strings.Join(systemParts, "\n")), out, nil
}

func anthropicToolResultBlock(text string) (map[string]any, error) {
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("tool message 不是合法 JSON")
	}
	envelope, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool message 不是合法 JSON")
	}

	toolCallID, _ := envelope["tool_call_id"].(string)
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return nil, fmt.Errorf("tool message 缺少 tool_call_id")
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
		return "", nil, fmt.Errorf("response 根不是对象")
	}
	content, ok := root["content"].([]any)
	if !ok || len(content) == 0 {
		return "", nil, fmt.Errorf("response 缺少 content")
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
			return "", nil, fmt.Errorf("content[%d] 缺少 tool_use.id", idx)
		}
		toolName, _ := item["name"].(string)
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			return "", nil, fmt.Errorf("content[%d] 缺少 tool_use.name", idx)
		}

		argumentsJSON := map[string]any{}
		if rawInput, ok := item["input"]; ok && rawInput != nil {
			obj, ok := rawInput.(map[string]any)
			if !ok {
				return "", nil, fmt.Errorf("%w: content[%d].input 必须是 JSON object", errAnthropicToolUseInput, idx)
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
