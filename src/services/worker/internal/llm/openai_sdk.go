package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/google/uuid"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type openAISDKGateway struct {
	cfg       OpenAIGatewayConfig
	transport protocolTransport
	protocol  OpenAIProtocolConfig
	client    openai.Client
	configErr error
	quirks    *QuirkStore
}

func NewOpenAIGatewaySDK(cfg OpenAIGatewayConfig) Gateway {
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
	if protocol.PrimaryKind == "" && len(protocol.AdvancedPayloadJSON) == 0 {
		protocol, configErr = parseOpenAIProtocolConfig(cfg.APIMode, cfg.AdvancedJSON)
	}
	if protocol.PrimaryKind == "" {
		protocol.PrimaryKind = ProtocolKindOpenAIResponses
		fallback := ProtocolKindOpenAIChatCompletions
		protocol.FallbackKind = &fallback
	}

	normalizedTransport := newProtocolTransport(transport, "https://api.openai.com/v1", nil)
	cfg.Transport = normalizedTransport.cfg
	cfg.Protocol = protocol
	cfg.EmitDebugEvents = normalizedTransport.cfg.EmitDebugEvents
	cfg.TotalTimeout = normalizedTransport.cfg.TotalTimeout
	cfg.BaseURL = normalizedTransport.cfg.BaseURL

	opts := []option.RequestOption{
		option.WithAPIKey(strings.TrimSpace(normalizedTransport.cfg.APIKey)),
		option.WithBaseURL(sdkBaseURL(normalizedTransport)),
		option.WithHTTPClient(sdkHTTPClient(normalizedTransport)),
	}
	for key, value := range normalizedTransport.cfg.DefaultHeaders {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			opts = append(opts, option.WithHeader(key, value))
		}
	}

	return &openAISDKGateway{cfg: cfg, transport: normalizedTransport, protocol: protocol, client: openai.NewClient(opts...), configErr: configErr, quirks: NewQuirkStore()}
}

func (g *openAISDKGateway) ProtocolKind() ProtocolKind { return g.protocol.PrimaryKind }

func (g *openAISDKGateway) Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error {
	if g.configErr != nil {
		return yield(StreamRunFailed{Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: g.configErr.Error()}})
	}
	if g.transport.baseURLErr != nil {
		return yield(StreamRunFailed{Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: "OpenAI base_url blocked", Details: map[string]any{"reason": g.transport.baseURLErr.Error()}}})
	}
	PrepareRequestModelInputImages(&request)
	ctx, stopTimeout, markActivity := withStreamIdleTimeout(ctx, g.transport.cfg.TotalTimeout)
	defer stopTimeout()

	switch g.protocol.PrimaryKind {
	case ProtocolKindOpenAIChatCompletions:
		return g.chatCompletions(ctx, request, yield, markActivity, 0)
	case ProtocolKindOpenAIResponses:
		allowFallback := g.protocol.FallbackKind != nil && *g.protocol.FallbackKind == ProtocolKindOpenAIChatCompletions
		if err := g.responses(ctx, request, yield, allowFallback, markActivity, 0); err != nil {
			var fallback *openAIResponsesNotSupportedError
			if errors.As(err, &fallback) && allowFallback {
				status := fallback.StatusCode
				if emitErr := yield(StreamProviderFallback{ProviderKind: "openai", FromAPIMode: "responses", ToAPIMode: "chat_completions", Reason: "responses_not_supported", StatusCode: &status}); emitErr != nil {
					return emitErr
				}
				return g.chatCompletions(ctx, request, yield, markActivity, 0)
			}
			return err
		}
		return nil
	default:
		return yield(StreamRunFailed{Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: fmt.Sprintf("unsupported OpenAI protocol kind: %s", g.protocol.PrimaryKind)}})
	}
}

