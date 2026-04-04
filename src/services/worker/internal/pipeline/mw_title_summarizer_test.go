package pipeline

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/personas"

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

func (titleSummarizerTestDB) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, nil
}

func (titleSummarizerTestDB) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (titleSummarizerTestDB) BeginTx(_ context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return nil, fmt.Errorf("BeginTx should not be called in this test")
}

func TestTitleSummarizerMiddleware_NilConfigPassThrough(t *testing.T) {
	mw := NewTitleSummarizerMiddleware(nil, nil, nil, false, nil)

	rc := &RunContext{
		Emitter: events.NewEmitter("test"),
	}

	var reached bool
	h := Build([]RunMiddleware{mw}, func(_ context.Context, _ *RunContext) error {
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

	mw := NewTitleSummarizerMiddleware(nil, nil, auxGateway, false, nil)

	rc := &RunContext{
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
	h := Build([]RunMiddleware{mw}, func(_ context.Context, _ *RunContext) error {
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

	mw := NewTitleSummarizerMiddleware(titleSummarizerTestDB{}, nil, auxGateway, false, nil)

	rc := &RunContext{
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
	SetTitleSummarizerGeneratorForTest(func(context.Context, TitleSummarizerDB, *redis.Client, eventbus.EventBus, llm.Gateway, uuid.UUID, uuid.UUID, string, []llm.Message, string, int) {
		select {
		case started <- struct{}{}:
		default:
		}
	})
	defer ResetTitleSummarizerGeneratorForTest()

	h := Build([]RunMiddleware{mw}, func(_ context.Context, _ *RunContext) error {
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

	mw := NewTitleSummarizerMiddleware(titleSummarizerTestDB{}, nil, auxGateway, false, nil)

	rc := &RunContext{
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

	SetTitleSummarizerGeneratorForTest(func(context.Context, TitleSummarizerDB, *redis.Client, eventbus.EventBus, llm.Gateway, uuid.UUID, uuid.UUID, string, []llm.Message, string, int) {
		select {
		case started <- struct{}{}:
		default:
		}
	})
	defer ResetTitleSummarizerGeneratorForTest()

	h := Build([]RunMiddleware{mw}, func(_ context.Context, _ *RunContext) error {
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

type captureTitleRequestGateway struct {
	request llm.Request
	reply   string
}

func (g *captureTitleRequestGateway) Stream(_ context.Context, req llm.Request, yield func(llm.StreamEvent) error) error {
	g.request = req
	if g.reply != "" {
		if err := yield(llm.StreamMessageDelta{ContentDelta: g.reply, Role: "assistant"}); err != nil {
			return err
		}
	}
	return yield(llm.StreamRunCompleted{})
}

func TestTitleSummarizerIncludesUserPromptAndMaterials(t *testing.T) {
	gateway := &captureTitleRequestGateway{}
	runID := uuid.New()
	threadID := uuid.New()

	generateTitle(context.Background(), titleSummarizerTestDB{}, nil, nil, gateway, runID, threadID, "test-model", []llm.Message{
		{
			Role: "user",
			Content: []llm.TextPart{
				{Text: "概括一下这个总结"},
				{
					Type: "file",
					Attachment: &messagecontent.AttachmentRef{
						Filename: "run.log",
						MimeType: "text/plain",
					},
					ExtractedText: "这是一段运行日志",
				},
				{
					Type: "image",
					Attachment: &messagecontent.AttachmentRef{
						Filename: "screen.png",
						MimeType: "image/png",
					},
				},
			},
		},
	}, "", 32)

	if len(gateway.request.Messages) != 2 {
		t.Fatalf("expected 2 request messages, got %d", len(gateway.request.Messages))
	}
	userContent := gateway.request.Messages[1].Content
	if len(userContent) != 1 {
		t.Fatalf("expected a single user content part, got %d", len(userContent))
	}
	got := userContent[0].Text
	if !strings.Contains(got, "User prompt:\n概括一下这个总结") {
		t.Fatalf("expected user prompt in request, got %q", got)
	}
	if !strings.Contains(got, "Materials:\n[附件: run.log]\n这是一段运行日志") {
		t.Fatalf("expected file material in request, got %q", got)
	}
	if !strings.Contains(got, "[图片: screen.png]") {
		t.Fatalf("expected image material in request, got %q", got)
	}
}

func TestTitleSummarizerDoesNotTruncateUserPrompt(t *testing.T) {
	gateway := &captureTitleRequestGateway{}
	longPrompt := strings.Repeat("长", 700)

	generateTitle(context.Background(), titleSummarizerTestDB{}, nil, nil, gateway, uuid.New(), uuid.New(), "test-model", []llm.Message{
		{
			Role:    "user",
			Content: []llm.TextPart{{Text: longPrompt}},
		},
	}, "", 32)

	got := gateway.request.Messages[1].Content[0].Text
	if !strings.Contains(got, "User prompt:\n"+longPrompt) {
		t.Fatal("expected full user prompt without truncation")
	}
}

func TestTitleSummarizerTruncatesMaterialsOnly(t *testing.T) {
	gateway := &captureTitleRequestGateway{}
	longMaterial := strings.Repeat("料", 2000)

	generateTitle(context.Background(), titleSummarizerTestDB{}, nil, nil, gateway, uuid.New(), uuid.New(), "test-model", []llm.Message{
		{
			Role: "user",
			Content: []llm.TextPart{
				{Text: "看这个附件"},
				{
					Type: "file",
					Attachment: &messagecontent.AttachmentRef{
						Filename: "huge.txt",
						MimeType: "text/plain",
					},
					ExtractedText: longMaterial,
				},
			},
		},
	}, "", 32)

	got := gateway.request.Messages[1].Content[0].Text
	if !strings.Contains(got, "User prompt:\n看这个附件") {
		t.Fatalf("expected user prompt preserved, got %q", got)
	}
	if strings.Contains(got, longMaterial) {
		t.Fatal("expected materials to be truncated")
	}
	if !strings.Contains(got, "[附件: huge.txt]") {
		t.Fatalf("expected file header in materials, got %q", got)
	}
}
