package executionconfig

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	defaultReasoningIterationsLimit = 0
	defaultToolContinuationLimit    = 32
)

type RequestedBudgets struct {
	ReasoningIterations    *int              `json:"reasoning_iterations,omitempty"`
	ToolContinuationBudget *int              `json:"tool_continuation_budget,omitempty"`
	MaxOutputTokens        *int              `json:"max_output_tokens,omitempty"`
	ToolTimeoutMs          *int              `json:"tool_timeout_ms,omitempty"`
	ToolBudget             map[string]any    `json:"tool_budget,omitempty"`
	PerToolSoftLimits      PerToolSoftLimits `json:"per_tool_soft_limits,omitempty"`
	Temperature            *float64          `json:"temperature,omitempty"`
	TopP                   *float64          `json:"top_p,omitempty"`
	MaxCostMicros          *int64            `json:"max_cost_micros,omitempty"`
	MaxTotalOutputTokens   *int64            `json:"max_total_output_tokens,omitempty"`
}

type PlatformLimits struct {
	AgentReasoningIterations int
	ToolContinuationBudget   int
}

type AgentConfigProfile struct {
	Name            string
	SystemPrompt    *string
	Temperature     *float64
	MaxOutputTokens *int
	TopP            *float64
	ReasoningMode   string
}

type PersonaProfile struct {
	SoulMD                  string
	PromptMD                string
	PreferredCredentialName *string
	ResolvedAgentConfigName *string
	Budgets                 RequestedBudgets
}

type EffectiveProfile struct {
	ResolvedAgentConfigName string            `json:"resolved_agent_config_name,omitempty"`
	SystemPrompt            string            `json:"system_prompt,omitempty"`
	ReasoningIterations     int               `json:"reasoning_iterations"`
	ToolContinuationBudget  int               `json:"tool_continuation_budget"`
	MaxOutputTokens         *int              `json:"max_output_tokens,omitempty"`
	Temperature             *float64          `json:"temperature,omitempty"`
	TopP                    *float64          `json:"top_p,omitempty"`
	ReasoningMode           string            `json:"reasoning_mode,omitempty"`
	ToolTimeoutMs           *int              `json:"tool_timeout_ms,omitempty"`
	ToolBudget              map[string]any    `json:"tool_budget,omitempty"`
	PerToolSoftLimits       PerToolSoftLimits `json:"per_tool_soft_limits,omitempty"`
	MaxCostMicros           *int64            `json:"max_cost_micros,omitempty"`
	MaxTotalOutputTokens    *int64            `json:"max_total_output_tokens,omitempty"`
	PreferredCredentialName string            `json:"preferred_credential_name,omitempty"`
}

func NormalizePlatformLimits(limits PlatformLimits) PlatformLimits {
	if limits.AgentReasoningIterations < 0 {
		limits.AgentReasoningIterations = defaultReasoningIterationsLimit
	}
	if limits.ToolContinuationBudget <= 0 {
		limits.ToolContinuationBudget = defaultToolContinuationLimit
	}
	return limits
}

