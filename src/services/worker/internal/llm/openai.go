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

type OpenAIGatewayConfig struct {
	APIKey           string
	BaseURL          string
	APIMode          string
	EmitDebugEvents  bool
	TotalTimeout     time.Duration
}

type OpenAIGateway struct {
	cfg    OpenAIGatewayConfig
	client *http.Client
}

func NewOpenAIGateway(cfg OpenAIGatewayConfig) *OpenAIGateway {
	timeout := cfg.TotalTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	cfg.BaseURL = baseURL
	if strings.TrimSpace(cfg.APIMode) == "" {
		cfg.APIMode = "auto"
	}
	cfg.TotalTimeout = timeout
	return &OpenAIGateway{
		cfg: cfg,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (g *OpenAIGateway) Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error {
	apiMode := g.cfg.APIMode
	if apiMode != "auto" && apiMode != "responses" && apiMode != "chat_completions" {
		apiMode = "auto"
	}
	if apiMode == "responses" {
		if err := yield(StreamProviderFallback{
			ProviderKind: "openai",
			FromAPIMode:  "responses",
			ToAPIMode:    "chat_completions",
			Reason:       "go gateway 暂未实现 responses，回退到 chat_completions",
		}); err != nil {
			return err
		}
		apiMode = "chat_completions"
	}
	if apiMode == "auto" {
		apiMode = "chat_completions"
	}

	if apiMode != "chat_completions" {
		failed := StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI api_mode 未实现",
				Details:    map[string]any{"api_mode": apiMode},
			},
		}
		return yield(failed)
	}

	return g.chatCompletions(ctx, request, yield)
}

func (g *OpenAIGateway) chatCompletions(ctx context.Context, request Request, yield func(StreamEvent) error) error {
	llmCallID := uuid.NewString()

	payload := map[string]any{
		"model":    request.Model,
		"messages": toOpenAIChatMessages(request.Messages),
	}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	if request.MaxOutputTokens != nil {
		payload["max_tokens"] = *request.MaxOutputTokens
	}
	if len(request.Tools) > 0 {
		payload["tools"] = toOpenAITools(request.Tools)
		payload["tool_choice"] = "auto"
	}

	if g.cfg.EmitDebugEvents {
		baseURL := g.cfg.BaseURL
		path := "/chat/completions"
		if err := yield(StreamLlmRequest{
			LlmCallID:    llmCallID,
			ProviderKind: "openai",
			APIMode:      "chat_completions",
			BaseURL:      &baseURL,
			Path:         &path,
			PayloadJSON:  payload,
		}); err != nil {
			return err
		}
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		failed := StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI 请求序列化失败",
			},
		}
		return yield(failed)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.BaseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(g.cfg.APIKey))
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		failed := StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassProviderRetryable,
				Message:    "OpenAI 网络错误",
			},
		}
		return yield(failed)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096*4))

	status := resp.StatusCode
	if status < 200 || status >= 300 {
		errClass := errorClassFromStatus(status)
		message := "OpenAI 请求失败"
		if strings.TrimSpace(string(body)) != "" {
			message = "OpenAI 请求失败（响应体已截断）"
		}
		failed := StreamRunFailed{
			Error: GatewayError{
				ErrorClass: errClass,
				Message:    message,
				Details:    map[string]any{"status_code": status},
			},
		}
		if g.cfg.EmitDebugEvents {
			raw := string(body)
			chunk := StreamLlmResponseChunk{
				LlmCallID:    llmCallID,
				ProviderKind: "openai",
				APIMode:      "chat_completions",
				Raw:          raw,
				StatusCode:   &status,
				Truncated:    true,
			}
			_ = yield(chunk)
		}
		return yield(failed)
	}

	if g.cfg.EmitDebugEvents {
		raw := string(body)
		chunk := StreamLlmResponseChunk{
			LlmCallID:    llmCallID,
			ProviderKind: "openai",
			APIMode:      "chat_completions",
			Raw:          raw,
			StatusCode:   &status,
			Truncated:    true,
		}
		if err := yield(chunk); err != nil {
			return err
		}
	}

	content, err := parseOpenAIChatContent(body)
	if err != nil {
		failed := StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI 响应解析失败",
				Details:    map[string]any{"reason": err.Error()},
			},
		}
		return yield(failed)
	}

	if strings.TrimSpace(content) != "" {
		if err := yield(StreamMessageDelta{ContentDelta: content, Role: "assistant"}); err != nil {
			return err
		}
	}

	return yield(StreamRunCompleted{})
}

func errorClassFromStatus(status int) string {
	switch status {
	case 408, 425, 429:
		return ErrorClassProviderRetryable
	default:
		if status >= 500 && status <= 599 {
			return ErrorClassProviderRetryable
		}
		return ErrorClassProviderNonRetryable
	}
}

func toOpenAIChatMessages(messages []Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		text := joinParts(message.Content)
		out = append(out, map[string]any{
			"role":    message.Role,
			"content": text,
		})
	}
	return out
}

func joinParts(parts []TextPart) string {
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(part.Text)
	}
	return b.String()
}

func toOpenAITools(specs []ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		fn := map[string]any{
			"name":       spec.Name,
			"parameters": mapOrEmpty(spec.JSONSchema),
		}
		if spec.Description != nil {
			fn["description"] = *spec.Description
		}
		out = append(out, map[string]any{
			"type":     "function",
			"function": fn,
		})
	}
	return out
}

func parseOpenAIChatContent(body []byte) (string, error) {
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return "", fmt.Errorf("response 根不是对象")
	}
	choices, ok := root["choices"].([]any)
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("response 缺少 choices")
	}
	first, ok := choices[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("choices[0] 类型错误")
	}
	message, ok := first["message"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("choices[0].message 类型错误")
	}
	content, _ := message["content"].(string)
	return content, nil
}

