package pipeline_test

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"

	"github.com/google/uuid"
)

func TestTitleSummarizerMiddleware_NilConfigPassThrough(t *testing.T) {
	mw := pipeline.NewTitleSummarizerMiddleware(nil, nil, nil, false, nil)

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

func TestTitleSummarizerMiddleware_WithConfigCallsNextImmediately(t *testing.T) {
	stubCfg, _ := llm.StubGatewayConfigFromEnv()
	stubCfg.Enabled = true
	stubCfg.DeltaCount = 1
	stubCfg.DeltaInterval = 0
	stubGateway := llm.NewStubGateway(stubCfg)

	mw := pipeline.NewTitleSummarizerMiddleware(nil, nil, stubGateway, false, nil)

	rc := &pipeline.RunContext{
		Run: data.Run{
			ID:       uuid.New(),
			ThreadID: uuid.New(),
		},
		Emitter: events.NewEmitter("test"),
		TitleSummarizer: &personas.TitleSummarizerConfig{
			Prompt:    "Test prompt",
			MaxTokens: 10,
		},
		Messages: []llm.Message{
			{
				Role:    "user",
				Content: []llm.TextPart{{Text: "Hello"}},
			},
		},
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
		t.Fatal("terminal handler was not called immediately")
	}
}
