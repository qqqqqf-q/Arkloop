package executor

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
)

func buildMinimalRC(gateway llm.Gateway, systemPrompt string, agentConfig *pipeline.ResolvedAgentConfig) *pipeline.RunContext {
	return &pipeline.RunContext{
		Run: data.Run{
			ID:       uuid.New(),
			AccountID:    uuid.New(),
			ThreadID: uuid.New(),
		},
		TraceID:      "test-trace",
		Gateway:      gateway,
		Messages:     []llm.Message{},
		SystemPrompt: systemPrompt,
		AgentConfig:  agentConfig,
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{
				ID:    "default",
				Model: "stub",
			},
		},
		ReasoningIterations:    5,
		ToolContinuationBudget: 32,
		InputJSON:              map[string]any{},
		ToolBudget:             map[string]any{},
		PerToolSoftLimits:      tools.DefaultPerToolSoftLimits(),
		FinalSpecs:             []llm.ToolSpec{},
	}
}

func TestSimpleExecutor_EmitsExpectedEvents(t *testing.T) {
	gateway := llm.NewStubGateway(llm.StubGatewayConfig{
		Enabled:    true,
		DeltaCount: 2,
	})

	ex := &SimpleExecutor{}
	emitter := events.NewEmitter("trace")
	rc := buildMinimalRC(gateway, "", nil)

	var got []events.RunEvent
	err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	deltaCount, completedCount := 0, 0
	for _, ev := range got {
		switch ev.Type {
		case "message.delta":
			deltaCount++
		case "run.completed":
			completedCount++
		}
	}
	if deltaCount != 2 {
		t.Fatalf("expected 2 message.delta, got %d", deltaCount)
	}
	if completedCount != 1 {
		t.Fatalf("expected 1 run.completed, got %d", completedCount)
	}
}

func TestSimpleExecutor_SystemPromptInjected(t *testing.T) {
	var capturedMessages []llm.Message
	gateway := &captureRequestGateway{
		onCapture: func(req llm.Request) {
			capturedMessages = append(capturedMessages, req.Messages...)
		},
	}

	ex := &SimpleExecutor{}
	emitter := events.NewEmitter("trace")
	rc := buildMinimalRC(gateway, "you are a helpful assistant", nil)

	_ = ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error { return nil })

	if len(capturedMessages) == 0 {
		t.Fatal("no messages captured")
	}
	first := capturedMessages[0]
	if first.Role != "system" {
		t.Fatalf("expected first message role=system, got %q", first.Role)
	}
	if len(first.Content) == 0 || first.Content[0].Text != "you are a helpful assistant" {
		t.Fatalf("unexpected system content: %+v", first.Content)
	}
	if first.Content[0].CacheControl != nil {
		t.Fatalf("cache_control should be nil when PromptCacheControl is not set")
	}
}

func TestSimpleExecutor_SystemPromptCacheControl(t *testing.T) {
	var capturedMessages []llm.Message
	gateway := &captureRequestGateway{
		onCapture: func(req llm.Request) {
			capturedMessages = append(capturedMessages, req.Messages...)
		},
	}

	ex := &SimpleExecutor{}
	emitter := events.NewEmitter("trace")
	agentConfig := &pipeline.ResolvedAgentConfig{PromptCacheControl: "system_prompt"}
	rc := buildMinimalRC(gateway, "cached prompt", agentConfig)

	_ = ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error { return nil })

	if len(capturedMessages) == 0 || capturedMessages[0].Role != "system" {
		t.Fatal("expected system message")
	}
	cc := capturedMessages[0].Content[0].CacheControl
	if cc == nil || *cc != "ephemeral" {
		t.Fatalf("expected cache_control=ephemeral, got %v", cc)
	}
}

func TestSimpleExecutor_NoSystemPromptWhenEmpty(t *testing.T) {
	var capturedMessages []llm.Message
	gateway := &captureRequestGateway{
		onCapture: func(req llm.Request) {
			capturedMessages = append(capturedMessages, req.Messages...)
		},
	}

	ex := &SimpleExecutor{}
	emitter := events.NewEmitter("trace")
	userMsg := llm.Message{Role: "user", Content: []llm.TextPart{{Text: "hello"}}}
	rc := buildMinimalRC(gateway, "   ", nil)
	rc.Messages = []llm.Message{userMsg}

	_ = ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error { return nil })

	if len(capturedMessages) == 0 {
		t.Fatal("expected at least one message")
	}
	if capturedMessages[0].Role == "system" {
		t.Fatal("should not inject system message when SystemPrompt is blank")
	}
}

func TestNewSimpleExecutor_Factory(t *testing.T) {
	ex, err := NewSimpleExecutor(nil)
	if err != nil {
		t.Fatalf("factory with nil config failed: %v", err)
	}
	if ex == nil {
		t.Fatal("factory returned nil")
	}

	ex2, err := NewSimpleExecutor(map[string]any{"ignored": true})
	if err != nil {
		t.Fatalf("factory with config failed: %v", err)
	}
	if ex2 == nil {
		t.Fatal("factory returned nil")
	}
}

// captureRequestGateway 记录首次 Stream 调用的 Request，随后返回 run.completed。
type captureRequestGateway struct {
	onCapture func(req llm.Request)
}

func (g *captureRequestGateway) Stream(_ context.Context, req llm.Request, yield func(llm.StreamEvent) error) error {
	if g.onCapture != nil {
		g.onCapture(req)
	}
	return yield(llm.StreamRunCompleted{})
}
