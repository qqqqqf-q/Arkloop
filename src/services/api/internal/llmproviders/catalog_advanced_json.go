package llmproviders

import "strings"

// AvailableCatalogAdvancedKey advanced_json 内嵌 list-available-models 标准化快照。
// Worker compact 从快照内 context_length 读取窗口；不设单独顶层键。
const AvailableCatalogAdvancedKey = "available_catalog"

// RouteAdvancedJSONFromAvailableModel 从上游列表单条生成写入 llm_route.advanced_json 的 map。
func RouteAdvancedJSONFromAvailableModel(am AvailableModel) map[string]any {
	cat := map[string]any{
		"id":   am.ID,
		"name": am.Name,
	}
	if strings.TrimSpace(am.Type) != "" {
		cat["type"] = am.Type
	}
	if am.ContextLength != nil {
		cat["context_length"] = *am.ContextLength
	}
	if am.MaxOutputTokens != nil {
		cat["max_output_tokens"] = *am.MaxOutputTokens
	}
	if len(am.InputModalities) > 0 {
		cat["input_modalities"] = append([]string(nil), am.InputModalities...)
	}
	if len(am.OutputModalities) > 0 {
		cat["output_modalities"] = append([]string(nil), am.OutputModalities...)
	}
	if am.ToolCalling != nil && *am.ToolCalling {
		cat["tool_calling"] = true
	}
	if am.Reasoning != nil && *am.Reasoning {
		cat["reasoning"] = true
	}
	if am.DefaultTemperature != nil {
		cat["default_temperature"] = *am.DefaultTemperature
	}
	return map[string]any{
		AvailableCatalogAdvancedKey: cat,
	}
}