func (g *openAISDKGateway) chatCompletions(ctx context.Context, request Request, yield func(StreamEvent) error, markActivity func(), attempt int) error {
	llmCallID := uuid.NewString()
	payload, payloadBytes, requestEvent, err := g.chatPayload(request, llmCallID)
	if err != nil {
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: "OpenAI chat messages construction failed", Details: map[string]any{"reason": err.Error()}}})
	}
	if RequestPayloadTooLarge(payloadBytes) {
		if err := yield(requestEvent); err != nil {
			return err
		}
		return yield(PreflightOversizeFailure(llmCallID, payloadBytes))
	}
	*requestEvent.NetworkAttempted = true
	if err := yield(requestEvent); err != nil {
		return err
	}

	responseCapture := newProviderResponseCapture()
	streamCtx := withProviderResponseCapture(ctx, responseCapture)
	params := openai.ChatCompletionNewParams{Model: openai.ChatModel(request.Model)}
	stream := g.client.Chat.Completions.NewStreaming(streamCtx, params, openAISDKPayloadOptions(payload)...)
	defer func() { _ = stream.Close() }()
	state := newOpenAISDKChatState(ctx, llmCallID, yield)
	for stream.Next() {
		if markActivity != nil {
			markActivity()
		}
		chunk := stream.Current()
		if err := g.emitDebugChunk(llmCallID, "chat_completions", chunk.RawJSON(), nil, yield); err != nil {
			return err
		}
		if err := state.handle(chunk); err != nil {
			return err
		}
	}
	if err := stream.Err(); err != nil {
		if attempt == 0 {
			if id, ok := openAISDKDetectQuirk(err); ok {
				if emitErr := yield(StreamQuirkLearned{LlmCallID: llmCallID, ProviderKind: "openai", QuirkID: string(id)}); emitErr != nil {
					return emitErr
				}
				g.quirks.Set(id)
				return g.chatCompletions(ctx, request, yield, markActivity, attempt+1)
			}
		}
		if emitErr := g.emitDebugErrorChunk(llmCallID, "chat_completions", err, yield); emitErr != nil {
			return emitErr
		}
		return state.fail(openAISDKStreamErrorToGateway(err, "OpenAI request failed", payloadBytes, "chat_completions", responseCapture, streamCtx))
	}
	return state.complete()
}

func (g *openAISDKGateway) responses(ctx context.Context, request Request, yield func(StreamEvent) error, allowFallback bool, markActivity func(), attempt int) error {
	llmCallID := uuid.NewString()
	payload, payloadBytes, requestEvent, err := g.responsesPayload(request, llmCallID)
	if err != nil {
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: "OpenAI responses input construction failed", Details: map[string]any{"reason": err.Error()}}})
	}
	if RequestPayloadTooLarge(payloadBytes) {
		if err := yield(requestEvent); err != nil {
			return err
		}
		return yield(PreflightOversizeFailure(llmCallID, payloadBytes))
	}
	*requestEvent.NetworkAttempted = true
	if err := yield(requestEvent); err != nil {
		return err
	}

	responseCapture := newProviderResponseCapture()
	streamCtx := withProviderResponseCapture(ctx, responseCapture)
	params := responses.ResponseNewParams{Model: responses.ResponsesModel(request.Model)}
	stream := g.client.Responses.NewStreaming(streamCtx, params, openAISDKPayloadOptions(payload)...)
	defer func() { _ = stream.Close() }()
	state := newOpenAISDKResponsesState(ctx, llmCallID, yield)
	for stream.Next() {
		if markActivity != nil {
			markActivity()
		}
		event := stream.Current()
		if err := g.emitDebugChunk(llmCallID, "responses", event.RawJSON(), nil, yield); err != nil {
			return err
		}
		if err := state.handle(event); err != nil {
			return err
		}
	}
	if err := stream.Err(); err != nil {
		if unsupported, ok := openAISDKUnsupportedResponsesError(err, allowFallback); ok {
			return &unsupported
		}
		if attempt == 0 {
			if id, ok := openAISDKDetectQuirk(err); ok {
				if emitErr := yield(StreamQuirkLearned{LlmCallID: llmCallID, ProviderKind: "openai", QuirkID: string(id)}); emitErr != nil {
					return emitErr
				}
				g.quirks.Set(id)
				return g.responses(ctx, request, yield, allowFallback, markActivity, attempt+1)
			}
		}
		if emitErr := g.emitDebugErrorChunk(llmCallID, "responses", err, yield); emitErr != nil {
			return emitErr
		}
		return state.fail(openAISDKStreamErrorToGateway(err, "OpenAI responses request failed", payloadBytes, "responses", responseCapture, streamCtx))
	}
	return state.complete()
}

func (g *openAISDKGateway) chatPayload(request Request, llmCallID string) (map[string]any, int, StreamLlmRequest, error) {
	PrepareRequestModelInputImages(&request)
	messagesPayload, err := toOpenAIChatMessages(request.Messages)
	if err != nil {
		return nil, 0, StreamLlmRequest{}, err
	}
	payload := map[string]any{"model": request.Model, "messages": messagesPayload, "stream": true, "stream_options": map[string]any{"include_usage": true}}
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
	g.quirks.ApplyAll(payload, openAIQuirks)
	return g.providerRequest(request, llmCallID, "chat_completions", "/chat/completions", payload)
}

func (g *openAISDKGateway) responsesPayload(request Request, llmCallID string) (map[string]any, int, StreamLlmRequest, error) {
	payload, err := buildOpenAIResponsesPayload(request, g.protocol, g.quirks)
	if err != nil {
		return nil, 0, StreamLlmRequest{}, err
	}
	return g.providerRequest(request, llmCallID, "responses", "/responses", payload)
}

