package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	sharedoutbound "arkloop/services/shared/outboundurl"

	"arkloop/services/worker/internal/stablejson"
	"github.com/google/uuid"
)

type OpenAIGatewayConfig struct {
	APIKey          string
	BaseURL         string
	APIMode         string
	AdvancedJSON    map[string]any
	EmitDebugEvents bool
	TotalTimeout    time.Duration
}

type OpenAIGateway struct {
	cfg        OpenAIGatewayConfig
	client     *http.Client
	baseURLErr error
}

const (
	openAIMaxErrorBodyBytes  = 4096
	openAIMaxDebugChunkBytes = 8192
	openAIMaxResponseBytes   = 1024 * 1024
)

// critical fields denied in advanced_json to prevent overriding core request structure
var openAIAdvancedJSONDenylist = map[string]struct{}{
	"model":          {},
	"messages":       {},
	"input":          {},
	"stream":         {},
	"stream_options": {},
	"tools":          {},
	"tool_choice":    {},
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
	normalizedBaseURL, baseURLErr := sharedoutbound.DefaultPolicy().NormalizeBaseURL(baseURL)
	if baseURLErr == nil {
		baseURL = normalizedBaseURL
	}
	cfg.BaseURL = baseURL
	if strings.TrimSpace(cfg.APIMode) == "" {
		cfg.APIMode = "auto"
	}
	cfg.TotalTimeout = timeout
	if cfg.AdvancedJSON == nil {
		cfg.AdvancedJSON = map[string]any{}
	}
	return &OpenAIGateway{
		cfg:        cfg,
		client:     sharedoutbound.DefaultPolicy().NewHTTPClient(timeout),
		baseURLErr: baseURLErr,
	}
}

