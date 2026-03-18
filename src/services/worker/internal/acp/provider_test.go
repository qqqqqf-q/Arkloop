package acp

import (
	"testing"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
)

func TestResolveProviderInvocationUsesActiveProviderConfig(t *testing.T) {
	rt := &sharedtoolruntime.RuntimeSnapshot{ACPHostKind: "sandbox", SandboxBaseURL: "http://sandbox.internal"}
	invocation, err := ResolveProviderInvocation("", map[string]sharedtoolruntime.ProviderConfig{
		ProviderGroupACP: {
			GroupName:    ProviderGroupACP,
			ProviderName: DefaultProviderID,
			ConfigJSON: map[string]any{
				"host_kind":     "local",
				"auth_strategy": "provider_native",
				"cwd":           "/tmp/project",
				"env_overrides": map[string]any{"FOO": "bar"},
			},
		},
	}, rt, "")
	if err != nil {
		t.Fatalf("ResolveProviderInvocation returned error: %v", err)
	}
	if invocation.Provider.ID != DefaultProviderID {
		t.Fatalf("unexpected provider id: %q", invocation.Provider.ID)
	}
	if invocation.Provider.HostKind != HostKindLocal {
		t.Fatalf("unexpected host kind: %q", invocation.Provider.HostKind)
	}
	if invocation.Provider.AuthStrategy != AuthStrategyProviderNative {
		t.Fatalf("unexpected auth strategy: %q", invocation.Provider.AuthStrategy)
	}
	if invocation.Cwd != "/tmp/project" {
		t.Fatalf("unexpected cwd: %q", invocation.Cwd)
	}
	if invocation.Env["FOO"] != "bar" {
		t.Fatalf("expected env override to be applied, got %#v", invocation.Env)
	}
}

func TestResolveProviderInvocationFallsBackToDefaultProvider(t *testing.T) {
	rt := &sharedtoolruntime.RuntimeSnapshot{ACPHostKind: "local"}
	invocation, err := ResolveProviderInvocation("", nil, rt, "/repo")
	if err != nil {
		t.Fatalf("ResolveProviderInvocation returned error: %v", err)
	}
	if invocation.Provider.ID != DefaultProviderID {
		t.Fatalf("unexpected provider id: %q", invocation.Provider.ID)
	}
	if invocation.Provider.HostKind != HostKindLocal {
		t.Fatalf("unexpected host kind: %q", invocation.Provider.HostKind)
	}
	if invocation.Provider.AuthStrategy != AuthStrategyProviderNative {
		t.Fatalf("unexpected auth strategy: %q", invocation.Provider.AuthStrategy)
	}
	if invocation.Cwd != "/repo" {
		t.Fatalf("unexpected cwd: %q", invocation.Cwd)
	}
}
