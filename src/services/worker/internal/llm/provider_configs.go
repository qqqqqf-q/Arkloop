package llm

import (
	"errors"
	"time"
)

const defaultAnthropicVersion = "2023-06-01"
const defaultAnthropicMaxResponseBytes = 16 * 1024
const anthropicMaxDebugChunkBytes = 8192
const defaultAnthropicMaxTokens = 32768

var errAnthropicToolUseInput = errors.New("anthropic_tool_use_input")
var errAnthropicStreamTerminated = errors.New("anthropic_stream_terminated")

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
	openAIMaxErrorBodyBytes  = 4096
	openAIMaxDebugChunkBytes = 8192
	openAIMaxResponseBytes   = 1024 * 1024
)

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

const (
	defaultGeminiBaseURL        = "https://generativelanguage.googleapis.com"
	defaultGeminiThinkingBudget = 8192
	geminiMaxErrorBodyBytes     = 4096
	geminiMaxDebugChunkBytes    = 8192
)

var geminiAdvancedJSONDenylist = map[string]struct{}{
	"contents":          {},
	"systemInstruction": {},
	"tools":             {},
	"toolConfig":        {},
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

type GeminiGatewayConfig struct {
	Transport    TransportConfig
	Protocol     GeminiProtocolConfig
	APIKey       string
	BaseURL      string
	AdvancedJSON map[string]any
	TotalTimeout time.Duration
}

type openAIResponsesNotSupportedError struct {
	StatusCode int
}

func (e *openAIResponsesNotSupportedError) Error() string {
	return "openai responses not supported"
}