func (g *OpenAIGateway) Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error {
	if g.baseURLErr != nil {
		return yield(StreamRunFailed{Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: "OpenAI base_url blocked", Details: map[string]any{"reason": g.baseURLErr.Error()}}})
	}
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

	messagesPayload, err := toOpenAIChatMessages(request.Messages)
	if err != nil {
		return yield(StreamRunFailed{
		LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI chat messages construction failed",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}

	payload := map[string]any{
		"model":          request.Model,
		"messages":       messagesPayload,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
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
	for k := range g.cfg.AdvancedJSON {
		if _, denied := openAIAdvancedJSONDenylist[k]; denied {
			return yield(StreamRunFailed{
			LlmCallID: llmCallID,
				Error: GatewayError{
					ErrorClass: ErrorClassInternalError,
					Message:    fmt.Sprintf("advanced_json must not set critical field: %s", k),
					Details:    map[string]any{"denied_key": k},
				},
			})
		}
	}
	for k, v := range g.cfg.AdvancedJSON {
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}
	// reasoning_mode 控制是否发送 reasoning_effort 参数
	switch request.ReasoningMode {
	case "enabled":
		if _, ok := payload["reasoning_effort"]; !ok {
			payload["reasoning_effort"] = "medium"
		}
	case "disabled":
		delete(payload, "reasoning_effort")
	default: // "auto", "none", ""
		// AdvancedJSON 已注入时保留
	}

	baseURL := g.cfg.BaseURL
	path := "/chat/completions"
	stats := ComputeRequestStats(request)
	if err := yield(StreamLlmRequest{
		LlmCallID:          llmCallID,
		ProviderKind:       "openai",
		APIMode:            "chat_completions",
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
		failed := StreamRunFailed{
		LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI request serialization failed",
			},
		}
		return yield(failed)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.BaseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return yield(StreamRunFailed{
		LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI request construction failed",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(g.cfg.APIKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := g.client.Do(req)
	if err != nil {
		var denied sharedoutbound.DeniedError
		if errors.As(err, &denied) {
			failed := StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: "OpenAI base_url blocked", Details: map[string]any{"reason": denied.Error()}}}
			return yield(failed)
		}
		failed := StreamRunFailed{
		LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassProviderRetryable,
				Message:    "OpenAI network error",
			},
		}
		return yield(failed)
	}
	defer resp.Body.Close()

	status := resp.StatusCode
	if status < 200 || status >= 300 {
		body, bodyTruncated, _ := readAllWithLimit(resp.Body, openAIMaxErrorBodyBytes)
		message, details := openAIErrorMessageAndDetails(body, status, "OpenAI request failed")

		errClass := errorClassFromStatus(status)
		failed := StreamRunFailed{
		LlmCallID: llmCallID,
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

	content, toolCalls, usage, cost, err := parseOpenAIChatCompletion(body)
	if err != nil {
		return yield(openAIParseFailure(err, "OpenAI response parse failed", "OpenAI tool_call arguments parse failed", llmCallID))
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

	return yield(StreamRunCompleted{LlmCallID: llmCallID, Usage: usage, Cost: cost})
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
		LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI responses input construction failed",
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
	for k := range g.cfg.AdvancedJSON {
		if _, denied := openAIAdvancedJSONDenylist[k]; denied {
			return yield(StreamRunFailed{
			LlmCallID: llmCallID,
				Error: GatewayError{
					ErrorClass: ErrorClassInternalError,
					Message:    fmt.Sprintf("advanced_json must not set critical field: %s", k),
					Details:    map[string]any{"denied_key": k},
				},
			})
		}
	}
	for k, v := range g.cfg.AdvancedJSON {
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}
	// reasoning_mode 控制是否发送 reasoning 参数
	switch request.ReasoningMode {
	case "enabled":
		if rObj, ok := payload["reasoning"].(map[string]any); ok {
			if _, has := rObj["summary"]; !has {
				rObj["summary"] = "auto"
			}
		} else {
			payload["reasoning"] = map[string]any{"summary": "auto"}
		}
	case "disabled":
		delete(payload, "reasoning")
	default: // "auto", "none", ""
		// AdvancedJSON 已注入 reasoning 时，补全 summary
		if rObj, ok := payload["reasoning"].(map[string]any); ok {
			if _, has := rObj["summary"]; !has {
				rObj["summary"] = "auto"
			}
		}
	}

	baseURL := g.cfg.BaseURL
	path := "/responses"
	stats := ComputeRequestStats(request)
	if err := yield(StreamLlmRequest{
		LlmCallID:          llmCallID,
		ProviderKind:       "openai",
		APIMode:            "responses",
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
				Message:    "OpenAI request serialization failed",
			},
		})
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.BaseURL+"/responses", bytes.NewReader(encoded))
	if err != nil {
		return yield(StreamRunFailed{
		LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI request construction failed",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(g.cfg.APIKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := g.client.Do(req)
	if err != nil {
		var denied sharedoutbound.DeniedError
		if errors.As(err, &denied) {
			return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: "OpenAI base_url blocked", Details: map[string]any{"reason": denied.Error()}}})
		}
		return yield(StreamRunFailed{
		LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassProviderRetryable,
				Message:    "OpenAI network error",
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
		message, details := openAIErrorMessageAndDetails(body, status, "OpenAI request failed")
		return yield(StreamRunFailed{
		LlmCallID: llmCallID,
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

	content, toolCalls, usage, cost, err := parseOpenAIResponses(body)
	if err != nil {
		return yield(openAIParseFailure(err, "OpenAI responses response parse failed", "OpenAI responses tool_call arguments parse failed", llmCallID))
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

	return yield(StreamRunCompleted{LlmCallID: llmCallID, Usage: usage, Cost: cost})
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
			Content          *string                         `json:"content"`
			ReasoningContent *string                         `json:"reasoning_content"`
			Refusal          *string                         `json:"refusal"`
			Role             *string                         `json:"role"`
			ToolCalls        []openAIChatCompletionToolDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		Cost *float64 `json:"cost"`
	} `json:"usage"`
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
			return nil, fmt.Errorf("tool_calls[%d] missing id", idx)
		}
		if strings.TrimSpace(item.Name) == "" {
			return nil, fmt.Errorf("tool_calls[%d] missing function.name", idx)
		}

		argumentsJSON := map[string]any{}
		joinedArgs := strings.TrimSpace(strings.Join(item.ArgumentParts, ""))
		if joinedArgs != "" {
			var parsedArgs any
			if err := json.Unmarshal([]byte(joinedArgs), &parsedArgs); err != nil {
				return nil, fmt.Errorf("%w: tool_calls[%d].function.arguments is not valid JSON", errOpenAIToolCallArguments, idx)
			}
			obj, ok := parsedArgs.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%w: tool_calls[%d].function.arguments must be a JSON object", errOpenAIToolCallArguments, idx)
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
	var streamedUsage *Usage
	var streamedCost *Cost
	var inThink bool
	emittedAnyOutput := false
	emittedMainOutput := false
	emittedToolCall := false
	finishReasonSeen := false
	doneSeen := false
	chunkCount := 0
	choiceChunkCount := 0
	lastFinishReason := ""
	sawRoleDelta := false
	contentFiltered := false

	err := forEachSSEData(ctx, body, func(data string) (retErr error) {
		defer func() {
			if retErr != nil {
				handlerFailed = true
			}
		}()
		if terminalEmitted {
			return nil
		}
		if strings.TrimSpace(data) != "" && data != "[DONE]" {
			chunkCount++
		}
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
			if terminalEmitted {
				return nil
			}
			doneSeen = true
			calls, err := toolBuffer.Drain()
			if err != nil {
				return yield(openAIParseFailure(err, "OpenAI response parse failed", "OpenAI tool_call arguments parse failed", llmCallID))
			}
			for _, call := range calls {
				emittedToolCall = true
				if err := yield(call); err != nil {
					return err
				}
			}
			if !emittedMainOutput && !emittedToolCall {
				terminalEmitted = true
				if contentFiltered {
					return yield(StreamRunFailed{
					LlmCallID: llmCallID,
						Error: GatewayError{
							ErrorClass: ErrorClassPolicyDenied,
							Message:    "OpenAI content filtered",
						},
					})
				}
				details := map[string]any{
					"finish_reason_seen": finishReasonSeen,
					"done_seen":          doneSeen,
					"chunk_count":        chunkCount,
					"choice_chunk_count": choiceChunkCount,
					"last_finish_reason": lastFinishReason,
					"saw_role_delta":     sawRoleDelta,
				}
				if streamedUsage != nil {
					details["usage"] = streamedUsage.ToJSON()
				}
				// 模型有 reasoning/thinking 输出但无可见内容：属于 provider/model 异常行为，可重试。
				// 纯空响应（无任何输出）：仍为 internal.error。
				errClass := ErrorClassInternalError
				errMsg := "OpenAI stream completed without content"
				if emittedAnyOutput {
					errClass = ErrorClassProviderRetryable
					errMsg = "LLM generated only internal reasoning without visible output"
				}
				return yield(StreamRunFailed{
					LlmCallID: llmCallID,
					Error: GatewayError{
						ErrorClass: errClass,
						Message:    errMsg,
						Details:    details,
					},
					Usage: streamedUsage,
					Cost:  streamedCost,
				})
			}
			terminalEmitted = true
			return yield(StreamRunCompleted{LlmCallID: llmCallID, Usage: streamedUsage, Cost: streamedCost})
		}

		var parsed openAIChatCompletionStreamChunk
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			terminalEmitted = true
			return yield(StreamRunFailed{
				LlmCallID: llmCallID,
				Error: GatewayError{
					ErrorClass: ErrorClassInternalError,
					Message:    "OpenAI stream chunk parse failed",
					Details: map[string]any{
						"reason":          err.Error(),
						"chunk":           raw,
						"chunk_truncated": rawTruncated,
					},
				},
			})
		}
		// 不论 choices 是否为空，只要 chunk 携带了 usage 就捕获（取最后一次）
		// OpenRouter 等代理会把 usage 附在最后一个有 choices 的 chunk 上
		if parsed.Usage != nil {
			cached := 0
			if parsed.Usage.PromptTokensDetails != nil {
				cached = parsed.Usage.PromptTokensDetails.CachedTokens
			}
			streamedUsage = parseChatCompletionUsage(parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens, cached)
			streamedCost = costFromFloat64(parsed.Usage.Cost)
		}
		if len(parsed.Choices) == 0 {
			return nil
		}
		choiceChunkCount++
		choice := parsed.Choices[0]
		role := "assistant"
		if choice.Delta.Role != nil && strings.TrimSpace(*choice.Delta.Role) != "" {
			sawRoleDelta = true
			role = strings.TrimSpace(*choice.Delta.Role)
		}
		if choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "" {
			thinkingChannel := "thinking"
			emittedAnyOutput = true
			if err := yield(StreamMessageDelta{ContentDelta: *choice.Delta.ReasoningContent, Role: role, Channel: &thinkingChannel}); err != nil {
				return err
			}
		}
		if choice.Delta.Refusal != nil && *choice.Delta.Refusal != "" {
			emittedAnyOutput = true
			emittedMainOutput = true
			if err := yield(StreamMessageDelta{ContentDelta: *choice.Delta.Refusal, Role: role}); err != nil {
				return err
			}
		}
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			thinkingPart, mainPart := splitThinkContent(&inThink, *choice.Delta.Content)
			thinkingChannel := "thinking"
			if thinkingPart != "" {
				emittedAnyOutput = true
				if err := yield(StreamMessageDelta{ContentDelta: thinkingPart, Role: role, Channel: &thinkingChannel}); err != nil {
					return err
				}
			}
			if mainPart != "" {
				emittedAnyOutput = true
				emittedMainOutput = true
				if err := yield(StreamMessageDelta{ContentDelta: mainPart, Role: role}); err != nil {
					return err
				}
			}
		}
		for idx, toolDelta := range choice.Delta.ToolCalls {
			toolBuffer.Add(toolDelta, idx)
			if toolDelta.Function.Arguments != "" {
				if err := yield(ToolCallArgumentDelta{
					ToolCallIndex:  idx,
					ToolCallID:     toolDelta.ID,
					ToolName:       toolDelta.Function.Name,
					ArgumentsDelta: toolDelta.Function.Arguments,
				}); err != nil {
					return err
				}
			}
		}

		if choice.FinishReason != nil {
			reason := strings.TrimSpace(*choice.FinishReason)
			if reason != "" {
				finishReasonSeen = true
				lastFinishReason = reason
				if strings.EqualFold(reason, "content_filter") {
					contentFiltered = true
				}
				if strings.EqualFold(reason, "tool_calls") {
					calls, err := toolBuffer.Drain()
					if err != nil {
						return yield(openAIParseFailure(err, "OpenAI response parse failed", "OpenAI tool_call arguments parse failed", llmCallID))
					}
					for _, call := range calls {
						emittedToolCall = true
						if err := yield(call); err != nil {
							return err
						}
					}
				}
				// include_usage 会在 finish_reason 之后追加 usage-only chunk（choices 为空），此处不提前结束。
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
		// 已收到 finish_reason，后续流中断也视为完成（最多丢失 usage）。
		if finishReasonSeen {
			calls, drainErr := toolBuffer.Drain()
			if drainErr != nil {
				return yield(StreamRunFailed{
					LlmCallID: llmCallID,
					Error: GatewayError{
						ErrorClass: ErrorClassProviderRetryable,
						Message:    "SSE stream ended before tool_calls completed",
						Details:    map[string]any{"reason": drainErr.Error()},
					},
				})
			}
			for _, call := range calls {
				emittedToolCall = true
				if err := yield(call); err != nil {
					return err
				}
			}
			if !emittedMainOutput && !emittedToolCall {
				terminalEmitted = true
				if contentFiltered {
					return yield(StreamRunFailed{
					LlmCallID: llmCallID,
						Error: GatewayError{
							ErrorClass: ErrorClassPolicyDenied,
							Message:    "OpenAI content filtered",
						},
					})
				}
				details := map[string]any{
					"finish_reason_seen": finishReasonSeen,
					"done_seen":          doneSeen,
					"chunk_count":        chunkCount,
					"choice_chunk_count": choiceChunkCount,
					"last_finish_reason": lastFinishReason,
					"saw_role_delta":     sawRoleDelta,
				}
				if streamedUsage != nil {
					details["usage"] = streamedUsage.ToJSON()
				}
				// 模型有 reasoning/thinking 输出但无可见内容：属于 provider/model 异常行为，可重试。
				// 纯空响应（无任何输出）：仍为 internal.error。
				errClass := ErrorClassInternalError
				errMsg := "OpenAI stream ended without content"
				if emittedAnyOutput {
					errClass = ErrorClassProviderRetryable
					errMsg = "LLM generated only internal reasoning without visible output"
				}
				return yield(StreamRunFailed{
					LlmCallID: llmCallID,
					Error: GatewayError{
						ErrorClass: errClass,
						Message:    errMsg,
						Details:    details,
					},
				})
			}
			terminalEmitted = true
			return yield(StreamRunCompleted{LlmCallID: llmCallID, Usage: streamedUsage})
		}
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassProviderRetryable,
				Message:    "SSE stream read interrupted",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}
	if terminalEmitted {
		return nil
	}

	// 一些 OpenAI 兼容代理在流式结束时不会发送 [DONE]。
	// 尝试在 EOF 时回收未 drain 的 tool_calls，并视情况完成本次流。
	calls, drainErr := toolBuffer.Drain()
	if drainErr != nil {
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassProviderRetryable,
				Message:    "SSE stream ended before tool_calls completed",
				Details:    map[string]any{"reason": drainErr.Error()},
			},
		})
	}
	for _, call := range calls {
		emittedToolCall = true
		if err := yield(call); err != nil {
			return err
		}
	}
	if emittedMainOutput || emittedToolCall {
		terminalEmitted = true
		return yield(StreamRunCompleted{LlmCallID: llmCallID, Usage: streamedUsage})
	}
	if streamedUsage != nil || emittedAnyOutput || finishReasonSeen {
		if contentFiltered {
			return yield(StreamRunFailed{
				LlmCallID: llmCallID,
				Error: GatewayError{
					ErrorClass: ErrorClassPolicyDenied,
					Message:    "OpenAI content filtered",
				},
			})
		}
		details := map[string]any{
			"finish_reason_seen": finishReasonSeen,
			"done_seen":          doneSeen,
			"chunk_count":        chunkCount,
			"choice_chunk_count": choiceChunkCount,
			"last_finish_reason": lastFinishReason,
			"saw_role_delta":     sawRoleDelta,
		}
		if streamedUsage != nil {
			details["usage"] = streamedUsage.ToJSON()
		}
		// 模型有 reasoning/thinking 输出但无可见内容：属于 provider/model 异常行为，可重试。
		// 纯空响应（无任何输出）：仍为 internal.error。
		errClass := ErrorClassInternalError
		errMsg := "OpenAI stream completed without content"
		if emittedAnyOutput {
			errClass = ErrorClassProviderRetryable
			errMsg = "LLM generated only internal reasoning without visible output"
		}
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: errClass,
				Message:    errMsg,
				Details:    details,
			},
		})
	}
	return yield(StreamRunFailed{LlmCallID: llmCallID, Error: InternalStreamEndedError()})
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
		if terminalEmitted {
			return nil
		}
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
			return yield(StreamRunFailed{LlmCallID: llmCallID, Error: InternalStreamEndedError()})
		}

		var parsed any
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			terminalEmitted = true
			return yield(StreamRunFailed{
				LlmCallID: llmCallID,
				Error: GatewayError{
					ErrorClass: ErrorClassInternalError,
					Message:    "OpenAI responses stream chunk parse failed",
					Details: map[string]any{
						"reason":          err.Error(),
						"chunk":           raw,
						"chunk_truncated": rawTruncated,
					},
				},
			})
		}
		root, ok := parsed.(map[string]any)
		if !ok {
			return nil
		}

		typ, _ := root["type"].(string)
		if delta := openAIResponsesDeltaText(root); delta != "" {
			thinkingChannel := "thinking"
			if openAIResponsesIsReasoningDelta(typ) {
				if err := yield(StreamMessageDelta{ContentDelta: delta, Role: "assistant", Channel: &thinkingChannel}); err != nil {
					return err
				}
			} else {
				if err := yield(StreamMessageDelta{ContentDelta: delta, Role: "assistant"}); err != nil {
					return err
				}
			}
		}

		if typ == "response.completed" {
			respObj, _ := root["response"].(map[string]any)
			outputRaw, _ := respObj["output"].([]any)
			toolCalls, err := openAIResponsesToolCalls(outputRaw)
			if err != nil {
				return yield(openAIParseFailure(err, "OpenAI responses response parse failed", "OpenAI responses tool_call arguments parse failed", llmCallID))
			}
			for _, call := range toolCalls {
				if err := yield(call); err != nil {
					return err
				}
			}
			var usage *Usage
			var cost *Cost
			if usageObj, ok := respObj["usage"].(map[string]any); ok {
				usage = parseResponsesUsage(usageObj)
				cost = parseResponsesCost(usageObj)
			}
			terminalEmitted = true
			return yield(StreamRunCompleted{LlmCallID: llmCallID, Usage: usage, Cost: cost})
		}

		if typ == "response.failed" || typ == "response.error" {
			message := "OpenAI responses failed"
			if errObj, ok := root["error"].(map[string]any); ok {
				if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
					message = strings.TrimSpace(msg)
				}
			}
			terminalEmitted = true
			return yield(StreamRunFailed{
				LlmCallID: llmCallID,
				Error: GatewayError{
					ErrorClass: ErrorClassProviderNonRetryable,
					Message:    message,
				},
			})
		}

		if errObj, ok := root["error"].(map[string]any); ok && errObj != nil {
			message := "OpenAI responses returned error"
			if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
				message = strings.TrimSpace(msg)
			}
			terminalEmitted = true
			return yield(StreamRunFailed{
				LlmCallID: llmCallID,
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
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassProviderRetryable,
				Message:    "SSE stream read interrupted",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}
	if terminalEmitted {
		return nil
	}
	return yield(StreamRunFailed{LlmCallID: llmCallID, Error: InternalStreamEndedError()})
}

func toOpenAIChatMessages(messages []Message) ([]map[string]any, error) {
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

		contentBlocks, hasStructured, err := toOpenAIChatContentBlocks(message.Content)
		if err != nil {
			return nil, err
		}
		if hasStructured {
			out = append(out, map[string]any{"role": message.Role, "content": contentBlocks})
			continue
		}
		out = append(out, map[string]any{"role": message.Role, "content": text})
	}
	return out, nil
}

