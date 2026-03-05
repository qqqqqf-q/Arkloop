package pipeline_test

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/pipeline"

	"github.com/google/uuid"
)

func TestAgentConfigMiddleware_NilPoolPassThrough(t *testing.T) {
	mw := pipeline.NewAgentConfigMiddleware(nil)

	rc := &pipeline.RunContext{
		Run: data.Run{
			ID:       uuid.New(),
			ThreadID: uuid.New(),
			OrgID:    uuid.New(),
		},
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
