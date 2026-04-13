package llm

import (
	"bufio"
	"bytes"
	"context"
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

	"arkloop/services/worker/internal/stablejson"
	"github.com/google/uuid"
)

type OpenAIGatewayConfig struct {
	Transport       TransportConfig
	Protocol        OpenAIProtocolConfig
	APIKey          string
	BaseURL         string
	APIMode         string
	AdvancedJSON    map[string]any
	EmitDebugEvents bool
	TotalTimeout    time.Duration
}

type OpenAIGateway struct {
	cfg       OpenAIGatewayConfig
	transport protocolTransport
	protocol  OpenAIProtocolConfig
	configErr error
}

const (
	openAIMaxErrorBodyBytes  = 4096
	openAIMaxDebugChunkBytes = 8192
	openAIMaxResponseBytes   = 1024 * 1024
)

// critical fields denied in advanced_json to prevent overriding core request structure
var openAIAdvancedJSONDenylist = map[string]struct{}{
	"model":          {},
	"instructions":   {},
	"messages":       {},
	"input":          {},
	"stream":         {},
	"stream_options": {},
	"tools":          {},
	"tool_choice":    {},
}

func NewOpenAIGateway(cfg OpenAIGatewayConfig) *OpenAIGateway {
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
		transport.EmitDebugEvents = cfg.EmitDebugEvents
	}
	if transport.MaxResponseBytes <= 0 {
		transport.MaxResponseBytes = openAIMaxResponseBytes
	}

	protocol := cfg.Protocol
	var configErr error
	if protocol.PrimaryKind == "" {
		protocol, configErr = parseOpenAIProtocolConfig(cfg.APIMode, cfg.AdvancedJSON)
	}

	normalizedTransport := newProtocolTransport(transport, "https://api.openai.com/v1", nil)
	cfg.Transport = normalizedTransport.cfg
	cfg.Protocol = protocol
	cfg.EmitDebugEvents = normalizedTransport.cfg.EmitDebugEvents
	cfg.TotalTimeout = normalizedTransport.cfg.TotalTimeout
	cfg.BaseURL = normalizedTransport.cfg.BaseURL

	return &OpenAIGateway{
		cfg:       cfg,
		transport: normalizedTransport,
		protocol:  protocol,
		configErr: configErr,
	}
}

func (g *OpenAIGateway) ProtocolKind() ProtocolKind {
	if g.protocol.PrimaryKind == "" {
		return ProtocolKindOpenAIResponses
	}
	return g.protocol.PrimaryKind
}

