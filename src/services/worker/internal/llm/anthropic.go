package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"arkloop/services/worker/internal/stablejson"
	"github.com/google/uuid"
)

const defaultAnthropicVersion = "2023-06-01"
const defaultAnthropicMaxResponseBytes = 16 * 1024
const anthropicMaxDebugChunkBytes = 8192
const defaultAnthropicMaxTokens = 32768

var errAnthropicToolUseInput = errors.New("anthropic_tool_use_input")
var errAnthropicStreamTerminated = errors.New("anthropic_stream_terminated")

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

const (
	anthropicAdvancedKeyVersion      = "anthropic_version"
	anthropicAdvancedKeyExtraHeaders = "extra_headers"
	anthropicBetaHeader              = "anthropic-beta"
	anthropicMinThinkingBudget       = 1024
	anthropicLowThinkingBudget       = 4096
	defaultAnthropicThinkingBudget   = 8192
	anthropicHighThinkingBudget      = 16384
	anthropicMaxThinkingBudget       = 32768
)

type anthropicAdvancedJSONError struct {
	Message string
	Details map[string]any
}

func (e anthropicAdvancedJSONError) Error() string { return e.Message }

type anthropicAdvancedConfig struct {
	Version      *string
	ExtraHeaders map[string]string
	Payload      map[string]any
}

type AnthropicGatewayConfig struct {
	Transport        TransportConfig
	Protocol         AnthropicProtocolConfig
	APIKey           string
	BaseURL          string
	AnthropicVersion string
	EmitDebugEvents  bool
	TotalTimeout     time.Duration
	MaxResponseBytes int
	AdvancedJSON     map[string]any
}

type AnthropicGateway struct {
	cfg       AnthropicGatewayConfig
	transport protocolTransport
	protocol  AnthropicProtocolConfig
	configErr error
}

func NewAnthropicGateway(cfg AnthropicGatewayConfig) *AnthropicGateway {
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

	normalizedTransport := newProtocolTransport(transport, "https://api.anthropic.com", normalizeAnthropicBaseURL)
	cfg.Transport = normalizedTransport.cfg
	cfg.Protocol = protocol
	cfg.EmitDebugEvents = normalizedTransport.cfg.EmitDebugEvents
	cfg.TotalTimeout = normalizedTransport.cfg.TotalTimeout
	cfg.MaxResponseBytes = normalizedTransport.cfg.MaxResponseBytes
	cfg.BaseURL = normalizedTransport.cfg.BaseURL

	return &AnthropicGateway{
		cfg:       cfg,
		transport: normalizedTransport,
		protocol:  protocol,
		configErr: configErr,
	}
}

func (g *AnthropicGateway) ProtocolKind() ProtocolKind {
	return ProtocolKindAnthropicMessages
}

func (g *AnthropicGateway) Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error {
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
	ctx, stopTimeout, _ := withStreamIdleTimeout(ctx, g.transport.cfg.TotalTimeout)
	defer stopTimeout()
	llmCallID := uuid.NewString()

	system, messages, err := toAnthropicMessagesWithPlan(request.Messages, request.PromptPlan)
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
	maxTokens := defaultAnthropicMaxTokens
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
		if tc := anthropicToolChoice(request.ToolChoice); tc != nil {
			payload["tool_choice"] = tc
		}
	}
	payload["stream"] = true

	// merge keys not already present in payload
	for k, v := range g.protocol.AdvancedPayloadJSON {
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}
	applyAnthropicReasoningMode(payload, request.ReasoningMode)
	baseURL := g.transport.cfg.BaseURL
	path := "/v1/messages"
	stats := ComputeRequestStats(request)
	debugPayload, redactedHints := sanitizeDebugPayloadJSON(payload)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "Anthropic request serialization failed",
			},
		})
	}
	networkAttempted := false
	requestEvent := StreamLlmRequest{
		LlmCallID:            llmCallID,
		ProviderKind:         "anthropic",
		APIMode:              "messages",
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
		SessionPrefixHash:    stats.SessionPrefixHash,
		VolatileTailHash:     stats.VolatileTailHash,
		ToolSchemaHash:       stats.ToolSchemaHash,
		StablePrefixBytes:    stats.StablePrefixBytes,
		SessionPrefixBytes:   stats.SessionPrefixBytes,
		VolatileTailBytes:    stats.VolatileTailBytes,
		CacheCandidateBytes:  stats.CacheCandidateBytes,
	}
	if RequestPayloadTooLarge(len(encoded)) {
		if err := yield(requestEvent); err != nil {
			return err
		}
		return yield(PreflightOversizeFailure(llmCallID, len(encoded)))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.transport.endpoint("/v1/messages"), bytes.NewReader(encoded))
	if err != nil {
		if err := yield(requestEvent); err != nil {
			return err
		}
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassInternalError,
				Message:    "Anthropic request construction failed",
				Details:    map[string]any{"reason": err.Error()},
			},
		})
	}
	networkAttempted = true
	if err := yield(requestEvent); err != nil {
		return err
	}
	req.Header.Set("x-api-key", strings.TrimSpace(g.transport.cfg.APIKey))
	req.Header.Set("anthropic-version", g.protocol.Version)
	for k, v := range g.protocol.ExtraHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	g.transport.applyDefaultHeaders(req)

	resp, err := g.transport.client.Do(req)
	if err != nil {
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: ErrorClassProviderRetryable,
				Message:    "Anthropic network error",
			},
		})
	}
	defer resp.Body.Close()

	status := resp.StatusCode

	if status < 200 || status >= 300 {
		body, bodyTruncated, _ := readAllWithLimit(resp.Body, g.transport.cfg.MaxResponseBytes)
		if g.transport.cfg.EmitDebugEvents {
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
		message, details := anthropicErrorMessageAndDetails(body, status)
		if status == http.StatusRequestEntityTooLarge {
			details["network_attempted"] = true
			details = OversizeFailureDetails(len(encoded), OversizePhaseProvider, details)
		}
		return yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: errorClassFromStatus(status),
				Message:    message,
				Details:    details,
			},
		})
	}

	return g.streamAnthropicSSE(ctx, resp.Body, llmCallID, yield)
}

