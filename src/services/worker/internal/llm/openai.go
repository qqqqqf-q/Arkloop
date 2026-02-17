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

type OpenAIGatewayConfig struct {
	APIKey          string
	BaseURL         string
	APIMode         string
	EmitDebugEvents bool
	TotalTimeout    time.Duration
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

	if apiMode == "chat_completions" {
		return g.chatCompletions(ctx, request, yield)
	}

	if apiMode == "responses" {
		return g.responses(ctx, request, yield, false)
	}

	if apiMode != "auto" {
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI api_mode 未实现",
				Details:    map[string]any{"api_mode": apiMode},
			},
		})
	}

	if err := g.responses(ctx, request, yield, true); err != nil {
		var notSupported *openAIResponsesNotSupportedError
		if errors.As(err, &notSupported) {
			if err := yield(StreamProviderFallback{
				ProviderKind: "openai",
				FromAPIMode:  "responses",
				ToAPIMode:    "chat_completions",
				Reason:       "responses_endpoint_not_supported",
				StatusCode:   &notSupported.StatusCode,
			}); err != nil {
				return err
			}
			return g.chatCompletions(ctx, request, yield)
		}
		return err
	}

	return nil
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

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))

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

	content, toolCalls, err := parseOpenAIChatCompletion(body)
	if err != nil {
		if errors.Is(err, errOpenAIToolCallArguments) {
			failed := StreamRunFailed{
				Error: GatewayError{
					ErrorClass: ErrorClassProviderNonRetryable,
					Message:    "OpenAI tool_call 参数解析失败",
					Details:    map[string]any{"reason": err.Error()},
				},
			}
			return yield(failed)
		}
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

	for _, call := range toolCalls {
		if err := yield(call); err != nil {
			return err
		}
	}

	return yield(StreamRunCompleted{})
}

type openAIResponsesNotSupportedError struct {
	StatusCode int
}

func (e *openAIResponsesNotSupportedError) Error() string {
	return fmt.Sprintf("openai responses not supported (status=%d)", e.StatusCode)
}

func (g *OpenAIGateway) responses(ctx context.Context, request Request, yield func(StreamEvent) error, allowFallback bool) error {
	llmCallID := uuid.NewString()

	input, err := toOpenAIResponsesInput(request.Messages)
	if err != nil {
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI responses 输入构造失败",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}

	payload := map[string]any{
		"model": request.Model,
		"input": input,
	}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	if request.MaxOutputTokens != nil {
		payload["max_output_tokens"] = *request.MaxOutputTokens
	}
	if len(request.Tools) > 0 {
		payload["tools"] = toOpenAIResponsesTools(request.Tools)
		payload["tool_choice"] = "auto"
	}

	if g.cfg.EmitDebugEvents {
		baseURL := g.cfg.BaseURL
		path := "/responses"
		if err := yield(StreamLlmRequest{
			LlmCallID:    llmCallID,
			ProviderKind: "openai",
			APIMode:      "responses",
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
				Message:    "OpenAI 请求序列化失败",
			},
		})
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.BaseURL+"/responses", bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(g.cfg.APIKey))
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassProviderRetryable,
				Message:    "OpenAI 网络错误",
			},
		})
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	status := resp.StatusCode

	if g.cfg.EmitDebugEvents {
		raw := string(body)
		chunk := StreamLlmResponseChunk{
			LlmCallID:    llmCallID,
			ProviderKind: "openai",
			APIMode:      "responses",
			Raw:          raw,
			StatusCode:   &status,
			Truncated:    true,
		}
		_ = yield(chunk)
	}

	if status < 200 || status >= 300 {
		if allowFallback && isOpenAIResponsesNotSupported(status, body) {
			return &openAIResponsesNotSupportedError{StatusCode: status}
		}

		errClass := errorClassFromStatus(status)
		message := "OpenAI 请求失败"
		if strings.TrimSpace(string(body)) != "" {
			message = "OpenAI 请求失败（响应体已截断）"
		}
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: errClass,
				Message:    message,
				Details:    map[string]any{"status_code": status},
			},
		})
	}

	content, toolCalls, err := parseOpenAIResponses(body)
	if err != nil {
		if errors.Is(err, errOpenAIToolCallArguments) {
			return yield(StreamRunFailed{
				Error: GatewayError{
					ErrorClass: ErrorClassProviderNonRetryable,
					Message:    "OpenAI responses tool_call 参数解析失败",
					Details:    map[string]any{"reason": err.Error()},
				},
			})
		}
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI responses 响应解析失败",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}

	if strings.TrimSpace(content) != "" {
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

		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			out = append(out, map[string]any{
				"role":       "assistant",
				"content":    text,
				"tool_calls": toOpenAIAssistantToolCalls(message.ToolCalls),
			})
			continue
		}

		if message.Role == "tool" {
			out = append(out, toOpenAIToolMessage(text))
			continue
		}

		out = append(out, map[string]any{"role": message.Role, "content": text})
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

func toOpenAIResponsesTools(specs []ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		payload := map[string]any{
			"type":       "function",
			"name":       spec.Name,
			"parameters": mapOrEmpty(spec.JSONSchema),
		}
		if spec.Description != nil {
			payload["description"] = *spec.Description
		}
		out = append(out, payload)
	}
	return out
}

