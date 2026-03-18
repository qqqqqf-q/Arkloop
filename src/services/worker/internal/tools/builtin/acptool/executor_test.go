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

func TestExecutor_NoACPHost(t *testing.T) {
	e := ToolExecutor{}
	args := map[string]any{"task": "fix the bug"}

	r := e.Execute(context.Background(), "acp_agent", args, makeExecCtx(nil), "tc-1")
	if r.Error == nil || r.Error.ErrorClass != "tool.acp_unavailable" {
		t.Fatalf("expected acp_unavailable, got %+v", r.Error)
	}

	snap := &sharedtoolruntime.RuntimeSnapshot{}
	r = e.Execute(context.Background(), "acp_agent", args, makeExecCtx(snap), "tc-2")
	if r.Error == nil || r.Error.ErrorClass != "tool.acp_unavailable" {
		t.Fatalf("expected acp_unavailable, got %+v", r.Error)
	}
}

func TestExecutor_MissingTask(t *testing.T) {
	e := ToolExecutor{}
	snap := &sharedtoolruntime.RuntimeSnapshot{SandboxBaseURL: "http://sandbox:8080"}
	ctx := makeExecCtx(snap)

	r := e.Execute(context.Background(), "acp_agent", map[string]any{}, ctx, "tc-3")
	if r.Error == nil || r.Error.ErrorClass != "tool.args_invalid" {
		t.Fatalf("expected args_invalid, got %+v", r.Error)
	}

	r = e.Execute(context.Background(), "acp_agent", map[string]any{"task": 123}, ctx, "tc-4")
	if r.Error == nil || r.Error.ErrorClass != "tool.args_invalid" {
		t.Fatalf("expected args_invalid, got %+v", r.Error)
	}
}

func TestExecutor_EmptyTask(t *testing.T) {
	e := ToolExecutor{}
	snap := &sharedtoolruntime.RuntimeSnapshot{SandboxBaseURL: "http://sandbox:8080"}
	ctx := makeExecCtx(snap)

	r := e.Execute(context.Background(), "acp_agent", map[string]any{"task": ""}, ctx, "tc-5")
	if r.Error == nil || r.Error.ErrorClass != "tool.args_invalid" {
		t.Fatalf("expected args_invalid, got %+v", r.Error)
	}

	r = e.Execute(context.Background(), "acp_agent", map[string]any{"task": "   \t\n  "}, ctx, "tc-6")
	if r.Error == nil || r.Error.ErrorClass != "tool.args_invalid" {
		t.Fatalf("expected args_invalid, got %+v", r.Error)
	}
}

func TestExecutor_LegacyAgentParamRejected(t *testing.T) {
	e := ToolExecutor{}
	snap := &sharedtoolruntime.RuntimeSnapshot{SandboxBaseURL: "http://sandbox:8080"}
	ctx := makeExecCtx(snap)

	r := e.Execute(context.Background(), "acp_agent", map[string]any{
		"task":  "fix it",
		"agent": "nonexistent",
	}, ctx, "tc-7")
	if r.Error == nil || r.Error.ErrorClass != "tool.args_invalid" {
		t.Fatalf("expected args_invalid for legacy agent param, got %+v", r.Error)
	}
}

func TestExecutor_UnknownProvider(t *testing.T) {
	e := ToolExecutor{}
	snap := &sharedtoolruntime.RuntimeSnapshot{SandboxBaseURL: "http://sandbox:8080"}
	ctx := makeExecCtx(snap)

	r := e.Execute(context.Background(), "acp_agent", map[string]any{
		"task":     "fix it",
		"provider": "acp.unknown",
	}, ctx, "tc-8")
	if r.Error == nil || r.Error.ErrorClass != "tool.args_invalid" {
		t.Fatalf("expected args_invalid for unknown provider, got %+v", r.Error)
	}
}
