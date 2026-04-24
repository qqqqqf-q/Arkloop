package llm

import (
	"errors"
	"strings"
)

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