func (g *OpenAIGateway) Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error {
	if g.configErr != nil {
		ge := GatewayError{ErrorClass: ErrorClassInternalError, Message: g.configErr.Error()}
		var cfgErr protocolConfigError
		if errors.As(g.configErr, &cfgErr) && len(cfgErr.Details) > 0 {
			ge.Details = cfgErr.Details
		}
		return yield(StreamRunFailed{Error: ge})
	}
	if g.transport.baseURLErr != nil {
		return yield(StreamRunFailed{Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: "OpenAI base_url blocked", Details: map[string]any{"reason": g.transport.baseURLErr.Error()}}})
	}

	if g.protocol.PrimaryKind == ProtocolKindOpenAIChatCompletions {
		return g.chatCompletions(ctx, request, yield)
	}

	if g.protocol.FallbackKind == nil {
		return g.responses(ctx, request, yield, false)
	}

	if err := g.responses(ctx, request, yield, true); err != nil {
		var notSupported *openAIResponsesNotSupportedError
		if errors.As(err, &notSupported) && *g.protocol.FallbackKind == ProtocolKindOpenAIChatCompletions {
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
		payload["tool_choice"] = openAIToolChoice(request.ToolChoice)
	}
	for k, v := range g.protocol.AdvancedPayloadJSON {
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}
	applyOpenAIChatReasoningMode(payload, request.ReasoningMode)

	baseURL := g.transport.cfg.BaseURL
	path := "/chat/completions"
	stats := ComputeRequestStats(request)
	debugPayload, redactedHints := sanitizeDebugPayloadJSON(payload)
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
	networkAttempted := false
	requestEvent := StreamLlmRequest{
		LlmCallID:            llmCallID,
		ProviderKind:         "openai",
		APIMode:              "chat_completions",
		BaseURL:              &baseURL,
		Path:                 &path,
		InputJSON:            request.ToJSON(),
		PayloadJSON:          debugPayload,
		RedactedHints:        redactedHints,
		SystemBytes:          stats.SystemBytes,
		ToolsBytes:           stats.ToolsBytes,
		MessagesBytes:        stats.MessagesBytes,
		AbstractRequestBytes: stats.AbstractRequestBytes,
		ProviderPayloadBytes: len(encoded),
		ImagePartCount:       stats.ImagePartCount,
		Base64ImageBytes:     stats.Base64ImageBytes,
		NetworkAttempted:     &networkAttempted,
		RoleBytes:            stats.RoleBytes,
		ToolSchemaBytesMap:   stats.ToolSchemaBytesMap,
		StablePrefixHash:     stats.StablePrefixHash,
	}
	if RequestPayloadTooLarge(len(encoded)) {
		if err := yield(requestEvent); err != nil {
			return err
		}
		return yield(PreflightOversizeFailure(llmCallID, len(encoded)))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.transport.endpoint("/chat/completions"), bytes.NewReader(encoded))
	if err != nil {
		if err := yield(requestEvent); err != nil {
			return err
		}
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI request construction failed",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}
	networkAttempted = true
	if err := yield(requestEvent); err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(g.transport.cfg.APIKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	g.transport.applyDefaultHeaders(req)

	resp, err := g.transport.client.Do(req)
	if err != nil {
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
		if status == http.StatusRequestEntityTooLarge {
			details["network_attempted"] = true
			details = OversizeFailureDetails(len(encoded), OversizePhaseProvider, details)
		}

		errClass := errorClassFromStatus(status)
		failed := StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: errClass,
				Message:    message,
				Details:    details,
			},
		}
		if g.transport.cfg.EmitDebugEvents {
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
	if g.transport.cfg.EmitDebugEvents {
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

	instructions, inputMessages := splitOpenAIResponsesInstructions(request.Messages)
	input, err := toOpenAIResponsesInput(inputMessages)
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
	if instructions != "" {
		payload["instructions"] = instructions
	}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	if request.MaxOutputTokens != nil {
		payload["max_output_tokens"] = *request.MaxOutputTokens
	}
	if len(request.Tools) > 0 {
		payload["tools"] = toOpenAIResponsesTools(request.Tools)
		payload["tool_choice"] = openAIToolChoice(request.ToolChoice)
	}
	for k, v := range g.protocol.AdvancedPayloadJSON {
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}
	applyOpenAIResponsesReasoningMode(payload, request.ReasoningMode)

	baseURL := g.transport.cfg.BaseURL
	path := "/responses"
	stats := ComputeRequestStats(request)
	debugPayload, redactedHints := sanitizeDebugPayloadJSON(payload)
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
	networkAttempted := false
	requestEvent := StreamLlmRequest{
		LlmCallID:            llmCallID,
		ProviderKind:         "openai",
		APIMode:              "responses",
		BaseURL:              &baseURL,
		Path:                 &path,
		PayloadJSON:          debugPayload,
		RedactedHints:        redactedHints,
		SystemBytes:          stats.SystemBytes,
		ToolsBytes:           stats.ToolsBytes,
		MessagesBytes:        stats.MessagesBytes,
		AbstractRequestBytes: stats.AbstractRequestBytes,
		ProviderPayloadBytes: len(encoded),
		ImagePartCount:       stats.ImagePartCount,
		Base64ImageBytes:     stats.Base64ImageBytes,
		NetworkAttempted:     &networkAttempted,
		RoleBytes:            stats.RoleBytes,
		ToolSchemaBytesMap:   stats.ToolSchemaBytesMap,
		StablePrefixHash:     stats.StablePrefixHash,
	}
	if RequestPayloadTooLarge(len(encoded)) {
		if err := yield(requestEvent); err != nil {
			return err
		}
		return yield(PreflightOversizeFailure(llmCallID, len(encoded)))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.transport.endpoint("/responses"), bytes.NewReader(encoded))
	if err != nil {
		if err := yield(requestEvent); err != nil {
			return err
		}
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "OpenAI request construction failed",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}
	networkAttempted = true
	if err := yield(requestEvent); err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(g.transport.cfg.APIKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	g.transport.applyDefaultHeaders(req)

	resp, err := g.transport.client.Do(req)
	if err != nil {
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
		if g.transport.cfg.EmitDebugEvents {
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
		if status == http.StatusRequestEntityTooLarge {
			details["network_attempted"] = true
			details = OversizeFailureDetails(len(encoded), OversizePhaseProvider, details)
		}
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
	if g.transport.cfg.EmitDebugEvents {
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

func openAIReasoningEffort(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "enabled":
		return "medium", true
	case "none", "off":
		return "none", true
	case "minimal", "low", "medium", "high", "xhigh":
		return strings.ToLower(strings.TrimSpace(mode)), true
	case "max", "maximum", "extra_high", "extra-high", "extra high":
		return "xhigh", true
	default:
		return "", false
	}
}

func openAIReasoningDisabled(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "disabled":
		return true
	default:
		return false
	}
}

func applyOpenAIChatReasoningMode(payload map[string]any, mode string) {
	if effort, ok := openAIReasoningEffort(mode); ok {
		payload["reasoning_effort"] = effort
		return
	}
	if openAIReasoningDisabled(mode) {
		delete(payload, "reasoning_effort")
	}
}

func applyOpenAIResponsesReasoningMode(payload map[string]any, mode string) {
	if effort, ok := openAIReasoningEffort(mode); ok {
		rObj := map[string]any{}
		if existing, ok := payload["reasoning"].(map[string]any); ok {
			for key, value := range existing {
				rObj[key] = value
			}
		}
		rObj["effort"] = effort
		if effort == "none" {
			delete(rObj, "summary")
		} else {
			if _, has := rObj["summary"]; !has {
				rObj["summary"] = "auto"
			}
		}
		payload["reasoning"] = rObj
		return
	}
	if openAIReasoningDisabled(mode) {
		delete(payload, "reasoning")
		return
	}
	if rObj, ok := payload["reasoning"].(map[string]any); ok {
		if _, has := rObj["summary"]; !has {
			rObj["summary"] = "auto"
		}
	}
}

type openAIChatCompletionStreamChunk struct {
	Error *struct {
		Message  string         `json:"message"`
		Type     string         `json:"type"`
		Code     any            `json:"code"`
		Metadata map[string]any `json:"metadata"`
	} `json:"error"`
	Choices []struct {
		Delta struct {
			Content          *string                         `json:"content"`
			ReasoningContent *string                         `json:"reasoning_content"`
			Reasoning        *string                         `json:"reasoning"`
			ReasoningDetails json.RawMessage                 `json:"reasoning_details"`
			Obfuscation      *string                         `json:"obfuscation"`
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
			ToolName:      CanonicalToolName(item.Name),
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
	reasoningAliasChunkCount := 0
	reasoningDetailsChunkCount := 0
	obfuscationChunkCount := 0

	err := forEachSSEData(ctx, body, streamActivityMarker(ctx), func(data string) (retErr error) {
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

		if g.transport.cfg.EmitDebugEvents {
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
				details := openAIChatStreamFailureDetails(
					finishReasonSeen,
					doneSeen,
					chunkCount,
					choiceChunkCount,
					lastFinishReason,
					sawRoleDelta,
					reasoningAliasChunkCount,
					reasoningDetailsChunkCount,
					obfuscationChunkCount,
				)
				if streamedUsage != nil {
					details["usage"] = streamedUsage.ToJSON()
				}
				errClass, errMsg := openAIChatEmptyStreamFailure(emittedAnyOutput, choiceChunkCount, sawRoleDelta, finishReasonSeen)
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
		if parsed.Error != nil {
			terminalEmitted = true
			details := map[string]any{}
			if parsed.Error.Message != "" {
				details["provider_message"] = parsed.Error.Message
			}
			if parsed.Error.Type != "" {
				details["type"] = parsed.Error.Type
			}
			if parsed.Error.Code != nil {
				details["code"] = parsed.Error.Code
			}
			if len(parsed.Error.Metadata) > 0 {
				details["metadata"] = parsed.Error.Metadata
			}
			return yield(StreamRunFailed{
				LlmCallID: llmCallID,
				Error: GatewayError{
					ErrorClass: ErrorClassProviderRetryable,
					Message:    "OpenAI stream returned error",
					Details:    details,
				},
				Usage: streamedUsage,
				Cost:  streamedCost,
			})
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
		if choice.Delta.Reasoning != nil && *choice.Delta.Reasoning != "" {
			reasoningAliasChunkCount++
			thinkingChannel := "thinking"
			emittedAnyOutput = true
			if err := yield(StreamMessageDelta{ContentDelta: *choice.Delta.Reasoning, Role: role, Channel: &thinkingChannel}); err != nil {
				return err
			}
		}
		if hasOpenAIReasoningDetails(choice.Delta.ReasoningDetails) {
			reasoningDetailsChunkCount++
			emittedAnyOutput = true
		}
		if choice.Delta.Obfuscation != nil && *choice.Delta.Obfuscation != "" {
			obfuscationChunkCount++
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
					ToolName:       CanonicalToolName(toolDelta.Function.Name),
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
				details := openAIChatStreamFailureDetails(
					finishReasonSeen,
					doneSeen,
					chunkCount,
					choiceChunkCount,
					lastFinishReason,
					sawRoleDelta,
					reasoningAliasChunkCount,
					reasoningDetailsChunkCount,
					obfuscationChunkCount,
				)
				if streamedUsage != nil {
					details["usage"] = streamedUsage.ToJSON()
				}
				errClass, errMsg := openAIChatEmptyStreamFailure(emittedAnyOutput, choiceChunkCount, sawRoleDelta, finishReasonSeen)
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
		details := openAIChatStreamFailureDetails(
			finishReasonSeen,
			doneSeen,
			chunkCount,
			choiceChunkCount,
			lastFinishReason,
			sawRoleDelta,
			reasoningAliasChunkCount,
			reasoningDetailsChunkCount,
			obfuscationChunkCount,
		)
		if streamedUsage != nil {
			details["usage"] = streamedUsage.ToJSON()
		}
		errClass, errMsg := openAIChatEmptyStreamFailure(emittedAnyOutput, choiceChunkCount, sawRoleDelta, finishReasonSeen)
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: errClass,
				Message:    errMsg,
				Details:    details,
			},
		})
	}
	return yield(StreamRunFailed{LlmCallID: llmCallID, Error: RetryableStreamEndedError()})
}

func openAIChatEmptyStreamFailure(emittedAnyOutput bool, choiceChunkCount int, sawRoleDelta bool, finishReasonSeen bool) (string, string) {
	// 模型吐出了 thinking，或者至少开始了 choice/role 流，但最终没有任何可见内容，
	// 这更像 provider/model 侧的异常输出，应该允许 agent loop 自动重试。
	if emittedAnyOutput {
		return ErrorClassProviderRetryable, "LLM generated only internal reasoning without visible output"
	}
	if choiceChunkCount > 0 || sawRoleDelta || finishReasonSeen {
		return ErrorClassProviderRetryable, "OpenAI stream emitted metadata without visible output"
	}
	return ErrorClassInternalError, "OpenAI stream completed without content"
}

func openAIChatStreamFailureDetails(
	finishReasonSeen bool,
	doneSeen bool,
	chunkCount int,
	choiceChunkCount int,
	lastFinishReason string,
	sawRoleDelta bool,
	reasoningAliasChunkCount int,
	reasoningDetailsChunkCount int,
	obfuscationChunkCount int,
) map[string]any {
	return map[string]any{
		"finish_reason_seen":            finishReasonSeen,
		"done_seen":                     doneSeen,
		"chunk_count":                   chunkCount,
		"choice_chunk_count":            choiceChunkCount,
		"last_finish_reason":            lastFinishReason,
		"saw_role_delta":                sawRoleDelta,
		"reasoning_alias_chunk_count":   reasoningAliasChunkCount,
		"reasoning_details_chunk_count": reasoningDetailsChunkCount,
		"obfuscation_chunk_count":       obfuscationChunkCount,
	}
}

func hasOpenAIReasoningDetails(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null" && trimmed != "[]" && trimmed != "{}"
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
	toolBuffers := map[int]*openAIResponsesToolBuffer{}
	toolBufferByItemID := map[string]*openAIResponsesToolBuffer{}
	emittedTextOutput := false
	emittedToolDelta := false

	err := forEachSSEData(ctx, body, streamActivityMarker(ctx), func(data string) (retErr error) {
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

		if g.transport.cfg.EmitDebugEvents {
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
			calls, err := openAIResponsesBufferedToolCalls(toolBuffers)
			if err != nil {
				terminalEmitted = true
				return yield(openAIParseFailure(err, "OpenAI responses response parse failed", "OpenAI responses tool_call arguments parse failed", llmCallID))
			}
			for _, call := range calls {
				if err := yield(call); err != nil {
					return err
				}
			}
			if emittedTextOutput || emittedToolDelta || len(calls) > 0 {
				terminalEmitted = true
				return yield(StreamRunCompleted{LlmCallID: llmCallID})
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
				emittedTextOutput = true
				if err := yield(StreamMessageDelta{ContentDelta: delta, Role: "assistant"}); err != nil {
					return err
				}
			}
		}
		if delta := openAIResponsesToolArgumentsDelta(root, toolBuffers, toolBufferByItemID); delta != nil {
			emittedToolDelta = true
			if err := yield(*delta); err != nil {
				return err
			}
		}

		if typ == "response.completed" {
			respObj, _ := root["response"].(map[string]any)
			assistantMessage, toolCalls, usage, cost, err := parseOpenAIResponsesAssistantResponse(respObj)
			if err != nil {
				return yield(openAIParseFailure(err, "OpenAI responses response parse failed", "OpenAI responses tool_call arguments parse failed", llmCallID))
			}
			content := VisibleMessageText(assistantMessage)
			if content != "" && !emittedTextOutput {
				emittedTextOutput = true
				if err := yield(StreamMessageDelta{ContentDelta: content, Role: "assistant"}); err != nil {
					return err
				}
			}
			for _, call := range toolCalls {
				if err := yield(call); err != nil {
					return err
				}
			}
			terminalEmitted = true
			return yield(StreamRunCompleted{
				LlmCallID:        llmCallID,
				Usage:            usage,
				Cost:             cost,
				AssistantMessage: &assistantMessage,
			})
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
	calls, err := openAIResponsesBufferedToolCalls(toolBuffers)
	if err != nil {
		return yield(openAIParseFailure(err, "OpenAI responses response parse failed", "OpenAI responses tool_call arguments parse failed", llmCallID))
	}
	for _, call := range calls {
		if err := yield(call); err != nil {
			return err
		}
	}
	if emittedTextOutput || emittedToolDelta || len(calls) > 0 {
		return yield(StreamRunCompleted{LlmCallID: llmCallID})
	}
	return yield(StreamRunFailed{LlmCallID: llmCallID, Error: RetryableStreamEndedError()})
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
			base := toOpenAIToolMessage(text)
			imageParts := collectImageBlocks(message.Content)
			if len(imageParts) == 0 {
				out = append(out, base)
				continue
			}
			contentArr := []map[string]any{
				{"type": "text", "text": base["content"]},
			}
			contentArr = append(contentArr, imageParts...)
			base["content"] = contentArr
			out = append(out, base)
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

func openAIToolChoice(tc *ToolChoice) any {
	if tc == nil {
		return "auto"
	}
	switch tc.Mode {
	case "required":
		return "required"
	case "specific":
		return map[string]any{
			"type":     "function",
			"function": map[string]any{"name": CanonicalToolName(tc.ToolName)},
		}
	default:
		return "auto"
	}
}

func toOpenAITools(specs []ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		name := CanonicalToolName(spec.Name)
		if name == "" {
			name = spec.Name
		}
		fn := map[string]any{
			"name":       name,
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
		name := CanonicalToolName(spec.Name)
		if name == "" {
			name = spec.Name
		}
		payload := map[string]any{
			"type":       "function",
			"name":       name,
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
			ToolName:      CanonicalToolName(toolName),
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
	for index, message := range messages {
		text := joinParts(message.Content)
		if message.Role == "assistant" {
			assistantItems, err := toOpenAIResponsesAssistantItems(message, index)
			if err != nil {
				return nil, err
			}
			items = append(items, assistantItems...)
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
			if imgBlocks, err := toOpenAIResponsesImageBlocks(message.Content); err != nil {
				return nil, err
			} else if len(imgBlocks) > 0 {
				items = append(items, map[string]any{
					"type":    "message",
					"role":    "user",
					"content": imgBlocks,
				})
			}
			continue
		}

		contentBlocks, err := toOpenAIResponsesContentBlocks(message.Content)
		if err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"type":    "message",
			"role":    strings.TrimSpace(message.Role),
			"content": contentBlocks,
		})
	}
	return items, nil
}

func splitOpenAIResponsesInstructions(messages []Message) (string, []Message) {
	instructions := make([]string, 0, 1)
	filtered := make([]Message, 0, len(messages))
	for _, message := range messages {
		if strings.TrimSpace(message.Role) != "system" {
			filtered = append(filtered, message)
			continue
		}
		text := strings.TrimSpace(joinParts(message.Content))
		if text != "" {
			instructions = append(instructions, text)
		}
	}
	return strings.Join(instructions, "\n\n"), filtered
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
			if text := openAIImageAttachmentKeyText(part); text != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": text})
			}
			blocks = append(blocks, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": dataURL},
			})
		}
	}
	return blocks, hasStructured, nil
}

func toOpenAIResponsesAssistantItems(message Message, index int) ([]map[string]any, error) {
	items := make([]map[string]any, 0, len(message.ToolCalls)+1)
	contentBlocks := toOpenAIResponsesAssistantContentBlocks(message.Content)
	if len(contentBlocks) > 0 {
		item := map[string]any{
			"id":      fmt.Sprintf("msg_hist_%d", index),
			"type":    "message",
			"role":    "assistant",
			"status":  "completed",
			"content": contentBlocks,
		}
		if message.Phase != nil && strings.TrimSpace(*message.Phase) != "" {
			item["phase"] = strings.TrimSpace(*message.Phase)
		}
		items = append(items, item)
	}
	for callIndex, call := range message.ToolCalls {
		call = CanonicalToolCall(call)
		argumentsJSON, err := stablejson.Encode(mapOrEmpty(call.ArgumentsJSON))
		if err != nil {
			argumentsJSON = "{}"
		}
		itemID := strings.TrimSpace(call.ToolCallID)
		if itemID == "" {
			itemID = fmt.Sprintf("fc_hist_%d_%d", index, callIndex)
		}
		callID := strings.TrimSpace(call.ToolCallID)
		if callID == "" {
			callID = itemID
		}
		items = append(items, map[string]any{
			"id":        itemID,
			"type":      "function_call",
			"call_id":   callID,
			"name":      call.ToolName,
			"arguments": argumentsJSON,
			"status":    "completed",
		})
	}
	return items, nil
}

func toOpenAIResponsesAssistantContentBlocks(parts []ContentPart) []map[string]any {
	blocks := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part.Kind() {
		case "text":
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			blocks = append(blocks, map[string]any{
				"type":        "output_text",
				"text":        part.Text,
				"annotations": []any{},
			})
		case "file":
			text := PartPromptText(part)
			if strings.TrimSpace(text) == "" {
				continue
			}
			blocks = append(blocks, map[string]any{
				"type":        "output_text",
				"text":        text,
				"annotations": []any{},
			})
		}
	}
	return blocks
}

func toOpenAIResponsesContentBlocks(parts []ContentPart) ([]map[string]any, error) {
	blocks := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part.Kind() {
		case "text":
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			blocks = append(blocks, map[string]any{"type": "input_text", "text": part.Text})
		case "file":
			text := PartPromptText(part)
			if strings.TrimSpace(text) == "" {
				continue
			}
			blocks = append(blocks, map[string]any{"type": "input_text", "text": text})
		case "image":
			dataURL, err := partDataURL(part)
			if err != nil {
				return nil, err
			}
			if text := openAIImageAttachmentKeyText(part); text != "" {
				blocks = append(blocks, map[string]any{"type": "input_text", "text": text})
			}
			blocks = append(blocks, map[string]any{"type": "input_image", "image_url": dataURL})
		}
	}
	if len(blocks) == 0 {
		blocks = append(blocks, map[string]any{"type": "input_text", "text": ""})
	}
	return blocks, nil
}

func openAIImageAttachmentKeyText(part ContentPart) string {
	if part.Attachment == nil {
		return ""
	}
	key := strings.TrimSpace(part.Attachment.Key)
	if key == "" {
		return ""
	}
	return "[attachment_key:" + key + "]"
}

func collectImageBlocks(parts []ContentPart) []map[string]any {
	var blocks []map[string]any
	for _, part := range parts {
		if part.Kind() != "image" {
			continue
		}
		dataURL, err := partDataURL(part)
		if err != nil {
			continue
		}
		blocks = append(blocks, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": dataURL},
		})
	}
	return blocks
}

func toOpenAIResponsesImageBlocks(parts []ContentPart) ([]map[string]any, error) {
	blocks := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		if part.Kind() != "image" {
			continue
		}
		dataURL, err := partDataURL(part)
		if err != nil {
			return nil, err
		}
		if text := openAIImageAttachmentKeyText(part); text != "" {
			blocks = append(blocks, map[string]any{"type": "input_text", "text": text})
		}
		blocks = append(blocks, map[string]any{"type": "input_image", "image_url": dataURL})
	}
	return blocks, nil
}

func partDataURL(part ContentPart) (string, error) {
	return modelInputImageDataURL(part)
}

func toOpenAIAssistantToolCalls(calls []ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		call = CanonicalToolCall(call)
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
	if rawName, ok := envelope["tool_name"].(string); ok {
		envelope["tool_name"] = CanonicalToolName(rawName)
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
		return isOpenAIResponsesUnknownEndpoint(body)
	default:
		return false
	}
}

func isOpenAIResponsesUnknownEndpoint(body []byte) bool {
	normalized := strings.ToLower(string(body))
	normalized = strings.Join(strings.Fields(normalized), " ")
	if normalized == "" {
		return false
	}

	hasPathRef := containsAnyString(normalized,
		"/responses",
		"v1/responses",
		"post /responses",
		"post /v1/responses",
	)
	hasEndpointRef := strings.Contains(normalized, "responses") && containsAnyString(normalized, "endpoint", "path", "route", "url")

	if hasPathRef {
		return containsAnyString(normalized,
			"unknown",
			"not found",
			"not supported",
			"unsupported",
			"invalid url",
			"unknown url",
			"unrecognized request url",
			"no route",
			"no handler",
		)
	}

	if hasEndpointRef {
		return containsAnyString(normalized,
			"unknown endpoint",
			"unsupported endpoint",
			"endpoint not found",
			"endpoint is not supported",
			"unknown path",
			"unsupported path",
			"path not found",
			"unknown route",
			"unsupported route",
			"route not found",
			"unknown url",
			"invalid url",
			"unrecognized request url",
			"no route",
			"no handler",
		)
	}

	return false
}

func containsAnyString(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

type openAIResponsesToolBuffer struct {
	ItemID      string
	OutputIndex int
	ToolCallID  string
	ToolName    string
	Arguments   strings.Builder
}

func openAIResponsesToolArgumentsDelta(
	event map[string]any,
	toolBuffers map[int]*openAIResponsesToolBuffer,
	toolBufferByItemID map[string]*openAIResponsesToolBuffer,
) *ToolCallArgumentDelta {
	typ, _ := event["type"].(string)
	switch typ {
	case "response.output_item.added", "response.output_item.done":
		item, _ := event["item"].(map[string]any)
		if item == nil {
			return nil
		}
		itemType, _ := item["type"].(string)
		if itemType != "function_call" {
			return nil
		}
		outputIndex := anyToInt(event["output_index"])
		buffer := toolBuffers[outputIndex]
		if buffer == nil {
			buffer = &openAIResponsesToolBuffer{OutputIndex: outputIndex}
			toolBuffers[outputIndex] = buffer
		}
		if buffer.ItemID == "" {
			buffer.ItemID = strings.TrimSpace(stringValueFromAny(item["id"]))
		}
		if buffer.ToolCallID == "" {
			buffer.ToolCallID = strings.TrimSpace(stringValueFromAny(item["call_id"]))
		}
		if buffer.ToolCallID == "" {
			buffer.ToolCallID = buffer.ItemID
		}
		if buffer.ToolName == "" {
			buffer.ToolName = CanonicalToolName(stringValueFromAny(item["name"]))
		}
		if buffer.Arguments.Len() == 0 {
			if arguments, ok := item["arguments"].(string); ok && arguments != "" {
				buffer.Arguments.WriteString(arguments)
			}
		}
		if buffer.ItemID != "" {
			toolBufferByItemID[buffer.ItemID] = buffer
		}
		return nil
	case "response.function_call_arguments.delta":
		outputIndex := anyToInt(event["output_index"])
		itemID, _ := event["item_id"].(string)
		delta, _ := event["delta"].(string)
		buffer := toolBuffers[outputIndex]
		if buffer == nil && strings.TrimSpace(itemID) != "" {
			buffer = toolBufferByItemID[strings.TrimSpace(itemID)]
		}
		if buffer == nil {
			buffer = &openAIResponsesToolBuffer{
				ItemID:      strings.TrimSpace(itemID),
				OutputIndex: outputIndex,
			}
			toolBuffers[outputIndex] = buffer
			if buffer.ItemID != "" {
				toolBufferByItemID[buffer.ItemID] = buffer
			}
		}
		if buffer.ToolCallID == "" {
			buffer.ToolCallID = buffer.ItemID
		}
		buffer.Arguments.WriteString(delta)
		if delta == "" || strings.TrimSpace(buffer.ToolCallID) == "" || strings.TrimSpace(buffer.ToolName) == "" {
			return nil
		}
		return &ToolCallArgumentDelta{
			ToolCallIndex:  buffer.OutputIndex,
			ToolCallID:     buffer.ToolCallID,
			ToolName:       CanonicalToolName(buffer.ToolName),
			ArgumentsDelta: delta,
		}
	default:
		return nil
	}
}

func openAIResponsesBufferedToolCalls(toolBuffers map[int]*openAIResponsesToolBuffer) ([]ToolCall, error) {
	if len(toolBuffers) == 0 {
		return nil, nil
	}
	indexes := make([]int, 0, len(toolBuffers))
	for index := range toolBuffers {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)

	output := make([]any, 0, len(indexes))
	for _, index := range indexes {
		buffer := toolBuffers[index]
		if buffer == nil {
			continue
		}
		if strings.TrimSpace(buffer.ToolName) == "" {
			continue
		}
		callID := strings.TrimSpace(buffer.ToolCallID)
		if callID == "" {
			callID = strings.TrimSpace(buffer.ItemID)
		}
		if callID == "" {
			continue
		}
		output = append(output, map[string]any{
			"id":        strings.TrimSpace(buffer.ItemID),
			"call_id":   callID,
			"type":      "function_call",
			"name":      CanonicalToolName(buffer.ToolName),
			"arguments": strings.TrimSpace(buffer.Arguments.String()),
		})
	}
	return openAIResponsesToolCalls(output)
}

func stringValueFromAny(value any) string {
	text, _ := value.(string)
	return text
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
	return parseOpenAIResponsesRoot(root)
}

func parseOpenAIResponsesRoot(root map[string]any) (string, []ToolCall, *Usage, *Cost, error) {
	message, toolCalls, usage, cost, err := parseOpenAIResponsesAssistantResponse(root)
	if err != nil {
		return "", nil, nil, nil, err
	}
	return VisibleMessageText(message), toolCalls, usage, cost, nil
}

func parseOpenAIResponsesAssistantResponse(root map[string]any) (Message, []ToolCall, *Usage, *Cost, error) {
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
			return Message{Role: "assistant", Content: []TextPart{{Text: contentBuilder.String()}}}, nil, nil, nil, nil
		}
		return Message{}, nil, nil, nil, fmt.Errorf("response missing output")
	}

	toolCalls := []ToolCall{}
	assistantMessage := Message{Role: "assistant"}
	for idx, rawItem := range rawOutput {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := item["type"].(string)

		if typ == "message" {
			parts, ok := item["content"].([]any)
			if !ok {
				continue
			}
			if phase, ok := item["phase"].(string); ok && strings.TrimSpace(phase) != "" {
				trimmed := strings.TrimSpace(phase)
				assistantMessage.Phase = &trimmed
			}
			for _, rawPart := range parts {
				part, ok := rawPart.(map[string]any)
				if !ok {
					continue
				}
				text, isOutputText := openAIResponsesMessageContentText(part)
				if text == "" {
					continue
				}
				if hasTopLevelText && isOutputText {
					continue
				}
				contentBuilder.WriteString(text)
				assistantMessage.Content = append(assistantMessage.Content, TextPart{Text: text})
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
			return Message{}, nil, nil, nil, fmt.Errorf("output[%d] missing function_call.id", idx)
		}

		toolName, _ := item["name"].(string)
		if strings.TrimSpace(toolName) == "" {
			toolName, _ = item["tool_name"].(string)
		}
		toolName = CanonicalToolName(toolName)
		if toolName == "" {
			return Message{}, nil, nil, nil, fmt.Errorf("output[%d] missing function_call.name", idx)
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
						return Message{}, nil, nil, nil, fmt.Errorf("%w: output[%d].arguments is not valid JSON", errOpenAIToolCallArguments, idx)
					}
					obj, ok := parsedArgs.(map[string]any)
					if !ok {
						return Message{}, nil, nil, nil, fmt.Errorf("%w: output[%d].arguments must be a JSON object", errOpenAIToolCallArguments, idx)
					}
					argumentsJSON = obj
				}
			default:
				return Message{}, nil, nil, nil, fmt.Errorf("%w: output[%d].arguments unsupported type", errOpenAIToolCallArguments, idx)
			}
		}

		toolCalls = append(toolCalls, ToolCall{
			ToolCallID:    toolCallID,
			ToolName:      CanonicalToolName(toolName),
			ArgumentsJSON: argumentsJSON,
		})
	}

	var usage *Usage
	var cost *Cost
	if usageObj, ok := root["usage"].(map[string]any); ok {
		usage = parseResponsesUsage(usageObj)
		cost = parseResponsesCost(usageObj)
	}

	if len(assistantMessage.Content) == 0 && strings.TrimSpace(contentBuilder.String()) != "" {
		assistantMessage.Content = []TextPart{{Text: contentBuilder.String()}}
	}
	return assistantMessage, toolCalls, usage, cost, nil
}

func openAIResponsesMessageContentText(part map[string]any) (string, bool) {
	partType := strings.TrimSpace(stringValueFromAny(part["type"]))
	switch partType {
	case "output_text", "text":
		return openAIResponsesTextValue(part["text"]), true
	case "refusal":
		return openAIResponsesTextValue(part["refusal"], part["text"]), false
	default:
		if text := openAIResponsesTextValue(part["refusal"]); text != "" {
			return text, false
		}
		return "", false
	}
}

func openAIResponsesTextValue(values ...any) string {
	for _, value := range values {
		switch casted := value.(type) {
		case string:
			if strings.TrimSpace(casted) != "" {
				return casted
			}
		case map[string]any:
			if text := openAIResponsesTextValue(casted["text"], casted["value"], casted["refusal"]); text != "" {
				return text
			}
		}
	}
	return ""
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
		toolName = CanonicalToolName(toolName)
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
			ToolName:      CanonicalToolName(toolName),
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
	if typ != "response.output_text.delta" && typ != "response.refusal.delta" && !openAIResponsesIsReasoningDelta(typ) {
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

	if len(body) > 0 {
		raw, truncated := truncateUTF8(string(body), openAIMaxErrorBodyBytes)
		details["provider_error_body"] = raw
		if truncated {
			details["provider_error_body_truncated"] = true
		}
	}

	if len(body) == 0 {
		return fallback, details
	}

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
		if msg, ok := root["message"].(string); ok && strings.TrimSpace(msg) != "" {
			return strings.TrimSpace(msg), details
		}
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

	if meta, ok := errObj["metadata"].(map[string]any); ok && len(meta) > 0 {
		if b, err := json.Marshal(meta); err == nil {
			metaStr, metaTrunc := truncateUTF8(string(b), 1024)
			details["openai_error_metadata_json"] = metaStr
			if metaTrunc {
				details["openai_error_metadata_truncated"] = true
			}
		}
	}

	if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg), details
	}

	var sb strings.Builder
	if v, ok := details["openai_error_type"].(string); ok && strings.TrimSpace(v) != "" {
		sb.WriteString(strings.TrimSpace(v))
	}
	if c, ok := details["openai_error_code"]; ok {
		if sb.Len() > 0 {
			sb.WriteString(": ")
		}
		sb.WriteString(fmt.Sprintf("%v", c))
	}
	if p, ok := details["openai_error_param"].(string); ok && strings.TrimSpace(p) != "" {
		if sb.Len() > 0 {
			sb.WriteString(", param=")
		}
		sb.WriteString(strings.TrimSpace(p))
	}
	if sb.Len() > 0 {
		return sb.String(), details
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

func forEachSSEData(ctx context.Context, r io.Reader, markActivity func(), handle func(string) error) error {
	reader := bufio.NewReader(r)
	dataLines := []string{}
	type readResult struct {
		line string
		err  error
	}
	var closer io.Closer
	if c, ok := r.(io.Closer); ok {
		closer = c
	}
	for {
		if err := streamContextError(ctx, nil); err != nil {
			if closer != nil {
				_ = closer.Close()
			}
			return err
		}

		resultCh := make(chan readResult, 1)
		go func() {
			line, err := reader.ReadString('\n')
			resultCh <- readResult{line: line, err: err}
		}()

		var result readResult
		select {
		case <-ctx.Done():
			if closer != nil {
				_ = closer.Close()
			}
			return streamContextError(ctx, nil)
		case result = <-resultCh:
		}
		if result.err != nil && result.err != io.EOF {
			return streamContextError(ctx, result.err)
		}
		if len(result.line) > 0 && markActivity != nil {
			markActivity()
		}

		cleaned := strings.TrimRight(result.line, "\r\n")
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

		if result.err == io.EOF {
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
