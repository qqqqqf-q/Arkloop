package pipeline_test

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/tools"
	builtin "arkloop/services/worker/internal/tools/builtin"
)

func TestMCPDiscoveryMiddleware_NoCachePassThrough(t *testing.T) {
	mw := pipeline.NewMCPDiscoveryMiddleware(
		nil, // no cache
		nil, // no queryer
		map[string]tools.Executor{"echo": builtin.EchoExecutor{}},
		[]llm.ToolSpec{builtin.EchoLlmSpec},
		map[string]struct{}{"echo": {}},
		tools.NewRegistry(),
	)

	rc := &pipeline.RunContext{
		Emitter: events.NewEmitter("test"),
	}

	var reached bool
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		reached = true
		if len(rc.ToolExecutors) != 1 {
			t.Fatalf("expected 1 executor, got %d", len(rc.ToolExecutors))
		}
		if len(rc.ToolSpecs) != 1 {
			t.Fatalf("expected 1 spec, got %d", len(rc.ToolSpecs))
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