func joinParts(parts []ContentPart) string {
	chunks := make([]string, 0, len(parts))
	for _, part := range parts {
		if text := strings.TrimSpace(PartPromptText(part)); text != "" {
			chunks = append(chunks, PartPromptText(part))
		}
	}
	return strings.Join(chunks, "\n\n")
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
	Usage *struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		Cost *float64 `json:"cost"`
	} `json:"usage"`
}

func parseOpenAIChatCompletion(body []byte) (string, []ToolCall, *Usage, *Cost, error) {
	var parsed openAIChatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", nil, nil, nil, err
	}
	if len(parsed.Choices) == 0 {
		return "", nil, nil, nil, fmt.Errorf("response missing choices")
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
			return "", nil, nil, nil, fmt.Errorf("tool_calls[%d] missing id", idx)
		}

		toolName := strings.TrimSpace(raw.Function.Name)
		if toolName == "" {
			return "", nil, nil, nil, fmt.Errorf("tool_calls[%d] missing function.name", idx)
		}

		argumentsJSON := map[string]any{}
		arguments := strings.TrimSpace(raw.Function.Arguments)
		if arguments != "" {
			var parsedArgs any
			if err := json.Unmarshal([]byte(arguments), &parsedArgs); err != nil {
				return "", nil, nil, nil, fmt.Errorf("%w: tool_calls[%d].function.arguments is not valid JSON", errOpenAIToolCallArguments, idx)
			}
			obj, ok := parsedArgs.(map[string]any)
			if !ok {
				return "", nil, nil, nil, fmt.Errorf("%w: tool_calls[%d].function.arguments must be a JSON object", errOpenAIToolCallArguments, idx)
			}
			argumentsJSON = obj
		}

		toolCalls = append(toolCalls, ToolCall{
			ToolCallID:    toolCallID,
			ToolName:      toolName,
			ArgumentsJSON: argumentsJSON,
		})
	}

	var usage *Usage
	var cost *Cost
	if parsed.Usage != nil {
		cached := 0
		if parsed.Usage.PromptTokensDetails != nil {
			cached = parsed.Usage.PromptTokensDetails.CachedTokens
		}
		usage = parseChatCompletionUsage(parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens, cached)
		cost = costFromFloat64(parsed.Usage.Cost)
	}

	return content, toolCalls, usage, cost, nil
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
				return nil, fmt.Errorf("tool message is not valid JSON")
			}
			toolCallID, _ := parsed["tool_call_id"].(string)
			toolCallID = strings.TrimSpace(toolCallID)
			if toolCallID == "" {
				return nil, fmt.Errorf("tool message missing tool_call_id")
			}
			items = append(items, map[string]any{
				"type":    "function_call_output",
				"call_id": toolCallID,
				"output":  toolOutputTextFromEnvelope(parsed),
			})
			continue
		}

		contentBlocks, err := toOpenAIResponsesContentBlocks(message.Content, contentType)
		if err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"role":    message.Role,
			"content": contentBlocks,
		})
	}
	return items, nil
}

