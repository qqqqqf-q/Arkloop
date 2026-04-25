package llm

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

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
	return strings.ToLower(strings.TrimSpace(mode)) == "disabled"
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
		reasoning := map[string]any{}
		if existing, ok := payload["reasoning"].(map[string]any); ok {
			for key, value := range existing {
				reasoning[key] = value
			}
		}
		reasoning["effort"] = effort
		if effort == "none" {
			delete(reasoning, "summary")
		} else if _, has := reasoning["summary"]; !has {
			reasoning["summary"] = "auto"
		}
		payload["reasoning"] = reasoning
		return
	}
	if openAIReasoningDisabled(mode) {
		delete(payload, "reasoning")
		return
	}
	if reasoning, ok := payload["reasoning"].(map[string]any); ok {
		if _, has := reasoning["summary"]; !has {
			reasoning["summary"] = "auto"
		}
	}
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
