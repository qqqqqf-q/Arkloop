package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
)

type anthropicSDKGateway struct {
	cfg       AnthropicGatewayConfig
	transport protocolTransport
	protocol  AnthropicProtocolConfig
	client    anthropic.Client
	configErr error
	quirks    *QuirkStore
}

func NewAnthropicGatewaySDK(cfg AnthropicGatewayConfig) Gateway {
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
		transport.MaxResponseBytes = cfg.MaxResponseBytes
	}
	if transport.MaxResponseBytes <= 0 {
		transport.MaxResponseBytes = defaultAnthropicMaxResponseBytes
	}

	protocol := cfg.Protocol
	var configErr error
	if protocol.Version == "" && len(protocol.AdvancedPayloadJSON) == 0 && len(protocol.ExtraHeaders) == 0 {
		protocol, configErr = parseAnthropicProtocolConfig(cfg.AdvancedJSON)
		if strings.TrimSpace(cfg.AnthropicVersion) != "" {
			protocol.Version = strings.TrimSpace(cfg.AnthropicVersion)
		}
	}
	if strings.TrimSpace(protocol.Version) == "" {
		protocol.Version = defaultAnthropicVersion
	}

	normalizedTransport := newProtocolTransport(transport, "https://api.anthropic.com", normalizeAnthropicBaseURL)
	cfg.Transport = normalizedTransport.cfg
	cfg.Protocol = protocol
	cfg.EmitDebugEvents = normalizedTransport.cfg.EmitDebugEvents
	cfg.TotalTimeout = normalizedTransport.cfg.TotalTimeout
	cfg.MaxResponseBytes = normalizedTransport.cfg.MaxResponseBytes
	cfg.BaseURL = normalizedTransport.cfg.BaseURL

	authOption := option.WithAPIKey(strings.TrimSpace(normalizedTransport.cfg.APIKey))
	if strings.EqualFold(strings.TrimSpace(normalizedTransport.cfg.AuthScheme), "bearer") {
		authOption = option.WithAuthToken(strings.TrimSpace(normalizedTransport.cfg.APIKey))
	}
	opts := []option.RequestOption{
		authOption,
		option.WithBaseURL(sdkBaseURL(normalizedTransport)),
		option.WithHTTPClient(sdkHTTPClient(normalizedTransport)),
		option.WithHeader("anthropic-version", protocol.Version),
	}
	for key, value := range normalizedTransport.cfg.DefaultHeaders {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			opts = append(opts, option.WithHeader(key, value))
		}
	}
	for key, value := range protocol.ExtraHeaders {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			opts = append(opts, option.WithHeader(key, value))
		}
	}

	return &anthropicSDKGateway{
		cfg:       cfg,
		transport: normalizedTransport,
		protocol:  protocol,
		client:    anthropic.NewClient(opts...),
		configErr: configErr,
		quirks:    NewQuirkStore(),
	}
}

func (g *anthropicSDKGateway) ProtocolKind() ProtocolKind {
	return ProtocolKindAnthropicMessages
}

func (g *anthropicSDKGateway) Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error {
	if g.configErr != nil {
		ge := GatewayError{ErrorClass: ErrorClassInternalError, Message: g.configErr.Error()}
		if typed, ok := g.configErr.(anthropicAdvancedJSONError); ok && len(typed.Details) > 0 {
			ge.Details = typed.Details
		}
		return yield(StreamRunFailed{Error: ge})
	}
	if g.transport.baseURLErr != nil {
		return yield(StreamRunFailed{Error: GatewayError{ErrorClass: ErrorClassInternalError, Message: "Anthropic base_url blocked", Details: map[string]any{"reason": g.transport.baseURLErr.Error()}}})
	}

	ctx, stopTimeout, markActivity := withStreamIdleTimeout(ctx, g.transport.cfg.TotalTimeout)
	defer stopTimeout()
	return g.streamAttempt(ctx, request, yield, markActivity, 0)
}

