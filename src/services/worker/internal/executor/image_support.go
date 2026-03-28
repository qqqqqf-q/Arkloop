package executor

import (
	"fmt"
	"strings"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
)

func filterImagePartsForRoute(selected *routing.SelectedProviderRoute, parts []llm.ContentPart, readImageSourcesVisible bool) []llm.ContentPart {
	if supportsImageInput(selected) {
		return parts
	}
	out := make([]llm.ContentPart, 0, len(parts))
	for _, part := range parts {
		if part.Kind() == messagecontent.PartTypeImage {
			out = append(out, llm.ContentPart{Type: messagecontent.PartTypeText, Text: imagePlaceholder(part, readImageSourcesVisible)})
			continue
		}
		out = append(out, part)
	}
	return out
}

func supportsImageInput(selected *routing.SelectedProviderRoute) bool {
	if selected == nil {
		return false
	}
	caps := routing.SelectedRouteModelCapabilities(selected)
	return caps.SupportsInputModality("image")
}

func imagePlaceholder(part llm.ContentPart, readImageSourcesVisible bool) string {
	suffix := imagePlaceholderSuffix(readImageSourcesVisible)
	if part.Attachment != nil {
		if name := strings.TrimSpace(part.Attachment.Filename); name != "" {
			return fmt.Sprintf("[图片: %s] %s", name, suffix)
		}
	}
	return "[图片] " + suffix
}

func imagePlaceholderSuffix(readImageSourcesVisible bool) string {
	if readImageSourcesVisible {
		return "当前模型不能直接查看图片；如需理解图片内容，请调用 read 工具读取该图片。"
	}
	return "当前模型不能直接查看图片；当前未配置可用的图片读取能力。"
}

func applyImageFilter(
	route *routing.SelectedProviderRoute,
	messages []llm.Message,
	readImageSourcesVisible bool,
) []llm.Message {
	if len(messages) == 0 {
		return messages
	}
	out := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		parts := filterImagePartsForRoute(route, msg.Content, readImageSourcesVisible)
		out = append(out, llm.Message{
			Role:         msg.Role,
			Content:      parts,
			ToolCalls:    msg.ToolCalls,
			OutputTokens: msg.OutputTokens,
		})
	}
	return out
}
