package routing

// RouteContextWindowTokens 读取 advanced_json.available_catalog.context_length（tokens）。
// 未配置时返回 0，由上层使用平台 fallback。
func RouteContextWindowTokens(rule ProviderRouteRule) int {
	return RouteModelCapabilities(rule).ContextLength
}