func parseAnthropicAdvancedJSON(raw map[string]any) (anthropicAdvancedConfig, error) {
	cfg := anthropicAdvancedConfig{
		ExtraHeaders: map[string]string{},
		Payload:      map[string]any{},
	}
	if raw == nil {
		return cfg, nil
	}

	for key, value := range raw {
		switch key {
		case anthropicAdvancedKeyVersion:
			version, ok := value.(string)
			if !ok || strings.TrimSpace(version) == "" {
				return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
					Message: "advanced_json.anthropic_version must be a non-empty string",
				}
			}
			v := strings.TrimSpace(version)
			cfg.Version = &v
		case anthropicAdvancedKeyExtraHeaders:
			headers, ok := value.(map[string]any)
			if !ok {
				return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
					Message: "advanced_json.extra_headers must be an object",
				}
			}
			for hk, hv := range headers {
				headerName := strings.ToLower(strings.TrimSpace(hk))
				if headerName != anthropicBetaHeader {
					return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
						Message: "advanced_json.extra_headers only supports anthropic-beta",
						Details: map[string]any{"invalid_header": hk},
					}
				}
				headerValue, ok := hv.(string)
				if !ok || strings.TrimSpace(headerValue) == "" {
					return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
						Message: "advanced_json.extra_headers.anthropic-beta must be a non-empty string",
					}
				}
				cfg.ExtraHeaders[anthropicBetaHeader] = strings.TrimSpace(headerValue)
			}
		default:
			if _, denied := anthropicAdvancedJSONDenylist[key]; denied {
				return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
					Message: fmt.Sprintf("advanced_json must not set critical field: %s", key),
					Details: map[string]any{"denied_key": key},
				}
			}
			cfg.Payload[key] = value
		}
	}

	return cfg, nil
}

func toAnthropicMessages(messages []Message) ([]map[string]any, []map[string]any, error) {
	return toAnthropicMessagesWithPlan(messages, nil)
}

func toAnthropicMessagesWithPlan(messages []Message, plan *PromptPlan) ([]map[string]any, []map[string]any, error) {
	systemBlocks := []map[string]any{}
	out := []map[string]any{}
	pendingToolResults := []map[string]any{}
	lastAssistantToolUseIDs := map[string]struct{}{}
	// 记录最后一个带 tool_use 的 assistant 在 out 中的索引，便于回退
	lastToolUseAssistantIdx := -1
	sourceToOut := map[int]int{}
	userSourceToOut := map[int]int{}

	if len(messages) == 0 {
		return systemBlocks, out, nil
	}

	planSystemBlocks := anthropicSystemBlocksFromPlan(plan)
	if len(planSystemBlocks) > 0 {
		systemBlocks = append(systemBlocks, planSystemBlocks...)
	}

	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			// 没有 tool_result 但有 tool_use -> 回退 assistant 中的 tool_use blocks
			if lastToolUseAssistantIdx >= 0 && lastToolUseAssistantIdx < len(out) {
				stripToolUseBlocks(out, lastToolUseAssistantIdx, lastAssistantToolUseIDs)
			}
			lastAssistantToolUseIDs = map[string]struct{}{}
			lastToolUseAssistantIdx = -1
			return
		}
		filtered := make([]map[string]any, 0, len(pendingToolResults))
		matchedToolUseIDs := map[string]struct{}{}
		for _, block := range pendingToolResults {
			id, _ := block["tool_use_id"].(string)
			if _, ok := lastAssistantToolUseIDs[id]; ok {
				filtered = append(filtered, block)
				matchedToolUseIDs[id] = struct{}{}
			} else {
				prevRole := ""
				prevSummary := ""
				if len(out) > 0 {
					prev := out[len(out)-1]
					prevRole, _ = prev["role"].(string)
					if content, ok := prev["content"].([]map[string]any); ok && len(content) > 0 {
						if t, ok := content[0]["text"].(string); ok {
							if len(t) > 100 {
								t = t[:100]
							}
							prevSummary = t
						}
					} else if t, ok := prev["content"].(string); ok {
						if len(t) > 100 {
							t = t[:100]
						}
						prevSummary = t
					}
				}
				slog.Warn("dropped orphan tool_result", "tool_use_id", id, "prev_role", prevRole, "prev_content_summary", prevSummary)
			}
		}
		pendingToolResults = []map[string]any{}
		if len(filtered) == 0 {
			if lastToolUseAssistantIdx >= 0 && lastToolUseAssistantIdx < len(out) {
				stripToolUseBlocks(out, lastToolUseAssistantIdx, lastAssistantToolUseIDs)
			}
			lastAssistantToolUseIDs = map[string]struct{}{}
			lastToolUseAssistantIdx = -1
			return
		}
		if lastToolUseAssistantIdx >= 0 && len(matchedToolUseIDs) < len(lastAssistantToolUseIDs) {
			stripToolUseBlocks(out, lastToolUseAssistantIdx, subtractToolUseIDs(lastAssistantToolUseIDs, matchedToolUseIDs))
		}
		out = append(out, map[string]any{
			"role":    "user",
			"content": filtered,
		})
		lastAssistantToolUseIDs = map[string]struct{}{}
		lastToolUseAssistantIdx = -1
	}

	for sourceIndex, message := range messages {
		text := joinParts(message.Content)
		if message.Role == "system" {
			if len(planSystemBlocks) == 0 && strings.TrimSpace(text) != "" {
				block := map[string]any{"type": "text", "text": text}
				for _, part := range message.Content {
					if cc := anthropicCacheControlFromHints(part.CacheHint, part.CacheControl); cc != nil {
						block["cache_control"] = cc
						break
					}
				}
				systemBlocks = append(systemBlocks, block)
			}
			continue
		}

		if message.Role == "tool" {
			imageParts := collectImageParts(message.Content)
			block, err := anthropicToolResultBlock(text, imageParts)
			if err != nil {
				return nil, nil, err
			}
			pendingToolResults = append(pendingToolResults, block)
			continue
		}

		flushToolResults()

		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			lastAssistantToolUseIDs = make(map[string]struct{}, len(message.ToolCalls))
			blocks, err := anthropicContentBlocks(message.Content)
			if err != nil {
				return nil, nil, err
			}
			for _, call := range message.ToolCalls {
				call = CanonicalToolCall(call)
				lastAssistantToolUseIDs[call.ToolCallID] = struct{}{}
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    call.ToolCallID,
					"name":  call.ToolName,
					"input": mapOrEmpty(call.ArgumentsJSON),
				})
			}
			lastToolUseAssistantIdx = len(out)
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": blocks,
			})
			sourceToOut[sourceIndex] = len(out) - 1
			continue
		}

		lastAssistantToolUseIDs = map[string]struct{}{}
		lastToolUseAssistantIdx = -1

		blocks, err := anthropicContentBlocks(message.Content)
		if err != nil {
			return nil, nil, err
		}
		if len(blocks) == 0 {
			blocks = []map[string]any{{"type": "text", "text": text}}
		}
		out = append(out, map[string]any{
			"role":    message.Role,
			"content": blocks,
		})
		sourceToOut[sourceIndex] = len(out) - 1
		if message.Role == "user" {
			userSourceToOut[sourceIndex] = len(out) - 1
		}
	}

	flushToolResults()

	// strip 后可能出现内容为空的 assistant 消息，无条件移除，避免 API 报 "text is required"。
	compacted := make([]map[string]any, 0, len(out))
	for _, msg := range out {
		if msg["role"] == "assistant" && isEmptyAssistantMsg(msg) {
			continue
		}
		compacted = append(compacted, msg)
	}

	if plan != nil {
		applyAnthropicMessageCachePlan(compacted, sourceToOut, userSourceToOut, plan.MessageCache)
	}

	return systemBlocks, compacted, nil
}

