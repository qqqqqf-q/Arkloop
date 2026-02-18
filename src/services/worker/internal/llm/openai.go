package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

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

const (
	openAIMaxErrorBodyBytes  = 4096
	openAIMaxDebugChunkBytes = 8192
	openAIMaxResponseBytes   = 1024 * 1024
)

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
		cfg:    cfg,
		client: &http.Client{},
	}
}

func (g *OpenAIGateway) Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error {
	ctx, cancel := context.WithTimeout(ctx, g.cfg.TotalTimeout)
	defer cancel()

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
		"stream":   true,
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
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI 请求构造失败",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(g.cfg.APIKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

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

	status := resp.StatusCode
	if status < 200 || status >= 300 {
		body, bodyTruncated, _ := readAllWithLimit(resp.Body, openAIMaxErrorBodyBytes)
		message, details := openAIErrorMessageAndDetails(body, status, "OpenAI 请求失败")

		errClass := errorClassFromStatus(status)
		failed := StreamRunFailed{
			Error: GatewayError{
				ErrorClass: errClass,
				Message:    message,
				Details:    details,
			},
		}
		if g.cfg.EmitDebugEvents {
			raw, rawTruncated := truncateUTF8(string(body), openAIMaxDebugChunkBytes)
			chunk := StreamLlmResponseChunk{
				LlmCallID:    llmCallID,
				ProviderKind: "openai",
				APIMode:      "chat_completions",
				Raw:          raw,
				StatusCode:   &status,
				Truncated:    bodyTruncated || rawTruncated,
			}
			_ = yield(chunk)
		}
		return yield(failed)
	}

	if isEventStream(resp.Header.Get("Content-Type")) {
		return g.streamChatCompletionsSSE(ctx, resp.Body, llmCallID, status, yield)
	}

	body, bodyTruncated, _ := readAllWithLimit(resp.Body, openAIMaxResponseBytes)
	if g.cfg.EmitDebugEvents {
		raw, rawTruncated := truncateUTF8(string(body), openAIMaxDebugChunkBytes)
		chunk := StreamLlmResponseChunk{
			LlmCallID:    llmCallID,
			ProviderKind: "openai",
			APIMode:      "chat_completions",
			Raw:          raw,
			StatusCode:   &status,
			Truncated:    bodyTruncated || rawTruncated,
		}
		if err := yield(chunk); err != nil {
			return err
		}
	}

	content, toolCalls, err := parseOpenAIChatCompletion(body)
	if err != nil {
		return yield(openAIParseFailure(err, "OpenAI 响应解析失败", "OpenAI tool_call 参数解析失败"))
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
		"model":  request.Model,
		"input":  input,
		"stream": true,
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
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI 请求构造失败",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(g.cfg.APIKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

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

	status := resp.StatusCode

	if status < 200 || status >= 300 {
		body, bodyTruncated, _ := readAllWithLimit(resp.Body, openAIMaxErrorBodyBytes)
		if g.cfg.EmitDebugEvents {
			raw, rawTruncated := truncateUTF8(string(body), openAIMaxDebugChunkBytes)
			chunk := StreamLlmResponseChunk{
				LlmCallID:    llmCallID,
				ProviderKind: "openai",
				APIMode:      "responses",
				Raw:          raw,
				StatusCode:   &status,
				Truncated:    bodyTruncated || rawTruncated,
			}
			_ = yield(chunk)
		}
		if allowFallback && isOpenAIResponsesNotSupported(status, body) {
			return &openAIResponsesNotSupportedError{StatusCode: status}
		}

		errClass := errorClassFromStatus(status)
		message, details := openAIErrorMessageAndDetails(body, status, "OpenAI 请求失败")
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: errClass,
				Message:    message,
				Details:    details,
			},
		})
	}

	if isEventStream(resp.Header.Get("Content-Type")) {
		return g.streamResponsesSSE(ctx, resp.Body, llmCallID, status, yield)
	}

	body, bodyTruncated, _ := readAllWithLimit(resp.Body, openAIMaxResponseBytes)
	if g.cfg.EmitDebugEvents {
		raw, rawTruncated := truncateUTF8(string(body), openAIMaxDebugChunkBytes)
		chunk := StreamLlmResponseChunk{
			LlmCallID:    llmCallID,
			ProviderKind: "openai",
			APIMode:      "responses",
			Raw:          raw,
			StatusCode:   &status,
			Truncated:    bodyTruncated || rawTruncated,
		}
		_ = yield(chunk)
	}

	content, toolCalls, err := parseOpenAIResponses(body)
	if err != nil {
		return yield(openAIParseFailure(err, "OpenAI responses 响应解析失败", "OpenAI responses tool_call 参数解析失败"))
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

type openAIChatCompletionStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   *string                         `json:"content"`
			Role      *string                         `json:"role"`
			ToolCalls []openAIChatCompletionToolDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

type openAIChatCompletionToolDelta struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIChatToolCallAccum struct {
	ID            string
	Name          string
	ArgumentParts []string
}

type openAIChatToolCallBuffer struct {
	calls map[int]*openAIChatToolCallAccum
}

func newOpenAIChatToolCallBuffer() *openAIChatToolCallBuffer {
	return &openAIChatToolCallBuffer{calls: map[int]*openAIChatToolCallAccum{}}
}

func (b *openAIChatToolCallBuffer) Add(delta openAIChatCompletionToolDelta, fallbackIndex int) {
	idx := fallbackIndex
	if delta.Index != nil {
		idx = *delta.Index
	}
	call, ok := b.calls[idx]
	if !ok {
		call = &openAIChatToolCallAccum{}
		b.calls[idx] = call
	}
	if value := strings.TrimSpace(delta.ID); value != "" {
		call.ID = value
	}
	if value := strings.TrimSpace(delta.Function.Name); value != "" {
		call.Name = value
	}
	if delta.Function.Arguments != "" {
		call.ArgumentParts = append(call.ArgumentParts, delta.Function.Arguments)
	}
}

func (b *openAIChatToolCallBuffer) Drain() ([]ToolCall, error) {
	if len(b.calls) == 0 {
		return nil, nil
	}

	indexes := make([]int, 0, len(b.calls))
	for idx := range b.calls {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)

	toolCalls := make([]ToolCall, 0, len(indexes))
	for _, idx := range indexes {
		item := b.calls[idx]
		if item == nil {
			continue
		}
		if strings.TrimSpace(item.ID) == "" {
			return nil, fmt.Errorf("tool_calls[%d] 缺少 id", idx)
		}
		if strings.TrimSpace(item.Name) == "" {
			return nil, fmt.Errorf("tool_calls[%d] 缺少 function.name", idx)
		}

		argumentsJSON := map[string]any{}
		joinedArgs := strings.TrimSpace(strings.Join(item.ArgumentParts, ""))
		if joinedArgs != "" {
			var parsedArgs any
			if err := json.Unmarshal([]byte(joinedArgs), &parsedArgs); err != nil {
				return nil, fmt.Errorf("%w: tool_calls[%d].function.arguments 不是合法 JSON", errOpenAIToolCallArguments, idx)
			}
			obj, ok := parsedArgs.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%w: tool_calls[%d].function.arguments 必须是 JSON object", errOpenAIToolCallArguments, idx)
			}
			argumentsJSON = obj
		}

		toolCalls = append(toolCalls, ToolCall{
			ToolCallID:    item.ID,
			ToolName:      item.Name,
			ArgumentsJSON: argumentsJSON,
		})
	}

	b.calls = map[int]*openAIChatToolCallAccum{}
	return toolCalls, nil
}