func buildOpenAIResponsesPayload(request Request, protocol OpenAIProtocolConfig, quirks *QuirkStore) (map[string]any, error) {
	PrepareRequestModelInputImages(&request)
	instructions, inputMessages := splitOpenAIResponsesInstructions(request.Messages)
	input, err := toOpenAIResponsesInput(inputMessages)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{"model": request.Model, "input": input, "stream": true}
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
		payload["tool_choice"] = openAIResponsesToolChoice(request.ToolChoice)
	}
	for k, v := range protocol.AdvancedPayloadJSON {
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}
	applyOpenAIResponsesReasoningMode(payload, request.ReasoningMode)
	if quirks != nil {
		quirks.ApplyAll(payload, openAIQuirks)
	}
	return payload, nil
}

func (g *openAISDKGateway) providerRequest(request Request, llmCallID string, apiMode string, path string, payload map[string]any) (map[string]any, int, StreamLlmRequest, error) {
	debugPayload, redactedHints := sanitizeDebugPayloadJSON(payload)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, StreamLlmRequest{}, err
	}
	baseURL := g.transport.cfg.BaseURL
	stats := ComputeRequestStats(request)
	networkAttempted := false
	return payload, len(encoded), StreamLlmRequest{LlmCallID: llmCallID, ProviderKind: "openai", APIMode: apiMode, BaseURL: &baseURL, Path: &path, InputJSON: request.ToJSON(), PayloadJSON: debugPayload, RedactedHints: redactedHints, SystemBytes: stats.SystemBytes, ToolsBytes: stats.ToolsBytes, MessagesBytes: stats.MessagesBytes, AbstractRequestBytes: stats.AbstractRequestBytes, ProviderPayloadBytes: len(encoded), ImagePartCount: stats.ImagePartCount, Base64ImageBytes: stats.Base64ImageBytes, NetworkAttempted: &networkAttempted, RoleBytes: stats.RoleBytes, ToolSchemaBytesMap: stats.ToolSchemaBytesMap, StablePrefixHash: stats.StablePrefixHash, SessionPrefixHash: stats.SessionPrefixHash, VolatileTailHash: stats.VolatileTailHash, ToolSchemaHash: stats.ToolSchemaHash, StablePrefixBytes: stats.StablePrefixBytes, SessionPrefixBytes: stats.SessionPrefixBytes, VolatileTailBytes: stats.VolatileTailBytes, CacheCandidateBytes: stats.CacheCandidateBytes}, nil
}

func (g *openAISDKGateway) emitDebugChunk(llmCallID string, apiMode string, raw string, statusCode *int, yield func(StreamEvent) error) error {
	if !g.transport.cfg.EmitDebugEvents || strings.TrimSpace(raw) == "" {
		return nil
	}
	truncatedRaw, rawTruncated := truncateUTF8(raw, openAIMaxDebugChunkBytes)
	var chunkJSON any
	_ = json.Unmarshal([]byte(raw), &chunkJSON)
	return yield(StreamLlmResponseChunk{LlmCallID: llmCallID, ProviderKind: "openai", APIMode: apiMode, Raw: truncatedRaw, ChunkJSON: chunkJSON, StatusCode: statusCode, Truncated: rawTruncated})
}

func (g *openAISDKGateway) emitDebugErrorChunk(llmCallID string, apiMode string, err error, yield func(StreamEvent) error) error {
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) {
		return nil
	}
	status := apiErr.StatusCode
	return g.emitDebugChunk(llmCallID, apiMode, string(apiErr.DumpResponse(true)), &status, yield)
}

func openAISDKPayloadOptions(payload map[string]any) []option.RequestOption {
	opts := make([]option.RequestOption, 0, len(payload))
	for key, value := range payload {
		opts = append(opts, option.WithJSONSet(key, value))
	}
	return opts
}

func openAISDKDetectQuirk(err error) (QuirkID, bool) {
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) {
		return "", false
	}
	rawBody := string(apiErr.DumpResponse(true))
	_, details := openAIErrorMessageAndDetails([]byte(apiErr.RawJSON()), apiErr.StatusCode, "")
	symptoms := DetectSymptoms(SymptomMatchContext{Status: apiErr.StatusCode, RawBody: rawBody, Details: details}, openAISymptoms)
	return detectQuirk(symptoms, openAIQuirks)
}

type openAISDKChatState struct {
	ctx             context.Context
	llmCallID       string
	yield           func(StreamEvent) error
	toolBuffer      *openAIChatToolCallBuffer
	visible         strings.Builder
	thinking        strings.Builder
	usage           *Usage
	cost            *Cost
	emittedMain     bool
	emittedTool     bool
	emittedAny      bool
	contentFiltered bool
	inThink         bool
	finished        bool
}

