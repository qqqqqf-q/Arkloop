package agent

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin"
	"github.com/google/uuid"
)

func TestAgentLoopRunsStubGateway(t *testing.T) {
	gateway := llm.NewStubGateway(llm.StubGatewayConfig{
		Enabled:         true,
		DeltaCount:      2,
		DeltaInterval:   0,
		EmitDebugEvents: false,
	})

	loop := NewLoop(gateway, nil)
	runID := uuid.New()
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(
		context.Background(),
		RunContext{
			RunID:         runID,
			TraceID:       "trace",
			InputJSON:     map[string]any{},
			MaxIterations: 3,
			CancelSignal:  func() bool { return false },
		},
		llm.Request{Model: "stub"},
		emitter,
		func(ev events.RunEvent) error {
			got = append(got, ev)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}

	deltaCount := 0
	completedCount := 0
	for _, ev := range got {
		if ev.Type == "message.delta" {
			deltaCount++
		}
		if ev.Type == "run.completed" {
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

func TestAgentLoopExecutesToolCalls(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.EchoAgentSpec); err != nil {
		t.Fatalf("register echo failed: %v", err)
	}

	allowlist := tools.AllowlistFromNames([]string{"echo"})
	policy := tools.NewPolicyEnforcer(registry, allowlist)
	executor := tools.NewDispatchingExecutor(registry, policy)
	if err := executor.Bind("echo", builtin.EchoExecutor{}); err != nil {
		t.Fatalf("bind echo failed: %v", err)
	}

	gateway := &scriptedGateway{}
	loop := NewLoop(gateway, executor)
	runID := uuid.New()
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(
		context.Background(),
		RunContext{
			RunID:         runID,
			TraceID:       "trace",
			InputJSON:     map[string]any{},
			MaxIterations: 3,
			ToolExecutor:  executor,
			ToolTimeoutMs: intPtr(1000),
			ToolBudget:    map[string]any{"foo": "bar"},
			CancelSignal:  func() bool { return false },
		},
		llm.Request{Model: "stub"},
		emitter,
		func(ev events.RunEvent) error {
			got = append(got, ev)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}

	seenToolCall := false
	seenToolResult := false
	seenCompleted := false
	for _, ev := range got {
		if ev.Type == "tool.call" {
			seenToolCall = true
		}
		if ev.Type == "tool.result" {
			seenToolResult = true
		}
		if ev.Type == "run.completed" {
			seenCompleted = true
		}
	}
	if !seenToolCall || !seenToolResult {
		t.Fatalf("expected tool.call and tool.result events")
	}
	if !seenCompleted {
		t.Fatalf("expected run.completed")
	}
}

func TestAgentLoopExecutesMultipleToolCallsInParallel(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(tools.AgentToolSpec{
		Name:        "slow_echo",
		Version:     "1",
		Description: "slow echo for parallel test",
		RiskLevel:   tools.RiskLevelLow,
		SideEffects: false,
	}); err != nil {
		t.Fatalf("register slow_echo failed: %v", err)
	}

	allowlist := tools.AllowlistFromNames([]string{"slow_echo"})
	policy := tools.NewPolicyEnforcer(registry, allowlist)
	dispatcher := tools.NewDispatchingExecutor(registry, policy)
	observer := &observedSlowExecutor{delay: 40 * time.Millisecond}
	if err := dispatcher.Bind("slow_echo", observer); err != nil {
		t.Fatalf("bind slow_echo failed: %v", err)
	}

	gateway := &multiToolCallGateway{}
	loop := NewLoop(gateway, dispatcher)
	runID := uuid.New()
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(
		context.Background(),
		RunContext{
			RunID:         runID,
			TraceID:       "trace",
			InputJSON:     map[string]any{},
			MaxIterations: 3,
			ToolExecutor:  dispatcher,
			ToolTimeoutMs: intPtr(1000),
			ToolBudget:    map[string]any{"foo": "bar"},
			CancelSignal:  func() bool { return false },
		},
		llm.Request{Model: "stub"},
		emitter,
		func(ev events.RunEvent) error {
			got = append(got, ev)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}
	if atomic.LoadInt32(&observer.maxActive) < 2 {
		t.Fatalf("expected parallel tool execution, max active = %d", atomic.LoadInt32(&observer.maxActive))
	}

	toolResults := 0
	for _, ev := range got {
		if ev.Type == "tool.result" {
			toolResults++
		}
	}
	if toolResults < 2 {
		t.Fatalf("expected at least 2 tool.result events, got %d", toolResults)
	}
}

func TestAgentLoopAggregatesUsageAcrossTurns(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.EchoAgentSpec); err != nil {
		t.Fatalf("register echo failed: %v", err)
	}
	allowlist := tools.AllowlistFromNames([]string{"echo"})
	policy := tools.NewPolicyEnforcer(registry, allowlist)
	executor := tools.NewDispatchingExecutor(registry, policy)
	if err := executor.Bind("echo", builtin.EchoExecutor{}); err != nil {
		t.Fatalf("bind echo failed: %v", err)
	}

	gateway := &usageScriptedGateway{}
	loop := NewLoop(gateway, executor)
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(
		context.Background(),
		RunContext{
			RunID:         uuid.New(),
			TraceID:       "trace",
			InputJSON:     map[string]any{},
			MaxIterations: 3,
			ToolExecutor:  executor,
			CancelSignal:  func() bool { return false },
		},
		llm.Request{Model: "stub"},
		emitter,
		func(ev events.RunEvent) error {
			got = append(got, ev)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}

	var completed events.RunEvent
	found := false
	for _, ev := range got {
		if ev.Type == "run.completed" {
			completed = ev
			found = true
		}
	}
	if !found {
		t.Fatalf("expected run.completed")
	}
	usage, ok := completed.DataJSON["usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected usage payload in run.completed")
	}
	if value := mustInt64(t, usage["input_tokens"]); value != 30 {
		t.Fatalf("expected input_tokens=30, got %d", value)
	}
	if value := mustInt64(t, usage["output_tokens"]); value != 8 {
		t.Fatalf("expected output_tokens=8, got %d", value)
	}
	if value := mustInt64(t, usage["cached_tokens"]); value != 10 {
		t.Fatalf("expected cached_tokens=10, got %d", value)
	}
}

func TestAgentLoopSearchToolTurnDoesNotInjectAssistantText(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.EchoAgentSpec); err != nil {
		t.Fatalf("register echo failed: %v", err)
	}

	allowlist := tools.AllowlistFromNames([]string{"echo"})
	policy := tools.NewPolicyEnforcer(registry, allowlist)
	executor := tools.NewDispatchingExecutor(registry, policy)
	if err := executor.Bind("echo", builtin.EchoExecutor{}); err != nil {
		t.Fatalf("bind echo failed: %v", err)
	}

	gateway := &captureRequestsGateway{}
	loop := NewLoop(gateway, executor)
	emitter := events.NewEmitter("trace")

	err := loop.Run(
		context.Background(),
		RunContext{
			RunID:         uuid.New(),
			TraceID:       "trace",
			AgentID:       "search",
			InputJSON:     map[string]any{},
			MaxIterations: 3,
			ToolExecutor:  executor,
			CancelSignal:  func() bool { return false },
		},
		llm.Request{Model: "stub"},
		emitter,
		func(ev events.RunEvent) error { return nil },
	)
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}

	if len(gateway.requests) < 2 {
		t.Fatalf("expected at least 2 llm requests, got %d", len(gateway.requests))
	}
	second := gateway.requests[1]

	var toolTurn *llm.Message
	for i := len(second.Messages) - 1; i >= 0; i-- {
		msg := second.Messages[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			toolTurn = &msg
			break
		}
	}
	if toolTurn == nil {
		t.Fatalf("expected assistant tool-call message in second request")
	}
	if len(toolTurn.Content) != 0 {
		t.Fatalf("expected assistant content to be empty for search tool turns, got %#v", toolTurn.Content)
	}
}

func TestAgentLoopDedupToolResultMessageInjection(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.EchoAgentSpec); err != nil {
		t.Fatalf("register echo failed: %v", err)
	}

	allowlist := tools.AllowlistFromNames([]string{"echo"})
	policy := tools.NewPolicyEnforcer(registry, allowlist)
	executor := tools.NewDispatchingExecutor(registry, policy)
	if err := executor.Bind("echo", builtin.EchoExecutor{}); err != nil {
		t.Fatalf("bind echo failed: %v", err)
	}

	gateway := &dupToolCallCaptureGateway{
		text: strings.Repeat("x", 2000),
	}
	loop := NewLoop(gateway, executor)
	emitter := events.NewEmitter("trace")

	err := loop.Run(
		context.Background(),
		RunContext{
			RunID:         uuid.New(),
			TraceID:       "trace",
			InputJSON:     map[string]any{},
			MaxIterations: 4,
			ToolExecutor:  executor,
			CancelSignal:  func() bool { return false },
		},
		llm.Request{Model: "stub"},
		emitter,
		func(ev events.RunEvent) error { return nil },
	)
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}

	if len(gateway.requests) < 3 {
		t.Fatalf("expected at least 3 llm requests, got %d", len(gateway.requests))
	}
	third := gateway.requests[2]

	toolMsgs := []llm.Message{}
	for _, msg := range third.Messages {
		if msg.Role == "tool" {
			toolMsgs = append(toolMsgs, msg)
		}
	}
	if len(toolMsgs) != 2 {
		t.Fatalf("expected 2 tool messages in third request, got %d", len(toolMsgs))
	}

	first := toolMsgs[0].Content[0].Text
	second := toolMsgs[1].Content[0].Text
	if len(second) >= len(first) {
		t.Fatalf("expected dedup tool message to be shorter, got %d >= %d", len(second), len(first))
	}
	if !strings.Contains(second, "same_args_as_previous") {
		t.Fatalf("expected dedup marker in tool message, got %q", second)
	}
}

