package personas

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	sharedexec "arkloop/services/shared/executionconfig"
	"arkloop/services/worker/internal/tools"
)

func parseRoleOverrides(value any) (map[string]RoleOverride, error) {
	if value == nil {
		return nil, nil
	}
	rawRoles, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("roles must be an object")
	}
	if len(rawRoles) == 0 {
		return map[string]RoleOverride{}, nil
	}
	out := make(map[string]RoleOverride, len(rawRoles))
	for rawName, rawValue := range rawRoles {
		name := strings.TrimSpace(rawName)
		if !idRegex.MatchString(name) {
			return nil, fmt.Errorf("roles key is invalid: %s", rawName)
		}
		override, err := parseRoleOverride(rawValue, "roles."+name)
		if err != nil {
			return nil, err
		}
		out[name] = override
	}
	return out, nil
}

func parseRoleOverridesJSON(raw []byte) (map[string]RoleOverride, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	return parseRoleOverrides(obj)
}

func parseRoleOverride(value any, label string) (RoleOverride, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return RoleOverride{}, fmt.Errorf("%s must be an object", label)
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
		"stream_thinking":      {},
		"prompt_cache_control": {},
	}
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			return RoleOverride{}, fmt.Errorf("%s contains unsupported field: %s", label, key)
		}
	}

	var out RoleOverride
	if rawValue, ok := raw["soul_md"]; ok {
		parsed, err := parseRoleTextOverride(rawValue, label+".soul_md")
		if err != nil {
			return RoleOverride{}, err
		}
		out.SoulMD = parsed
	}
	if rawValue, ok := raw["prompt_md"]; ok {
		parsed, err := parseRoleTextOverride(rawValue, label+".prompt_md")
		if err != nil {
			return RoleOverride{}, err
		}
		out.PromptMD = parsed
	}
	if rawValue, ok := raw["tool_allowlist"]; ok {
		parsed, err := asToolNameList(rawValue, label+".tool_allowlist")
		if err != nil {
			return RoleOverride{}, err
		}
		out.HasToolAllowlist = true
		out.ToolAllowlist = parsed
	}
	if rawValue, ok := raw["tool_denylist"]; ok {
		parsed, err := asToolNameList(rawValue, label+".tool_denylist")
		if err != nil {
			return RoleOverride{}, err
		}
		out.HasToolDenylist = true
		out.ToolDenylist = parsed
	}
	if rawValue, ok := raw["budgets"]; ok {
		parsed, err := parseBudgetsOverride(rawValue, label+".budgets")
		if err != nil {
			return RoleOverride{}, err
		}
		out.Budgets = parsed
	}
	if rawValue, ok := raw["preferred_credential"]; ok {
		parsed, err := parseOptionalStringOverride(rawValue, label+".preferred_credential")
		if err != nil {
			return RoleOverride{}, err
		}
		out.PreferredCredential = parsed
	}
	if rawValue, ok := raw["model"]; ok {
		parsed, err := parseOptionalStringOverride(rawValue, label+".model")
		if err != nil {
			return RoleOverride{}, err
		}
		out.Model = parsed
	}
	if rawValue, ok := raw["reasoning_mode"]; ok {
		parsed, err := parseEnumOverride(rawValue, label+".reasoning_mode", func(value string) string {
			return normalizePersonaReasoningMode(&value)
		})
		if err != nil {
			return RoleOverride{}, err
		}
		out.ReasoningMode = parsed
	}
	if rawValue, ok := raw["stream_thinking"]; ok {
		parsed, err := parseBoolOverride(rawValue, label+".stream_thinking")
		if err != nil {
			return RoleOverride{}, err
		}
		out.StreamThinking = parsed
	}
	if rawValue, ok := raw["prompt_cache_control"]; ok {
		parsed, err := parseEnumOverride(rawValue, label+".prompt_cache_control", func(value string) string {
			return normalizePersonaPromptCacheControl(&value)
		})
		if err != nil {
			return RoleOverride{}, err
		}
		out.PromptCacheControl = parsed
	}
	return out, nil
}

func parseRoleTextOverride(value any, label string) (StringOverride, error) {
	text, ok := value.(string)
	if !ok {
		return StringOverride{}, fmt.Errorf("%s must be a string", label)
	}
	return StringOverride{Set: true, Value: strings.TrimSpace(text)}, nil
}

func parseOptionalStringOverride(value any, label string) (OptionalStringOverride, error) {
	if value == nil {
		return OptionalStringOverride{Set: true}, nil
	}
	text, ok := value.(string)
	if !ok {
		return OptionalStringOverride{}, fmt.Errorf("%s must be a string or null", label)
	}
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return OptionalStringOverride{Set: true}, nil
	}
	return OptionalStringOverride{Set: true, Value: &cleaned}, nil
}