func (g *OpenAIGateway) streamChatCompletionsSSE(
	ctx context.Context,
	body io.Reader,
	llmCallID string,
	status int,
	yield func(StreamEvent) error,
) error {
	toolBuffer := newOpenAIChatToolCallBuffer()
	terminalEmitted := false
	var handlerFailed bool

	err := forEachSSEData(ctx, body, func(data string) (retErr error) {
		defer func() {
			if retErr != nil {
				handlerFailed = true
			}
		}()
		raw, rawTruncated := truncateUTF8(data, openAIMaxDebugChunkBytes)
		var chunkJSON any
		if strings.TrimSpace(data) != "" && data != "[DONE]" {
			_ = json.Unmarshal([]byte(data), &chunkJSON)
		}

		if g.cfg.EmitDebugEvents {
			chunk := StreamLlmResponseChunk{
				LlmCallID:    llmCallID,
				ProviderKind: "openai",
				APIMode:      "chat_completions",
				Raw:          raw,
				ChunkJSON:    chunkJSON,
				StatusCode:   &status,
				Truncated:    rawTruncated,
			}
			if err := yield(chunk); err != nil {
				return err
			}
		}

		if strings.TrimSpace(data) == "[DONE]" {
			calls, err := toolBuffer.Drain()
			if err != nil {
				return yield(openAIParseFailure(err, "OpenAI 响应解析失败", "OpenAI tool_call 参数解析失败"))
			}
			for _, call := range calls {
				if err := yield(call); err != nil {
					return err
				}
			}
			terminalEmitted = true
			return yield(StreamRunCompleted{})
		}

		var parsed openAIChatCompletionStreamChunk
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			return nil
		}
		if len(parsed.Choices) == 0 {
			return nil
		}
		choice := parsed.Choices[0]
		role := "assistant"
		if choice.Delta.Role != nil && strings.TrimSpace(*choice.Delta.Role) != "" {
			role = strings.TrimSpace(*choice.Delta.Role)
		}
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			if err := yield(StreamMessageDelta{ContentDelta: *choice.Delta.Content, Role: role}); err != nil {
				return err
			}
		}
		for idx, toolDelta := range choice.Delta.ToolCalls {
			toolBuffer.Add(toolDelta, idx)
		}

		if choice.FinishReason != nil && strings.EqualFold(strings.TrimSpace(*choice.FinishReason), "tool_calls") {
			calls, err := toolBuffer.Drain()
			if err != nil {
				return yield(openAIParseFailure(err, "OpenAI 响应解析失败", "OpenAI tool_call 参数解析失败"))
			}
			for _, call := range calls {
				if err := yield(call); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		if handlerFailed {
			return err
		}
		if terminalEmitted {
			return nil
		}
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassProviderRetryable,
				Message:    "SSE 流读取中断",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}
	if terminalEmitted {
		return nil
	}
	return yield(StreamRunFailed{Error: InternalStreamEndedError()})
}

func (g *OpenAIGateway) streamResponsesSSE(
	ctx context.Context,
	body io.Reader,
	llmCallID string,
	status int,
	yield func(StreamEvent) error,
) error {
	terminalEmitted := false
	var handlerFailed bool

	err := forEachSSEData(ctx, body, func(data string) (retErr error) {
		defer func() {
			if retErr != nil {
				handlerFailed = true
			}
		}()
		raw, rawTruncated := truncateUTF8(data, openAIMaxDebugChunkBytes)
		var chunkJSON any
		if strings.TrimSpace(data) != "" && data != "[DONE]" {
			_ = json.Unmarshal([]byte(data), &chunkJSON)
		}

		if g.cfg.EmitDebugEvents {
			chunk := StreamLlmResponseChunk{
				LlmCallID:    llmCallID,
				ProviderKind: "openai",
				APIMode:      "responses",
				Raw:          raw,
				ChunkJSON:    chunkJSON,
				StatusCode:   &status,
				Truncated:    rawTruncated,
			}
			if err := yield(chunk); err != nil {
				return err
			}
		}

		if strings.TrimSpace(data) == "[DONE]" {
			if terminalEmitted {
				return nil
			}
			terminalEmitted = true
			return yield(StreamRunFailed{Error: InternalStreamEndedError()})
		}

		var parsed any
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			return nil
		}
		root, ok := parsed.(map[string]any)
		if !ok {
			return nil
		}

		typ, _ := root["type"].(string)
		if delta := openAIResponsesDeltaText(root); delta != "" {
			if err := yield(StreamMessageDelta{ContentDelta: delta, Role: "assistant"}); err != nil {
				return err
			}
		}

		if typ == "response.completed" {
			respObj, _ := root["response"].(map[string]any)
			outputRaw, _ := respObj["output"].([]any)
			toolCalls, err := openAIResponsesToolCalls(outputRaw)
			if err != nil {
				return yield(openAIParseFailure(err, "OpenAI responses 响应解析失败", "OpenAI responses tool_call 参数解析失败"))
			}
			for _, call := range toolCalls {
				if err := yield(call); err != nil {
					return err
				}
			}
			terminalEmitted = true
			return yield(StreamRunCompleted{})
		}

		if typ == "response.failed" || typ == "response.error" {
			message := "OpenAI responses 失败"
			if errObj, ok := root["error"].(map[string]any); ok {
				if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
					message = strings.TrimSpace(msg)
				}
			}
			terminalEmitted = true
			return yield(StreamRunFailed{
				Error: GatewayError{
					ErrorClass: ErrorClassProviderNonRetryable,
					Message:    message,
				},
			})
		}

		if errObj, ok := root["error"].(map[string]any); ok && errObj != nil {
			message := "OpenAI responses 返回错误"
			if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
				message = strings.TrimSpace(msg)
			}
			terminalEmitted = true
			return yield(StreamRunFailed{
				Error: GatewayError{
					ErrorClass: ErrorClassProviderNonRetryable,
					Message:    message,
				},
			})
		}

		return nil
	})
	if err != nil {
		if handlerFailed {
			return err
		}
		if terminalEmitted {
			return nil
		}
		return yield(StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassProviderRetryable,
				Message:    "SSE 流读取中断",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}
	if terminalEmitted {
		return nil
	}
	return yield(StreamRunFailed{Error: InternalStreamEndedError()})
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

func openAIResponsesToolCalls(output []any) ([]ToolCall, error) {
	toolCalls := []ToolCall{}
	for idx, rawItem := range output {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		if typ, _ := item["type"].(string); typ != "function_call" {
			continue
		}

		toolCallID, _ := item["call_id"].(string)
		if strings.TrimSpace(toolCallID) == "" {
			toolCallID, _ = item["id"].(string)
		}
		toolCallID = strings.TrimSpace(toolCallID)
		if toolCallID == "" {
			return nil, fmt.Errorf("output[%d] 缺少 function_call.id", idx)
		}

		toolName, _ := item["name"].(string)
		if strings.TrimSpace(toolName) == "" {
			toolName, _ = item["tool_name"].(string)
		}
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			return nil, fmt.Errorf("output[%d] 缺少 function_call.name", idx)
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
						return nil, fmt.Errorf("%w: output[%d].arguments 不是合法 JSON", errOpenAIToolCallArguments, idx)
					}
					obj, ok := parsedArgs.(map[string]any)
					if !ok {
						return nil, fmt.Errorf("%w: output[%d].arguments 必须是 JSON object", errOpenAIToolCallArguments, idx)
					}
					argumentsJSON = obj
				}
			default:
				return nil, fmt.Errorf("%w: output[%d].arguments 类型不支持", errOpenAIToolCallArguments, idx)
			}
		}

		toolCalls = append(toolCalls, ToolCall{
			ToolCallID:    toolCallID,
			ToolName:      toolName,
			ArgumentsJSON: argumentsJSON,
		})
	}
	return toolCalls, nil
}

