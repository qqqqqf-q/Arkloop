package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
	DeltaSeen bool
}

type anthropicStreamError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
