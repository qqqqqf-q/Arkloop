package executor

import (
	"context"
	"testing"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
)

func buildMinimalRC(gateway llm.Gateway, systemPrompt string, agentConfig *pipeline.ResolvedAgentConfig, advance map[string]any) *pipeline.RunContext {
	return &pipeline.RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
			ThreadID:  uuid.New(),
		},
		TraceID:      "test-trace",
		Gateway:      gateway,
		Messages:     []llm.Message{},
		SystemPrompt: systemPrompt,
		AgentConfig:  agentConfig,
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{
				ID:           "default",
				Model:        "stub",
				AdvancedJSON: advance,
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
	gateway := llm.NewAuxGateway(llm.AuxGatewayConfig{
		Enabled:    true,
		DeltaCount: 2,
	})

	ex := &SimpleExecutor{}
	emitter := events.NewEmitter("trace")
	rc := buildMinimalRC(gateway, "", nil, nil)

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
	rc := buildMinimalRC(gateway, "you are a helpful assistant", nil, nil)

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
	rc := buildMinimalRC(gateway, "cached prompt", agentConfig, nil)

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
	rc := buildMinimalRC(gateway, "   ", nil, nil)
	rc.Messages = []llm.Message{userMsg}

	_ = ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error { return nil })

	if len(capturedMessages) == 0 {
		t.Fatal("expected at least one message")
	}
	if capturedMessages[0].Role == "system" {
		t.Fatal("should not inject system message when SystemPrompt is blank")
	}
}

func TestSimpleExecutor_HeartbeatWithCompactSnapshotStillSendsSingleSystemMessage(t *testing.T) {
	var capturedMessages []llm.Message
	gateway := &captureRequestGateway{
		onCapture: func(req llm.Request) {
			capturedMessages = append(capturedMessages, req.Messages...)
		},
	}

	ex := &SimpleExecutor{}
	emitter := events.NewEmitter("trace")
	rc := buildMinimalRC(gateway, "persona prompt", nil, nil)
	rc.InputJSON = map[string]any{"run_kind": "heartbeat", "heartbeat_interval_minutes": 30}
	rc.Messages = []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "[Context summary for continuation]\n<state_snapshot>\nexisting summary\n</state_snapshot>"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "latest real user input"}}},
	}
	rc.ThreadMessageIDs = []uuid.UUID{uuid.Nil, uuid.New()}

	mw := pipeline.NewHeartbeatPrepareMiddleware()
	err := mw(context.Background(), rc, func(ctx context.Context, nextRC *pipeline.RunContext) error {
		return ex.Execute(ctx, nextRC, emitter, func(ev events.RunEvent) error { return nil })
	})
	if err != nil {
		t.Fatalf("heartbeat middleware + execute failed: %v", err)
	}

	systemCount := 0
	for _, msg := range capturedMessages {
		if msg.Role == "system" {
			systemCount++
		}
	}
	if systemCount != 1 {
		t.Fatalf("expected exactly 1 system message, got %d: %#v", systemCount, capturedMessages)
	}
	if len(capturedMessages) < 4 {
		t.Fatalf("expected system + snapshot + latest user + heartbeat payload, got %#v", capturedMessages)
	}
	if capturedMessages[1].Role != "user" || capturedMessages[1].Content[0].Text != "[Context summary for continuation]\n<state_snapshot>\nexisting summary\n</state_snapshot>" {
		t.Fatalf("unexpected compact snapshot message: %#v", capturedMessages[1])
	}
	if capturedMessages[2].Role != "user" || capturedMessages[2].Content[0].Text != "latest real user input" {
		t.Fatalf("unexpected latest user message: %#v", capturedMessages[2])
	}
	if capturedMessages[3].Role != "user" || capturedMessages[3].Content[0].Text == "" {
		t.Fatalf("expected synthetic heartbeat user message, got %#v", capturedMessages[3])
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

func TestSimpleExecutor_ImageFilter(t *testing.T) {
	gateway := &captureRequestGateway{
		onCapture: func(req llm.Request) {
			if len(req.Messages) != 1 {
				t.Fatalf("expected 1 message")
			}
			if req.Messages[0].Content[0].Kind() != messagecontent.PartTypeText {
				t.Fatalf("expected image part downgraded to text")
			}
		},
	}

	advance := map[string]any{
		"available_catalog": map[string]any{
			"input_modalities": []string{"text"},
		},
	}
	rc := buildMinimalRC(gateway, "", nil, advance)
	rc.Messages = []llm.Message{
		{
			Role: "user",
			Content: []llm.ContentPart{
				{Type: messagecontent.PartTypeImage, Attachment: &messagecontent.AttachmentRef{Filename: "photo.jpg"}},
			},
		},
	}

	ex := &SimpleExecutor{}
	_ = ex.Execute(context.Background(), rc, events.NewEmitter("trace"), func(ev events.RunEvent) error { return nil })
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