func toOpenAIChatContentBlocks(parts []ContentPart) ([]map[string]any, bool, error) {
	blocks := make([]map[string]any, 0, len(parts))
	hasStructured := false
	for _, part := range parts {
		switch part.Kind() {
		case "text":
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			blocks = append(blocks, map[string]any{"type": "text", "text": part.Text})
		case "file":
			text := PartPromptText(part)
			if strings.TrimSpace(text) == "" {
				continue
			}
			hasStructured = true
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		case "image":
			dataURL, err := partDataURL(part)
			if err != nil {
				return nil, false, err
			}
			hasStructured = true
			blocks = append(blocks, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": dataURL},
			})
		}
	}
	return blocks, hasStructured, nil
}

func toOpenAIResponsesContentBlocks(parts []ContentPart, contentType string) ([]map[string]any, error) {
	blocks := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part.Kind() {
		case "text":
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			blocks = append(blocks, map[string]any{"type": contentType, "text": part.Text})
		case "file":
			text := PartPromptText(part)
			if strings.TrimSpace(text) == "" {
				continue
			}
			blocks = append(blocks, map[string]any{"type": contentType, "text": text})
		case "image":
			dataURL, err := partDataURL(part)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, map[string]any{"type": "input_image", "image_url": dataURL})
		}
	}
	if len(blocks) == 0 {
		blocks = append(blocks, map[string]any{"type": contentType, "text": ""})
	}
	return blocks, nil
}