func anthropicSystemBlocksFromPlan(plan *PromptPlan) []map[string]any {
	if plan == nil || len(plan.SystemBlocks) == 0 {
		return nil
	}
	type systemAccumulator struct {
		text       strings.Builder
		cacheType  string
		cacheScope string
	}

	flush := func(blocks []map[string]any, acc *systemAccumulator) []map[string]any {
		if acc == nil {
			return blocks
		}
		text := strings.TrimSpace(acc.text.String())
		if text == "" {
			return blocks
		}
		item := map[string]any{
			"type": "text",
			"text": text,
		}
		if acc.cacheType != "" {
			cc := map[string]any{"type": acc.cacheType}
			if acc.cacheScope != "" {
				cc["scope"] = acc.cacheScope
			}
			item["cache_control"] = cc
		}
		return append(blocks, item)
	}

	blocks := make([]map[string]any, 0, len(plan.SystemBlocks))
	var acc *systemAccumulator
	for _, block := range plan.SystemBlocks {
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		cacheType := ""
		cacheScope := ""
		if block.CacheEligible {
			cacheHint := &CacheHint{
				Action: CacheHintActionWrite,
				Scope:  cacheScopeFromStability(block.Stability),
			}
			if cc := anthropicCacheControlFromHints(cacheHint, nil); cc != nil {
				cacheType, _ = cc["type"].(string)
				cacheScope, _ = cc["scope"].(string)
			}
		}
		if acc == nil || acc.cacheType != cacheType || acc.cacheScope != cacheScope {
			blocks = flush(blocks, acc)
			acc = &systemAccumulator{
				cacheType:  cacheType,
				cacheScope: cacheScope,
			}
		}
		if acc.text.Len() > 0 {
			acc.text.WriteString("\n\n")
		}
		acc.text.WriteString(text)
	}
	return flush(blocks, acc)
}

func cacheScopeFromStability(stability string) string {
	switch strings.ToLower(strings.TrimSpace(stability)) {
	case CacheStabilityStablePrefix:
		return "global"
	case CacheStabilitySessionPrefix:
		return "org"
	default:
		return ""
	}
}

func anthropicCacheControlFromHints(hint *CacheHint, legacyCacheControl *string) map[string]any {
	if hint != nil && strings.EqualFold(strings.TrimSpace(hint.Action), CacheHintActionWrite) {
		payload := map[string]any{"type": "ephemeral"}
		if scope := strings.TrimSpace(hint.Scope); scope != "" {
			payload["scope"] = scope
		}
		return payload
	}
	if legacyCacheControl != nil && strings.TrimSpace(*legacyCacheControl) != "" {
		return map[string]any{"type": strings.TrimSpace(*legacyCacheControl)}
	}
	return nil
}

