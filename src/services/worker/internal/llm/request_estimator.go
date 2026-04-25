package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// EstimateProviderPayloadBytes 估算请求在目标 provider 最终 payload 形态下的 JSON bytes。
func EstimateProviderPayloadBytes(cfg ResolvedGatewayConfig, request Request) (int, error) {
	payload, err := buildProviderPayloadForEstimate(cfg, request)
	if err != nil {
		return 0, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	return len(encoded), nil
}

func buildProviderPayloadForEstimate(cfg ResolvedGatewayConfig, request Request) (map[string]any, error) {
	req := request
	if strings.TrimSpace(req.Model) == "" {
		req.Model = strings.TrimSpace(cfg.Model)
	}

	switch cfg.ProtocolKind {
	case ProtocolKindOpenAIChatCompletions:
		return buildOpenAIChatCompletionsPayloadForEstimate(cfg.OpenAI, req)
	case ProtocolKindOpenAIResponses:
		return buildOpenAIResponsesPayloadForEstimate(cfg.OpenAI, req)
	case ProtocolKindAnthropicMessages:
		return buildAnthropicMessagesPayloadForEstimate(cfg.Anthropic, req)
	case ProtocolKindGeminiGenerateContent:
		return buildGeminiPayloadForEstimate(cfg.Gemini, req)
	default:
		return nil, fmt.Errorf("unsupported protocol kind: %s", cfg.ProtocolKind)
	}
}

func buildOpenAIChatCompletionsPayloadForEstimate(cfg *OpenAIProtocolConfig, request Request) (map[string]any, error) {
	if cfg == nil {
		return nil, fmt.Errorf("missing openai protocol config")
	}
	messagesPayload, err := toOpenAIChatMessages(request.Messages)
	if err != nil {
		return nil, err
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
	for key, value := range cfg.AdvancedPayloadJSON {
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}
	applyOpenAIChatReasoningMode(payload, request.ReasoningMode)
	return payload, nil
}

func buildOpenAIResponsesPayloadForEstimate(cfg *OpenAIProtocolConfig, request Request) (map[string]any, error) {
	if cfg == nil {
		return nil, fmt.Errorf("missing openai protocol config")
	}
	instructions, inputMessages := splitOpenAIResponsesInstructions(request.Messages)
	input, err := toOpenAIResponsesInput(inputMessages)
	if err != nil {
		return nil, err
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
		payload["tool_choice"] = openAIResponsesToolChoice(request.ToolChoice)
	}
	for key, value := range cfg.AdvancedPayloadJSON {
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}
	applyOpenAIResponsesReasoningMode(payload, request.ReasoningMode)
	return payload, nil
}

func buildAnthropicMessagesPayloadForEstimate(cfg *AnthropicProtocolConfig, request Request) (map[string]any, error) {
	if cfg == nil {
		return nil, fmt.Errorf("missing anthropic protocol config")
	}
	system, messages, err := toAnthropicMessagesWithPlan(request.Messages, request.PromptPlan)
	if err != nil {
		return nil, err
	}
	maxTokens := defaultAnthropicMaxTokens
	if request.MaxOutputTokens != nil && *request.MaxOutputTokens > 0 {
		maxTokens = *request.MaxOutputTokens
	}

	payload := map[string]any{
		"model":      request.Model,
		"messages":   messages,
		"max_tokens": maxTokens,
		"stream":     true,
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
	for key, value := range cfg.AdvancedPayloadJSON {
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}
	applyAnthropicReasoningMode(payload, request.ReasoningMode)
	return payload, nil
}

func buildGeminiPayloadForEstimate(cfg *GeminiProtocolConfig, request Request) (map[string]any, error) {
	if cfg == nil {
		return nil, fmt.Errorf("missing gemini protocol config")
	}
	return toGeminiPayload(request, cfg.AdvancedPayloadJSON)
}
