package pipeline_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"arkloop/services/shared/eventbus"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"
)

type titleSummarizerTestRow struct {
	scan func(dest ...any) error
}

func (r titleSummarizerTestRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

type titleSummarizerTestDB struct{}

func (titleSummarizerTestDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return titleSummarizerTestRow{
		scan: func(dest ...any) error {
			ptr, ok := dest[0].(*int)
			if !ok {
				return fmt.Errorf("unexpected scan target: %T", dest[0])
			}
			*ptr = 1
			return nil
		},
	}
}

func (titleSummarizerTestDB) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (titleSummarizerTestDB) BeginTx(_ context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return nil, fmt.Errorf("BeginTx should not be called in this test")
}

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
		Gateway: auxGateway,
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

func TestTitleSummarizerMiddleware_StillReturnsErrorFromNext(t *testing.T) {
	stubCfg, _ := llm.AuxGatewayConfigFromEnv()
	stubCfg.Enabled = true
	stubCfg.DeltaCount = 1
	stubCfg.DeltaInterval = 0
	auxGateway := llm.NewAuxGateway(stubCfg)

	mw := pipeline.NewTitleSummarizerMiddleware(titleSummarizerTestDB{}, nil, auxGateway, false, nil)

	rc := &pipeline.RunContext{
		Run: data.Run{
			ID:       uuid.New(),
			ThreadID: uuid.New(),
		},
		Gateway: auxGateway,
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

	started := make(chan struct{}, 1)
	pipeline.SetTitleSummarizerGeneratorForTest(func(context.Context, pipeline.TitleSummarizerDB, *redis.Client, eventbus.EventBus, llm.Gateway, uuid.UUID, uuid.UUID, string, []llm.Message, string, int) {
		select {
		case started <- struct{}{}:
		default:
		}
	})
	defer pipeline.ResetTitleSummarizerGeneratorForTest()

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		return fmt.Errorf("boom")
	})
	if err := h(context.Background(), rc); err == nil {
		t.Fatal("expected middleware to return error")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected title generator to start asynchronously")
	}
}

func TestTitleSummarizerMiddleware_StartsAsyncBeforeNextReturns(t *testing.T) {
	stubCfg, _ := llm.AuxGatewayConfigFromEnv()
	stubCfg.Enabled = true
	stubCfg.DeltaCount = 1
	stubCfg.DeltaInterval = 0
	auxGateway := llm.NewAuxGateway(stubCfg)

	mw := pipeline.NewTitleSummarizerMiddleware(titleSummarizerTestDB{}, nil, auxGateway, false, nil)

	rc := &pipeline.RunContext{
		Run: data.Run{
			ID:       uuid.New(),
			ThreadID: uuid.New(),
		},
		Gateway: auxGateway,
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

	started := make(chan struct{}, 1)
	releaseNext := make(chan struct{})
	done := make(chan error, 1)

	pipeline.SetTitleSummarizerGeneratorForTest(func(context.Context, pipeline.TitleSummarizerDB, *redis.Client, eventbus.EventBus, llm.Gateway, uuid.UUID, uuid.UUID, string, []llm.Message, string, int) {
		select {
		case started <- struct{}{}:
		default:
		}
	})
	defer pipeline.ResetTitleSummarizerGeneratorForTest()

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		<-releaseNext
		return nil
	})

	go func() {
		done <- h(context.Background(), rc)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected title generator to start before next returned")
	}

	close(releaseNext)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("middleware did not finish")
	}
}