func applyAnthropicMessageCachePlan(out []map[string]any, sourceToOut map[int]int, userSourceToOut map[int]int, plan MessageCachePlan) {
	if len(out) == 0 {
		return
	}

	if plan.Enabled {
		clearMessageCacheControl(out)
		markerOutIdx := resolveMarkerMessageIndex(plan.MarkerMessageIndex, sourceToOut, len(out))
		if plan.SkipCacheWrite && markerOutIdx > 0 {
			markerOutIdx--
		}
		applySingleMessageCacheMarker(out, markerOutIdx)
		if plan.ToolResultCacheReferences {
			cutOutIdx := resolveToolResultCacheCutIndex(plan.ToolResultCacheCutIndex, sourceToOut, markerOutIdx)
			applyToolResultCacheReferences(out, cutOutIdx)
		}
	}

	applyCacheEdits(out, userSourceToOut, plan)
}

func clearMessageCacheControl(messages []map[string]any) {
	for _, msg := range messages {
		content, ok := msg["content"].([]map[string]any)
		if !ok {
			continue
		}
		for _, block := range content {
			delete(block, "cache_control")
		}
	}
}

func resolveMarkerMessageIndex(sourceIndex int, sourceToOut map[int]int, outLen int) int {
	if outLen == 0 {
		return -1
	}
	if sourceIndex >= 0 {
		if idx, ok := sourceToOut[sourceIndex]; ok {
			return idx
		}
	}
	return outLen - 1
}

func resolveToolResultCacheCutIndex(sourceIndex int, sourceToOut map[int]int, fallback int) int {
	if sourceIndex >= 0 {
		if idx, ok := sourceToOut[sourceIndex]; ok {
			return idx
		}
	}
	return fallback
}

func applySingleMessageCacheMarker(messages []map[string]any, markerOutIdx int) {
	for i := markerOutIdx; i >= 0 && i < len(messages); i-- {
		msg := messages[i]
		content, ok := msg["content"].([]map[string]any)
		if !ok || len(content) == 0 {
			continue
		}
		for j := len(content) - 1; j >= 0; j-- {
			block := content[j]
			blockType, _ := block["type"].(string)
			if !anthropicCacheMarkerBlockType(blockType) {
				continue
			}
			block["cache_control"] = map[string]any{"type": "ephemeral"}
			return
		}
	}
}

func anthropicCacheMarkerBlockType(blockType string) bool {
	switch strings.TrimSpace(blockType) {
	case "text", "tool_result", "tool_use":
		return true
	default:
		return false
	}
}

func applyToolResultCacheReferences(messages []map[string]any, cutOutIdx int) {
	if cutOutIdx <= 0 {
		return
	}
	for i := 0; i < cutOutIdx && i < len(messages); i++ {
		msg := messages[i]
		role, _ := msg["role"].(string)
		if role != "user" {
			continue
		}
		content, ok := msg["content"].([]map[string]any)
		if !ok {
			continue
		}
		for _, block := range content {
			blockType, _ := block["type"].(string)
			if blockType != "tool_result" {
				continue
			}
			if _, exists := block["cache_reference"]; exists {
				continue
			}
			toolUseID, _ := block["tool_use_id"].(string)
			toolUseID = strings.TrimSpace(toolUseID)
			if toolUseID == "" {
				continue
			}
			block["cache_reference"] = toolUseID
		}
	}
}

func applyCacheEdits(messages []map[string]any, userSourceToOut map[int]int, plan MessageCachePlan) {
	seenReferences := map[string]struct{}{}
	for _, block := range plan.PinnedCacheEdits {
		applyCacheEditsBlockAt(messages, userSourceToOut, block, seenReferences)
	}
	if plan.NewCacheEdits != nil {
		applyCacheEditsBlockAt(messages, userSourceToOut, *plan.NewCacheEdits, seenReferences)
	}
}

func applyCacheEditsBlockAt(messages []map[string]any, userSourceToOut map[int]int, block PromptCacheEditsBlock, seen map[string]struct{}) {
	if len(messages) == 0 {
		return
	}
	outIndex := -1
	if block.UserMessageIndex >= 0 {
		if idx, ok := userSourceToOut[block.UserMessageIndex]; ok {
			outIndex = idx
		}
	}
	if outIndex < 0 {
		for i := len(messages) - 1; i >= 0; i-- {
			role, _ := messages[i]["role"].(string)
			if role == "user" {
				outIndex = i
				break
			}
		}
	}
	if outIndex < 0 || outIndex >= len(messages) {
		return
	}
	cacheEditsBlock := buildAnthropicCacheEditsBlock(block, seen)
	if cacheEditsBlock == nil {
		return
	}
	content, ok := messages[outIndex]["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		messages[outIndex]["content"] = []map[string]any{cacheEditsBlock}
		return
	}
	content = append(content, cacheEditsBlock)
	messages[outIndex]["content"] = content
}

func buildAnthropicCacheEditsBlock(block PromptCacheEditsBlock, seen map[string]struct{}) map[string]any {
	edits := make([]map[string]any, 0, len(block.Edits))
	for _, edit := range block.Edits {
		action := strings.ToLower(strings.TrimSpace(edit.Type))
		if action == "" {
			action = CacheHintActionDelete
		}
		ref := strings.TrimSpace(edit.CacheReference)
		if ref == "" {
			continue
		}
		if _, exists := seen[ref]; exists {
			continue
		}
		seen[ref] = struct{}{}
		edits = append(edits, map[string]any{
			"type":            action,
			"cache_reference": ref,
		})
	}
	if len(edits) == 0 {
		return nil
	}
	return map[string]any{
		"type":  "cache_edits",
		"edits": edits,
	}
}

