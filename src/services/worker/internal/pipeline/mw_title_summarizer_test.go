package pipeline_test

import (
	"context"
	"fmt"
	"testing"

	"arkloop/services/shared/eventbus"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
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
	stubCfg, _ := llm.AuxGatewayConfigFromEnv()
	stubCfg.Enabled = true
	stubCfg.DeltaCount = 1
	stubCfg.DeltaInterval = 0
	auxGateway := llm.NewAuxGateway(stubCfg)

	mw := pipeline.NewTitleSummarizerMiddleware(nil, nil, auxGateway, false, nil)

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

func TestTitleSummarizerMiddleware_SkipsAsyncOnError(t *testing.T) {
	stubCfg, _ := llm.AuxGatewayConfigFromEnv()
	stubCfg.Enabled = true
	stubCfg.DeltaCount = 1
	stubCfg.DeltaInterval = 0
	auxGateway := llm.NewAuxGateway(stubCfg)

	mw := pipeline.NewTitleSummarizerMiddleware(nil, nil, auxGateway, false, nil)

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

	called := false
	pipeline.SetTitleSummarizerGeneratorForTest(func(context.Context, pipeline.TitleSummarizerDB, *redis.Client, eventbus.EventBus, llm.Gateway, uuid.UUID, uuid.UUID, string, []llm.Message, string, int) {
		called = true
	})
	defer pipeline.ResetTitleSummarizerGeneratorForTest()

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		return fmt.Errorf("boom")
	})
	if err := h(context.Background(), rc); err == nil {
		t.Fatal("expected middleware to return error")
	}
	if called {
		t.Fatal("expected title generator not to run")
	}
}
