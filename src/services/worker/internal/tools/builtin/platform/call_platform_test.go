package platform

import (
"context"
"fmt"
"testing"
"time"

"arkloop/services/worker/internal/subagentctl"
"arkloop/services/worker/internal/tools"

"github.com/google/uuid"
)

type mockSubAgentControl struct {
spawnFn func(ctx context.Context, req subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error)
waitFn  func(ctx context.Context, req subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error)
}

func (m *mockSubAgentControl) Spawn(ctx context.Context, req subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
if m.spawnFn != nil {
return m.spawnFn(ctx, req)
}
return subagentctl.StatusSnapshot{}, fmt.Errorf("not implemented")
}

func (m *mockSubAgentControl) Wait(ctx context.Context, req subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
if m.waitFn != nil {
return m.waitFn(ctx, req)
}
return subagentctl.StatusSnapshot{}, fmt.Errorf("not implemented")
}

func (m *mockSubAgentControl) SendInput(ctx context.Context, req subagentctl.SendInputRequest) (subagentctl.StatusSnapshot, error) {
return subagentctl.StatusSnapshot{}, nil
}
func (m *mockSubAgentControl) Resume(ctx context.Context, req subagentctl.ResumeRequest) (subagentctl.StatusSnapshot, error) {
return subagentctl.StatusSnapshot{}, nil
}
func (m *mockSubAgentControl) Close(ctx context.Context, req subagentctl.CloseRequest) (subagentctl.StatusSnapshot, error) {
return subagentctl.StatusSnapshot{}, nil
}
func (m *mockSubAgentControl) Interrupt(ctx context.Context, req subagentctl.InterruptRequest) (subagentctl.StatusSnapshot, error) {
return subagentctl.StatusSnapshot{}, nil
}
func (m *mockSubAgentControl) GetStatus(ctx context.Context, id uuid.UUID) (subagentctl.StatusSnapshot, error) {
return subagentctl.StatusSnapshot{}, nil
}
func (m *mockSubAgentControl) ListChildren(ctx context.Context) ([]subagentctl.StatusSnapshot, error) {
return nil, nil
}

func TestCallPlatform_NilControl(t *testing.T) {
executor := &CallPlatformExecutor{Control: nil}
result := executor.Execute(context.Background(), "call_platform", map[string]any{"task": "test"}, tools.ExecutionContext{}, "")
if result.Error == nil || result.Error.ErrorClass != "tool.not_initialized" {
t.Fatal("expected not_initialized error")
}
}

func TestCallPlatform_MissingTask(t *testing.T) {
executor := &CallPlatformExecutor{Control: &mockSubAgentControl{}}
result := executor.Execute(context.Background(), "call_platform", map[string]any{}, tools.ExecutionContext{}, "")
if result.Error == nil || result.Error.ErrorClass != "tool.args_invalid" {
t.Fatal("expected args_invalid error")
}
}

func TestCallPlatform_EmptyTask(t *testing.T) {
executor := &CallPlatformExecutor{Control: &mockSubAgentControl{}}
result := executor.Execute(context.Background(), "call_platform", map[string]any{"task": "  "}, tools.ExecutionContext{}, "")
if result.Error == nil || result.Error.ErrorClass != "tool.args_invalid" {
t.Fatal("expected args_invalid error for empty task")
}
}

func TestCallPlatform_SpawnFails(t *testing.T) {
mock := &mockSubAgentControl{
spawnFn: func(ctx context.Context, req subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
return subagentctl.StatusSnapshot{}, fmt.Errorf("spawn limit reached")
},
}
executor := &CallPlatformExecutor{Control: mock}
result := executor.Execute(context.Background(), "call_platform", map[string]any{"task": "configure email"}, tools.ExecutionContext{}, "")
if result.Error == nil || result.Error.ErrorClass != "tool.spawn_failed" {
t.Fatal("expected spawn_failed error")
}
}

