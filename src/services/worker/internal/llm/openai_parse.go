package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"arkloop/services/worker/internal/stablejson"
)

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
		callID := strings.TrimSpace(call.ToolCallID)
		if callID == "" {
			callID = fmt.Sprintf("call_hist_%d_%d", index, callIndex)
		}
		items = append(items, map[string]any{
			"id":        fmt.Sprintf("fc_hist_%d_%d", index, callIndex),
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
