package tools

import (
	"testing"

	"arkloop/services/worker_go/internal/events"
)

func TestPolicyEnforcerDeniesToolNotInAllowlist(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(AgentToolSpec{
		Name:        "echo",
		Version:     "1",
		Description: "x",
		RiskLevel:   RiskLevelLow,
	}); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	allowlist := AllowlistFromNames(nil)
	enforcer := NewPolicyEnforcer(registry, allowlist)
	emitter := events.NewEmitter("trace")

	decision := enforcer.RequestToolCall(emitter, "echo", map[string]any{"text": "hi"}, "")
	if decision.Allowed {
		t.Fatalf("expected denied")
	}
	if len(decision.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(decision.Events))
	}
	if decision.Events[0].Type != "tool.call" {
		t.Fatalf("unexpected first event: %s", decision.Events[0].Type)
	}
	if decision.Events[1].Type != "policy.denied" {
		t.Fatalf("unexpected second event: %s", decision.Events[1].Type)
	}
	if decision.Events[1].ErrorClass == nil || *decision.Events[1].ErrorClass != PolicyDeniedCode {
		t.Fatalf("unexpected error_class: %+v", decision.Events[1].ErrorClass)
	}
	if decision.Events[1].DataJSON["deny_reason"] != DenyReasonToolNotInAllowlist {
		t.Fatalf("unexpected deny_reason: %v", decision.Events[1].DataJSON["deny_reason"])
	}
}

func TestPolicyEnforcerDeniesUnknownTool(t *testing.T) {
	registry := NewRegistry()
	allowlist := AllowlistFromNames([]string{"missing"})
	enforcer := NewPolicyEnforcer(registry, allowlist)
	emitter := events.NewEmitter("trace")

	decision := enforcer.RequestToolCall(emitter, "missing", map[string]any{"x": 1}, "")
	if decision.Allowed {
		t.Fatalf("expected denied")
	}
	denied := decision.Events[len(decision.Events)-1]
	if denied.Type != "policy.denied" {
		t.Fatalf("unexpected event: %s", denied.Type)
	}
	if denied.DataJSON["deny_reason"] != DenyReasonToolUnknown {
		t.Fatalf("unexpected deny_reason: %v", denied.DataJSON["deny_reason"])
	}
}

func TestPolicyEnforcerDeniesInvalidArgs(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(AgentToolSpec{
		Name:        "echo",
		Version:     "1",
		Description: "x",
		RiskLevel:   RiskLevelLow,
	}); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	allowlist := AllowlistFromNames([]string{"echo"})
	enforcer := NewPolicyEnforcer(registry, allowlist)
	emitter := events.NewEmitter("trace")

	args := map[string]any{"bad": func() {}}
	decision := enforcer.RequestToolCall(emitter, "echo", args, "")
	if decision.Allowed {
		t.Fatalf("expected denied")
	}
	denied := decision.Events[len(decision.Events)-1]
	if denied.DataJSON["deny_reason"] != DenyReasonToolArgsInvalid {
		t.Fatalf("unexpected deny_reason: %v", denied.DataJSON["deny_reason"])
	}
}