func partDataURL(part ContentPart) (string, error) {
	if part.Attachment == nil {
		return "", fmt.Errorf("image attachment is required")
	}
	if len(part.Data) == 0 {
		return "", fmt.Errorf("image attachment data is required")
	}
	mimeType := strings.TrimSpace(part.Attachment.MimeType)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(part.Data), nil
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
	result, hasResult := envelope["result"]
	errObj, hasErr := envelope["error"]

	// 用可读文本格式而非 JSON 对象传递 tool 错误，让 LLM 更易理解并正常响应。
	// JSON 格式的 {"error":{...}} 会导致部分模型产生 reasoning-only 响应（无可见输出）。
	if hasErr && errObj != nil {
		msg := extractToolErrorMessage(errObj)
		if hasResult && result != nil {
			encoded, err := stablejson.Encode(result)
			if err == nil && encoded != "" {
				return "Error: " + msg + "\nPartial result: " + encoded
			}
		}
		return "Error: " + msg
	}

	if hasResult && result != nil {
		encoded, err := stablejson.Encode(result)
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

// extractToolErrorMessage 从 tool error 对象中提取可读文本，供 toolOutputTextFromEnvelope 使用。
func extractToolErrorMessage(errObj any) string {
	if m, ok := errObj.(map[string]any); ok {
		msg, _ := m["message"].(string)
		cls, _ := m["error_class"].(string)
		if cls != "" && msg != "" {
			return "[" + cls + "] " + msg
		}
		if msg != "" {
			return msg
		}
	}
	encoded, err := stablejson.Encode(errObj)
	if err == nil && encoded != "" {
		return encoded
	}
	return "tool execution failed"
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
	// 包含 "responses" 且有任意错误指示词（含 OpenRouter 的 "invalid" 格式）
	if strings.Contains(rawBody, "responses") &&
		(strings.Contains(rawBody, "unknown") ||
			strings.Contains(rawBody, "not found") ||
			strings.Contains(rawBody, "invalid") ||
			strings.Contains(rawBody, "not supported") ||
			strings.Contains(rawBody, "unsupported")) {
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

func parseOpenAIResponses(body []byte) (string, []ToolCall, *Usage, *Cost, error) {
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", nil, nil, nil, err
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return "", nil, nil, nil, fmt.Errorf("response root is not an object")
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
			return contentBuilder.String(), nil, nil, nil, nil
		}
		return "", nil, nil, nil, fmt.Errorf("response missing output")
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
			return "", nil, nil, nil, fmt.Errorf("output[%d] missing function_call.id", idx)
		}

		toolName, _ := item["name"].(string)
		if strings.TrimSpace(toolName) == "" {
			toolName, _ = item["tool_name"].(string)
		}
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			return "", nil, nil, nil, fmt.Errorf("output[%d] missing function_call.name", idx)
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
						return "", nil, nil, nil, fmt.Errorf("%w: output[%d].arguments is not valid JSON", errOpenAIToolCallArguments, idx)
					}
					obj, ok := parsedArgs.(map[string]any)
					if !ok {
						return "", nil, nil, nil, fmt.Errorf("%w: output[%d].arguments must be a JSON object", errOpenAIToolCallArguments, idx)
					}
					argumentsJSON = obj
				}
			default:
				return "", nil, nil, nil, fmt.Errorf("%w: output[%d].arguments unsupported type", errOpenAIToolCallArguments, idx)
			}
		}

		toolCalls = append(toolCalls, ToolCall{
			ToolCallID:    toolCallID,
			ToolName:      toolName,
			ArgumentsJSON: argumentsJSON,
		})
	}

	var usage *Usage
	var cost *Cost
	if usageObj, ok := root["usage"].(map[string]any); ok {
		usage = parseResponsesUsage(usageObj)
		cost = parseResponsesCost(usageObj)
	}

	return contentBuilder.String(), toolCalls, usage, cost, nil
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
			return nil, fmt.Errorf("output[%d] missing function_call.id", idx)
		}

		toolName, _ := item["name"].(string)
		if strings.TrimSpace(toolName) == "" {
			toolName, _ = item["tool_name"].(string)
		}
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			return nil, fmt.Errorf("output[%d] missing function_call.name", idx)
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
						return nil, fmt.Errorf("%w: output[%d].arguments is not valid JSON", errOpenAIToolCallArguments, idx)
					}
					obj, ok := parsedArgs.(map[string]any)
					if !ok {
						return nil, fmt.Errorf("%w: output[%d].arguments must be a JSON object", errOpenAIToolCallArguments, idx)
					}
					argumentsJSON = obj
				}
			default:
				return nil, fmt.Errorf("%w: output[%d].arguments unsupported type", errOpenAIToolCallArguments, idx)
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