func ResolveEffectiveProfile(
	platform PlatformLimits,
	agentConfig *AgentConfigProfile,
	persona *PersonaProfile,
) EffectiveProfile {
	normalized := NormalizePlatformLimits(platform)
	profile := EffectiveProfile{
		ReasoningIterations:    normalized.AgentReasoningIterations,
		ToolContinuationBudget: normalized.ToolContinuationBudget,
		ToolBudget:             map[string]any{},
		PerToolSoftLimits:      DefaultPerToolSoftLimits(),
	}

	var agentConfigPromptPrefix string
	var agentConfigMaxOutputTokens *int

	if agentConfig != nil {
		profile.ResolvedAgentConfigName = strings.TrimSpace(agentConfig.Name)
		if agentConfig.SystemPrompt != nil {
			agentConfigPromptPrefix = *agentConfig.SystemPrompt
		}
		agentConfigMaxOutputTokens = copyOptionalInt(agentConfig.MaxOutputTokens)
		profile.MaxOutputTokens = copyOptionalInt(agentConfig.MaxOutputTokens)
		profile.Temperature = copyOptionalFloat64(agentConfig.Temperature)
		profile.TopP = copyOptionalFloat64(agentConfig.TopP)
		profile.ReasoningMode = strings.TrimSpace(agentConfig.ReasoningMode)
	}

	if persona == nil {
		profile.SystemPrompt = strings.TrimSpace(agentConfigPromptPrefix)
		return profile
	}

	profile.SystemPrompt = joinPromptSegments(agentConfigPromptPrefix, persona.SoulMD, persona.PromptMD)

	if value := persona.Budgets.ReasoningIterations; value != nil && *value > 0 {
		if normalized.AgentReasoningIterations == 0 || *value < normalized.AgentReasoningIterations {
			profile.ReasoningIterations = *value
		}
	}
	if value := persona.Budgets.ToolContinuationBudget; value != nil && *value > 0 && *value < normalized.ToolContinuationBudget {
		profile.ToolContinuationBudget = *value
	}

	if persona.Budgets.MaxOutputTokens != nil {
		if agentConfigMaxOutputTokens != nil && *persona.Budgets.MaxOutputTokens > *agentConfigMaxOutputTokens {
			profile.MaxOutputTokens = copyOptionalInt(agentConfigMaxOutputTokens)
		} else {
			profile.MaxOutputTokens = copyOptionalInt(persona.Budgets.MaxOutputTokens)
		}
	}
	if persona.Budgets.Temperature != nil {
		profile.Temperature = copyOptionalFloat64(persona.Budgets.Temperature)
	}
	if persona.Budgets.TopP != nil {
		profile.TopP = copyOptionalFloat64(persona.Budgets.TopP)
	}

	if persona.Budgets.MaxCostMicros != nil && *persona.Budgets.MaxCostMicros > 0 {
		profile.MaxCostMicros = copyOptionalInt64(persona.Budgets.MaxCostMicros)
	}
	if persona.Budgets.MaxTotalOutputTokens != nil && *persona.Budgets.MaxTotalOutputTokens > 0 {
		profile.MaxTotalOutputTokens = copyOptionalInt64(persona.Budgets.MaxTotalOutputTokens)
	}
	profile.ToolTimeoutMs = copyOptionalInt(persona.Budgets.ToolTimeoutMs)
	for key, value := range persona.Budgets.ToolBudget {
		profile.ToolBudget[key] = value
	}
	profile.PerToolSoftLimits = MergePerToolSoftLimits(profile.PerToolSoftLimits, persona.Budgets.PerToolSoftLimits)
	if persona.PreferredCredentialName != nil {
		profile.PreferredCredentialName = strings.TrimSpace(*persona.PreferredCredentialName)
	}
	if persona.ResolvedAgentConfigName != nil && strings.TrimSpace(*persona.ResolvedAgentConfigName) != "" {
		profile.ResolvedAgentConfigName = strings.TrimSpace(*persona.ResolvedAgentConfigName)
	}

	return profile
}

func joinPromptSegments(parts ...string) string {
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		segments = append(segments, trimmed)
	}
	return strings.Join(segments, "\n\n")
}

func ParseRequestedBudgetsJSON(raw []byte) (RequestedBudgets, error) {
	if len(raw) == 0 {
		return RequestedBudgets{ToolBudget: map[string]any{}, PerToolSoftLimits: PerToolSoftLimits{}}, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return RequestedBudgets{}, fmt.Errorf("invalid budgets_json: %w", err)
	}
	return ParseRequestedBudgetsMap(obj)
}

