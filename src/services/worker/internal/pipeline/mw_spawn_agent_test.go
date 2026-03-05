package pipeline_test

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/tools"
	spawnagent "arkloop/services/worker/internal/tools/builtin/spawn_agent"

)

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

func TestSpawnAgentMiddleware_WithSpawnAddsTool(t *testing.T) {
	mw := pipeline.NewSpawnAgentMiddleware()

	registry := tools.NewRegistry()
	if err := registry.Register(spawnagent.AgentSpec); err != nil {
		t.Fatalf("register spawn_agent: %v", err)
	}

	rc := &pipeline.RunContext{
		Emitter:       events.NewEmitter("test"),
		ToolRegistry:  registry,
		ToolExecutors: map[string]tools.Executor{},
		AllowlistSet:  map[string]struct{}{},
		ToolSpecs:     []tools.LlmToolSpec{},
		SpawnChildRun: func(ctx context.Context, personaID string, input string) (string, error) {
			return "spawned output", nil
		},
	}

	var reached bool
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		reached = true
		if _, ok := rc.ToolExecutors[spawnagent.AgentSpec.Name]; !ok {
			t.Fatal("spawn_agent executor not added")
		}
		if _, ok := rc.AllowlistSet[spawnagent.AgentSpec.Name]; !ok {
			t.Fatal("spawn_agent not in allowlist")
		}
		if len(rc.ToolSpecs) != 1 || rc.ToolSpecs[0].Name != spawnagent.AgentSpec.Name {
			t.Fatal("spawn_agent spec not added")
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
