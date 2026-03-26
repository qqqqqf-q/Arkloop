package routing

import (
	"encoding/json"
	"strconv"
	"strings"
)

// 与 API/前端写入 advanced_json.available_catalog 时使用的键一致（勿随意改）。
const availableCatalogAdvancedKey = "available_catalog"

type ModelCapabilities struct {
	ContextLength    int
	MaxOutputTokens  int
	InputModalities  []string
	OutputModalities []string
}

func RouteModelCapabilities(rule ProviderRouteRule) ModelCapabilities {
	rawCatalog := routeAvailableCatalog(rule)
	if len(rawCatalog) == 0 {
		return ModelCapabilities{}
	}
	return ModelCapabilities{
		ContextLength:    normalizedPositiveInt(rawCatalog["context_length"]),
		MaxOutputTokens:  normalizedPositiveInt(rawCatalog["max_output_tokens"]),
		InputModalities:  normalizeStringSlice(rawCatalog["input_modalities"]),
		OutputModalities: normalizeStringSlice(rawCatalog["output_modalities"]),
	}
}

func SelectedRouteModelCapabilities(selected *SelectedProviderRoute) ModelCapabilities {
	if selected == nil {
		return ModelCapabilities{}
	}
	return RouteModelCapabilities(selected.Route)
}

func (c ModelCapabilities) SupportsInputModality(modality string) bool {
	return containsNormalizedString(c.InputModalities, modality)
}

func (c ModelCapabilities) SupportsOutputModality(modality string) bool {
	return containsNormalizedString(c.OutputModalities, modality)
}

func routeAvailableCatalog(rule ProviderRouteRule) map[string]any {
	if len(rule.AdvancedJSON) == 0 {
		return nil
	}
	rawCatalog, ok := rule.AdvancedJSON[availableCatalogAdvancedKey]
	if !ok {
		return nil
	}
	catalog, ok := rawCatalog.(map[string]any)
	if !ok {
		return nil
	}
	return catalog
}

func normalizedPositiveInt(raw any) int {
	n, ok := normalizePositiveIntJSON(raw)
	if !ok {
		return 0
	}
	return n
}

func normalizeStringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		return dedupeNormalizedStrings(value)
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				items = append(items, text)
			}
		}
		return dedupeNormalizedStrings(items)
	default:
		return nil
	}
}

func dedupeNormalizedStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		cleaned := strings.ToLower(strings.TrimSpace(item))
		if cleaned == "" {
			continue
		}
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func containsNormalizedString(items []string, target string) bool {
	cleanedTarget := strings.ToLower(strings.TrimSpace(target))
	if cleanedTarget == "" {
		return false
	}
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), cleanedTarget) {
			return true
		}
	}
	return false
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