func newOpenAISDKChatState(ctx context.Context, id string, yield func(StreamEvent) error) *openAISDKChatState {
	return &openAISDKChatState{ctx: ctx, llmCallID: id, yield: yield, toolBuffer: newOpenAIChatToolCallBuffer()}
}
func (s *openAISDKChatState) handle(chunk openai.ChatCompletionChunk) error {
	if raw := chunk.RawJSON(); raw != "" {
		s.usage, s.cost = openAISDKChatUsageFromRaw(raw, s.usage, s.cost)
	}
	if len(chunk.Choices) == 0 {
		return nil
	}
	choice := chunk.Choices[0]
	role := "assistant"
	if strings.TrimSpace(choice.Delta.Role) != "" {
		role = strings.TrimSpace(choice.Delta.Role)
	}
	if choice.Delta.Refusal != "" {
		s.visible.WriteString(choice.Delta.Refusal)
		s.emittedAny = true
		s.emittedMain = true
		if err := s.yield(StreamMessageDelta{ContentDelta: choice.Delta.Refusal, Role: role}); err != nil {
			return err
		}
	}
	if choice.Delta.Content != "" {
		thinkingPart, mainPart := splitThinkContent(&s.inThink, choice.Delta.Content)
		ch := "thinking"
		if thinkingPart != "" {
			s.thinking.WriteString(thinkingPart)
			s.emittedAny = true
			if err := s.yield(StreamMessageDelta{ContentDelta: thinkingPart, Role: role, Channel: &ch}); err != nil {
				return err
			}
		}
		if mainPart != "" {
			s.visible.WriteString(mainPart)
			s.emittedAny = true
			s.emittedMain = true
			if err := s.yield(StreamMessageDelta{ContentDelta: mainPart, Role: role}); err != nil {
				return err
			}
		}
	}
	var rawChoice map[string]any
	_ = json.Unmarshal([]byte(choice.RawJSON()), &rawChoice)
	if delta, _ := rawChoice["delta"].(map[string]any); delta != nil {
		if text, _ := delta["reasoning_content"].(string); text != "" {
			ch := "thinking"
			s.thinking.WriteString(text)
			s.emittedAny = true
			if err := s.yield(StreamMessageDelta{ContentDelta: text, Role: role, Channel: &ch}); err != nil {
				return err
			}
		}
		if text, _ := delta["reasoning"].(string); text != "" {
			ch := "thinking"
			s.thinking.WriteString(text)
			s.emittedAny = true
			if err := s.yield(StreamMessageDelta{ContentDelta: text, Role: role, Channel: &ch}); err != nil {
				return err
			}
		}
	}
	for _, toolDelta := range choice.Delta.ToolCalls {
		converted := openAIChatCompletionToolDelta{ID: toolDelta.ID, Type: toolDelta.Type}
		idx := int(toolDelta.Index)
		converted.Index = &idx
		converted.Function.Name = toolDelta.Function.Name
		converted.Function.Arguments = toolDelta.Function.Arguments
		s.toolBuffer.Add(converted, idx)
		if toolDelta.Function.Arguments != "" {
			if err := s.yield(ToolCallArgumentDelta{ToolCallIndex: idx, ToolCallID: toolDelta.ID, ToolName: CanonicalToolName(toolDelta.Function.Name), ArgumentsDelta: toolDelta.Function.Arguments}); err != nil {
				return err
			}
		}
	}
	if choice.FinishReason != "" {
		s.finished = true
		if strings.EqualFold(choice.FinishReason, "content_filter") {
			s.contentFiltered = true
		}
		if strings.EqualFold(choice.FinishReason, "tool_calls") {
			return s.drainTools()
		}
	}
	return nil
}
func (s *openAISDKChatState) drainTools() error {
	calls, warnings, err := s.toolBuffer.Drain()
	if err != nil {
		return s.yield(openAIParseFailure(err, "OpenAI response parse failed", "OpenAI tool_call arguments parse failed", s.llmCallID))
	}
	for _, call := range calls {
		s.emittedTool = true
		if err := s.yield(call); err != nil {
			return err
		}
	}
	for _, w := range warnings {
		s.emittedTool = true
		if err := s.yield(formatRepairToolResult(w, s.llmCallID)); err != nil {
			return err
		}
	}
	return nil
}
func (s *openAISDKChatState) fail(g GatewayError) error {
	return s.yield(StreamRunFailed{LlmCallID: s.llmCallID, Error: g, Usage: s.usage, Cost: s.cost})
}
func (s *openAISDKChatState) complete() error {
	if s.contentFiltered {
		return s.yield(StreamRunFailed{LlmCallID: s.llmCallID, Error: GatewayError{ErrorClass: ErrorClassPolicyDenied, Message: "OpenAI content filtered"}, Usage: s.usage, Cost: s.cost})
	}
	if !s.finished {
		return s.yield(StreamRunFailed{LlmCallID: s.llmCallID, Error: RetryableStreamEndedError(), Usage: s.usage, Cost: s.cost})
	}
	if err := s.drainTools(); err != nil {
		return err
	}
	if !s.emittedMain && !s.emittedTool {
		if s.emittedAny {
			return s.yield(StreamRunFailed{LlmCallID: s.llmCallID, Error: GatewayError{ErrorClass: ErrorClassProviderRetryable, Message: "LLM generated only internal reasoning without visible output"}, Usage: s.usage, Cost: s.cost})
		}
		return s.yield(StreamRunFailed{LlmCallID: s.llmCallID, Error: RetryableStreamEndedError(), Usage: s.usage, Cost: s.cost})
	}
	assistantMessage := s.assistantMessage()
	logProviderCompletionDebug(s.ctx, providerCompletionDebug{
		ProviderKind:     "openai",
		APIMode:          "chat_completions",
		LlmCallID:        s.llmCallID,
		AssistantMessage: &assistantMessage,
	})
	return s.yield(StreamRunCompleted{LlmCallID: s.llmCallID, Usage: s.usage, Cost: s.cost, AssistantMessage: &assistantMessage})
}
func (s *openAISDKChatState) assistantMessage() Message {
	parts := make([]ContentPart, 0, 2)
	if text := s.thinking.String(); text != "" {
		parts = append(parts, ContentPart{Type: "thinking", Text: text})
	}
	if text := s.visible.String(); text != "" {
		parts = append(parts, TextPart{Text: text})
	}
	return Message{Role: "assistant", Content: parts}
}

