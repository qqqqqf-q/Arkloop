package routing

import (
	"encoding/json"
	"strconv"
	"strings"
)

// 与 API/前端写入 advanced_json.available_catalog 时使用的键一致（勿随意改）。
const availableCatalogAdvancedKey = "available_catalog"

// RouteContextWindowTokens 读取 advanced_json.available_catalog.context_length（tokens）。
// 未配置时返回 0，由上层使用平台 fallback。
func RouteContextWindowTokens(rule ProviderRouteRule) int {
	if len(rule.AdvancedJSON) == 0 {
		return 0
	}
	rawCat, ok := rule.AdvancedJSON[availableCatalogAdvancedKey]
	if !ok {
		return 0
	}
	cat, ok := rawCat.(map[string]any)
	if !ok {
		return 0
	}
	raw, ok := cat["context_length"]
	if !ok {
		return 0
	}
	n, ok := normalizePositiveIntJSON(raw)
	if !ok {
		return 0
	}
	return n
}

func normalizePositiveIntJSON(raw any) (int, bool) {
	switch v := raw.(type) {
	case int:
		if v > 0 {
			return v, true
		}
	case int64:
		if v > 0 && v <= 1<<50 {
			return int(v), true
		}
	case float64:
		if v > 0 && v < 1e12 {
			return int(v), true
		}
	case json.Number:
		x, err := strconv.ParseInt(string(v), 10, 64)
		if err == nil && x > 0 && x <= 1<<50 {
			return int(x), true
		}
	case string:
		x, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil && x > 0 {
			return x, true
		}
	}
	return 0, false
}