func (g *anthropicSDKGateway) streamAttempt(ctx context.Context, request Request, yield func(StreamEvent) error, markActivity func(), attempt int) error {
	llmCallID := uuid.NewString()
	PrepareRequestModelInputImages(&request)

	params, payload, providerPayloadBytes, err := g.messageParams(request)
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

	baseURL := g.transport.cfg.BaseURL
	path := "/v1/messages"
	stats := ComputeRequestStats(request)
	debugPayload, redactedHints := sanitizeDebugPayloadJSON(payload)
	networkAttempted := false
	requestEvent := StreamLlmRequest{
		LlmCallID:            llmCallID,
		ProviderKind:         "anthropic",
		APIMode:              "messages",
		BaseURL:              &baseURL,
		Path:                 &path,
		InputJSON:            request.ToJSON(),
		PayloadJSON:          debugPayload,
		RedactedHints:        redactedHints,
		SystemBytes:          stats.SystemBytes,
		ToolsBytes:           stats.ToolsBytes,
		MessagesBytes:        stats.MessagesBytes,
		AbstractRequestBytes: stats.AbstractRequestBytes,
		ProviderPayloadBytes: providerPayloadBytes,
		ImagePartCount:       stats.ImagePartCount,
		Base64ImageBytes:     stats.Base64ImageBytes,
		NetworkAttempted:     &networkAttempted,
		RoleBytes:            stats.RoleBytes,
		ToolSchemaBytesMap:   stats.ToolSchemaBytesMap,
		StablePrefixHash:     stats.StablePrefixHash,
		SessionPrefixHash:    stats.SessionPrefixHash,
		VolatileTailHash:     stats.VolatileTailHash,
		ToolSchemaHash:       stats.ToolSchemaHash,
		StablePrefixBytes:    stats.StablePrefixBytes,
		SessionPrefixBytes:   stats.SessionPrefixBytes,
		VolatileTailBytes:    stats.VolatileTailBytes,
		CacheCandidateBytes:  stats.CacheCandidateBytes,
	}
	if RequestPayloadTooLarge(providerPayloadBytes) {
		if err := yield(requestEvent); err != nil {
			return err
		}
		return yield(PreflightOversizeFailure(llmCallID, providerPayloadBytes))
	}

	networkAttempted = true
	if err := yield(requestEvent); err != nil {
		return err
	}

	opts := make([]option.RequestOption, 0, len(g.protocol.AdvancedPayloadJSON)+4)
	if anthropicSDKMessagesRequireRawJSON(payload) {
		opts = append(opts, option.WithJSONSet("messages", payload["messages"]))
	}
	// quirk 修改的字段必须显式覆盖 SDK params struct 序列化结果，
	// 否则 ApplyAll 对 payload 的修改不会到达上游。新增 quirk 时在这里挂对应 key。
	if g.quirks.Has(QuirkStripUnsignedThinking) {
		opts = append(opts, option.WithJSONSet("messages", payload["messages"]))
	}
	if g.quirks.Has(QuirkEchoEmptyTextOnThink) {
		opts = append(opts, option.WithJSONSet("messages", payload["messages"]))
	}
	if g.quirks.Has(QuirkForceTempOneOnThink) {
		if v, ok := payload["temperature"]; ok {
			opts = append(opts, option.WithJSONSet("temperature", v))
		}
		if v, ok := payload["thinking"]; ok {
			opts = append(opts, option.WithJSONSet("thinking", v))
		}
	}
	for key, value := range g.protocol.AdvancedPayloadJSON {
		opts = append(opts, option.WithJSONSet(key, value))
	}
	responseCapture := newProviderResponseCapture()
	streamCtx := withProviderResponseCapture(ctx, responseCapture)
	stream := g.client.Messages.NewStreaming(streamCtx, params, opts...)
	defer func() { _ = stream.Close() }()

	state := newAnthropicSDKStreamState(ctx, llmCallID, yield)
	for stream.Next() {
		if markActivity != nil {
			markActivity()
		}
		event := stream.Current()
		if err := g.emitDebugChunk(llmCallID, event.RawJSON(), nil, yield); err != nil {
			return err
		}
		if err := state.handle(event); err != nil {
			if errors.Is(err, errAnthropicStreamTerminated) {
				return nil
			}
			return err
		}
	}
	if err := stream.Err(); err != nil {
		if attempt == 0 {
			if id, ok := anthropicSDKDetectQuirk(err, responseCapture); ok {
				if emitErr := yield(StreamQuirkLearned{LlmCallID: llmCallID, ProviderKind: "anthropic", QuirkID: string(id)}); emitErr != nil {
					return emitErr
				}
				g.quirks.Set(id)
				return g.streamAttempt(ctx, request, yield, markActivity, attempt+1)
			}
		}
		if emitErr := g.emitDebugErrorChunk(llmCallID, err, yield); emitErr != nil {
			return emitErr
		}
		if failErr := state.fail(anthropicSDKErrorToGateway(err, providerPayloadBytes, "messages", true, responseCapture, streamCtx)); failErr != nil && !errors.Is(failErr, errAnthropicStreamTerminated) {
			return failErr
		}
		return nil
	}
	return state.complete()
}

func (g *anthropicSDKGateway) emitDebugChunk(llmCallID string, raw string, statusCode *int, yield func(StreamEvent) error) error {
	if !g.transport.cfg.EmitDebugEvents || strings.TrimSpace(raw) == "" {
		return nil
	}
	truncatedRaw, rawTruncated := truncateUTF8(raw, anthropicMaxDebugChunkBytes)
	var chunkJSON any
	_ = json.Unmarshal([]byte(raw), &chunkJSON)
	return yield(StreamLlmResponseChunk{LlmCallID: llmCallID, ProviderKind: "anthropic", APIMode: "messages", Raw: truncatedRaw, ChunkJSON: chunkJSON, StatusCode: statusCode, Truncated: rawTruncated})
}