func parseEnumOverride(value any, label string, normalize func(string) string) (EnumStringOverride, error) {
	text, ok := value.(string)
	if !ok {
		return EnumStringOverride{}, fmt.Errorf("%s must be a string", label)
	}
	return EnumStringOverride{Set: true, Value: normalize(text)}, nil
}

func parseBoolOverride(value any, label string) (BoolOverride, error) {
	b, ok := value.(bool)
	if !ok {
		return BoolOverride{}, fmt.Errorf("%s must be a boolean", label)
	}
	return BoolOverride{Set: true, Value: b}, nil
}

func parseBudgetsOverride(value any, label string) (BudgetsOverride, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return BudgetsOverride{}, fmt.Errorf("%s must be an object", label)
	}
	parsed, err := sharedexec.ParseRequestedBudgetsMap(raw)
	if err != nil {
		return BudgetsOverride{}, err
	}
	out := BudgetsOverride{}
	if _, ok := raw["reasoning_iterations"]; ok {
		out.HasReasoningIterations = true
		out.ReasoningIterations = parsed.ReasoningIterations
	}
	if _, ok := raw["tool_continuation_budget"]; ok {
		out.HasToolContinuationBudget = true
		out.ToolContinuationBudget = parsed.ToolContinuationBudget
	}
	if _, ok := raw["max_output_tokens"]; ok {
		out.HasMaxOutputTokens = true
		out.MaxOutputTokens = parsed.MaxOutputTokens
	}
	if _, ok := raw["tool_timeout_ms"]; ok {
		out.HasToolTimeoutMs = true
		out.ToolTimeoutMs = parsed.ToolTimeoutMs
	}
	if _, ok := raw["tool_budget"]; ok {
		out.HasToolBudget = true
		out.ToolBudget = cloneToolBudget(parsed.ToolBudget)
	}
	if _, ok := raw["per_tool_soft_limits"]; ok {
		out.HasPerToolSoftLimits = true
		out.PerToolSoftLimits = tools.CopyPerToolSoftLimits(parsed.PerToolSoftLimits)
	}
	if _, ok := raw["temperature"]; ok {
		out.HasTemperature = true
		out.Temperature = parsed.Temperature
	}
	if _, ok := raw["top_p"]; ok {
		out.HasTopP = true
		out.TopP = parsed.TopP
	}
	return out, nil
}

func ApplyRoleOverride(def Definition, role string) (Definition, bool) {
	cleaned := strings.TrimSpace(role)
	if cleaned == "" || len(def.Roles) == 0 {
		return cloneDefinition(def), false
	}
	override, ok := def.Roles[cleaned]
	if !ok {
		return cloneDefinition(def), false
	}
	merged := cloneDefinition(def)
	if override.SoulMD.Set {
		merged.RoleSoulMD = override.SoulMD.Value
	}
	if override.PromptMD.Set {
		merged.RolePromptMD = override.PromptMD.Value
	}
	if override.HasToolAllowlist {
		merged.ToolAllowlist = append([]string(nil), override.ToolAllowlist...)
	}
	if override.HasToolDenylist {
		merged.ToolDenylist = append([]string(nil), override.ToolDenylist...)
	}
	applyBudgetsOverride(&merged.Budgets, override.Budgets)
	if override.PreferredCredential.Set {
		merged.PreferredCredential = cloneStringPtr(override.PreferredCredential.Value)
	}
	if override.Model.Set {
		merged.Model = cloneStringPtr(override.Model.Value)
	}
	if override.ReasoningMode.Set {
		merged.ReasoningMode = override.ReasoningMode.Value
	}
	if override.StreamThinking.Set {
		merged.StreamThinking = override.StreamThinking.Value
	}
	if override.PromptCacheControl.Set {
		merged.PromptCacheControl = override.PromptCacheControl.Value
	}
	return merged, true
}

func applyBudgetsOverride(target *Budgets, override BudgetsOverride) {
	if target == nil {
		return
	}
	if override.HasReasoningIterations {
		target.ReasoningIterations = copyOptionalInt(override.ReasoningIterations)
	}
	if override.HasToolContinuationBudget {
		target.ToolContinuationBudget = copyOptionalInt(override.ToolContinuationBudget)
	}
	if override.HasMaxOutputTokens {
		target.MaxOutputTokens = copyOptionalInt(override.MaxOutputTokens)
	}
	if override.HasToolTimeoutMs {
		target.ToolTimeoutMs = copyOptionalInt(override.ToolTimeoutMs)
	}
	if override.HasToolBudget {
		target.ToolBudget = cloneToolBudget(override.ToolBudget)
	}
	if override.HasPerToolSoftLimits {
		target.PerToolSoftLimits = tools.CopyPerToolSoftLimits(override.PerToolSoftLimits)
	}
	if override.HasTemperature {
		target.Temperature = copyOptionalFloat64(override.Temperature)
	}
	if override.HasTopP {
		target.TopP = copyOptionalFloat64(override.TopP)
	}
}

