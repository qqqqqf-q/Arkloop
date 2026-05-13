package pipeline

import (
	"strings"
	"unicode/utf8"
)

// splitByRuneLimit 按 rune 计数拆分长文本。超出 limit 时优先在换行符处断开，
// 其次在空格处断开，最后 resort 到硬截断。
func splitByRuneLimit(text string, limit int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}
	runes := []rune(text)
	var parts []string
	for len(runes) > 0 {
		end := limit
		if end > len(runes) {
			end = len(runes)
		}
		// 尝试在自然断点处断开
		if end < len(runes) {
			window := string(runes[:end])
			if idx := strings.LastIndex(window, "\n"); idx > 0 {
				end = utf8.RuneCountInString(window[:idx+1])
			} else if idx := strings.LastIndex(window, " "); idx > 0 {
				end = utf8.RuneCountInString(window[:idx+1])
			}
		}
		part := strings.TrimSpace(string(runes[:end]))
		if part != "" {
			parts = append(parts, part)
		}
		runes = []rune(strings.TrimSpace(string(runes[end:])))
	}
	return parts
}