func (g *anthropicSDKGateway) emitDebugErrorChunk(llmCallID string, err error, yield func(StreamEvent) error) error {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return nil
	}
	status := apiErr.StatusCode
	return g.emitDebugChunk(llmCallID, string(apiErr.DumpResponse(true)), &status, yield)
}

func (g *anthropicSDKGateway) anthropicReasoningMode(mode string) string {
	mode = strings.TrimSpace(mode)
	if mode != "" && !strings.EqualFold(mode, "auto") {
		return mode
	}
	if isDeepSeekAnthropicBaseURL(g.transport.cfg.BaseURL) {
		return "disabled"
	}
	return mode
}

func isDeepSeekAnthropicBaseURL(baseURL string) bool {
	baseURL = strings.ToLower(strings.TrimSpace(baseURL))
	return strings.Contains(baseURL, "api.deepseek.com")
}

func anthropicSDKDetectQuirk(err error, responseCapture *providerResponseCapture) (QuirkID, bool) {
	status := 0
	rawBody := ""
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		status = apiErr.StatusCode
		rawBody = string(apiErr.DumpResponse(true))
	}
	if responseCapture != nil {
		details := responseCapture.details()
		if status == 0 {
			status, _ = details["status_code"].(int)
		}
		if tail, _ := details["provider_response_tail"].(string); strings.TrimSpace(tail) != "" {
			rawBody += "\n" + tail
		}
	}
	return detectQuirk(status, rawBody, anthropicQuirks)
}

func (g *anthropicSDKGateway) messageParams(request Request) (anthropic.MessageNewParams, map[string]any, int, error) {
	systemMaps, messageMaps, err := toAnthropicMessagesWithPlan(request.Messages, request.PromptPlan)
	if err != nil {
		return anthropic.MessageNewParams{}, nil, 0, err
	}
	maxTokens := defaultAnthropicMaxTokens
	if request.MaxOutputTokens != nil && *request.MaxOutputTokens > 0 {
		maxTokens = *request.MaxOutputTokens
	}

	payload := map[string]any{
		"model":      request.Model,
		"messages":   messageMaps,
		"max_tokens": maxTokens,
		"stream":     true,
	}
	if len(systemMaps) > 0 {
		payload["system"] = systemMaps
	}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	if len(request.Tools) > 0 {
		payload["tools"] = toAnthropicTools(request.Tools)
		if tc := anthropicToolChoice(request.ToolChoice); tc != nil {
			payload["tool_choice"] = tc
		}
	}
	for key, value := range g.protocol.AdvancedPayloadJSON {
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}
	applyAnthropicReasoningMode(payload, g.anthropicReasoningMode(request.ReasoningMode))
	enforceAnthropicCacheControlLimit(payload)
	g.quirks.ApplyAll(payload, anthropicQuirks)

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(request.Model),
		MaxTokens: int64(maxTokens),
		Messages:  make([]anthropic.MessageParam, 0, len(messageMaps)),
	}
	if request.Temperature != nil {
		params.Temperature = anthropic.Float(*request.Temperature)
	}
	if len(systemMaps) > 0 {
		params.System, err = anthropicSDKSystemBlocks(systemMaps)
		if err != nil {
			return anthropic.MessageNewParams{}, nil, 0, err
		}
	}
	params.Messages, err = anthropicSDKMessages(messageMaps)
	if err != nil {
		return anthropic.MessageNewParams{}, nil, 0, err
	}
	if len(request.Tools) > 0 {
		params.Tools, err = anthropicSDKTools(request.Tools)
		if err != nil {
			return anthropic.MessageNewParams{}, nil, 0, err
		}
		if payloadTools, ok := payload["tools"].([]map[string]any); ok {
			params.Tools, err = anthropicSDKToolsFromPayload(payloadTools)
			if err != nil {
				return anthropic.MessageNewParams{}, nil, 0, err
			}
		}
		params.ToolChoice = anthropicSDKToolChoice(request.ToolChoice)
	}
	params.Thinking = anthropicSDKThinking(payload)
	if mt := anyToInt(payload["max_tokens"]); mt > 0 {
		params.MaxTokens = int64(mt)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return anthropic.MessageNewParams{}, nil, 0, fmt.Errorf("anthropic request serialization failed: %w", err)
	}
	return params, payload, len(encoded), nil
}

func anthropicSDKMessagesRequireRawJSON(payload map[string]any) bool {
	messages, ok := payload["messages"].([]map[string]any)
	if !ok {
		return false
	}
	for _, message := range messages {
		content, ok := message["content"].([]map[string]any)
		if !ok {
			continue
		}
		for _, block := range content {
			if typ, _ := block["type"].(string); typ == "cache_edits" {
				return true
			}
			if typ, _ := block["type"].(string); typ == "thinking" {
				if strings.TrimSpace(stringValueFromAny(block["signature"])) == "" {
					return true
				}
			}
			if _, ok := block["cache_reference"]; ok {
				return true
			}
		}
	}
	return false
}