func TestCallPlatform_SpawnPersonaID(t *testing.T) {
var capturedReq subagentctl.SpawnRequest
id := uuid.New()
mock := &mockSubAgentControl{
spawnFn: func(ctx context.Context, req subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
capturedReq = req
return subagentctl.StatusSnapshot{SubAgentID: id, Status: "running"}, nil
},
waitFn: func(ctx context.Context, req subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
return subagentctl.StatusSnapshot{SubAgentID: id, Status: "completed"}, nil
},
}
executor := &CallPlatformExecutor{Control: mock}
executor.Execute(context.Background(), "call_platform", map[string]any{"task": "add smtp"}, tools.ExecutionContext{}, "")

if capturedReq.PersonaID != "platform" {
t.Fatalf("expected persona_id=platform, got %s", capturedReq.PersonaID)
}
if capturedReq.ContextMode != "isolated" {
t.Fatalf("expected context_mode=isolated, got %s", capturedReq.ContextMode)
}
if capturedReq.Input != "add smtp" {
t.Fatalf("expected input='add smtp', got %s", capturedReq.Input)
}
if capturedReq.SourceType != "platform_agent" {
t.Fatalf("expected source_type=platform_agent, got %s", capturedReq.SourceType)
}
}

func TestCallPlatform_Success(t *testing.T) {
id := uuid.New()
output := "SMTP configured successfully"
mock := &mockSubAgentControl{
spawnFn: func(ctx context.Context, req subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
return subagentctl.StatusSnapshot{SubAgentID: id, Status: "running"}, nil
},
waitFn: func(ctx context.Context, req subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
return subagentctl.StatusSnapshot{
SubAgentID: id,
Status:     "completed",
LastOutput: &output,
}, nil
},
}
executor := &CallPlatformExecutor{Control: mock}
result := executor.Execute(context.Background(), "call_platform", map[string]any{"task": "configure email"}, tools.ExecutionContext{}, "")

if result.Error != nil {
t.Fatalf("unexpected error: %v", result.Error)
}
if result.ResultJSON["status"] != "completed" {
t.Fatalf("expected completed, got %v", result.ResultJSON["status"])
}
if result.ResultJSON["output"] != output {
t.Fatalf("expected output=%s, got %v", output, result.ResultJSON["output"])
}
}

func TestCallPlatform_WaitTimeout(t *testing.T) {
id := uuid.New()
mock := &mockSubAgentControl{
spawnFn: func(ctx context.Context, req subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
return subagentctl.StatusSnapshot{SubAgentID: id, Status: "running"}, nil
},
waitFn: func(ctx context.Context, req subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
return subagentctl.StatusSnapshot{}, fmt.Errorf("timeout after %v", req.Timeout)
},
}
executor := &CallPlatformExecutor{Control: mock}
result := executor.Execute(context.Background(), "call_platform", map[string]any{"task": "heavy task"}, tools.ExecutionContext{}, "")

if result.Error != nil {
t.Fatal("wait timeout should return result, not error")
}
if result.ResultJSON["status"] != "timeout" {
t.Fatalf("expected timeout status, got %v", result.ResultJSON["status"])
}
}

func TestCallPlatform_SuccessWithError(t *testing.T) {
id := uuid.New()
errMsg := "provider not found"
mock := &mockSubAgentControl{
spawnFn: func(ctx context.Context, req subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
return subagentctl.StatusSnapshot{SubAgentID: id, Status: "running"}, nil
},
waitFn: func(ctx context.Context, req subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
return subagentctl.StatusSnapshot{
SubAgentID: id,
Status:     "failed",
LastError:  &errMsg,
}, nil
},
}
executor := &CallPlatformExecutor{Control: mock}
result := executor.Execute(context.Background(), "call_platform", map[string]any{"task": "delete provider x"}, tools.ExecutionContext{}, "")

if result.Error != nil {
t.Fatal("should return result even on agent failure")
}
if result.ResultJSON["error"] != errMsg {
t.Fatalf("expected error=%s, got %v", errMsg, result.ResultJSON["error"])
}
}

func TestCallPlatform_WaitTimeout5Min(t *testing.T) {
var capturedWaitReq subagentctl.WaitRequest
id := uuid.New()
mock := &mockSubAgentControl{
spawnFn: func(ctx context.Context, req subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
return subagentctl.StatusSnapshot{SubAgentID: id, Status: "running"}, nil
},
waitFn: func(ctx context.Context, req subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
capturedWaitReq = req
return subagentctl.StatusSnapshot{SubAgentID: id, Status: "completed"}, nil
},
}
executor := &CallPlatformExecutor{Control: mock}
executor.Execute(context.Background(), "call_platform", map[string]any{"task": "test"}, tools.ExecutionContext{}, "")

if capturedWaitReq.Timeout != 5*time.Minute {
t.Fatalf("expected 5min timeout, got %v", capturedWaitReq.Timeout)
}
}