type scriptedGateway struct {
	calls int
}

func (g *scriptedGateway) Stream(ctx context.Context, request llm.Request, yield func(llm.StreamEvent) error) error {
	_ = ctx
	_ = request
	g.calls++
	if g.calls == 1 {
		if err := yield(llm.ToolCall{
			ToolCallID:    "call_1",
			ToolName:      "echo",
			ArgumentsJSON: map[string]any{"text": "hi"},
		}); err != nil {
			return err
		}
		return yield(llm.StreamRunCompleted{})
	}

	if err := sleep(ctx, 1*time.Millisecond); err != nil {
		return err
	}
	if err := yield(llm.StreamMessageDelta{ContentDelta: "done", Role: "assistant"}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{})
}

type multiToolCallGateway struct {
	calls int
}

func (g *multiToolCallGateway) Stream(ctx context.Context, request llm.Request, yield func(llm.StreamEvent) error) error {
	_ = ctx
	_ = request
	g.calls++
	if g.calls == 1 {
		if err := yield(llm.ToolCall{
			ToolCallID:    "call_1",
			ToolName:      "slow_echo",
			ArgumentsJSON: map[string]any{"text": "a"},
		}); err != nil {
			return err
		}
		if err := yield(llm.ToolCall{
			ToolCallID:    "call_2",
			ToolName:      "slow_echo",
			ArgumentsJSON: map[string]any{"text": "b"},
		}); err != nil {
			return err
		}
		return yield(llm.StreamRunCompleted{})
	}

	if err := yield(llm.StreamMessageDelta{ContentDelta: "done", Role: "assistant"}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{})
}