type openAISDKResponsesState struct {
	ctx                context.Context
	llmCallID          string
	yield              func(StreamEvent) error
	providerKind       string
	apiMode            string
	visible            strings.Builder
	thinking           strings.Builder
	completed          bool
	emittedVisibleText bool
	toolBuffers        map[int]*openAIResponsesToolBuffer
	toolBufferByItemID map[string]*openAIResponsesToolBuffer
}

func newOpenAISDKResponsesState(ctx context.Context, id string, yield func(StreamEvent) error) *openAISDKResponsesState {
	return &openAISDKResponsesState{ctx: ctx, llmCallID: id, yield: yield, providerKind: "openai", apiMode: "responses", toolBuffers: map[int]*openAIResponsesToolBuffer{}, toolBufferByItemID: map[string]*openAIResponsesToolBuffer{}}
}
func (s *openAISDKResponsesState) handle(event responses.ResponseStreamEventUnion) error {
	return s.handleRaw(event.RawJSON())
}

func (s *openAISDKResponsesState) handleRaw(raw string) error {
	var root map[string]any
	_ = json.Unmarshal([]byte(raw), &root)
	typ, _ := root["type"].(string)
	if delta := openAIResponsesToolArgumentsDelta(root, s.toolBuffers, s.toolBufferByItemID); delta != nil {
		if err := s.yield(*delta); err != nil {
			return err
		}
	}
	if delta := openAIResponsesDeltaText(root); delta != "" {
		ch := "thinking"
		if openAIResponsesIsReasoningDelta(typ) {
			s.thinking.WriteString(delta)
			if err := s.yield(StreamMessageDelta{ContentDelta: delta, Role: "assistant", Channel: &ch}); err != nil {
				return err
			}
		} else {
			s.visible.WriteString(delta)
			s.emittedVisibleText = true
			if err := s.yield(StreamMessageDelta{ContentDelta: delta, Role: "assistant"}); err != nil {
				return err
			}
		}
	}
	if typ == "response.completed" {
		respObj, _ := root["response"].(map[string]any)
		assistantMessage, toolCalls, usage, cost, warnings, err := parseOpenAIResponsesAssistantResponse(respObj)
		if err != nil {
			if !isOpenAIResponsesMissingOutput(err) || !s.hasStreamedCompletionData() {
				return s.yield(openAIParseFailure(err, "OpenAI responses response parse failed", "OpenAI responses tool_call arguments parse failed", s.llmCallID))
			}
			assistantMessage = Message{Role: "assistant"}
			if usageObj, ok := respObj["usage"].(map[string]any); ok {
				usage = parseResponsesUsage(usageObj)
				cost = parseResponsesCost(usageObj)
			}
		}
		if len(toolCalls) == 0 && len(s.toolBuffers) > 0 {
			var bufferedWarnings []ParseWarning
			toolCalls, bufferedWarnings, err = openAIResponsesBufferedToolCalls(s.toolBuffers)
			if err != nil {
				return s.yield(openAIParseFailure(err, "OpenAI responses response parse failed", "OpenAI responses tool_call arguments parse failed", s.llmCallID))
			}
			warnings = append(warnings, bufferedWarnings...)
		}
		s.applyStreamedThinking(&assistantMessage)
		s.applyStreamedVisibleText(&assistantMessage)
		if !s.emittedVisibleText {
			if text := VisibleMessageText(assistantMessage); text != "" {
				s.emittedVisibleText = true
				if err := s.yield(StreamMessageDelta{ContentDelta: text, Role: "assistant"}); err != nil {
					return err
				}
			}
		}
		for _, call := range toolCalls {
			if err := s.yield(call); err != nil {
				return err
			}
		}
		for _, w := range warnings {
			if err := s.yield(formatRepairToolResult(w, s.llmCallID)); err != nil {
				return err
			}
		}
		s.completed = true
		logProviderCompletionDebug(s.ctx, providerCompletionDebug{
			ProviderKind:     s.providerKind,
			APIMode:          s.apiMode,
			LlmCallID:        s.llmCallID,
			AssistantMessage: &assistantMessage,
			ToolCallCount:    len(toolCalls),
		})
		return s.yield(StreamRunCompleted{LlmCallID: s.llmCallID, Usage: usage, Cost: cost, AssistantMessage: &assistantMessage})
	}
	if typ == "response.failed" || typ == "response.error" || typ == "error" {
		message := "OpenAI responses failed"
		if errObj, ok := root["error"].(map[string]any); ok {
			if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
				message = strings.TrimSpace(msg)
			}
		} else if msg, ok := root["message"].(string); ok && strings.TrimSpace(msg) != "" {
			message = strings.TrimSpace(msg)
		}
		s.completed = true
		return s.yield(StreamRunFailed{LlmCallID: s.llmCallID, Error: GatewayError{ErrorClass: ErrorClassProviderNonRetryable, Message: message}})
	}
	return nil
}