func anthropicSDKEmptyInput(input any) bool {
	switch typed := input.(type) {
	case map[string]any:
		return len(typed) == 0
	default:
		encoded, err := json.Marshal(input)
		return err == nil && string(encoded) == "{}"
	}
}

func anthropicSDKSystemBlocks(blocks []map[string]any) ([]anthropic.TextBlockParam, error) {
	out := make([]anthropic.TextBlockParam, 0, len(blocks))
	for _, block := range blocks {
		text, _ := block["text"].(string)
		if strings.TrimSpace(text) == "" {
			continue
		}
		param := anthropic.TextBlockParam{Text: text}
		if cc, ok := block["cache_control"].(map[string]any); ok && anthropicSDKCacheControl(cc) {
			param.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		out = append(out, param)
	}
	return out, nil
}

func anthropicSDKMessages(messages []map[string]any) ([]anthropic.MessageParam, error) {
	out := make([]anthropic.MessageParam, 0, len(messages))
	for _, message := range messages {
		role, _ := message["role"].(string)
		content, ok := message["content"].([]map[string]any)
		if !ok {
			return nil, fmt.Errorf("anthropic message content must be blocks")
		}
		blocks, err := anthropicSDKContentBlocks(content)
		if err != nil {
			return nil, err
		}
		switch role {
		case "user":
			out = append(out, anthropic.NewUserMessage(blocks...))
		case "assistant":
			out = append(out, anthropic.NewAssistantMessage(blocks...))
		default:
			return nil, fmt.Errorf("unsupported anthropic message role %q", role)
		}
	}
	return out, nil
}

func anthropicSDKContentBlocks(blocks []map[string]any) ([]anthropic.ContentBlockParamUnion, error) {
	out := make([]anthropic.ContentBlockParamUnion, 0, len(blocks))
	for _, block := range blocks {
		converted, err := anthropicSDKContentBlock(block)
		if err != nil {
			return nil, err
		}
		if converted != nil {
			out = append(out, *converted)
		}
	}
	return out, nil
}

func anthropicSDKContentBlock(block map[string]any) (*anthropic.ContentBlockParamUnion, error) {
	switch typ, _ := block["type"].(string); typ {
	case "text":
		text, _ := block["text"].(string)
		if strings.TrimSpace(text) == "" {
			return nil, nil
		}
		param := anthropic.TextBlockParam{Text: text}
		if cc, ok := block["cache_control"].(map[string]any); ok && anthropicSDKCacheControl(cc) {
			param.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		return &anthropic.ContentBlockParamUnion{OfText: &param}, nil
	case "thinking":
		signature, _ := block["signature"].(string)
		signature = strings.TrimSpace(signature)
		if signature == "" {
			return nil, nil
		}
		thinking, _ := block["thinking"].(string)
		param := anthropic.NewThinkingBlock(signature, thinking)
		return &param, nil
	case "redacted_thinking":
		data, _ := block["data"].(string)
		param := anthropic.NewRedactedThinkingBlock(data)
		return &param, nil
	case "tool_use":
		id, _ := block["id"].(string)
		name, _ := block["name"].(string)
		input := mapOrEmpty(nil)
		if obj, ok := block["input"].(map[string]any); ok {
			input = mapOrEmpty(obj)
		}
		param := anthropic.ToolUseBlockParam{ID: id, Name: name, Input: input}
		if cc, ok := block["cache_control"].(map[string]any); ok && anthropicSDKCacheControl(cc) {
			param.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		return &anthropic.ContentBlockParamUnion{OfToolUse: &param}, nil
	case "tool_result":
		param, err := anthropicSDKToolResultBlock(block)
		if err != nil {
			return nil, err
		}
		return &anthropic.ContentBlockParamUnion{OfToolResult: param}, nil
	case "image":
		param, err := anthropicSDKImageBlock(block)
		if err != nil {
			return nil, err
		}
		return &anthropic.ContentBlockParamUnion{OfImage: param}, nil
	case "cache_edits":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported anthropic content block type %q", typ)
	}
}

func anthropicSDKToolResultBlock(block map[string]any) (*anthropic.ToolResultBlockParam, error) {
	toolUseID, _ := block["tool_use_id"].(string)
	param := &anthropic.ToolResultBlockParam{ToolUseID: strings.TrimSpace(toolUseID)}
	if isError, _ := block["is_error"].(bool); isError {
		param.IsError = anthropic.Bool(true)
	}
	if cc, ok := block["cache_control"].(map[string]any); ok && anthropicSDKCacheControl(cc) {
		param.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	switch content := block["content"].(type) {
	case string:
		param.Content = []anthropic.ToolResultBlockParamContentUnion{{OfText: &anthropic.TextBlockParam{Text: content}}}
	case []map[string]any:
		parts := make([]anthropic.ToolResultBlockParamContentUnion, 0, len(content))
		for _, item := range content {
			switch typ, _ := item["type"].(string); typ {
			case "text":
				text, _ := item["text"].(string)
				parts = append(parts, anthropic.ToolResultBlockParamContentUnion{OfText: &anthropic.TextBlockParam{Text: text}})
			case "image":
				image, err := anthropicSDKImageBlock(item)
				if err != nil {
					return nil, err
				}
				parts = append(parts, anthropic.ToolResultBlockParamContentUnion{OfImage: image})
			}
		}
		param.Content = parts
	case []any:
		converted := make([]map[string]any, 0, len(content))
		for _, item := range content {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			converted = append(converted, obj)
		}
		return anthropicSDKToolResultBlock(map[string]any{"tool_use_id": toolUseID, "is_error": block["is_error"], "cache_control": block["cache_control"], "content": converted})
	}
	return param, nil
}

func anthropicSDKImageBlock(block map[string]any) (*anthropic.ImageBlockParam, error) {
	source, ok := block["source"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("anthropic image block missing source")
	}
	mediaType, _ := source["media_type"].(string)
	data, _ := source["data"].(string)
	if strings.TrimSpace(mediaType) == "" || strings.TrimSpace(data) == "" {
		return nil, fmt.Errorf("anthropic image block missing media data")
	}
	base64Source := &anthropic.Base64ImageSourceParam{
		MediaType: anthropic.Base64ImageSourceMediaType(mediaType),
		Data:      data,
	}
	return &anthropic.ImageBlockParam{Source: anthropic.ImageBlockParamSourceUnion{OfBase64: base64Source}}, nil
}

func anthropicSDKRawContentBlock(block map[string]any) (*anthropic.ContentBlockParamUnion, error) {
	encoded, err := json.Marshal(block)
	if err != nil {
		return nil, err
	}
	var param anthropic.ContentBlockParamUnion
	if err := json.Unmarshal(encoded, &param); err != nil {
		return nil, err
	}
	return &param, nil
}

func anthropicSDKCacheControl(block map[string]any) bool {
	typ, _ := block["type"].(string)
	return strings.TrimSpace(typ) == "ephemeral"
}

func anthropicSDKTools(specs []ToolSpec) ([]anthropic.ToolUnionParam, error) {
	sortedSpecs := append([]ToolSpec(nil), specs...)
	sort.SliceStable(sortedSpecs, func(i, j int) bool {
		left := CanonicalToolName(sortedSpecs[i].Name)
		if left == "" {
			left = sortedSpecs[i].Name
		}
		right := CanonicalToolName(sortedSpecs[j].Name)
		if right == "" {
			right = sortedSpecs[j].Name
		}
		return left < right
	})
	out := make([]anthropic.ToolUnionParam, 0, len(sortedSpecs))
	for _, spec := range sortedSpecs {
		name := CanonicalToolName(spec.Name)
		if name == "" {
			name = spec.Name
		}
		tool := anthropic.ToolParam{
			Name:        name,
			InputSchema: anthropic.ToolInputSchemaParam{ExtraFields: mapOrEmpty(spec.JSONSchema)},
		}
		if spec.Description != nil {
			tool.Description = anthropic.String(*spec.Description)
		}
		if cc := anthropicCacheControlFromHints(spec.CacheHint, nil); cc != nil && anthropicSDKCacheControl(cc) {
			tool.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tool})
	}
	return out, nil
}

func anthropicSDKToolsFromPayload(tools []map[string]any) ([]anthropic.ToolUnionParam, error) {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, payload := range tools {
		name, _ := payload["name"].(string)
		inputSchema, _ := payload["input_schema"].(map[string]any)
		tool := anthropic.ToolParam{
			Name:        strings.TrimSpace(name),
			InputSchema: anthropic.ToolInputSchemaParam{ExtraFields: mapOrEmpty(inputSchema)},
		}
		if description, _ := payload["description"].(string); strings.TrimSpace(description) != "" {
			tool.Description = anthropic.String(description)
		}
		if cc, ok := payload["cache_control"].(map[string]any); ok && anthropicSDKCacheControl(cc) {
			tool.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tool})
	}
	return out, nil
}

func anthropicSDKToolChoice(tc *ToolChoice) anthropic.ToolChoiceUnionParam {
	if tc == nil {
		return anthropic.ToolChoiceUnionParam{}
	}
	switch tc.Mode {
	case "required":
		return anthropic.ToolChoiceUnionParam{OfAny: &anthropic.ToolChoiceAnyParam{}}
	case "specific":
		return anthropic.ToolChoiceUnionParam{OfTool: &anthropic.ToolChoiceToolParam{Name: CanonicalToolName(tc.ToolName)}}
	default:
		return anthropic.ToolChoiceUnionParam{}
	}
}

func anthropicSDKThinking(payload map[string]any) anthropic.ThinkingConfigParamUnion {
	thinking, ok := payload["thinking"].(map[string]any)
	if !ok {
		return anthropic.ThinkingConfigParamUnion{}
	}
	typ, _ := thinking["type"].(string)
	if strings.TrimSpace(typ) == "disabled" {
		return anthropic.ThinkingConfigParamUnion{OfDisabled: &anthropic.ThinkingConfigDisabledParam{}}
	}
	budget := anyToInt(thinking["budget_tokens"])
	if budget <= 0 {
		budget = defaultAnthropicThinkingBudget
	}
	return anthropic.ThinkingConfigParamOfEnabled(int64(budget))
}

type anthropicSDKStreamState struct {
	ctx             context.Context
	llmCallID       string
	yield           func(StreamEvent) error
	usage           *Usage
	cost            *Cost
	toolBuffers     map[int]*anthropicToolUseBuffer
	assistantBlocks map[int]*anthropicAssistantBlock
	completed       bool
}

func newAnthropicSDKStreamState(ctx context.Context, llmCallID string, yield func(StreamEvent) error) *anthropicSDKStreamState {
	return &anthropicSDKStreamState{
		ctx:             ctx,
		llmCallID:       llmCallID,
		yield:           yield,
		toolBuffers:     map[int]*anthropicToolUseBuffer{},
		assistantBlocks: map[int]*anthropicAssistantBlock{},
	}
}

func (s *anthropicSDKStreamState) handle(event anthropic.MessageStreamEventUnion) error {
	if parsed := anthropicSDKUsageFromMessage(event.Message); parsed != nil {
		s.usage = parsed
	}
	if parsed := anthropicSDKUsageFromDelta(event.Usage); parsed != nil {
		s.usage = parsed
	}
	if cost := anthropicSDKCostFromRaw(event.Message.RawJSON()); cost != nil {
		s.cost = cost
	}

	switch ev := event.AsAny().(type) {
	case anthropic.MessageStartEvent:
		if parsed := anthropicSDKUsageFromMessage(ev.Message); parsed != nil {
			s.usage = parsed
		}
		if cost := anthropicSDKCostFromRaw(ev.Message.RawJSON()); cost != nil {
			s.cost = cost
		}
	case anthropic.ContentBlockStartEvent:
		return s.handleContentBlockStart(ev)
	case anthropic.ContentBlockDeltaEvent:
		return s.handleContentBlockDelta(ev)
	case anthropic.ContentBlockStopEvent:
		return s.handleContentBlockStop(int(ev.Index))
	case anthropic.MessageDeltaEvent:
		if parsed := anthropicSDKUsageFromDelta(ev.Usage); parsed != nil {
			s.usage = parsed
		}
		if strings.TrimSpace(string(ev.Delta.StopReason)) == "refusal" {
			return s.fail(GatewayError{ErrorClass: ErrorClassPolicyDenied, Message: "Anthropic response refused", Details: map[string]any{"stop_reason": string(ev.Delta.StopReason)}})
		}
	case anthropic.MessageStopEvent:
		s.completed = true
	}
	return nil
}

func (s *anthropicSDKStreamState) handleContentBlockStart(event anthropic.ContentBlockStartEvent) error {
	idx := int(event.Index)
	block := event.ContentBlock
	switch block.Type {
	case "text":
		buffer := &anthropicAssistantBlock{Type: "text"}
		buffer.Text.WriteString(block.Text)
		s.assistantBlocks[idx] = buffer
		if strings.TrimSpace(block.Text) == "" {
			return nil
		}
		return s.yield(StreamMessageDelta{ContentDelta: block.Text, Role: "assistant"})
	case "thinking":
		buffer := &anthropicAssistantBlock{Type: "thinking", Signature: strings.TrimSpace(block.Signature)}
		buffer.Text.WriteString(block.Thinking)
		s.assistantBlocks[idx] = buffer
		if strings.TrimSpace(block.Thinking) == "" {
			return nil
		}
		channel := "thinking"
		return s.yield(StreamMessageDelta{ContentDelta: block.Thinking, Role: "assistant", Channel: &channel})
	case "tool_use":
		buffer := &anthropicToolUseBuffer{ID: strings.TrimSpace(block.ID), Name: strings.TrimSpace(block.Name)}
		s.toolBuffers[idx] = buffer
		if block.Input == nil || anthropicSDKEmptyInput(block.Input) {
			return nil
		}
		encoded, err := json.Marshal(block.Input)
		if err != nil {
			return err
		}
		buffer.JSON.Write(encoded)
		return s.yield(ToolCallArgumentDelta{ToolCallIndex: idx, ToolCallID: buffer.ID, ToolName: CanonicalToolName(buffer.Name), ArgumentsDelta: string(encoded)})
	default:
		return nil
	}
}

func (s *anthropicSDKStreamState) handleContentBlockDelta(event anthropic.ContentBlockDeltaEvent) error {
	idx := int(event.Index)
	delta := event.Delta
	switch delta.Type {
	case "text_delta":
		return s.appendAssistantDelta(idx, "text", delta.Text, nil)
	case "thinking_delta":
		channel := "thinking"
		return s.appendAssistantDelta(idx, "thinking", delta.Thinking, &channel)
	case "signature_delta":
		if buffer := s.assistantBlocks[idx]; buffer != nil {
			buffer.Signature = strings.TrimSpace(delta.Signature)
		}
		return nil
	case "input_json_delta":
		buffer := s.toolBuffers[idx]
		if buffer == nil {
			return nil
		}
		buffer.JSON.WriteString(delta.PartialJSON)
		return s.yield(ToolCallArgumentDelta{ToolCallIndex: idx, ToolCallID: buffer.ID, ToolName: CanonicalToolName(buffer.Name), ArgumentsDelta: delta.PartialJSON})
	default:
		return nil
	}
}

func (s *anthropicSDKStreamState) appendAssistantDelta(idx int, blockType string, text string, channel *string) error {
	if text == "" {
		return nil
	}
	if buffer := s.assistantBlocks[idx]; buffer != nil {
		if text == buffer.Text.String() && !buffer.DeltaSeen {
			buffer.DeltaSeen = true
			return nil
		}
		buffer.DeltaSeen = true
		buffer.Text.WriteString(text)
	} else {
		s.assistantBlocks[idx] = &anthropicAssistantBlock{Type: blockType}
		s.assistantBlocks[idx].Text.WriteString(text)
	}
	return s.yield(StreamMessageDelta{ContentDelta: text, Role: "assistant", Channel: channel})
}

func (s *anthropicSDKStreamState) handleContentBlockStop(idx int) error {
	buffer := s.toolBuffers[idx]
	if buffer == nil {
		return nil
	}
	delete(s.toolBuffers, idx)
	if strings.TrimSpace(buffer.ID) == "" || strings.TrimSpace(buffer.Name) == "" {
		return s.fail(GatewayError{ErrorClass: ErrorClassProviderNonRetryable, Message: "Anthropic tool_use input parse failed", Details: map[string]any{"reason": "content block missing tool_use id or name"}})
	}
	toolName := CanonicalToolName(buffer.Name)
	argumentsJSON := map[string]any{}
	rawArgs := strings.TrimSpace(buffer.JSON.String())
	if rawArgs != "" {
		var parsed any
		if err := json.Unmarshal([]byte(rawArgs), &parsed); err != nil {
			if yieldErr := s.yield(ToolCall{ToolCallID: buffer.ID, ToolName: toolName, ArgumentsJSON: argumentsJSON}); yieldErr != nil {
				return yieldErr
			}
			return s.yield(formatRepairToolResult(ParseWarning{
				ToolCallID: buffer.ID,
				ToolName:   toolName,
				Message:    fmt.Sprintf("Anthropic tool_use input is not valid JSON. Arguments must be a valid JSON object. Raw: %s", truncateRaw(rawArgs, 200)),
			}, s.llmCallID))
		}
		obj, ok := parsed.(map[string]any)
		if !ok {
			if yieldErr := s.yield(ToolCall{ToolCallID: buffer.ID, ToolName: toolName, ArgumentsJSON: argumentsJSON}); yieldErr != nil {
				return yieldErr
			}
			return s.yield(formatRepairToolResult(ParseWarning{
				ToolCallID: buffer.ID,
				ToolName:   toolName,
				Message:    fmt.Sprintf("Anthropic tool_use input must be a JSON object, got %T", parsed),
			}, s.llmCallID))
		}
		argumentsJSON = obj
	}
	return s.yield(ToolCall{ToolCallID: buffer.ID, ToolName: toolName, ArgumentsJSON: argumentsJSON})
}

func (s *anthropicSDKStreamState) fail(gatewayErr GatewayError) error {
	if err := s.yield(StreamRunFailed{LlmCallID: s.llmCallID, Error: gatewayErr, Usage: s.usage, Cost: s.cost}); err != nil {
		return err
	}
	return errAnthropicStreamTerminated
}

func (s *anthropicSDKStreamState) complete() error {
	if !s.completed {
		streamErr := InternalStreamEndedError()
		if s.usage != nil || len(s.assistantBlocks) > 0 || len(s.toolBuffers) > 0 {
			streamErr = RetryableStreamEndedError()
		}
		return s.yield(StreamRunFailed{LlmCallID: s.llmCallID, Error: streamErr, Usage: s.usage, Cost: s.cost})
	}
	assistantMessage := Message{Role: "assistant", Content: anthropicAssistantMessageParts(s.assistantBlocks)}
	logProviderCompletionDebug(s.ctx, providerCompletionDebug{
		ProviderKind:     "anthropic",
		APIMode:          "messages",
		LlmCallID:        s.llmCallID,
		AssistantMessage: &assistantMessage,
	})
	return s.yield(StreamRunCompleted{LlmCallID: s.llmCallID, Usage: s.usage, Cost: s.cost, AssistantMessage: &assistantMessage})
}

func anthropicSDKUsageFromMessage(message anthropic.Message) *Usage {
	return anthropicSDKUsage(message.Usage.InputTokens, message.Usage.OutputTokens, message.Usage.CacheCreationInputTokens, message.Usage.CacheReadInputTokens)
}

func anthropicSDKUsageFromDelta(usage anthropic.MessageDeltaUsage) *Usage {
	return anthropicSDKUsage(usage.InputTokens, usage.OutputTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens)
}

func anthropicSDKUsage(input int64, output int64, cacheCreate int64, cacheRead int64) *Usage {
	if input == 0 && output == 0 && cacheCreate == 0 && cacheRead == 0 {
		return nil
	}
	u := &Usage{}
	if input > 0 {
		iv := int(input)
		u.InputTokens = &iv
	}
	if output > 0 {
		ov := int(output)
		u.OutputTokens = &ov
	}
	if cacheCreate > 0 {
		cv := int(cacheCreate)
		u.CacheCreationInputTokens = &cv
	}
	if cacheRead > 0 {
		rv := int(cacheRead)
		u.CacheReadInputTokens = &rv
	}
	return u
}

func anthropicSDKCostFromRaw(raw string) *Cost {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var root map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return nil
	}
	if usage, ok := root["usage"].(map[string]any); ok {
		return parseResponsesCost(usage)
	}
	return parseResponsesCost(root)
}

func anthropicSDKErrorToGateway(err error, payloadBytes int, apiMode string, streaming bool, responseCapture *providerResponseCapture, ctx context.Context) GatewayError {
	if errors.Is(err, errAnthropicStreamTerminated) {
		return GatewayError{ErrorClass: ErrorClassInternalStreamEnded, Message: err.Error()}
	}
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		message, details := anthropicErrorMessageAndDetails([]byte(apiErr.RawJSON()), apiErr.StatusCode)
		if details == nil {
			details = map[string]any{}
		}
		details["provider_kind"] = "anthropic"
		details["api_mode"] = apiMode
		details["network_attempted"] = true
		if streaming {
			details["streaming"] = true
		}
		if errorType := strings.TrimSpace(string(apiErr.Type())); errorType != "" {
			details["anthropic_error_type"] = errorType
		}
		if requestID := strings.TrimSpace(apiErr.RequestID); requestID != "" {
			details["provider_request_id"] = requestID
		} else if apiErr.Response != nil {
			if requestID := sdkProviderRequestID(apiErr.Response.Header); requestID != "" {
				details["provider_request_id"] = requestID
			}
		}
		if apiErr.StatusCode == http.StatusRequestEntityTooLarge {
			details = OversizeFailureDetails(payloadBytes, OversizePhaseProvider, details)
		}
		details = mergeProviderResponseCaptureDetails(details, responseCapture)
		return GatewayError{ErrorClass: anthropicSDKErrorClass(apiErr, details), Message: message, Details: details}
	}
	details := sdkTransportErrorDetails(err, "anthropic", apiMode, streaming, true)
	details = mergeContextErrorDetails(details, err, ctx)
	details = mergeProviderResponseCaptureDetails(details, responseCapture)
	return GatewayError{ErrorClass: ErrorClassProviderRetryable, Message: "Anthropic network error", Details: details}
}