func cloneDefinition(def Definition) Definition {
	cloned := def
	cloned.Description = cloneStringPtr(def.Description)
	cloned.SelectorName = cloneStringPtr(def.SelectorName)
	cloned.SelectorOrder = copyOptionalInt(def.SelectorOrder)
	cloned.ToolAllowlist = append([]string(nil), def.ToolAllowlist...)
	cloned.ToolDenylist = append([]string(nil), def.ToolDenylist...)
	cloned.CoreTools = append([]string(nil), def.CoreTools...)
	cloned.Budgets = cloneBudgets(def.Budgets)
	cloned.ExecutorConfig = cloneToolBudget(def.ExecutorConfig)
	cloned.PreferredCredential = cloneStringPtr(def.PreferredCredential)
	cloned.Model = cloneStringPtr(def.Model)
	cloned.Roles = cloneRoleOverrides(def.Roles)
	if def.TitleSummarizer != nil {
		cloned.TitleSummarizer = &TitleSummarizerConfig{
			Prompt:    def.TitleSummarizer.Prompt,
			MaxTokens: def.TitleSummarizer.MaxTokens,
		}
	}
	if def.ResultSummarizer != nil {
		cloned.ResultSummarizer = &ResultSummarizerConfig{
			Prompt:         def.ResultSummarizer.Prompt,
			MaxTokens:      def.ResultSummarizer.MaxTokens,
			ThresholdBytes: def.ResultSummarizer.ThresholdBytes,
		}
	}
	return cloned
}

func cloneBudgets(budgets Budgets) Budgets {
	return Budgets{
		ReasoningIterations:    copyOptionalInt(budgets.ReasoningIterations),
		ToolContinuationBudget: copyOptionalInt(budgets.ToolContinuationBudget),
		MaxOutputTokens:        copyOptionalInt(budgets.MaxOutputTokens),
		ToolTimeoutMs:          copyOptionalInt(budgets.ToolTimeoutMs),
		ToolBudget:             cloneToolBudget(budgets.ToolBudget),
		PerToolSoftLimits:      tools.CopyPerToolSoftLimits(budgets.PerToolSoftLimits),
		Temperature:            copyOptionalFloat64(budgets.Temperature),
		TopP:                   copyOptionalFloat64(budgets.TopP),
	}
}

func cloneRoleOverrides(overrides map[string]RoleOverride) map[string]RoleOverride {
	if len(overrides) == 0 {
		return nil
	}
	out := make(map[string]RoleOverride, len(overrides))
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		override := overrides[key]
		out[key] = RoleOverride{
			SoulMD:              override.SoulMD,
			PromptMD:            override.PromptMD,
			HasToolAllowlist:    override.HasToolAllowlist,
			ToolAllowlist:       append([]string(nil), override.ToolAllowlist...),
			HasToolDenylist:     override.HasToolDenylist,
			ToolDenylist:        append([]string(nil), override.ToolDenylist...),
			Budgets:             cloneBudgetsOverride(override.Budgets),
			PreferredCredential: cloneOptionalStringOverride(override.PreferredCredential),
			Model:               cloneOptionalStringOverride(override.Model),
			ReasoningMode:       override.ReasoningMode,
			StreamThinking:      override.StreamThinking,
			PromptCacheControl:  override.PromptCacheControl,
		}
	}
	return out
}

func cloneBudgetsOverride(override BudgetsOverride) BudgetsOverride {
	return BudgetsOverride{
		HasReasoningIterations:    override.HasReasoningIterations,
		ReasoningIterations:       copyOptionalInt(override.ReasoningIterations),
		HasToolContinuationBudget: override.HasToolContinuationBudget,
		ToolContinuationBudget:    copyOptionalInt(override.ToolContinuationBudget),
		HasMaxOutputTokens:        override.HasMaxOutputTokens,
		MaxOutputTokens:           copyOptionalInt(override.MaxOutputTokens),
		HasToolTimeoutMs:          override.HasToolTimeoutMs,
		ToolTimeoutMs:             copyOptionalInt(override.ToolTimeoutMs),
		HasToolBudget:             override.HasToolBudget,
		ToolBudget:                cloneToolBudget(override.ToolBudget),
		HasPerToolSoftLimits:      override.HasPerToolSoftLimits,
		PerToolSoftLimits:         tools.CopyPerToolSoftLimits(override.PerToolSoftLimits),
		HasTemperature:            override.HasTemperature,
		Temperature:               copyOptionalFloat64(override.Temperature),
		HasTopP:                   override.HasTopP,
		TopP:                      copyOptionalFloat64(override.TopP),
	}
}

func cloneOptionalStringOverride(override OptionalStringOverride) OptionalStringOverride {
	return OptionalStringOverride{Set: override.Set, Value: cloneStringPtr(override.Value)}
}

func copyOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyOptionalFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneToolBudget(value map[string]any) map[string]any {
	if len(value) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}