var errOpenAIToolCallArguments = errors.New("openai_tool_call_arguments")

type openAIChatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content   *string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

func parseOpenAIChatCompletion(body []byte) (string, []ToolCall, error) {
	var parsed openAIChatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", nil, err
	}
	if len(parsed.Choices) == 0 {
		return "", nil, fmt.Errorf("response 缺少 choices")
	}

	message := parsed.Choices[0].Message
	content := ""
	if message.Content != nil {
		content = *message.Content
	}

	toolCalls := make([]ToolCall, 0, len(message.ToolCalls))
	for idx, raw := range message.ToolCalls {
		toolCallID := strings.TrimSpace(raw.ID)
		if toolCallID == "" {
			return "", nil, fmt.Errorf("tool_calls[%d] 缺少 id", idx)
		}

		toolName := strings.TrimSpace(raw.Function.Name)
		if toolName == "" {
			return "", nil, fmt.Errorf("tool_calls[%d] 缺少 function.name", idx)
		}

		argumentsJSON := map[string]any{}
		arguments := strings.TrimSpace(raw.Function.Arguments)
		if arguments != "" {
			var parsedArgs any
			if err := json.Unmarshal([]byte(arguments), &parsedArgs); err != nil {
				return "", nil, fmt.Errorf("%w: tool_calls[%d].function.arguments 不是合法 JSON", errOpenAIToolCallArguments, idx)
			}
			obj, ok := parsedArgs.(map[string]any)
			if !ok {
				return "", nil, fmt.Errorf("%w: tool_calls[%d].function.arguments 必须是 JSON object", errOpenAIToolCallArguments, idx)
			}
			argumentsJSON = obj
		}

		toolCalls = append(toolCalls, ToolCall{
			ToolCallID:    toolCallID,
			ToolName:      toolName,
			ArgumentsJSON: argumentsJSON,
		})
	}

	return content, toolCalls, nil
}

func toOpenAIResponsesInput(messages []Message) ([]map[string]any, error) {
	items := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		text := joinParts(message.Content)
		contentType := "input_text"
		if message.Role == "assistant" {
			contentType = "output_text"
		}

		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			if strings.TrimSpace(text) != "" {
				items = append(items, map[string]any{
					"role": "assistant",
					"content": []map[string]any{
						{"type": contentType, "text": text},
					},
				})
			}

			for _, call := range message.ToolCalls {
				argumentsJSON, err := stablejson.Encode(mapOrEmpty(call.ArgumentsJSON))
				if err != nil {
					argumentsJSON = "{}"
				}
				items = append(items, map[string]any{
					"type":      "function_call",
					"call_id":   call.ToolCallID,
					"name":      call.ToolName,
					"arguments": argumentsJSON,
				})
			}
			continue
		}

		if message.Role == "tool" {
			parsed := map[string]any{}
			if err := json.Unmarshal([]byte(text), &parsed); err != nil {
				return nil, fmt.Errorf("tool message 不是合法 JSON")
			}
			toolCallID, _ := parsed["tool_call_id"].(string)
			toolCallID = strings.TrimSpace(toolCallID)
			if toolCallID == "" {
				return nil, fmt.Errorf("tool message 缺少 tool_call_id")
			}
			items = append(items, map[string]any{
				"type":    "function_call_output",
				"call_id": toolCallID,
				"output":  toolOutputTextFromEnvelope(parsed),
			})
			continue
		}

		items = append(items, map[string]any{
			"role": message.Role,
			"content": []map[string]any{
				{"type": contentType, "text": text},
			},
		})
	}
	return items, nil
}

func toOpenAIAssistantToolCalls(calls []ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		argumentsJSON, err := stablejson.Encode(mapOrEmpty(call.ArgumentsJSON))
		if err != nil {
			argumentsJSON = "{}"
		}

		out = append(out, map[string]any{
			"id":   call.ToolCallID,
			"type": "function",
			"function": map[string]any{
				"name":      call.ToolName,
				"arguments": argumentsJSON,
			},
		})
	}
	return out
}

func toOpenAIToolMessage(text string) map[string]any {
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return map[string]any{"role": "tool", "content": text}
	}

	envelope, ok := parsed.(map[string]any)
	if !ok {
		return map[string]any{"role": "tool", "content": text}
	}

	toolCallID, _ := envelope["tool_call_id"].(string)
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return map[string]any{"role": "tool", "content": text}
	}

	return map[string]any{
		"role":         "tool",
		"tool_call_id": toolCallID,
		"content":      toolOutputTextFromEnvelope(envelope),
	}
}