func ParseRequestedBudgetsMap(raw map[string]any) (RequestedBudgets, error) {
	if raw == nil {
		return RequestedBudgets{ToolBudget: map[string]any{}, PerToolSoftLimits: PerToolSoftLimits{}}, nil
	}
	for key := range raw {
		switch key {
		case "reasoning_iterations", "tool_continuation_budget", "max_output_tokens", "tool_timeout_ms", "tool_budget", "per_tool_soft_limits", "temperature", "top_p", "max_cost_micros", "max_total_output_tokens":
		default:
			return RequestedBudgets{}, fmt.Errorf("budgets contains unsupported field: %s", key)
		}
	}

	reasoningIterations, err := asOptionalPositiveInt(raw["reasoning_iterations"], "budgets.reasoning_iterations")
	if err != nil {
		return RequestedBudgets{}, err
	}
	toolContinuationBudget, err := asOptionalPositiveInt(raw["tool_continuation_budget"], "budgets.tool_continuation_budget")
	if err != nil {
		return RequestedBudgets{}, err
	}
	maxOutputTokens, err := asOptionalPositiveInt(raw["max_output_tokens"], "budgets.max_output_tokens")
	if err != nil {
		return RequestedBudgets{}, err
	}
	toolTimeoutMs, err := asOptionalPositiveInt(raw["tool_timeout_ms"], "budgets.tool_timeout_ms")
	if err != nil {
		return RequestedBudgets{}, err
	}
	maxCostMicros, err := asOptionalPositiveInt64(raw["max_cost_micros"], "budgets.max_cost_micros")
	if err != nil {
		return RequestedBudgets{}, err
	}
	maxTotalOutputTokens, err := asOptionalPositiveInt64(raw["max_total_output_tokens"], "budgets.max_total_output_tokens")
	if err != nil {
		return RequestedBudgets{}, err
	}
	temperature, err := asOptionalFloat64(raw["temperature"], "budgets.temperature")
	if err != nil {
		return RequestedBudgets{}, err
	}
	topP, err := asOptionalFloat64(raw["top_p"], "budgets.top_p")
	if err != nil {
		return RequestedBudgets{}, err
	}

	toolBudget := map[string]any{}
	if rawToolBudget, ok := raw["tool_budget"]; ok && rawToolBudget != nil {
		mapValue, ok := rawToolBudget.(map[string]any)
		if !ok {
			return RequestedBudgets{}, fmt.Errorf("budgets.tool_budget must be an object")
		}
		toolBudget = copyToolBudget(mapValue)
	}

	perToolSoftLimits, err := asPerToolSoftLimits(raw["per_tool_soft_limits"])
	if err != nil {
		return RequestedBudgets{}, err
	}

	return RequestedBudgets{
		ReasoningIterations:    reasoningIterations,
		ToolContinuationBudget: toolContinuationBudget,
		MaxOutputTokens:        maxOutputTokens,
		ToolTimeoutMs:          toolTimeoutMs,
		ToolBudget:             toolBudget,
		PerToolSoftLimits:      perToolSoftLimits,
		Temperature:            temperature,
		TopP:                   topP,
		MaxCostMicros:          maxCostMicros,
		MaxTotalOutputTokens:   maxTotalOutputTokens,
	}, nil
}

func asPerToolSoftLimits(value any) (PerToolSoftLimits, error) {
	if value == nil {
		return PerToolSoftLimits{}, nil
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("budgets.per_tool_soft_limits must be an object")
	}
	result := PerToolSoftLimits{}
	for toolName, rawLimit := range raw {
		if toolName != "exec_command" && toolName != "write_stdin" {
			return nil, fmt.Errorf("budgets.per_tool_soft_limits.%s is unsupported", toolName)
		}
		limitMap, ok := rawLimit.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("budgets.per_tool_soft_limits.%s must be an object", toolName)
		}
		limit, err := asToolSoftLimit(toolName, limitMap)
		if err != nil {
			return nil, err
		}
		result[toolName] = limit
	}
	return result, nil
}

