package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const defaultAnthropicVersion = "2023-06-01"

type AnthropicGatewayConfig struct {
	APIKey          string
	BaseURL         string
	AnthropicVersion string
	EmitDebugEvents bool
	TotalTimeout    time.Duration
	AdvancedJSON    map[string]any
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

	system, messages := splitSystemMessage(request.Messages)
	maxTokens := 1024
	if request.MaxOutputTokens != nil && *request.MaxOutputTokens > 0 {
		maxTokens = *request.MaxOutputTokens
	}

	payload := map[string]any{
		"model":      request.Model,
		"messages":   toAnthropicMessages(messages),
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
		return err
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

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096*4))
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

	content, err := parseAnthropicText(body)
	if err != nil {
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "Anthropic 响应解析失败",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}

	if strings.TrimSpace(content) != "" {
		if err := yield(StreamMessageDelta{ContentDelta: content, Role: "assistant"}); err != nil {
			return err
		}
	}

	return yield(StreamRunCompleted{})
}

func splitSystemMessage(messages []Message) (string, []Message) {
	out := make([]Message, 0, len(messages))
	systemParts := []string{}
	for _, message := range messages {
		if message.Role == "system" {
			systemParts = append(systemParts, joinParts(message.Content))
			continue
		}
		out = append(out, message)
	}
	return strings.TrimSpace(strings.Join(systemParts, "\n")), out
}

func toAnthropicMessages(messages []Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		out = append(out, map[string]any{
			"role": message.Role,
			"content": []map[string]any{
				{"type": "text", "text": joinParts(message.Content)},
			},
		})
	}
	return out
}

func toAnthropicTools(specs []ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		payload := map[string]any{
			"name":        spec.Name,
			"input_schema": mapOrEmpty(spec.JSONSchema),
		}
		if spec.Description != nil {
			payload["description"] = *spec.Description
		}
		out = append(out, payload)
	}
	return out
}

func parseAnthropicText(body []byte) (string, error) {
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return "", fmt.Errorf("response 根不是对象")
	}
	content, ok := root["content"].([]any)
	if !ok || len(content) == 0 {
		return "", fmt.Errorf("response 缺少 content")
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("content[0] 类型错误")
	}
	if typ, _ := first["type"].(string); typ != "text" {
		return "", fmt.Errorf("content[0] 非 text")
	}
	text, _ := first["text"].(string)
	return text, nil
}