type usageScriptedGateway struct {
	calls int
}

func (g *usageScriptedGateway) Stream(ctx context.Context, request llm.Request, yield func(llm.StreamEvent) error) error {
	_ = ctx
	_ = request
	g.calls++
	if g.calls == 1 {
		if err := yield(llm.ToolCall{
			ToolCallID:    "call_1",
			ToolName:      "echo",
			ArgumentsJSON: map[string]any{"text": "hi"},
		}); err != nil {
			return err
		}
		return yield(llm.StreamRunCompleted{
			Usage: &llm.Usage{
				InputTokens:  intPtr(10),
				OutputTokens: intPtr(3),
				CachedTokens: intPtr(4),
			},
		})
	}
	if err := yield(llm.StreamMessageDelta{ContentDelta: "done", Role: "assistant"}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{
		Usage: &llm.Usage{
			InputTokens:  intPtr(20),
			OutputTokens: intPtr(5),
			CachedTokens: intPtr(6),
		},
	})
}

type observedSlowExecutor struct {
	delay     time.Duration
	active    int32
	maxActive int32
}

func (e *observedSlowExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	_ = ctx
	_ = toolName
	_ = args
	_ = execCtx
	_ = toolCallID

	current := atomic.AddInt32(&e.active, 1)
	for {
		peak := atomic.LoadInt32(&e.maxActive)
		if current <= peak {
			break
		}
		if atomic.CompareAndSwapInt32(&e.maxActive, peak, current) {
			break
		}
	}
	time.Sleep(e.delay)
	atomic.AddInt32(&e.active, -1)

	return tools.ExecutionResult{ResultJSON: map[string]any{"ok": true}}
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func intPtr(value int) *int {
	return &value
}

func mustInt64(t *testing.T, value any) int64 {
	t.Helper()
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	default:
		t.Fatalf("unexpected numeric type %T", value)
		return 0
	}
}

