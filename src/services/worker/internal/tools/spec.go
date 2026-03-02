package tools

import (
	"fmt"
	"strings"

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
	LlmName        string
	Version        string
	Description    string
	RiskLevel      RiskLevel
	RequiredScopes []string
	SideEffects    bool
}

func (s AgentToolSpec) ToToolCallJSON() map[string]any {
	payload := map[string]any{
		"tool_name":       s.Name,
		"tool_version":    s.Version,
		"risk_level":      string(s.RiskLevel),
		"required_scopes": append([]string{}, s.RequiredScopes...),
		"side_effects":    s.SideEffects,
	}
	if s.LlmName != "" {
		payload["llm_name"] = s.LlmName
	}
	return payload
}

func (s AgentToolSpec) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("tool name must not be empty")
	}
	if s.LlmName != "" && strings.TrimSpace(s.LlmName) == "" {
		return fmt.Errorf("tool llm_name must not be empty when set")
	}
	if s.Version == "" {
		return fmt.Errorf("tool version must not be empty")
	}
	if s.Description == "" {
		return fmt.Errorf("tool description must not be empty")
	}
	if s.RiskLevel == "" {
		return fmt.Errorf("tool risk_level must not be empty")
	}
	return nil
}

type LlmToolSpec = llm.ToolSpec
