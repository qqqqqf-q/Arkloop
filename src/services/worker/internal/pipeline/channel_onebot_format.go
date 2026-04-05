package pipeline

import (
	"regexp"
	"strings"
)

var (
	reHeading    = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	reBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reItalic     = regexp.MustCompile(`(?:^|[^*])\*([^*]+?)\*(?:[^*]|$)`)
	reStrike     = regexp.MustCompile(`~~(.+?)~~`)
	reLink       = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reInlineCode = regexp.MustCompile("`([^`]+)`")
	reListItem   = regexp.MustCompile(`(?m)^(\s*)[-*]\s+`)
)

// FormatOneBotAssistantText 将 LLM Markdown 输出优化为 QQ 纯文本排版。
// QQ 不支持 HTML/Markdown 渲染，此函数仅做文本层面的格式友好化。
func FormatOneBotAssistantText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}

	// heading -> 加粗文字 + 换行分隔
	text = reHeading.ReplaceAllString(text, "【$1】")
	// bold -> 保留文字（QQ 无粗体）
	text = reBold.ReplaceAllString(text, "$1")
	// strikethrough -> 保留文字
	text = reStrike.ReplaceAllString(text, "$1")
	// link -> text (url)
	text = reLink.ReplaceAllStringFunc(text, func(match string) string {
		parts := reLink.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		linkText := strings.TrimSpace(parts[1])
		linkURL := strings.TrimSpace(parts[2])
		if linkText == linkURL {
			return linkURL
		}
		return linkText + " (" + linkURL + ")"
	})
	// list item: - / * -> ·
	text = reListItem.ReplaceAllString(text, "${1}· ")

	return text
}