func isOpenAIResponsesMissingOutput(err error) bool {
	return errors.Is(err, errOpenAIResponsesMissingOutput)
}

func (s *openAISDKResponsesState) hasStreamedCompletionData() bool {
	if s == nil {
		return false
	}
	return s.visible.Len() > 0 || s.thinking.Len() > 0 || len(s.toolBuffers) > 0
}

func (s *openAISDKResponsesState) fail(g GatewayError) error {
	return s.yield(StreamRunFailed{LlmCallID: s.llmCallID, Error: g})
}
func (s *openAISDKResponsesState) complete() error {
	if s.completed {
		return nil
	}
	return s.yield(StreamRunFailed{LlmCallID: s.llmCallID, Error: RetryableStreamEndedError()})
}

func (s *openAISDKResponsesState) applyStreamedThinking(message *Message) {
	if s == nil || message == nil {
		return
	}
	text := s.thinking.String()
	if text == "" {
		return
	}
	if _, ok := openAIReasoningContent(message.Content); ok {
		return
	}
	message.Content = append([]ContentPart{{Type: "thinking", Text: text}}, message.Content...)
}

func (s *openAISDKResponsesState) applyStreamedVisibleText(message *Message) {
	if s == nil || message == nil {
		return
	}
	if VisibleMessageText(*message) != "" {
		return
	}
	text := s.visible.String()
	if text == "" {
		return
	}
	message.Content = append(message.Content, TextPart{Text: text})
}

func openAISDKChatUsageFromRaw(raw string, currentUsage *Usage, currentCost *Cost) (*Usage, *Cost) {
	var root map[string]any
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return currentUsage, currentCost
	}
	usageObj, ok := root["usage"].(map[string]any)
	if !ok {
		return currentUsage, currentCost
	}
	usage := parseChatCompletionUsage(anyNumberToInt(usageObj["prompt_tokens"]), anyNumberToInt(usageObj["completion_tokens"]), anyNumberToInt(nestedAny(usageObj, "prompt_tokens_details", "cached_tokens")))
	cost := parseResponsesCost(usageObj)
	if cost == nil {
		cost = currentCost
	}
	if usage == nil {
		usage = currentUsage
	}
	return usage, cost
}
func anyNumberToInt(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	case int:
		return v
	default:
		return 0
	}
}
func nestedAny(root map[string]any, key string, child string) any {
	obj, _ := root[key].(map[string]any)
	if obj == nil {
		return nil
	}
	return obj[child]
}
func openAISDKErrorToGateway(err error, fallback string, payloadBytes int) GatewayError {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		message, details := openAIErrorMessageAndDetails([]byte(apiErr.RawJSON()), apiErr.StatusCode, fallback)
		details["provider_kind"] = "openai"
		details["network_attempted"] = true
		if apiErr.Response != nil {
			if requestID := sdkProviderRequestID(apiErr.Response.Header); requestID != "" {
				details["provider_request_id"] = requestID
			}
		}
		if apiErr.StatusCode == http.StatusRequestEntityTooLarge {
			details = OversizeFailureDetails(payloadBytes, OversizePhaseProvider, details)
		}
		symptoms := DetectSymptoms(SymptomMatchContext{Status: apiErr.StatusCode, RawBody: string(apiErr.RawJSON()), Details: details}, openAISymptoms)
		details = MergeSymptomsIntoDetails(details, symptoms)
		return GatewayError{ErrorClass: classifyOpenAIStatus(apiErr.StatusCode, details), Message: message, Details: details}
	}
	return GatewayError{ErrorClass: ErrorClassProviderRetryable, Message: "OpenAI network error", Details: map[string]any{"reason": err.Error()}}
}

