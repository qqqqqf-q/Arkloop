package executor

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/tools"
)

// multiTurnGateway 第一次 Stream 返回一个工具调用，第二次返回 run.completed。
// 可选地，在第二次调用时检查 messages 内容（用于验证注入）。
type multiTurnGateway struct {
	callCount    int
	onSecondCall func(req llm.Request)
}

func (g *multiTurnGateway) Stream(_ context.Context, req llm.Request, yield func(llm.StreamEvent) error) error {
	g.callCount++
	if g.callCount == 1 {
		// 第一次：返回工具调用 + completed
		if err := yield(llm.ToolCall{
			ToolCallID:    "call-1",
			ToolName:      "test_tool",
			ArgumentsJSON: map[string]any{"key": "val"},
		}); err != nil {
			return err
		}
		return yield(llm.StreamRunCompleted{})
	}
	// 第二次及后续：检查 messages 后返回 completed
	if g.onSecondCall != nil {
		g.onSecondCall(req)
	}
	return yield(llm.StreamRunCompleted{})
}

type captureGateway struct {
	requests []llm.Request
	events   [][]llm.StreamEvent
}

func (g *captureGateway) Stream(_ context.Context, req llm.Request, yield func(llm.StreamEvent) error) error {
	g.requests = append(g.requests, req)
	index := len(g.requests) - 1
	if index >= len(g.events) {
		index = len(g.events) - 1
	}
	for _, event := range g.events[index] {
		if err := yield(event); err != nil {
			return err
		}
	}
	return nil
}

func buildMinimalToolExecutor() *tools.DispatchingExecutor {
	reg := tools.NewRegistry()
	enforcer := tools.NewPolicyEnforcer(reg, tools.AllowlistFromNames(nil))
	return tools.NewDispatchingExecutor(reg, enforcer)
}

