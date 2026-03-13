package spawnagent

import (
	"context"
	"testing"
	"time"

	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
)

type stubControl struct {
	spawn func(context.Context, subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error)
	wait  func(context.Context, subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error)
}

func (s stubControl) Spawn(ctx context.Context, req subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
	return s.spawn(ctx, req)
}
func (s stubControl) SendInput(context.Context, subagentctl.SendInputRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (s stubControl) Wait(ctx context.Context, req subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
	return s.wait(ctx, req)
}
func (s stubControl) Resume(context.Context, subagentctl.ResumeRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (s stubControl) Close(context.Context, subagentctl.CloseRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (s stubControl) Interrupt(context.Context, subagentctl.InterruptRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (s stubControl) GetStatus(context.Context, uuid.UUID) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (s stubControl) ListChildren(context.Context) ([]subagentctl.StatusSnapshot, error) {
	return nil, nil
}

func TestToolExecutorSpawnReturnsHandle(t *testing.T) {
	subAgentID := uuid.New()
	runID := uuid.New()
	exec := &ToolExecutor{Control: stubControl{spawn: func(_ context.Context, req subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
		if req.PersonaID != "researcher@1" || req.Input != "collect facts" || req.ContextMode != "fork_recent" {
			t.Fatalf("unexpected spawn request: %#v", req)
		}
		if req.Role == nil || *req.Role != "worker" {
			t.Fatalf("unexpected role: %#v", req.Role)
		}
		if req.Nickname == nil || *req.Nickname != "Atlas" {
			t.Fatalf("unexpected nickname: %#v", req.Nickname)
		}
		return subagentctl.StatusSnapshot{SubAgentID: subAgentID, ParentRunID: uuid.New(), RootRunID: uuid.New(), Depth: 1, Status: "queued", CurrentRunID: &runID}, nil
	}, wait: func(_ context.Context, _ subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
		return subagentctl.StatusSnapshot{}, nil
	}}}

	result := exec.Execute(context.Background(), AgentSpec.Name, map[string]any{
		"persona_id":   "researcher@1",
		"role":         "worker",
		"nickname":     "Atlas",
		"context_mode": "fork_recent",
		"inherit": map[string]any{
			"runtime": true,
		},
		"input": "collect facts",
	}, tools.ExecutionContext{ToolAllowlist: []string{"browser"}, RouteID: "route_parent", Model: "gpt-4.1"}, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if got := result.ResultJSON["sub_agent_id"]; got != subAgentID.String() {
		t.Fatalf("unexpected sub_agent_id: %#v", got)
	}
	if got := result.ResultJSON["current_run_id"]; got != runID.String() {
		t.Fatalf("unexpected current_run_id: %#v", got)
	}
	if got := result.ResultJSON["status"]; got != "queued" {
		t.Fatalf("unexpected status: %#v", got)
	}
}

func TestToolExecutorSpawnRejectsMissingContextMode(t *testing.T) {
	exec := &ToolExecutor{Control: stubControl{spawn: func(_ context.Context, _ subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
		return subagentctl.StatusSnapshot{}, nil
	}, wait: func(_ context.Context, _ subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
		return subagentctl.StatusSnapshot{}, nil
	}}}

	result := exec.Execute(context.Background(), AgentSpec.Name, map[string]any{"persona_id": "researcher@1", "input": "collect facts"}, tools.ExecutionContext{}, "")
	if result.Error == nil {
		t.Fatal("expected error")
	}
	if result.Error.Message != "context_mode must be a non-empty string" {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
}

func TestToolExecutorWaitReturnsOutput(t *testing.T) {
	subAgentID := uuid.New()
	output := "done"
	exec := &ToolExecutor{Control: stubControl{spawn: func(_ context.Context, _ subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
		return subagentctl.StatusSnapshot{}, nil
	}, wait: func(_ context.Context, req subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
		if req.SubAgentID != subAgentID || req.Timeout != 3*time.Second {
			t.Fatalf("unexpected wait request: %#v", req)
		}
		return subagentctl.StatusSnapshot{SubAgentID: subAgentID, ParentRunID: uuid.New(), RootRunID: uuid.New(), Depth: 1, Status: "completed", LastOutput: &output}, nil
	}}}

	result := exec.Execute(context.Background(), WaitAgentSpec.Name, map[string]any{"sub_agent_id": subAgentID.String(), "timeout_seconds": 3.0}, tools.ExecutionContext{}, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if got := result.ResultJSON["output"]; got != output {
		t.Fatalf("unexpected output: %#v", got)
	}
	if got := result.ResultJSON["status"]; got != "completed" {
		t.Fatalf("unexpected status: %#v", got)
	}
}
