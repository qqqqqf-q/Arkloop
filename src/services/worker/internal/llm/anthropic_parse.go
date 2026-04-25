package llm

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

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
			if strings.TrimSpace(block.Text.String()) != "" {
				parts = append(parts, TextPart{Text: block.Text.String()})
			}
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
	mode = strings.TrimSpace(mode)
	if budget, ok := anthropicThinkingBudget(mode); ok {
		payload["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": budget,
		}
		ensureAnthropicMaxTokensForThinking(payload)
		return
	}
	if anthropicThinkingDisabled(mode) {
		payload["thinking"] = map[string]any{"type": "disabled"}
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