func asToolSoftLimit(toolName string, raw map[string]any) (ToolSoftLimit, error) {
	allowed := map[string]struct{}{"max_output_bytes": {}}
	if toolName == "write_stdin" {
		allowed["max_continuations"] = struct{}{}
		allowed["max_yield_time_ms"] = struct{}{}
	}
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			return ToolSoftLimit{}, fmt.Errorf("budgets.per_tool_soft_limits.%s.%s is unsupported", toolName, key)
		}
	}
	maxOutputBytes, err := asOptionalBoundedPositiveInt(raw["max_output_bytes"], fmt.Sprintf("budgets.per_tool_soft_limits.%s.max_output_bytes", toolName), HardMaxToolSoftLimitOutputBytes)
	if err != nil {
		return ToolSoftLimit{}, err
	}
	limit := ToolSoftLimit{MaxOutputBytes: maxOutputBytes}
	if toolName == "write_stdin" {
		maxContinuations, err := asOptionalBoundedPositiveInt(raw["max_continuations"], "budgets.per_tool_soft_limits.write_stdin.max_continuations", HardMaxToolSoftLimitContinuations)
		if err != nil {
			return ToolSoftLimit{}, err
		}
		maxYieldTimeMs, err := asOptionalBoundedPositiveInt(raw["max_yield_time_ms"], "budgets.per_tool_soft_limits.write_stdin.max_yield_time_ms", HardMaxToolSoftLimitYieldTimeMs)
		if err != nil {
			return ToolSoftLimit{}, err
		}
		limit.MaxContinuations = maxContinuations
		limit.MaxYieldTimeMs = maxYieldTimeMs
	}
	return limit, nil
}

func asOptionalPositiveInt(value any, label string) (*int, error) {
	return asOptionalBoundedPositiveInt(value, label, 0)
}

func asOptionalBoundedPositiveInt(value any, label string, max int) (*int, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := asInt(value, label)
	if err != nil {
		return nil, err
	}
	if parsed <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer", label)
	}
	if max > 0 && parsed > max {
		return nil, fmt.Errorf("%s must be less than or equal to %d", label, max)
	}
	return &parsed, nil
}

func asOptionalFloat64(value any, label string) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	switch n := value.(type) {
	case float64:
		copy := n
		return &copy, nil
	case float32:
		copy := float64(n)
		return &copy, nil
	case int:
		copy := float64(n)
		return &copy, nil
	case int64:
		copy := float64(n)
		return &copy, nil
	case json.Number:
		copy, err := n.Float64()
		if err != nil {
			return nil, fmt.Errorf("%s must be a number", label)
		}
		return &copy, nil
	default:
		return nil, fmt.Errorf("%s must be a number", label)
	}
}

func asInt(value any, label string) (int, error) {
	switch n := value.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		if float64(int(n)) != n {
			return 0, fmt.Errorf("%s must be an integer", label)
		}
		return int(n), nil
	case float32:
		if float32(int(n)) != n {
			return 0, fmt.Errorf("%s must be an integer", label)
		}
		return int(n), nil
	case json.Number:
		parsed, err := n.Int64()
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer", label)
		}
		return int(parsed), nil
	default:
		return 0, fmt.Errorf("%s must be an integer", label)
	}
}

func copyToolBudget(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func copyOptionalFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func copyOptionalInt64(v *int64) *int64 {
	if v == nil {
		return nil
	}
	cp := *v
	return &cp
}

func asOptionalPositiveInt64(value any, field string) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case float64:
		i := int64(v)
		if float64(i) != v || i <= 0 {
			return nil, fmt.Errorf("%s must be a positive integer", field)
		}
		return &i, nil
	case json.Number:
		i, err := v.Int64()
		if err != nil || i <= 0 {
			return nil, fmt.Errorf("%s must be a positive integer", field)
		}
		return &i, nil
	default:
		return nil, fmt.Errorf("%s must be a positive integer", field)
	}
}