func subtractToolUseIDs(all map[string]struct{}, keep map[string]struct{}) map[string]struct{} {
	if len(all) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(all))
	for id := range all {
		if _, ok := keep[id]; ok {
			continue
		}
		out[id] = struct{}{}
	}
	return out
}

// stripToolUseBlocks 从 out[idx] 的 content 中移除所有 tool_use blocks。
// 如果移除后 content 为空，整条消息也从 out 中清除（置为空 assistant）。
func stripToolUseBlocks(out []map[string]any, idx int, toolUseIDs map[string]struct{}) {
	msg := out[idx]
	content, ok := msg["content"].([]map[string]any)
	if !ok {
		return
	}
	filtered := make([]map[string]any, 0, len(content))
	for _, block := range content {
		if block["type"] == "tool_use" {
			if id, _ := block["id"].(string); id != "" {
				if _, match := toolUseIDs[id]; match {
					slog.Warn("stripped orphan tool_use from assistant", "tool_use_id", id)
					continue
				}
			}
		}
		filtered = append(filtered, block)
	}
	if len(filtered) == 0 {
		out[idx]["content"] = []map[string]any{}
		return
	}
	out[idx]["content"] = filtered
}

// isEmptyAssistantMsg 判断一条 assistant 消息是否仅含空 text block（strip 后的残留）。
func isEmptyAssistantMsg(msg map[string]any) bool {
	blocks, ok := msg["content"].([]map[string]any)
	if !ok {
		return false
	}
	for _, b := range blocks {
		if b["type"] != "text" {
			return false
		}
		if t, _ := b["text"].(string); strings.TrimSpace(t) != "" {
			return false
		}
	}
	return true
}

func anthropicContentBlocks(parts []ContentPart) ([]map[string]any, error) {
	blocks := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part.Kind() {
		case "text":
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			block := map[string]any{"type": "text", "text": part.Text}
			if cc := anthropicCacheControlFromHints(part.CacheHint, part.CacheControl); cc != nil {
				block["cache_control"] = cc
			}
			blocks = append(blocks, block)
		case "thinking":
			block := map[string]any{
				"type":     "thinking",
				"thinking": part.Text,
			}
			if strings.TrimSpace(part.Signature) != "" {
				block["signature"] = strings.TrimSpace(part.Signature)
			}
			blocks = append(blocks, block)
		case "redacted_thinking":
			blocks = append(blocks, map[string]any{
				"type": "redacted_thinking",
				"data": part.Text,
			})
		case "file":
			text := PartPromptText(part)
			if strings.TrimSpace(text) == "" {
				continue
			}
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		case "image":
			mimeType, data, err := modelInputImage(part)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(part.Attachment.Key) != "" {
				blocks = append(blocks, map[string]any{
					"type": "text",
					"text": "[attachment_key:" + part.Attachment.Key + "]",
				})
			}
			blocks = append(blocks, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": mimeType,
					"data":       base64.StdEncoding.EncodeToString(data),
				},
			})
		}
	}
	return blocks, nil
}

func anthropicToolResultBlock(text string, imageParts []ContentPart) (map[string]any, error) {
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

	contentText, err := stablejson.Encode(contentSource)
	if err != nil {
		contentText = "{}"
	}

	block := map[string]any{
		"type":        "tool_result",
		"tool_use_id": toolCallID,
	}
	if isError {
		block["is_error"] = true
	}

	if len(imageParts) == 0 {
		block["content"] = contentText
		return block, nil
	}

	// content block 数组：text + image blocks
	contentBlocks := []map[string]any{
		{"type": "text", "text": contentText},
	}
	for _, part := range imageParts {
		mimeType, data, err := modelInputImage(part)
		if err != nil {
			return nil, err
		}
		contentBlocks = append(contentBlocks, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": mimeType,
				"data":       base64.StdEncoding.EncodeToString(data),
			},
		})
	}
	block["content"] = contentBlocks
	return block, nil
}

func collectImageParts(parts []ContentPart) []ContentPart {
	var images []ContentPart
	for _, part := range parts {
		if part.Kind() == "image" && part.Attachment != nil && len(part.Data) > 0 {
			images = append(images, part)
		}
	}
	return images
}

func anthropicToolChoice(tc *ToolChoice) map[string]any {
	if tc == nil {
		return nil
	}
	switch tc.Mode {
	case "required":
		return map[string]any{"type": "any"}
	case "specific":
		return map[string]any{"type": "tool", "name": CanonicalToolName(tc.ToolName)}
	default:
		return nil
	}
}

func toAnthropicTools(specs []ToolSpec) []map[string]any {
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

	out := make([]map[string]any, 0, len(sortedSpecs))
	for _, spec := range sortedSpecs {
		name := CanonicalToolName(spec.Name)
		if name == "" {
			name = spec.Name
		}
		payload := map[string]any{
			"name":         name,
			"input_schema": mapOrEmpty(spec.JSONSchema),
		}
		if spec.Description != nil {
			payload["description"] = *spec.Description
		}
		if cc := anthropicCacheControlFromHints(spec.CacheHint, nil); cc != nil {
			payload["cache_control"] = cc
		}
		out = append(out, payload)
	}
	return out
}