func TestNewInteractiveExecutor_DefaultConfig(t *testing.T) {
	ex, err := NewInteractiveExecutor(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ie := ex.(*InteractiveExecutor)
	if ie.checkInEvery != defaultCheckInEvery {
		t.Errorf("checkInEvery want %d, got %d", defaultCheckInEvery, ie.checkInEvery)
	}
	if ie.maxWaitSeconds != defaultMaxWaitSeconds {
		t.Errorf("maxWaitSeconds want %d, got %d", defaultMaxWaitSeconds, ie.maxWaitSeconds)
	}
}

func TestNewInteractiveExecutor_CustomConfig(t *testing.T) {
	ex, err := NewInteractiveExecutor(map[string]any{
		"check_in_every":   float64(3),
		"max_wait_seconds": float64(60),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ie := ex.(*InteractiveExecutor)
	if ie.checkInEvery != 3 {
		t.Errorf("checkInEvery want 3, got %d", ie.checkInEvery)
	}
	if ie.maxWaitSeconds != 60 {
		t.Errorf("maxWaitSeconds want 60, got %d", ie.maxWaitSeconds)
	}
}

func TestNewInteractiveExecutor_InvalidConfig(t *testing.T) {
	cases := []map[string]any{
		{"check_in_every": float64(0)},
		{"check_in_every": float64(-1)},
		{"max_wait_seconds": "bad"},
	}
	for _, cfg := range cases {
		if _, err := NewInteractiveExecutor(cfg); err == nil {
			t.Errorf("expected error for config %v", cfg)
		}
	}
}

// TestInteractiveExecutor_CheckIn_InjectsMessage 验证：
// iter=1 满足 check_in_every=1，触发 run.input_requested，注入消息后第二次 LLM call 能看到该消息。
func TestInteractiveExecutor_CheckIn_InjectsMessage(t *testing.T) {
	var secondCallMessages []llm.Message
	gw := &multiTurnGateway{
		onSecondCall: func(req llm.Request) {
			secondCallMessages = req.Messages
		},
	}

	ex := &InteractiveExecutor{checkInEvery: 1, maxWaitSeconds: 5}
	emitter := events.NewEmitter("trace")

	rc := buildMinimalRC(gw, "", nil, nil)
	rc.ToolExecutor = buildMinimalToolExecutor()
	rc.WaitForInput = func(_ context.Context) (string, bool) {
		return "user check-in message", true
	}

	var emittedTypes []string
	err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		emittedTypes = append(emittedTypes, ev.Type)
		return nil
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// 应有 run.input_requested 事件
	found := false
	for _, typ := range emittedTypes {
		if typ == pipeline.EventTypeInputRequested {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %s event, got: %v", pipeline.EventTypeInputRequested, emittedTypes)
	}

	// 第二次 LLM call 的 messages 应包含注入的用户消息
	injectedFound := false
	for _, msg := range secondCallMessages {
		if msg.Role == "user" {
			for _, part := range msg.Content {
				if part.Text == "user check-in message" {
					injectedFound = true
				}
			}
		}
	}
	if !injectedFound {
		t.Errorf("injected user message not found in second LLM call messages")
	}
}

func TestInteractiveExecutor_SteeringInputScannedBeforeInjection(t *testing.T) {
	gw := &captureGateway{
		events: [][]llm.StreamEvent{{llm.StreamRunCompleted{}}},
	}
	ex := &InteractiveExecutor{checkInEvery: 5, maxWaitSeconds: 5}
	emitter := events.NewEmitter("trace")

	rc := buildMinimalRC(gw, "", nil, nil)
	rc.ToolExecutor = buildMinimalToolExecutor()
	var polls int
	var phases []string
	rc.PollSteeringInput = func(_ context.Context) (string, bool) {
		if polls > 0 {
			return "", false
		}
		polls++
		return "runtime steering", true
	}
	rc.UserPromptScanFunc = func(_ context.Context, text string, phase string) error {
		if text != "runtime steering" {
			t.Fatalf("unexpected scan text: %q", text)
		}
		phases = append(phases, phase)
		return nil
	}

	if err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error { return nil }); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(phases) != 1 || phases[0] != "steering_input" {
		t.Fatalf("unexpected scan phases: %v", phases)
	}
	if len(gw.requests) != 1 {
		t.Fatalf("expected one gateway request, got %d", len(gw.requests))
	}

	injectedFound := false
	for _, msg := range gw.requests[0].Messages {
		if msg.Role != "user" {
			continue
		}
		for _, part := range msg.Content {
			if part.Text == "runtime steering" {
				injectedFound = true
			}
		}
	}
	if !injectedFound {
		t.Fatalf("runtime steering message not found in first LLM request: %#v", gw.requests[0].Messages)
	}
}

// TestInteractiveExecutor_NoCheckInBeforeThreshold 验证 check_in_every=5 时 iter=1 不触发。
func TestInteractiveExecutor_NoCheckInBeforeThreshold(t *testing.T) {
	gw := &multiTurnGateway{}
	ex := &InteractiveExecutor{checkInEvery: 5, maxWaitSeconds: 5}
	emitter := events.NewEmitter("trace")

	rc := buildMinimalRC(gw, "", nil, nil)
	rc.ToolExecutor = buildMinimalToolExecutor()
	rc.WaitForInput = func(_ context.Context) (string, bool) {
		t.Fatal("WaitForInput should not be called when iter=1 and check_in_every=5")
		return "", false
	}

	var emittedTypes []string
	err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		emittedTypes = append(emittedTypes, ev.Type)
		return nil
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	for _, typ := range emittedTypes {
		if typ == pipeline.EventTypeInputRequested {
			t.Errorf("unexpected %s event when iter=1 and check_in_every=5", pipeline.EventTypeInputRequested)
		}
	}
}

// TestInteractiveExecutor_SkipsWhenNoWaitForInput 验证 WaitForInput 为 nil 时不发 run.input_requested。
func TestInteractiveExecutor_SkipsWhenNoWaitForInput(t *testing.T) {
	gw := &multiTurnGateway{}
	ex := &InteractiveExecutor{checkInEvery: 1, maxWaitSeconds: 5}
	emitter := events.NewEmitter("trace")

	rc := buildMinimalRC(gw, "", nil, nil)
	rc.ToolExecutor = buildMinimalToolExecutor()
	// WaitForInput 保持 nil（默认值）

	var emittedTypes []string
	err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		emittedTypes = append(emittedTypes, ev.Type)
		return nil
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	for _, typ := range emittedTypes {
		if typ == pipeline.EventTypeInputRequested {
			t.Errorf("unexpected %s event when WaitForInput is nil", pipeline.EventTypeInputRequested)
		}
	}
}

func TestInteractiveExecutor_SteeringInjectedAfterTool(t *testing.T) {
	var secondCallMessages []llm.Message
	gw := &multiTurnGateway{
		onSecondCall: func(req llm.Request) {
			secondCallMessages = req.Messages
		},
	}

	ex := &InteractiveExecutor{checkInEvery: 99, maxWaitSeconds: 5}
	emitter := events.NewEmitter("trace")

	rc := buildMinimalRC(gw, "", nil, nil)
	rc.ToolExecutor = buildMinimalToolExecutor()

	pollCount := 0
	rc.PollSteeringInput = func(_ context.Context) (string, bool) {
		pollCount++
		if pollCount == 2 {
			return "runtime steering", true
		}
		return "", false
	}

	var sawSteering bool
	err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		if ev.Type == "run.steering_injected" {
			sawSteering = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !sawSteering {
		t.Fatal("expected run.steering_injected event")
	}

	injectedFound := false
	for _, msg := range secondCallMessages {
		if msg.Role != "user" {
			continue
		}
		for _, part := range msg.Content {
			if part.Text == "runtime steering" {
				injectedFound = true
			}
		}
	}
	if !injectedFound {
		t.Fatalf("steering message not found in second LLM call: %#v", secondCallMessages)
	}
}
