package tools

import (
	"strings"

	"arkloop/services/worker_go/internal/events"
	"arkloop/services/worker_go/internal/stablejson"
	"github.com/google/uuid"
)

const (
	PolicyDeniedCode = "policy.denied"

	DenyReasonToolNotInAllowlist = "tool.not_in_allowlist"
	DenyReasonToolArgsInvalid    = "tool.args_invalid"
	DenyReasonToolUnknown        = "tool.unknown"
)

type ToolCallDecision struct {
	ToolCallID string
	Allowed    bool
	Events     []events.RunEvent
}

type PolicyEnforcer struct {
	registry  *Registry
	allowlist Allowlist
}

func NewPolicyEnforcer(registry *Registry, allowlist Allowlist) *PolicyEnforcer {
	return &PolicyEnforcer{
		registry:  registry,
		allowlist: allowlist,
	}
}

func (p *PolicyEnforcer) RequestToolCall(
	emitter events.Emitter,
	toolName string,
	argsJSON map[string]any,
	toolCallID string,
) ToolCallDecision {
	resolvedID := resolveToolCallID(toolCallID)

	argsHash, err := stablejson.Sha256(argsJSON)
	if err != nil {
		argsHash = ""
	}

	spec, hasSpec := p.registry.Get(toolName)
	callPayload := map[string]any{
		"tool_call_id": resolvedID,
		"tool_name":    toolName,
	}
	if argsHash != "" {
		callPayload["args_hash"] = argsHash
	}
	if hasSpec {
		for key, value := range spec.ToToolCallJSON() {
			callPayload[key] = value
		}
	}

	callEvent := emitter.Emit("tool.call", callPayload, stringPtr(toolName), nil)

	if argsHash == "" {
		denied := emitter.Emit(
			"policy.denied",
			map[string]any{
				"code":        PolicyDeniedCode,
				"message":     "工具参数非法",
				"deny_reason": DenyReasonToolArgsInvalid,
				"tool_call_id": resolvedID,
				"tool_name":    toolName,
				"allowlist":    p.allowlist.ToSortedList(),
			},
			stringPtr(toolName),
			stringPtr(PolicyDeniedCode),
		)
		return ToolCallDecision{
			ToolCallID: resolvedID,
			Allowed:    false,
			Events:     []events.RunEvent{callEvent, denied},
		}
	}

	if !hasSpec {
		denied := emitter.Emit(
			"policy.denied",
			map[string]any{
				"code":        PolicyDeniedCode,
				"message":     "工具未注册",
				"deny_reason": DenyReasonToolUnknown,
				"tool_call_id": resolvedID,
				"tool_name":    toolName,
				"args_hash":    argsHash,
				"allowlist":    p.allowlist.ToSortedList(),
			},
			stringPtr(toolName),
			stringPtr(PolicyDeniedCode),
		)
		return ToolCallDecision{
			ToolCallID: resolvedID,
			Allowed:    false,
			Events:     []events.RunEvent{callEvent, denied},
		}
	}

	if !p.allowlist.Allows(toolName) {
		deniedPayload := map[string]any{
			"code":        PolicyDeniedCode,
			"message":     "工具不在 allowlist 内",
			"deny_reason": DenyReasonToolNotInAllowlist,
			"tool_call_id": resolvedID,
			"tool_name":    toolName,
			"args_hash":    argsHash,
			"allowlist":    p.allowlist.ToSortedList(),
		}
		for key, value := range spec.ToToolCallJSON() {
			deniedPayload[key] = value
		}
		denied := emitter.Emit(
			"policy.denied",
			deniedPayload,
			stringPtr(toolName),
			stringPtr(PolicyDeniedCode),
		)
		return ToolCallDecision{
			ToolCallID: resolvedID,
			Allowed:    false,
			Events:     []events.RunEvent{callEvent, denied},
		}
	}

	return ToolCallDecision{
		ToolCallID: resolvedID,
		Allowed:    true,
		Events:     []events.RunEvent{callEvent},
	}
}

func resolveToolCallID(toolCallID string) string {
	cleaned := strings.TrimSpace(toolCallID)
	if cleaned == "" {
		return uuid.NewString()
	}
	return cleaned
}

func stringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
