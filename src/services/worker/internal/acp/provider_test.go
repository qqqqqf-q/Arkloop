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

func TestResolveProviderInvocationCommandStringOverride(t *testing.T) {
	rt := &sharedtoolruntime.RuntimeSnapshot{ACPHostKind: "local"}
	invocation, err := ResolveProviderInvocation("", map[string]sharedtoolruntime.ProviderConfig{
		ProviderGroupACP: {
			GroupName:    ProviderGroupACP,
			ProviderName: DefaultProviderID,
			ConfigJSON: map[string]any{
				"command": "myagent run acp-mode",
			},
		},
	}, rt, "")
	if err != nil {
		t.Fatalf("ResolveProviderInvocation: %v", err)
	}
	if invocation.Provider.Command != "myagent" {
		t.Fatalf("command = %q, want myagent", invocation.Provider.Command)
	}
	want := []string{"run", "acp-mode"}
	if len(invocation.Provider.Args) != len(want) {
		t.Fatalf("args = %v, want %v", invocation.Provider.Args, want)
	}
	for i := range want {
		if invocation.Provider.Args[i] != want[i] {
			t.Fatalf("args = %v, want %v", invocation.Provider.Args, want)
		}
	}
}

func TestResolveProviderInvocationCommandArrayOverride(t *testing.T) {
	rt := &sharedtoolruntime.RuntimeSnapshot{ACPHostKind: "local"}
	invocation, err := ResolveProviderInvocation("", map[string]sharedtoolruntime.ProviderConfig{
		ProviderGroupACP: {
			GroupName:    ProviderGroupACP,
			ProviderName: DefaultProviderID,
			ConfigJSON: map[string]any{
				"command": []any{"opencode", "acp", "--verbose"},
			},
		},
	}, rt, "")
	if err != nil {
		t.Fatalf("ResolveProviderInvocation: %v", err)
	}
	if invocation.Provider.Command != "opencode" {
		t.Fatalf("command = %q", invocation.Provider.Command)
	}
	if len(invocation.Provider.Args) != 2 || invocation.Provider.Args[0] != "acp" || invocation.Provider.Args[1] != "--verbose" {
		t.Fatalf("args = %v", invocation.Provider.Args)
	}
}

func TestResolveProviderInvocationExtraArgsAppended(t *testing.T) {
	rt := &sharedtoolruntime.RuntimeSnapshot{ACPHostKind: "local"}
	invocation, err := ResolveProviderInvocation("", map[string]sharedtoolruntime.ProviderConfig{
		ProviderGroupACP: {
			GroupName:    ProviderGroupACP,
			ProviderName: DefaultProviderID,
			ConfigJSON: map[string]any{
				"command":    []any{"opencode", "acp"},
				"extra_args": "--experimental-acp",
			},
		},
	}, rt, "")
	if err != nil {
		t.Fatalf("ResolveProviderInvocation: %v", err)
	}
	if invocation.Provider.Command != "opencode" {
		t.Fatalf("command = %q", invocation.Provider.Command)
	}
	want := []string{"acp", "--experimental-acp"}
	if len(invocation.Provider.Args) != len(want) {
		t.Fatalf("args = %v, want %v", invocation.Provider.Args, want)
	}
	for i := range want {
		if invocation.Provider.Args[i] != want[i] {
			t.Fatalf("args = %v, want %v", invocation.Provider.Args, want)
		}
	}
}