// openAIResponsesIsReasoningDelta 判断 responses API 事件是否为 reasoning（思考）类 delta。
// o3 系列模型使用 response.reasoning_summary_text.delta 等类型发出 reasoning 内容。
func openAIResponsesIsReasoningDelta(typ string) bool {
	return strings.HasPrefix(typ, "response.reasoning") && strings.HasSuffix(typ, ".delta")
}

// splitThinkContent 按 <think>/<think> 边界将一段 delta 拆分为 thinking 部分和 main 部分。
// inThink 为跨 chunk 的状态标志，函数会原地修改它。
// 不处理跨 chunk 的部分 tag（如 "<thi" + "nk>"），实践中 LLM 不会如此切割 tag。
func splitThinkContent(inThink *bool, delta string) (thinkingPart, mainPart string) {
	if *inThink {
		if idx := strings.Index(delta, "</think>"); idx >= 0 {
			thinkingPart = delta[:idx]
			mainPart = delta[idx+len("</think>"):]
			*inThink = false
		} else {
			thinkingPart = delta
		}
	} else {
		if idx := strings.Index(delta, "<think>"); idx >= 0 {
			mainPart = delta[:idx]
			rest := delta[idx+len("<think>"):]
			*inThink = true
			// rest 部分可能已含 </think>，递归处理一次
			tPart, mPart := splitThinkContent(inThink, rest)
			thinkingPart = tPart
			mainPart += mPart
		} else {
			mainPart = delta
		}
	}
	return
}

