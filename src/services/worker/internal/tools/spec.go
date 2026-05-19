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

type InterruptBehavior string

const (
	InterruptBehaviorBlock  InterruptBehavior = "block"
	InterruptBehaviorCancel InterruptBehavior = "cancel"
)

type HardTimeoutMode string

const (
	HardTimeoutModeEnforced HardTimeoutMode = "enforced"
	HardTimeoutModeIgnored  HardTimeoutMode = "ignored"
)

type AgentToolSpec struct {
	Name                      string
	LlmName                   string
	Version                   string
	Description               string
	RiskLevel                 RiskLevel
	RequiredScopes            []string
	SideEffects               bool
	ConcurrencySafe           *bool
	InterruptBehavior         InterruptBehavior
	RequiresExclusiveAccess   bool
	SupportsProgressHeartbeat bool
	HardTimeoutMode           HardTimeoutMode
	Annotations               *llm.ToolAnnotations
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
	payload["capabilities"] = s.Capabilities().ToJSON()
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

type ToolCapabilities struct {
	ConcurrencySafe           bool
	InterruptBehavior         InterruptBehavior
	RequiresExclusiveAccess   bool
	SupportsProgressHeartbeat bool
	HardTimeoutMode           HardTimeoutMode
}

func (s AgentToolSpec) Capabilities() ToolCapabilities {
	concurrencySafe := !s.SideEffects
	if s.ConcurrencySafe != nil {
		concurrencySafe = *s.ConcurrencySafe
	}
	if s.RequiresExclusiveAccess {
		concurrencySafe = false
	}

	interruptBehavior := s.InterruptBehavior
	if interruptBehavior == "" {
		if concurrencySafe {
			interruptBehavior = InterruptBehaviorCancel
		} else {
			interruptBehavior = InterruptBehaviorBlock
		}
	}

	hardTimeoutMode := s.HardTimeoutMode
	if hardTimeoutMode == "" {
		hardTimeoutMode = HardTimeoutModeEnforced
	}

	return ToolCapabilities{
		ConcurrencySafe:           concurrencySafe,
		InterruptBehavior:         interruptBehavior,
		RequiresExclusiveAccess:   s.RequiresExclusiveAccess,
		SupportsProgressHeartbeat: s.SupportsProgressHeartbeat,
		HardTimeoutMode:           hardTimeoutMode,
	}
}

func (c ToolCapabilities) ToJSON() map[string]any {
	return map[string]any{
		"concurrency_safe":            c.ConcurrencySafe,
		"interrupt_behavior":          string(c.InterruptBehavior),
		"requires_exclusive_access":   c.RequiresExclusiveAccess,
		"supports_progress_heartbeat": c.SupportsProgressHeartbeat,
		"hard_timeout_mode":           string(c.HardTimeoutMode),
	}
}