func openAIResponsesDeltaText(event map[string]any) string {
	typ, _ := event["type"].(string)
	if typ == "" || !strings.HasSuffix(typ, ".delta") {
		return ""
	}

	if rawDelta, ok := event["delta"].(string); ok {
		return rawDelta
	}

	deltaObj, ok := event["delta"].(map[string]any)
	if !ok {
		return ""
	}

	if value, ok := deltaObj["value"].(string); ok {
		return value
	}
	if value, ok := deltaObj["text"].(string); ok {
		return value
	}
	rawContent, ok := deltaObj["content"].([]any)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, rawItem := range rawContent {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		if txt, ok := item["text"].(string); ok {
			b.WriteString(txt)
			continue
		}
		txtObj, ok := item["text"].(map[string]any)
		if !ok {
			continue
		}
		if value, ok := txtObj["value"].(string); ok {
			b.WriteString(value)
		}
	}
	return b.String()
}

func openAIParseFailure(err error, message string, toolCallMessage string) StreamRunFailed {
	if errors.Is(err, errOpenAIToolCallArguments) {
		return StreamRunFailed{
			Error: GatewayError{
				ErrorClass: ErrorClassProviderNonRetryable,
				Message:    toolCallMessage,
				Details:    map[string]any{"reason": err.Error()},
			},
		}
	}
	return StreamRunFailed{
		Error: GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    message,
			Details:    map[string]any{"reason": err.Error()},
		},
	}
}