func parseAnthropicMessage(body []byte) (content string, thinkingText string, toolCalls []ToolCall, err error) {
	var parsed any
	if err = json.Unmarshal(body, &parsed); err != nil {
		return
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		err = fmt.Errorf("response root is not an object")
		return
	}
	rawContent, ok := root["content"].([]any)
	if !ok || len(rawContent) == 0 {
		err = fmt.Errorf("response missing content")
		return
	}

	var textBuilder strings.Builder
	var thinkingBuilder strings.Builder
	toolCalls = []ToolCall{}

	for idx, rawItem := range rawContent {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := item["type"].(string)
		if typ == "thinking" {
			text, _ := item["thinking"].(string)
			thinkingBuilder.WriteString(text)
			continue
		}
		if typ == "text" {
			text, _ := item["text"].(string)
			textBuilder.WriteString(text)
			continue
		}
		if typ != "tool_use" {
			continue
		}

		toolCallID, _ := item["id"].(string)
		toolCallID = strings.TrimSpace(toolCallID)
		if toolCallID == "" {
			err = fmt.Errorf("content[%d] missing tool_use.id", idx)
			return
		}
		toolName, _ := item["name"].(string)
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			err = fmt.Errorf("content[%d] missing tool_use.name", idx)
			return
		}

		argumentsJSON := map[string]any{}
		if rawInput, ok := item["input"]; ok && rawInput != nil {
			obj, ok := rawInput.(map[string]any)
			if !ok {
				err = fmt.Errorf("%w: content[%d].input must be a JSON object", errAnthropicToolUseInput, idx)
				return
			}
			argumentsJSON = obj
		}

		toolCalls = append(toolCalls, ToolCall{
			ToolCallID:    toolCallID,
			ToolName:      CanonicalToolName(toolName),
			ArgumentsJSON: argumentsJSON,
		})
	}

	content = textBuilder.String()
	thinkingText = thinkingBuilder.String()
	return
}

// ensureAnthropicMaxTokensForThinking 确保 max_tokens > budget_tokens，
// 当 thinking 已注入但 max_tokens 不足时自动上调。
func ensureAnthropicMaxTokensForThinking(payload map[string]any) {
	tObj, ok := payload["thinking"].(map[string]any)
	if !ok {
		return
	}
	budget := anyToInt(tObj["budget_tokens"])
	if budget <= 0 {
		return
	}
	maxTokens := anyToInt(payload["max_tokens"])
	if maxTokens <= budget {
		payload["max_tokens"] = budget + maxTokens
	}
}

func anthropicThinkingBudget(mode string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "enabled", "medium":
		return defaultAnthropicThinkingBudget, true
	case "minimal":
		return anthropicMinThinkingBudget, true
	case "low":
		return anthropicLowThinkingBudget, true
	case "high":
		return anthropicHighThinkingBudget, true
	case "max", "maximum", "xhigh", "extra_high", "extra-high", "extra high":
		return anthropicMaxThinkingBudget, true
	default:
		return 0, false
	}
}

func anthropicThinkingDisabled(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "disabled", "none", "off":
		return true
	default:
		return false
	}
}

func applyAnthropicReasoningMode(payload map[string]any, mode string) {
	if budget, ok := anthropicThinkingBudget(mode); ok {
		payload["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": budget,
		}
		ensureAnthropicMaxTokensForThinking(payload)
		return
	}
	if anthropicThinkingDisabled(mode) {
		delete(payload, "thinking")
		return
	}
	if tObj, ok := payload["thinking"].(map[string]any); ok {
		tObj["type"] = "enabled"
		if _, has := tObj["budget_tokens"]; !has {
			tObj["budget_tokens"] = defaultAnthropicThinkingBudget
		}
		ensureAnthropicMaxTokensForThinking(payload)
	}
}

func anyToInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}

func anthropicErrorMessageAndDetails(body []byte, status int) (string, map[string]any) {
	details := map[string]any{"status_code": status}

	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		// body is not JSON; include raw body in message for visibility
		details["raw_body"] = string(body)
		return fmt.Sprintf("Anthropic request failed: status=%d body=%q", status, string(body)), details
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		details["raw_body"] = string(body)
		return fmt.Sprintf("Anthropic request failed: status=%d body=%q", status, string(body)), details
	}
	errObj, ok := root["error"].(map[string]any)
	if !ok {
		details["raw_body"] = string(body)
		return fmt.Sprintf("Anthropic request failed: status=%d body=%q", status, string(body)), details
	}
	if errType, ok := errObj["type"].(string); ok && strings.TrimSpace(errType) != "" {
		details["anthropic_error_type"] = strings.TrimSpace(errType)
	}
	if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg), details
	}
	details["raw_body"] = string(body)
	return fmt.Sprintf("Anthropic request failed: status=%d body=%q", status, string(body)), details
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

type anthropicStreamEvent struct {
	Type         string                  `json:"type"`
	Index        *int                    `json:"index"`
	ContentBlock *anthropicStreamBlock   `json:"content_block"`
	Delta        *anthropicStreamDelta   `json:"delta"`
	Message      *anthropicStreamMessage `json:"message"`
	Usage        map[string]any          `json:"usage"`
	Error        *anthropicStreamError   `json:"error"`
}

type anthropicStreamMessage struct {
	Usage map[string]any `json:"usage"`
}

type anthropicStreamBlock struct {
	Type     string         `json:"type"`
	Text     string         `json:"text"`
	Thinking string         `json:"thinking"`
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Input    map[string]any `json:"input"`
}

type anthropicStreamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Thinking    string `json:"thinking"`
	PartialJSON string `json:"partial_json"`
	Signature   string `json:"signature"`
	StopReason  string `json:"stop_reason"`
}