func openAISDKStreamErrorToGateway(err error, fallback string, payloadBytes int, apiMode string, responseCapture *providerResponseCapture, ctx context.Context) GatewayError {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		gatewayErr := openAISDKErrorToGateway(err, fallback, payloadBytes)
		if gatewayErr.Details == nil {
			gatewayErr.Details = map[string]any{}
		}
		gatewayErr.Details["api_mode"] = apiMode
		gatewayErr.Details["network_attempted"] = true
		gatewayErr.Details["streaming"] = true
		return gatewayErr
	}
	details := sdkTransportErrorDetails(err, "openai", apiMode, true, true)
	details = mergeContextErrorDetails(details, err, ctx)
	details = mergeProviderResponseCaptureDetails(details, responseCapture)
	// stream 场景下 provider 可能 200 + SSE error event，错误埋在 tail 里。
	tail, _ := details["provider_response_tail"].(string)
	statusCode, _ := details["status_code"].(int)
	symptoms := DetectSymptoms(SymptomMatchContext{Status: statusCode, RawBody: tail, Details: details}, openAISymptoms)
	details = MergeSymptomsIntoDetails(details, symptoms)
	errClass := ErrorClassProviderRetryable
	if DetailsHaveSymptom(details, SymptomContextLengthExceeded) {
		errClass = ErrorClassProviderNonRetryable
	}
	return GatewayError{ErrorClass: errClass, Message: "OpenAI network error", Details: details}
}

func classifyOpenAIStatus(status int, details map[string]any) string {
	if DetailsHaveSymptom(details, SymptomContextLengthExceeded) {
		return ErrorClassProviderNonRetryable
	}
	if status == http.StatusBadRequest {
		code, _ := details["openai_error_code"].(string)
		errorType, _ := details["openai_error_type"].(string)
		if code == "rate_limit_exceeded" || errorType == "rate_limit_error" {
			return ErrorClassProviderRetryable
		}
		return ErrorClassProviderNonRetryable
	}
	return classifyHTTPStatus(status)
}
func openAISDKUnsupportedResponsesError(err error, allow bool) (openAIResponsesNotSupportedError, bool) {
	var apiErr *openai.Error
	if !allow || !errors.As(err, &apiErr) {
		return openAIResponsesNotSupportedError{}, false
	}
	if apiErr.StatusCode == http.StatusNotFound || strings.Contains(strings.ToLower(apiErr.RawJSON()), "model_not_found") {
		return openAIResponsesNotSupportedError{StatusCode: apiErr.StatusCode}, true
	}
	return openAIResponsesNotSupportedError{}, false
}

func (g *openAISDKGateway) GenerateImage(ctx context.Context, model string, req ImageGenerationRequest) (GeneratedImage, error) {
	if g == nil {
		return GeneratedImage{}, GatewayError{ErrorClass: ErrorClassConfigMissing, Message: "openai gateway is not initialized"}
	}
	if g.configErr != nil {
		return GeneratedImage{}, GatewayError{ErrorClass: ErrorClassConfigInvalid, Message: g.configErr.Error()}
	}
	if g.transport.baseURLErr != nil {
		return GeneratedImage{}, GatewayError{ErrorClass: ErrorClassConfigInvalid, Message: "OpenAI base_url blocked", Details: map[string]any{"reason": g.transport.baseURLErr.Error()}}
	}
	if req.ForceOpenAIImageAPI || g.protocol.PrimaryKind == ProtocolKindOpenAIChatCompletions {
		if len(req.InputImages) > 0 {
			return g.generateImageWithEditsAPI(ctx, model, req)
		}
		return g.generateImageWithImagesAPI(ctx, model, req)
	}
	return g.generateImageWithResponsesAPI(ctx, model, req)
}