func openAIParseFailure(err error, message string, toolCallMessage string, llmCallID string) StreamRunFailed {
	if errors.Is(err, errOpenAIToolCallArguments) {
		return StreamRunFailed{
		LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassProviderNonRetryable,
				Message:    toolCallMessage,
				Details:    map[string]any{"reason": err.Error()},
			},
		}
	}
	return StreamRunFailed{
	LlmCallID: llmCallID,
		Error: GatewayError{
			ErrorClass: ErrorClassInternalError,
			Message:    message,
			Details:    map[string]any{"reason": err.Error()},
		},
	}
}

func openAIErrorMessageAndDetails(body []byte, status int, fallback string) (string, map[string]any) {
	details := map[string]any{"status_code": status}

	if len(body) == 0 {
		return fallback, details
	}

	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		raw, _ := truncateUTF8(string(body), 512)
		details["raw_body"] = raw
		return fallback, details
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		raw, _ := truncateUTF8(string(body), 512)
		details["raw_body"] = raw
		return fallback, details
	}
	errObj, ok := root["error"].(map[string]any)
	if !ok {
		raw, _ := truncateUTF8(string(body), 512)
		details["raw_body"] = raw
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

func parseChatCompletionUsage(promptTokens, completionTokens, cachedTokens int) *Usage {
	if promptTokens == 0 && completionTokens == 0 {
		return nil
	}
	u := &Usage{}
	if promptTokens > 0 {
		u.InputTokens = &promptTokens
	}
	if completionTokens > 0 {
		u.OutputTokens = &completionTokens
	}
	if cachedTokens > 0 {
		u.CachedTokens = &cachedTokens
	}
	return u
}

func parseResponsesUsage(usageObj map[string]any) *Usage {
	input, hasInput := usageObj["input_tokens"].(float64)
	output, hasOutput := usageObj["output_tokens"].(float64)
	if !hasInput && !hasOutput {
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
	// OpenAI Responses API: input_tokens_details.cached_tokens
	if details, ok := usageObj["input_tokens_details"].(map[string]any); ok {
		if cached, ok := details["cached_tokens"].(float64); ok && cached > 0 {
			cv := int(cached)
			u.CachedTokens = &cv
		}
	}
	return u
}

func parseResponsesCost(usageObj map[string]any) *Cost {
	if usageObj == nil {
		return nil
	}
	raw, ok := usageObj["cost"]
	if !ok {
		return nil
	}
	switch value := raw.(type) {
	case float64:
		return costFromFloat64(&value)
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return nil
		}
		return costFromFloat64(&parsed)
	default:
		return nil
	}
}

func costFromFloat64(value *float64) *Cost {
	if value == nil || *value <= 0 {
		return nil
	}
	return &Cost{
		Currency:     "USD",
		AmountMicros: int(math.Round(*value * 1_000_000)),
	}
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
