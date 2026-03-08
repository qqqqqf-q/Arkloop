package app

import (
	"context"
	"os"
	"reflect"
	"testing"

	"arkloop/services/worker/internal/tools"
)

func TestResolveBaseToolAllowlistNamesIgnoresEnvAllowlist(t *testing.T) {
	t.Setenv("ARKLOOP_TOOL_ALLOWLIST", "tool_b")

	registry := tools.NewRegistry()
	for _, spec := range []tools.AgentToolSpec{
		{Name: "tool_a", Version: "1", Description: "a", RiskLevel: tools.RiskLevelLow},
		{Name: "tool_b", Version: "1", Description: "b", RiskLevel: tools.RiskLevelLow},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatalf("register tool: %v", err)
		}
	}

	got := resolveBaseToolAllowlistNames(context.Background(), registry)
	want := []string{"tool_a", "tool_b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected allowlist names: got %v want %v", got, want)
	}

	if raw := os.Getenv("ARKLOOP_TOOL_ALLOWLIST"); raw == "" {
		t.Fatal("expected env to stay set during test")
	}
}
