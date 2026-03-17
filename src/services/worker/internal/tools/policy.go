package tools

import (
	"strings"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/stablejson"
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
	callEvent, resolvedID, argsHash, hasSpec := p.buildToolCallEvent(emitter, toolName, argsJSON, toolCallID)

	if argsHash == "" {
		denied := emitter.Emit(
			"policy.denied",
			map[string]any{
				"code":         PolicyDeniedCode,
				"message":      "tool arguments invalid",
				"deny_reason":  DenyReasonToolArgsInvalid,
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
				"code":         PolicyDeniedCode,
				"message":      "tool not registered",
				"deny_reason":  DenyReasonToolUnknown,
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
			"code":         PolicyDeniedCode,
			"message":      "tool not in allowlist",
			"deny_reason":  DenyReasonToolNotInAllowlist,
			"tool_call_id": resolvedID,
			"tool_name":    toolName,
			"args_hash":    argsHash,
			"allowlist":    p.allowlist.ToSortedList(),
		}
		if spec, ok := p.registry.Get(toolName); ok {
			for key, value := range spec.ToToolCallJSON() {
				deniedPayload[key] = value
			}
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

func (p *PolicyEnforcer) BuildToolCallEvent(
	emitter events.Emitter,
	toolName string,
	argsJSON map[string]any,
	toolCallID string,
) events.RunEvent {
	callEvent, _, _, _ := p.buildToolCallEvent(emitter, toolName, argsJSON, toolCallID)
	return callEvent
}

func (p *PolicyEnforcer) buildToolCallEvent(
	emitter events.Emitter,
	toolName string,
	argsJSON map[string]any,
	toolCallID string,
) (events.RunEvent, string, string, bool) {
	resolvedID := resolveToolCallID(toolCallID)

	argsHash, err := stablejson.Sha256(argsJSON)
	if err != nil {
		argsHash = ""
	}

	spec, hasSpec := p.registry.Get(toolName)
	callPayload := map[string]any{
		"tool_call_id": resolvedID,
		"tool_name":    toolName,
		"arguments":    argsJSON,
	}
	if argsHash != "" {
		callPayload["args_hash"] = argsHash
	}
	if hasSpec {
		for key, value := range spec.ToToolCallJSON() {
			callPayload[key] = value
		}
	}

	return emitter.Emit("tool.call", callPayload, stringPtr(toolName), nil), resolvedID, argsHash, hasSpec
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