type captureRequestsGateway struct {
	requests []llm.Request
	calls    int
}

func (g *captureRequestsGateway) Stream(ctx context.Context, request llm.Request, yield func(llm.StreamEvent) error) error {
	_ = ctx
	g.requests = append(g.requests, request)
	g.calls++
	if g.calls == 1 {
		if err := yield(llm.StreamMessageDelta{ContentDelta: `{"tool":"echo","args":{"text":"hi"}}`, Role: "assistant"}); err != nil {
			return err
		}
		if err := yield(llm.ToolCall{
			ToolCallID:    "call_1",
			ToolName:      "echo",
			ArgumentsJSON: map[string]any{"text": "hi"},
		}); err != nil {
			return err
		}
		return yield(llm.StreamRunCompleted{})
	}
	if err := yield(llm.StreamMessageDelta{ContentDelta: "done", Role: "assistant"}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{})
}

type dupToolCallCaptureGateway struct {
	requests []llm.Request
	calls    int
	text     string
}

func (g *dupToolCallCaptureGateway) Stream(ctx context.Context, request llm.Request, yield func(llm.StreamEvent) error) error {
	_ = ctx
	g.requests = append(g.requests, request)
	g.calls++

	if g.calls == 1 {
		if err := yield(llm.ToolCall{
			ToolCallID:    "call_1",
			ToolName:      "echo",
			ArgumentsJSON: map[string]any{"text": g.text},
		}); err != nil {
			return err
		}
		return yield(llm.StreamRunCompleted{})
	}
	if g.calls == 2 {
		if err := yield(llm.ToolCall{
			ToolCallID:    "call_2",
			ToolName:      "echo",
			ArgumentsJSON: map[string]any{"text": g.text},
		}); err != nil {
			return err
		}
		return yield(llm.StreamRunCompleted{})
	}

	if err := yield(llm.StreamMessageDelta{ContentDelta: "done", Role: "assistant"}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{})
}