func (g *openAISDKGateway) generateImageWithResponsesAPI(ctx context.Context, model string, req ImageGenerationRequest) (GeneratedImage, error) {
	payload := copyAnyMap(g.protocol.AdvancedPayloadJSON)
	payload["model"] = strings.TrimSpace(model)
	blocks, err := imageGenerationOpenAIBlocks(req)
	if err != nil {
		return GeneratedImage{}, err
	}
	payload["input"] = []map[string]any{{"role": "user", "content": blocks}}
	payload["tools"] = []map[string]any{imageGenerationOpenAITool(req)}
	var body []byte
	if err := g.client.Execute(ctx, http.MethodPost, "responses", payload, &body); err != nil {
		return GeneratedImage{}, openAIImageSDKError(err)
	}
	return parseOpenAIResponsesImage(body, model)
}

func (g *openAISDKGateway) generateImageWithImagesAPI(ctx context.Context, model string, req ImageGenerationRequest) (GeneratedImage, error) {
	params := openAIImageGenerateParams(model, req)
	response, err := g.client.Images.Generate(ctx, params)
	if err != nil {
		return GeneratedImage{}, openAIImageSDKError(err)
	}
	return parseOpenAIImagesAPIResponse([]byte(response.RawJSON()), model)
}

func (g *openAISDKGateway) generateImageWithEditsAPI(ctx context.Context, model string, req ImageGenerationRequest) (GeneratedImage, error) {
	images := make([]io.Reader, 0, len(req.InputImages))
	for idx, image := range req.InputImages {
		mimeType, data, err := modelInputImage(image)
		if err != nil {
			return GeneratedImage{}, GatewayError{ErrorClass: ErrorClassConfigInvalid, Message: "OpenAI image input encoding failed", Details: map[string]any{"index": idx, "reason": err.Error()}}
		}
		images = append(images, openai.File(bytes.NewReader(data), openAIImageFilename(idx, mimeType), mimeType))
	}
	params := openAIImageEditParams(model, req, images)
	response, err := g.client.Images.Edit(ctx, params)
	if err != nil {
		return GeneratedImage{}, openAIImageSDKError(err)
	}
	return parseOpenAIImagesAPIResponse([]byte(response.RawJSON()), model)
}

func openAIImageGenerateParams(model string, req ImageGenerationRequest) openai.ImageGenerateParams {
	params := openai.ImageGenerateParams{Model: openai.ImageModel(strings.TrimSpace(model)), Prompt: req.Prompt, ResponseFormat: openai.ImageGenerateParamsResponseFormatB64JSON}
	if req.Size != "" {
		params.Size = openai.ImageGenerateParamsSize(req.Size)
	}
	if req.Quality != "" {
		params.Quality = openai.ImageGenerateParamsQuality(req.Quality)
	}
	if req.Background != "" {
		params.Background = openai.ImageGenerateParamsBackground(req.Background)
	}
	if req.OutputFormat != "" {
		params.OutputFormat = openai.ImageGenerateParamsOutputFormat(req.OutputFormat)
	}
	return params
}

func openAIImageEditParams(model string, req ImageGenerationRequest, images []io.Reader) openai.ImageEditParams {
	params := openai.ImageEditParams{Model: openai.ImageModel(strings.TrimSpace(model)), Prompt: req.Prompt, ResponseFormat: openai.ImageEditParamsResponseFormatB64JSON}
	if len(images) == 1 {
		params.Image = openai.ImageEditParamsImageUnion{OfFile: images[0]}
	} else {
		params.Image = openai.ImageEditParamsImageUnion{OfFileArray: images}
	}
	if req.Size != "" {
		params.Size = openai.ImageEditParamsSize(req.Size)
	}
	if req.Quality != "" {
		params.Quality = openai.ImageEditParamsQuality(req.Quality)
	}
	if req.Background != "" {
		params.Background = openai.ImageEditParamsBackground(req.Background)
	}
	if req.OutputFormat != "" {
		params.OutputFormat = openai.ImageEditParamsOutputFormat(req.OutputFormat)
	}
	return params
}

func openAIImageFilename(idx int, mimeType string) string {
	extensions, err := mime.ExtensionsByType(mimeType)
	if err == nil && len(extensions) > 0 {
		return fmt.Sprintf("image_%d%s", idx, extensions[0])
	}
	return fmt.Sprintf("image_%d", idx)
}

func openAIImageSDKError(err error) GatewayError {
	gatewayErr := openAISDKErrorToGateway(err, "OpenAI image request failed", 0)
	if gatewayErr.Message == "OpenAI network error" {
		gatewayErr.Message = "OpenAI image network error"
	}
	return gatewayErr
}
