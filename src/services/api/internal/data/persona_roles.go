package data

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	sharedexec "arkloop/services/shared/executionconfig"
)

var personaRoleKeyRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

func NormalizePersonaRolesJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage("{}"), nil
	}
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("roles must be valid json object: %w", err)
	}
	normalized, err := NormalizePersonaRolesValue(obj)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func NormalizePersonaRolesMap(raw map[string]any) (map[string]any, error) {
	return normalizePersonaRolesObject(raw)
}

func NormalizePersonaRolesValue(value any) (map[string]any, error) {
	if value == nil {
		return map[string]any{}, nil
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("roles must be an object")
	}
	return normalizePersonaRolesObject(raw)
}

func normalizePersonaRolesObject(raw map[string]any) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	normalized := make(map[string]any, len(raw))
	for rawName, rawValue := range raw {
		name := strings.TrimSpace(rawName)
		if !personaRoleKeyRegex.MatchString(name) {
			return nil, fmt.Errorf("roles key is invalid: %s", rawName)
		}
		role, err := normalizePersonaRoleOverride(rawValue, "roles."+name)
		if err != nil {
			return nil, err
		}
		normalized[name] = role
	}
	return normalized, nil
}

func normalizePersonaRoleOverride(value any, label string) (map[string]any, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", label)
	}
	allowed := map[string]struct{}{
		"soul_md":              {},
		"prompt_md":            {},
		"tool_allowlist":       {},
		"tool_denylist":        {},
		"budgets":              {},
		"preferred_credential": {},
		"model":                {},
		"reasoning_mode":       {},
		"prompt_cache_control": {},
	}
	normalized := make(map[string]any, len(raw))
	for key, rawValue := range raw {
		if _, ok := allowed[key]; !ok {
			return nil, fmt.Errorf("%s contains unsupported field: %s", label, key)
		}
		switch key {
		case "soul_md", "prompt_md":
			text, ok := rawValue.(string)
			if !ok {
				return nil, fmt.Errorf("%s.%s must be a string", label, key)
			}
			normalized[key] = strings.TrimSpace(text)
		case "tool_allowlist", "tool_denylist":
			list, err := normalizeToolNameList(rawValue, label+"."+key)
			if err != nil {
				return nil, err
			}
			normalized[key] = list
		case "budgets":
			budgets, err := normalizeRoleBudgets(rawValue, label+".budgets")
			if err != nil {
				return nil, err
			}
			normalized[key] = budgets
		case "preferred_credential", "model":
			binding, err := normalizeOptionalRoleString(rawValue, label+"."+key)
			if err != nil {
				return nil, err
			}
			normalized[key] = binding
		case "reasoning_mode":
			text, ok := rawValue.(string)
			if !ok {
				return nil, fmt.Errorf("%s.reasoning_mode must be a string", label)
			}
			normalized[key] = normalizePersonaReasoningMode(text)
		case "prompt_cache_control":
			text, ok := rawValue.(string)
			if !ok {
				return nil, fmt.Errorf("%s.prompt_cache_control must be a string", label)
			}
			normalized[key] = normalizePersonaPromptCacheControl(text)
		}
	}
	return normalized, nil
}

func normalizeOptionalRoleString(value any, label string) (any, error) {
	if value == nil {
		return nil, nil
	}
	text, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("%s must be a string or null", label)
	}
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return nil, nil
	}
	return cleaned, nil
}

func normalizeToolNameList(value any, label string) ([]string, error) {
	items := []any{}
	switch raw := value.(type) {
	case []any:
		items = raw
	case []string:
		items = make([]any, 0, len(raw))
		for _, item := range raw {
			items = append(items, item)
		}
	default:
		return nil, fmt.Errorf("%s must be an array", label)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be a string array", label)
		}
		cleaned := strings.TrimSpace(text)
		if cleaned == "" {
			return nil, fmt.Errorf("%s must not contain empty values", label)
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	return out, nil
}

func normalizeRoleBudgets(value any, label string) (map[string]any, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", label)
	}
	parsed, err := sharedexec.ParseRequestedBudgetsMap(raw)
	if err != nil {
		return nil, err
	}
	normalized := make(map[string]any, len(raw))
	if _, ok := raw["reasoning_iterations"]; ok {
		if parsed.ReasoningIterations == nil {
			normalized["reasoning_iterations"] = nil
		} else {
			normalized["reasoning_iterations"] = *parsed.ReasoningIterations
		}
	}
	if _, ok := raw["tool_continuation_budget"]; ok {
		if parsed.ToolContinuationBudget == nil {
			normalized["tool_continuation_budget"] = nil
		} else {
			normalized["tool_continuation_budget"] = *parsed.ToolContinuationBudget
		}
	}
	if _, ok := raw["max_output_tokens"]; ok {
		if parsed.MaxOutputTokens == nil {
			normalized["max_output_tokens"] = nil
		} else {
			normalized["max_output_tokens"] = *parsed.MaxOutputTokens
		}
	}
	if _, ok := raw["tool_timeout_ms"]; ok {
		if parsed.ToolTimeoutMs == nil {
			normalized["tool_timeout_ms"] = nil
		} else {
			normalized["tool_timeout_ms"] = *parsed.ToolTimeoutMs
		}
	}
	if _, ok := raw["tool_budget"]; ok {
		normalized["tool_budget"] = parsed.ToolBudget
	}
	if _, ok := raw["per_tool_soft_limits"]; ok {
		normalized["per_tool_soft_limits"] = parsed.PerToolSoftLimits
	}
	if _, ok := raw["temperature"]; ok {
		if parsed.Temperature == nil {
			normalized["temperature"] = nil
		} else {
			normalized["temperature"] = *parsed.Temperature
		}
	}
	if _, ok := raw["top_p"]; ok {
		if parsed.TopP == nil {
			normalized["top_p"] = nil
		} else {
			normalized["top_p"] = *parsed.TopP
		}
	}
	return normalized, nil
}
