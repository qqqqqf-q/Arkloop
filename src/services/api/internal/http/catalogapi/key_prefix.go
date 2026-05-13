package catalogapi

import "strings"

// computeKeyPrefix 取 API Key 前 12 个 UTF-8 字符用于展示识别。
func computeKeyPrefix(apiKey string) string {
	runes := []rune(strings.TrimSpace(apiKey))
	if len(runes) <= 12 {
		return string(runes)
	}
	return string(runes[:12])
}
