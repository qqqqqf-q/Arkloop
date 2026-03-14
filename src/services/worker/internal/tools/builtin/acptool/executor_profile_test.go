package acptool

import (
	"context"
	"fmt"
	"testing"

	sharedconfig "arkloop/services/shared/config"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

func makeExecCtxWithAccount(snapshot *sharedtoolruntime.RuntimeSnapshot) tools.ExecutionContext {
	accountID := uuid.New()
	return tools.ExecutionContext{
		RunID:           uuid.New(),
		AccountID:       &accountID,
		Emitter:         events.NewEmitter("test-trace"),
		RuntimeSnapshot: snapshot,
	}
}

func TestExecutor_ProfileWithoutResolver(t *testing.T) {
	// When profile is set but ConfigResolver is nil, execution should proceed normally
	// past profile resolution (it will fail at bridge level, not at profile resolution).
	e := ToolExecutor{}

	snap := &sharedtoolruntime.RuntimeSnapshot{
		SandboxBaseURL:   "http://sandbox:19003",
		SandboxAuthToken: "test-token",
	}

	result := e.Execute(context.Background(), "acp_agent", map[string]any{
		"task":    "test task",
		"profile": "balanced",
	}, makeExecCtxWithAccount(snap), "call-1")

	// Should fail at bridge level (can't connect to sandbox), NOT at profile resolution
	if result.Error == nil {
		t.Fatal("expected error (bridge connection should fail)")
	}
	if result.Error.ErrorClass == "tool.profile_invalid" {
		t.Error("should NOT fail at profile resolution when ConfigResolver is nil")
	}
}

func TestExecutor_InvalidProfile(t *testing.T) {
	// When profile is set with a mock resolver that returns error for unknown profiles,
	// the executor should return a profile_invalid error.
	e := ToolExecutor{
		ConfigResolver: &failResolver{},
	}

	snap := &sharedtoolruntime.RuntimeSnapshot{
		SandboxBaseURL:   "http://sandbox:19003",
		SandboxAuthToken: "test-token",
	}

	result := e.Execute(context.Background(), "acp_agent", map[string]any{
		"task":    "test task",
		"profile": "nonexistent",
	}, makeExecCtxWithAccount(snap), "call-2")

	if result.Error == nil {
		t.Fatal("expected error for invalid profile")
	}
	if result.Error.ErrorClass != "tool.profile_invalid" {
		t.Errorf("ErrorClass = %q, want %q", result.Error.ErrorClass, "tool.profile_invalid")
	}
}

func TestExecutor_ProfileLocalModeSkipsProxy(t *testing.T) {
	// When sandbox URL is localhost, profile resolution should be skipped
	// even if ConfigResolver is set.
	e := ToolExecutor{
		ConfigResolver: &failResolver{},
	}

	snap := &sharedtoolruntime.RuntimeSnapshot{
		SandboxBaseURL:   "http://localhost:19003",
		SandboxAuthToken: "test-token",
	}

	result := e.Execute(context.Background(), "acp_agent", map[string]any{
		"task":    "test task",
		"profile": "balanced",
	}, makeExecCtxWithAccount(snap), "call-3")

	// Should fail at bridge level (can't connect), NOT at profile resolution
	if result.Error == nil {
		t.Fatal("expected error (bridge connection should fail)")
	}
	if result.Error.ErrorClass == "tool.profile_invalid" {
		t.Error("should NOT fail at profile resolution for localhost sandbox")
	}
}

// failResolver always returns an error for Resolve calls.
type failResolver struct{}

func (f *failResolver) Resolve(_ context.Context, key string, _ sharedconfig.Scope) (string, error) {
	return "", fmt.Errorf("config key not registered: %s", key)
}

func (f *failResolver) ResolvePrefix(_ context.Context, _ string, _ sharedconfig.Scope) (map[string]string, error) {
	return nil, fmt.Errorf("not implemented")
}