func toolOutputTextFromEnvelope(envelope map[string]any) string {
	if result, ok := envelope["result"]; ok && result != nil {
		encoded, err := stablejson.Encode(result)
		if err == nil && encoded != "" {
			return encoded
		}
	}

	if errObj, ok := envelope["error"]; ok && errObj != nil {
		payload := map[string]any{"error": errObj}
		encoded, err := stablejson.Encode(payload)
		if err == nil && encoded != "" {
			return encoded
		}
	}

	encoded, err := stablejson.Encode(envelope)
	if err == nil && encoded != "" {
		return encoded
	}

	encodedBytes, err := json.Marshal(envelope)
	if err != nil {
		return "{}"
	}
	return string(encodedBytes)
}

func isOpenAIResponsesNotSupported(status int, body []byte) bool {
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return true
	case http.StatusBadRequest:
	default:
		return false
	}

	rawBody := strings.ToLower(strings.TrimSpace(string(body)))
	if strings.Contains(rawBody, "responses") && (strings.Contains(rawBody, "unknown") || strings.Contains(rawBody, "not found")) {
		return true
	}

	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return false
	}
	errObj, _ := root["error"].(map[string]any)
	message, _ := errObj["message"].(string)
	joined := strings.ToLower(strings.TrimSpace(message) + " " + strings.TrimSpace(string(body)))
	return strings.Contains(joined, "responses") && (strings.Contains(joined, "unknown") || strings.Contains(joined, "not found"))
}

func parseOpenAIResponses(body []byte) (string, []ToolCall, error) {
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", nil, err
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return "", nil, fmt.Errorf("response 根不是对象")
	}

	var contentBuilder strings.Builder
	hasTopLevelText := false
	if rawOutputText, ok := root["output_text"].(string); ok {
		if strings.TrimSpace(rawOutputText) != "" {
			contentBuilder.WriteString(rawOutputText)
			hasTopLevelText = true
		}
	}

	rawOutput, ok := root["output"].([]any)
	if !ok {
		if contentBuilder.Len() > 0 {
			return contentBuilder.String(), nil, nil
		}
		return "", nil, fmt.Errorf("response 缺少 output")
	}

	toolCalls := []ToolCall{}
	for idx, rawItem := range rawOutput {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := item["type"].(string)

		if typ == "message" {
			if hasTopLevelText {
				continue
			}
			parts, ok := item["content"].([]any)
			if !ok {
				continue
			}
			for _, rawPart := range parts {
				part, ok := rawPart.(map[string]any)
				if !ok {
					continue
				}
				partType, _ := part["type"].(string)
				if partType != "output_text" && partType != "text" {
					continue
				}
				text, _ := part["text"].(string)
				contentBuilder.WriteString(text)
			}
			continue
		}

		if typ != "function_call" {
			continue
		}

		toolCallID, _ := item["call_id"].(string)
		if strings.TrimSpace(toolCallID) == "" {
			toolCallID, _ = item["id"].(string)
		}
		toolCallID = strings.TrimSpace(toolCallID)
		if toolCallID == "" {
			return "", nil, fmt.Errorf("output[%d] 缺少 function_call.id", idx)
		}

		toolName, _ := item["name"].(string)
		if strings.TrimSpace(toolName) == "" {
			toolName, _ = item["tool_name"].(string)
		}
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			return "", nil, fmt.Errorf("output[%d] 缺少 function_call.name", idx)
		}

		argumentsJSON := map[string]any{}
		if rawArgs, ok := item["arguments"]; ok && rawArgs != nil {
			switch casted := rawArgs.(type) {
			case map[string]any:
				argumentsJSON = casted
			case string:
				arguments := strings.TrimSpace(casted)
				if arguments != "" {
					var parsedArgs any
					if err := json.Unmarshal([]byte(arguments), &parsedArgs); err != nil {
						return "", nil, fmt.Errorf("%w: output[%d].arguments 不是合法 JSON", errOpenAIToolCallArguments, idx)
					}
					obj, ok := parsedArgs.(map[string]any)
					if !ok {
						return "", nil, fmt.Errorf("%w: output[%d].arguments 必须是 JSON object", errOpenAIToolCallArguments, idx)
					}
					argumentsJSON = obj
				}
			default:
				return "", nil, fmt.Errorf("%w: output[%d].arguments 类型不支持", errOpenAIToolCallArguments, idx)
			}
		}

		toolCalls = append(toolCalls, ToolCall{
			ToolCallID:    toolCallID,
			ToolName:      toolName,
			ArgumentsJSON: argumentsJSON,
		})
	}

	return contentBuilder.String(), toolCalls, nil
}
