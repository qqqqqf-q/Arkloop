package pipeline_test

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/tools"
	spawnagent "arkloop/services/worker/internal/tools/builtin/spawn_agent"
	"github.com/google/uuid"
)

type noopSubAgentControl struct{}

func (noopSubAgentControl) Spawn(context.Context, subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{SubAgentID: uuid.New()}, nil
}
func (noopSubAgentControl) SendInput(context.Context, subagentctl.SendInputRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (noopSubAgentControl) Wait(context.Context, subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (noopSubAgentControl) Resume(context.Context, subagentctl.ResumeRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (noopSubAgentControl) Close(context.Context, subagentctl.CloseRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (noopSubAgentControl) Interrupt(context.Context, subagentctl.InterruptRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (noopSubAgentControl) GetStatus(context.Context, uuid.UUID) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (noopSubAgentControl) ListChildren(context.Context) ([]subagentctl.StatusSnapshot, error) {
	return nil, nil
}

func TestSpawnAgentMiddleware_NilSpawnPassThrough(t *testing.T) {
	mw := pipeline.NewSpawnAgentMiddleware()

	rc := &pipeline.RunContext{
		Emitter: events.NewEmitter("test"),
	}

	var reached bool
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		reached = true
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reached {
		t.Fatal("terminal handler was not called")
	}
}

func TestSpawnAgentMiddleware_WithControlAddsTools(t *testing.T) {
	mw := pipeline.NewSpawnAgentMiddleware()

	registry := tools.NewRegistry()
	for _, spec := range []tools.AgentToolSpec{
		spawnagent.AgentSpec,
		spawnagent.SendInputSpec,
		spawnagent.WaitAgentSpec,
		spawnagent.ResumeAgentSpec,
		spawnagent.CloseAgentSpec,
		spawnagent.InterruptAgentSpec,
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatalf("register %s: %v", spec.Name, err)
		}
	}

	rc := &pipeline.RunContext{
		Emitter:         events.NewEmitter("test"),
		ToolRegistry:    registry,
		ToolExecutors:   map[string]tools.Executor{},
		AllowlistSet:    map[string]struct{}{},
		ToolSpecs:       []llm.ToolSpec{},
		SubAgentControl: noopSubAgentControl{},
	}

	var reached bool
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		reached = true
		expectedNames := []string{
			spawnagent.AgentSpec.Name,
			spawnagent.SendInputSpec.Name,
			spawnagent.WaitAgentSpec.Name,
			spawnagent.ResumeAgentSpec.Name,
			spawnagent.CloseAgentSpec.Name,
			spawnagent.InterruptAgentSpec.Name,
		}
		for _, name := range expectedNames {
			if _, ok := rc.ToolExecutors[name]; !ok {
				t.Fatalf("%s executor not added", name)
			}
			if _, ok := rc.AllowlistSet[name]; !ok {
				t.Fatalf("%s not in allowlist", name)
			}
		}
		if len(rc.ToolSpecs) != len(expectedNames) {
			t.Fatalf("unexpected tool spec count: %d", len(rc.ToolSpecs))
		}
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reached {
		t.Fatal("terminal handler was not called")
	}
}