type anthropicToolUseBuffer struct {
	ID   string
	Name string
	JSON strings.Builder
}

type anthropicAssistantBlock struct {
	Type      string
	Text      strings.Builder
	Signature string
}

type anthropicStreamError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (g *AnthropicGateway) streamAnthropicSSE(ctx context.Context, body io.Reader, llmCallID string, yield func(StreamEvent) error) error {
	var usage *Usage
	toolBuffers := map[int]*anthropicToolUseBuffer{}
	assistantBlocks := map[int]*anthropicAssistantBlock{}
	completed := false

	failStream := func(errClass string, message string, details map[string]any) error {
		if err := yield(StreamRunFailed{
			LlmCallID: llmCallID,
			Error: GatewayError{
				ErrorClass: errClass,
				Message:    message,
				Details:    details,
			},
		}); err != nil {
			return err
		}
		return errAnthropicStreamTerminated
	}

	err := forEachSSEData(ctx, body, streamActivityMarker(ctx), func(data string) error {
		data = strings.TrimSpace(data)
		if data == "" {
			return nil
		}
		if g.transport.cfg.EmitDebugEvents {
			raw, rawTruncated := truncateUTF8(data, anthropicMaxDebugChunkBytes)
			if err := yield(StreamLlmResponseChunk{
				LlmCallID:    llmCallID,
				ProviderKind: "anthropic",
				APIMode:      "messages",
				Raw:          raw,
				Truncated:    rawTruncated,
			}); err != nil {
				return err
			}
		}

		var event anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return failStream(ErrorClassInternalError, "Anthropic response parse failed", map[string]any{"reason": err.Error()})
		}

		if parsed := parseAnthropicUsageMap(event.Usage); parsed != nil {
			usage = parsed
		}
		if event.Message != nil {
			if parsed := parseAnthropicUsageMap(event.Message.Usage); parsed != nil {
				usage = parsed
			}
		}

		idx := 0
		if event.Index != nil {
			idx = *event.Index
		}

		switch event.Type {
		case "error":
			return failStream(anthropicStreamErrorClass(event.Error), anthropicStreamErrorMessage(event.Error), anthropicStreamErrorDetails(event.Error))
		case "content_block_start":
			if event.ContentBlock == nil {
				return nil
			}
			switch event.ContentBlock.Type {
			case "text":
				buffer := &anthropicAssistantBlock{Type: "text"}
				buffer.Text.WriteString(event.ContentBlock.Text)
				assistantBlocks[idx] = buffer
				if strings.TrimSpace(event.ContentBlock.Text) == "" {
					return nil
				}
				return yield(StreamMessageDelta{ContentDelta: event.ContentBlock.Text, Role: "assistant"})
			case "thinking":
				buffer := &anthropicAssistantBlock{Type: "thinking"}
				buffer.Text.WriteString(event.ContentBlock.Thinking)
				assistantBlocks[idx] = buffer
				if strings.TrimSpace(event.ContentBlock.Thinking) == "" {
					return nil
				}
				channel := "thinking"
				return yield(StreamMessageDelta{ContentDelta: event.ContentBlock.Thinking, Role: "assistant", Channel: &channel})
			case "tool_use":
				buffer := &anthropicToolUseBuffer{
					ID:   strings.TrimSpace(event.ContentBlock.ID),
					Name: strings.TrimSpace(event.ContentBlock.Name),
				}
				toolBuffers[idx] = buffer
				if len(event.ContentBlock.Input) == 0 {
					return nil
				}
				encoded, err := stablejson.Encode(event.ContentBlock.Input)
				if err != nil {
					return err
				}
				buffer.JSON.WriteString(encoded)
				return yield(ToolCallArgumentDelta{
					ToolCallIndex:  idx,
					ToolCallID:     buffer.ID,
					ToolName:       CanonicalToolName(buffer.Name),
					ArgumentsDelta: encoded,
				})
			}
		case "content_block_delta":
			if event.Delta == nil {
				return nil
			}
			switch event.Delta.Type {
			case "text_delta":
				if buffer := assistantBlocks[idx]; buffer != nil {
					newDelta := anthropicUniqueDelta(buffer.Text.String(), event.Delta.Text)
					if newDelta == "" {
						return nil
					}
					buffer.Text.WriteString(newDelta)
					return yield(StreamMessageDelta{ContentDelta: newDelta, Role: "assistant"})
				}
				if event.Delta.Text == "" {
					return nil
				}
				return yield(StreamMessageDelta{ContentDelta: event.Delta.Text, Role: "assistant"})
			case "thinking_delta":
				if buffer := assistantBlocks[idx]; buffer != nil {
					newDelta := anthropicUniqueDelta(buffer.Text.String(), event.Delta.Thinking)
					if newDelta == "" {
						return nil
					}
					buffer.Text.WriteString(newDelta)
					channel := "thinking"
					return yield(StreamMessageDelta{ContentDelta: newDelta, Role: "assistant", Channel: &channel})
				}
				if event.Delta.Thinking == "" {
					return nil
				}
				channel := "thinking"
				return yield(StreamMessageDelta{ContentDelta: event.Delta.Thinking, Role: "assistant", Channel: &channel})
			case "signature_delta":
				if buffer := assistantBlocks[idx]; buffer != nil {
					buffer.Signature = strings.TrimSpace(event.Delta.Signature)
				}
				return nil
			case "input_json_delta":
				buffer := toolBuffers[idx]
				if buffer == nil {
					return nil
				}
				buffer.JSON.WriteString(event.Delta.PartialJSON)
				return yield(ToolCallArgumentDelta{
					ToolCallIndex:  idx,
					ToolCallID:     buffer.ID,
					ToolName:       CanonicalToolName(buffer.Name),
					ArgumentsDelta: event.Delta.PartialJSON,
				})
			}
		case "content_block_stop":
			buffer := toolBuffers[idx]
			if buffer == nil {
				return nil
			}
			delete(toolBuffers, idx)
			if strings.TrimSpace(buffer.ID) == "" || strings.TrimSpace(buffer.Name) == "" {
				return failStream(ErrorClassProviderNonRetryable, "Anthropic tool_use input parse failed", map[string]any{"reason": "content block missing tool_use id or name"})
			}
			argumentsJSON := map[string]any{}
			rawArgs := strings.TrimSpace(buffer.JSON.String())
			if rawArgs != "" {
				var parsed any
				if err := json.Unmarshal([]byte(rawArgs), &parsed); err != nil {
					return failStream(ErrorClassProviderNonRetryable, "Anthropic tool_use input parse failed", map[string]any{"reason": err.Error()})
				}
				obj, ok := parsed.(map[string]any)
				if !ok {
					return failStream(ErrorClassProviderNonRetryable, "Anthropic tool_use input parse failed", map[string]any{"reason": "tool_use input must be a JSON object"})
				}
				argumentsJSON = obj
			}
			return yield(ToolCall{
				ToolCallID:    buffer.ID,
				ToolName:      CanonicalToolName(buffer.Name),
				ArgumentsJSON: argumentsJSON,
			})
		case "message_delta":
			if event.Delta == nil {
				return nil
			}
			stopReason := strings.TrimSpace(event.Delta.StopReason)
			if stopReason == "refusal" {
				return failStream(ErrorClassPolicyDenied, "Anthropic response refused", map[string]any{"stop_reason": stopReason})
			}
		case "message_stop":
			completed = true
			return nil
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errAnthropicStreamTerminated) {
			return nil
		}
		return err
	}
	if !completed {
		streamErr := InternalStreamEndedError()
		if usage != nil || len(assistantBlocks) > 0 || len(toolBuffers) > 0 {
			streamErr = RetryableStreamEndedError()
		}
		return yield(StreamRunFailed{LlmCallID: llmCallID, Error: streamErr})
	}
	assistantMessage := Message{Role: "assistant", Content: anthropicAssistantMessageParts(assistantBlocks)}
	return yield(StreamRunCompleted{LlmCallID: llmCallID, Usage: usage, AssistantMessage: &assistantMessage})
}