func openAIErrorMessageAndDetails(body []byte, status int, fallback string) (string, map[string]any) {
	details := map[string]any{"status_code": status}

	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fallback, details
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return fallback, details
	}
	errObj, ok := root["error"].(map[string]any)
	if !ok {
		return fallback, details
	}

	for _, key := range []string{"type", "code", "param"} {
		if value, exists := errObj[key]; exists && value != nil {
			switch casted := value.(type) {
			case string:
				if strings.TrimSpace(casted) != "" {
					details["openai_error_"+key] = strings.TrimSpace(casted)
				}
			case float64, bool, int:
				details["openai_error_"+key] = casted
			default:
				details["openai_error_"+key] = fmt.Sprintf("%v", casted)
			}
		}
	}

	if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg), details
	}
	return fallback, details
}

func isEventStream(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

func truncateUTF8(value string, maxBytes int) (string, bool) {
	if maxBytes <= 0 {
		return "", true
	}
	raw := []byte(value)
	if len(raw) <= maxBytes {
		return value, false
	}
	truncated := raw[:maxBytes]
	for len(truncated) > 0 && !utf8.Valid(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return string(truncated), true
}

func readAllWithLimit(r io.Reader, maxBytes int) ([]byte, bool, error) {
	if maxBytes <= 0 {
		maxBytes = openAIMaxErrorBodyBytes
	}
	limited := io.LimitReader(r, int64(maxBytes)+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	if len(body) <= maxBytes {
		return body, false, nil
	}
	return body[:maxBytes], true, nil
}

func forEachSSEData(ctx context.Context, r io.Reader, handle func(string) error) error {
	reader := bufio.NewReader(r)
	dataLines := []string{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}

		cleaned := strings.TrimRight(line, "\r\n")
		if cleaned == "" {
			if len(dataLines) > 0 {
				data := strings.Join(dataLines, "\n")
				dataLines = dataLines[:0]
				if err := handle(data); err != nil {
					return err
				}
			}
		} else if strings.HasPrefix(cleaned, ":") {
			// ignore
		} else if strings.HasPrefix(cleaned, "data:") {
			dataLines = append(dataLines, strings.TrimLeft(cleaned[len("data:"):], " "))
		}

		if err == io.EOF {
			break
		}
	}

	if len(dataLines) > 0 {
		if err := handle(strings.Join(dataLines, "\n")); err != nil {
			return err
		}
	}
	return nil
}
