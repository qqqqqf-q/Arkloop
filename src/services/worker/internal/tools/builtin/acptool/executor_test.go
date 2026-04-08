package acptool

import (
	"context"
	"sync/atomic"
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
	entry := globalACPHandleStore.create(handleID, "run-test", ctx, func() {})
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

func TestExecutor_WaitACPUsesDefaultTimeout(t *testing.T) {
	te := ToolExecutor{}
	ctx := context.Background()
	handleID := "wait-default-timeout-test"
	entry := globalACPHandleStore.create(handleID, "run-timeout", ctx, func() {})
	t.Cleanup(func() {
		globalACPHandleStore.mu.Lock()
		delete(globalACPHandleStore.entries, handleID)
		globalACPHandleStore.mu.Unlock()
	})

	previousDefault := defaultWaitACPTimeout
	defaultWaitACPTimeout = 80 * time.Millisecond
	t.Cleanup(func() {
		defaultWaitACPTimeout = previousDefault
	})

	execCtx := tools.ExecutionContext{Emitter: events.NewEmitter("test")}
	res := te.executeWaitACP(ctx, map[string]any{"handle_id": handleID}, execCtx, time.Now())
	status, _ := res.ResultJSON["status"].(string)
	if status != "running" {
		t.Fatalf("expected running status after timeout, got %v", res.ResultJSON)
	}
	timedOut, _ := res.ResultJSON["timeout"].(bool)
	if !timedOut {
		t.Fatalf("expected timeout=true, got %v", res.ResultJSON)
	}

	// Keep the entry alive until cleanup to avoid masking the timeout assertion.
	_ = entry
}

func TestExecutor_CleanupRunClosesSpawnHandles(t *testing.T) {
	te := ToolExecutor{}
	var cancelledA1 int32
	var cancelledA2 int32
	var cancelledB int32

	entryA1 := globalACPHandleStore.create("cleanup-run-a1", "run-a", context.Background(), func() {
		atomic.AddInt32(&cancelledA1, 1)
	})
	entryA2 := globalACPHandleStore.create("cleanup-run-a2", "run-a", context.Background(), func() {
		atomic.AddInt32(&cancelledA2, 1)
	})
	_ = globalACPHandleStore.create("cleanup-run-b1", "run-b", context.Background(), func() {
		atomic.AddInt32(&cancelledB, 1)
	})
	t.Cleanup(func() {
		globalACPHandleStore.mu.Lock()
		delete(globalACPHandleStore.entries, "cleanup-run-a1")
		delete(globalACPHandleStore.entries, "cleanup-run-a2")
		delete(globalACPHandleStore.entries, "cleanup-run-b1")
		globalACPHandleStore.mu.Unlock()
	})

	if err := te.CleanupRun(context.Background(), "run-a", "cancelled"); err != nil {
		t.Fatalf("cleanup run failed: %v", err)
	}
	if globalACPHandleStore.get("cleanup-run-a1") != nil || globalACPHandleStore.get("cleanup-run-a2") != nil {
		t.Fatalf("expected run-a handles removed from store")
	}
	if globalACPHandleStore.get("cleanup-run-b1") == nil {
		t.Fatalf("expected run-b handle to remain")
	}
	if got := atomic.LoadInt32(&cancelledA1); got != 1 {
		t.Fatalf("run-a handle 1 cancel count = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&cancelledA2); got != 1 {
		t.Fatalf("run-a handle 2 cancel count = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&cancelledB); got != 0 {
		t.Fatalf("run-b handle cancel count = %d, want 0", got)
	}

	entryA1.mu.Lock()
	statusA1 := entryA1.status
	entryA1.mu.Unlock()
	if statusA1 != acpStatusClosed {
		t.Fatalf("run-a handle 1 status = %s, want closed", statusA1)
	}
	entryA2.mu.Lock()
	statusA2 := entryA2.status
	entryA2.mu.Unlock()
	if statusA2 != acpStatusClosed {
		t.Fatalf("run-a handle 2 status = %s, want closed", statusA2)
	}
	select {
	case <-entryA1.doneCh:
	default:
		t.Fatalf("run-a handle 1 done channel should be closed")
	}
	select {
	case <-entryA2.doneCh:
	default:
		t.Fatalf("run-a handle 2 done channel should be closed")
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