func anthropicSDKErrorClass(err *anthropic.Error, details map[string]any) string {
	if err == nil {
		return ErrorClassProviderRetryable
	}
	if err.StatusCode == http.StatusBadRequest {
		if errorType, _ := details["anthropic_error_type"].(string); errorType == "context_length_exceeded" || errorType == "invalid_value" {
			return ErrorClassProviderNonRetryable
		}
		return ErrorClassProviderNonRetryable
	}
	switch err.Type() {
	case anthropic.ErrorTypeOverloadedError, anthropic.ErrorTypeRateLimitError, anthropic.ErrorTypeTimeoutError, anthropic.ErrorTypeAPIError:
		return ErrorClassProviderRetryable
	case anthropic.ErrorTypeAuthenticationError, anthropic.ErrorTypeInvalidRequestError, anthropic.ErrorTypeNotFoundError, anthropic.ErrorTypePermissionError, anthropic.ErrorTypeBillingError:
		return ErrorClassProviderNonRetryable
	default:
		return classifyHTTPStatus(err.StatusCode)
	}
}

func anthropicSDKImageContentPart(part ContentPart) (map[string]any, error) {
	mimeType, data, err := modelInputImage(part)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type": "image",
		"source": map[string]any{
			"type":       "base64",
			"media_type": mimeType,
			"data":       base64.StdEncoding.EncodeToString(data),
		},
	}, nil
}
