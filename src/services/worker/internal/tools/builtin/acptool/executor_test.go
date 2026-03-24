package acptool

import (
	"context"
	"testing"
	"time"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/acp"
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

func TestExecutor_WaitACPStreamsCachedEvents(t *testing.T) {
	te := ToolExecutor{}
	ctx := context.Background()
	handleID := "wait-event-test"
	entry := globalACPHandleStore.create(handleID, ctx, func() {})
	entry.evMu.Lock()
	entry.cachedEvents = []events.RunEvent{
		{Type: "message.delta", DataJSON: map[string]any{"content_delta": "delta"}},
	}
	entry.evMu.Unlock()
	entry.mu.Lock()
	entry.status = acpStatusCompleted
	entry.mu.Unlock()
	defer func() {
		globalACPHandleStore.mu.Lock()
		delete(globalACPHandleStore.entries, handleID)
		globalACPHandleStore.mu.Unlock()
	}()

	var streamCount int
	execCtx := tools.ExecutionContext{
		StreamEvent: func(ev events.RunEvent) error {
			streamCount++
			return nil
		},
		Emitter: events.NewEmitter("test"),
	}

	res := te.executeWaitACP(ctx, map[string]any{"handle_id": handleID}, execCtx, time.Now())
	if streamCount != 1 {
		t.Fatalf("expected StreamEvent invocation, got %d", streamCount)
	}
	status, _ := res.ResultJSON["status"].(string)
	if status != "completed" {
		t.Fatalf("expected completed status, got %v", res.ResultJSON)
	}
}

func TestBuildRuntimeSessionKey(t *testing.T) {
	runID := "run-123"
	sandboxKey := buildRuntimeSessionKey(runID, acp.ResolvedProvider{
		ID:       acp.DefaultProviderID,
		HostKind: acp.HostKindSandbox,
	})
	localKey := buildRuntimeSessionKey(runID, acp.ResolvedProvider{
		ID:       acp.DefaultProviderID,
		HostKind: acp.HostKindLocal,
	})
	otherProviderKey := buildRuntimeSessionKey(runID, acp.ResolvedProvider{
		ID:       "acp.other",
		HostKind: acp.HostKindSandbox,
	})

	if sandboxKey != "run-123|acp.opencode|sandbox" {
		t.Fatalf("sandboxKey = %q", sandboxKey)
	}
	if localKey != "run-123|acp.opencode|local" {
		t.Fatalf("localKey = %q", localKey)
	}
	if otherProviderKey != "run-123|acp.other|sandbox" {
		t.Fatalf("otherProviderKey = %q", otherProviderKey)
	}
	if sandboxKey == localKey {
		t.Fatal("runtime session key should distinguish host kind")
	}
	if sandboxKey == otherProviderKey {
		t.Fatal("runtime session key should distinguish provider")
	}
}

func TestSessionHandshakeTimeoutMsClamp(t *testing.T) {
	if v := sessionHandshakeTimeoutMs(tools.ExecutionContext{}); v != 0 {
		t.Fatalf("nil timeout: got %d", v)
	}
	mid := 90000
	if v := sessionHandshakeTimeoutMs(tools.ExecutionContext{TimeoutMs: &mid}); v != 90000 {
		t.Fatalf("mid: got %d", v)
	}
	low := 1000
	if v := sessionHandshakeTimeoutMs(tools.ExecutionContext{TimeoutMs: &low}); v != 30000 {
		t.Fatalf("min clamp: got %d", v)
	}
	high := 99999999
	if v := sessionHandshakeTimeoutMs(tools.ExecutionContext{TimeoutMs: &high}); v != 300000 {
		t.Fatalf("max clamp: got %d", v)
	}
}