func anthropicStreamErrorClass(streamErr *anthropicStreamError) string {
	if streamErr == nil {
		return ErrorClassProviderRetryable
	}
	switch strings.TrimSpace(streamErr.Type) {
	case "overloaded_error", "rate_limit_error":
		return ErrorClassProviderRetryable
	case "authentication_error", "invalid_request_error", "not_found_error", "permission_error":
		return ErrorClassProviderNonRetryable
	default:
		return ErrorClassProviderRetryable
	}
}

func anthropicStreamErrorMessage(streamErr *anthropicStreamError) string {
	if streamErr == nil || strings.TrimSpace(streamErr.Message) == "" {
		return "Anthropic stream error"
	}
	return strings.TrimSpace(streamErr.Message)
}

func anthropicStreamErrorDetails(streamErr *anthropicStreamError) map[string]any {
	if streamErr == nil {
		return nil
	}
	details := map[string]any{}
	if value := strings.TrimSpace(streamErr.Type); value != "" {
		details["anthropic_error_type"] = value
	}
	if len(details) == 0 {
		return nil
	}
	return details
}

func anthropicAssistantMessageParts(blocks map[int]*anthropicAssistantBlock) []ContentPart {
	if len(blocks) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(blocks))
	for idx := range blocks {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)

	parts := make([]ContentPart, 0, len(indexes))
	for _, idx := range indexes {
		block := blocks[idx]
		if block == nil {
			continue
		}
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text.String()) == "" {
				continue
			}
			parts = append(parts, TextPart{Text: block.Text.String()})
		case "thinking":
			if strings.TrimSpace(block.Text.String()) == "" && strings.TrimSpace(block.Signature) == "" {
				continue
			}
			parts = append(parts, ContentPart{
				Type:      "thinking",
				Text:      block.Text.String(),
				Signature: strings.TrimSpace(block.Signature),
			})
		}
	}
	return parts
}

func anthropicUniqueDelta(existing string, incoming string) string {
	if incoming == "" {
		return ""
	}
	if existing == "" {
		return incoming
	}
	overlap := longestAnthropicTextOverlap(existing, incoming)
	if overlap <= 0 {
		return incoming
	}
	return incoming[overlap:]
}

func longestAnthropicTextOverlap(existing string, incoming string) int {
	incomingBoundaries := utf8Boundaries(incoming)
	for i := len(incomingBoundaries) - 1; i >= 0; i-- {
		overlap := incomingBoundaries[i]
		if overlap == 0 || overlap > len(existing) {
			continue
		}
		start := len(existing) - overlap
		if !utf8.RuneStart(existing[start]) {
			continue
		}
		if existing[start:] == incoming[:overlap] {
			return overlap
		}
	}
	return 0
}

func utf8Boundaries(value string) []int {
	boundaries := make([]int, 0, len(value)+1)
	for idx := range value {
		boundaries = append(boundaries, idx)
	}
	return append(boundaries, len(value))
}

func parseAnthropicUsageMap(usageObj map[string]any) *Usage {
	if len(usageObj) == 0 {
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
