package executor

import (
	"fmt"
	"strings"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
)

type imageToolVisibility int

const (
	imageToolUnavailable imageToolVisibility = iota
	imageToolDirectVisible
	imageToolSearchableOnly
)

func filterImagePartsForRoute(selected *routing.SelectedProviderRoute, parts []llm.ContentPart, visibility imageToolVisibility) []llm.ContentPart {
	if supportsImageInput(selected) {
		return parts
	}
	out := make([]llm.ContentPart, 0, len(parts))
	for _, part := range parts {
		if part.Kind() == messagecontent.PartTypeImage {
			out = append(out, llm.ContentPart{Type: messagecontent.PartTypeText, Text: imagePlaceholder(part, visibility)})
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

func imagePlaceholder(part llm.ContentPart, visibility imageToolVisibility) string {
	suffix := imagePlaceholderSuffix(visibility)
	if part.Attachment != nil {
		if name := strings.TrimSpace(part.Attachment.Filename); name != "" {
			return fmt.Sprintf("[图片: %s] %s", name, suffix)
		}
	}
	return "[图片] " + suffix
}

func imagePlaceholderSuffix(visibility imageToolVisibility) string {
	switch visibility {
	case imageToolDirectVisible:
		return "当前模型不能直接查看图片；如需理解图片内容，请直接调用 understand_image 工具。"
	case imageToolSearchableOnly:
		return "当前模型不能直接查看图片；如需理解图片内容，请先调用 search_tools 查找并激活 understand_image 工具。"
	default:
		return "当前模型不能直接查看图片；当前未配置可用的图片理解工具。"
	}
}

func resolveImageToolVisibility(finalSpecs []llm.ToolSpec, searchable map[string]llm.ToolSpec) imageToolVisibility {
	if toolSpecVisible(finalSpecs, "understand_image") {
		return imageToolDirectVisible
	}
	if searchable != nil {
		if _, ok := searchable["understand_image"]; ok {
			return imageToolSearchableOnly
		}
	}
	return imageToolUnavailable
}

func toolSpecVisible(specs []llm.ToolSpec, toolName string) bool {
	for _, spec := range specs {
		if spec.Name == toolName {
			return true
		}
	}
	return false
}

func applyImageFilter(
	route *routing.SelectedProviderRoute,
	messages []llm.Message,
	finalSpecs []llm.ToolSpec,
	searchable map[string]llm.ToolSpec,
) []llm.Message {
	if len(messages) == 0 {
		return messages
	}
	visibility := resolveImageToolVisibility(finalSpecs, searchable)
	out := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		parts := filterImagePartsForRoute(route, msg.Content, visibility)
		out = append(out, llm.Message{
			Role:         msg.Role,
			Content:      parts,
			ToolCalls:    msg.ToolCalls,
			OutputTokens: msg.OutputTokens,
		})
	}
	return out
}
