package tools

import (
	"fmt"

	"arkloop/services/worker/internal/llm"
)

type RiskLevel string

const (
	RiskLevelLow    RiskLevel = "low"
	RiskLevelMedium RiskLevel = "medium"
	RiskLevelHigh   RiskLevel = "high"
)

type AgentToolSpec struct {
	Name           string
	Version        string
	Description    string
	RiskLevel      RiskLevel
	RequiredScopes []string
	SideEffects    bool
}

func (s AgentToolSpec) ToToolCallJSON() map[string]any {
	payload := map[string]any{
		"tool_name":      s.Name,
		"tool_version":   s.Version,
		"risk_level":     string(s.RiskLevel),
		"required_scopes": append([]string{}, s.RequiredScopes...),
		"side_effects":   s.SideEffects,
	}
	return payload
}

func (s AgentToolSpec) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("tool name 不能为空")
	}
	if s.Version == "" {
		return fmt.Errorf("tool version 不能为空")
	}
	if s.Description == "" {
		return fmt.Errorf("tool description 不能为空")
	}
	if s.RiskLevel == "" {
		return fmt.Errorf("tool risk_level 不能为空")
	}
	return nil
}

type LlmToolSpec = llm.ToolSpec
