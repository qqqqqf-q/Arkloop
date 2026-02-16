package agent

import (
	"context"
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
