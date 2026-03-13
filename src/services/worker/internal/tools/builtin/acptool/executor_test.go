package acptool

import (
	"context"
	"testing"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

func makeExecCtx(snapshot *sharedtoolruntime.RuntimeSnapshot) tools.ExecutionContext {
	return tools.ExecutionContext{
		RunID:           uuid.New(),
		Emitter:         events.NewEmitter("test-trace"),
		RuntimeSnapshot: snapshot,
	}
}

func TestExecutor_NoSandbox(t *testing.T) {
	e := ToolExecutor{}
	args := map[string]any{"task": "fix the bug"}

	// nil RuntimeSnapshot
	r := e.Execute(context.Background(), "code_agent", args, makeExecCtx(nil), "tc-1")
	if r.Error == nil || r.Error.ErrorClass != "tool.sandbox_unavailable" {
		t.Fatalf("expected sandbox_unavailable, got %+v", r.Error)
	}

	// empty SandboxBaseURL
	snap := &sharedtoolruntime.RuntimeSnapshot{}
	r = e.Execute(context.Background(), "code_agent", args, makeExecCtx(snap), "tc-2")
	if r.Error == nil || r.Error.ErrorClass != "tool.sandbox_unavailable" {
		t.Fatalf("expected sandbox_unavailable, got %+v", r.Error)
	}
}

func TestExecutor_MissingTask(t *testing.T) {
	e := ToolExecutor{}
	snap := &sharedtoolruntime.RuntimeSnapshot{SandboxBaseURL: "http://sandbox:8080"}
	ctx := makeExecCtx(snap)

	// no task key
	r := e.Execute(context.Background(), "code_agent", map[string]any{}, ctx, "tc-3")
	if r.Error == nil || r.Error.ErrorClass != "tool.args_invalid" {
		t.Fatalf("expected args_invalid, got %+v", r.Error)
	}

	// task is not a string
	r = e.Execute(context.Background(), "code_agent", map[string]any{"task": 123}, ctx, "tc-4")
	if r.Error == nil || r.Error.ErrorClass != "tool.args_invalid" {
		t.Fatalf("expected args_invalid, got %+v", r.Error)
	}
}

func TestExecutor_EmptyTask(t *testing.T) {
	e := ToolExecutor{}
	snap := &sharedtoolruntime.RuntimeSnapshot{SandboxBaseURL: "http://sandbox:8080"}
	ctx := makeExecCtx(snap)

	// empty string
	r := e.Execute(context.Background(), "code_agent", map[string]any{"task": ""}, ctx, "tc-5")
	if r.Error == nil || r.Error.ErrorClass != "tool.args_invalid" {
		t.Fatalf("expected args_invalid, got %+v", r.Error)
	}

	// whitespace only
	r = e.Execute(context.Background(), "code_agent", map[string]any{"task": "   \t\n  "}, ctx, "tc-6")
	if r.Error == nil || r.Error.ErrorClass != "tool.args_invalid" {
		t.Fatalf("expected args_invalid, got %+v", r.Error)
	}
}
