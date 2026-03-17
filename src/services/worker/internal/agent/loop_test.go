package agent

import (
	"context"
	"fmt"
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
			RunID:               runID,
			TraceID:             "trace",
			InputJSON:           map[string]any{},
			ReasoningIterations: 3,
			CancelSignal:        func() bool { return false },
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
			RunID:               runID,
			TraceID:             "trace",
			InputJSON:           map[string]any{},
			ReasoningIterations: 3,
			ToolExecutor:        executor,
			ToolTimeoutMs:       intPtr(1000),
			ToolBudget:          map[string]any{"foo": "bar"},
			CancelSignal:        func() bool { return false },
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
			RunID:               runID,
			TraceID:             "trace",
			InputJSON:           map[string]any{},
			ReasoningIterations: 3,
			ToolExecutor:        dispatcher,
			ToolTimeoutMs:       intPtr(1000),
			ToolBudget:          map[string]any{"foo": "bar"},
			CancelSignal:        func() bool { return false },
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

func TestAgentLoopEmitsToolCallBeforeExecutorReturns(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.EchoAgentSpec); err != nil {
		t.Fatalf("register echo failed: %v", err)
	}

	allowlist := tools.AllowlistFromNames([]string{"echo"})
	dispatcher := tools.NewDispatchingExecutor(registry, tools.NewPolicyEnforcer(registry, allowlist))
	blocking := &blockingToolExecutor{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	if err := dispatcher.Bind("echo", blocking); err != nil {
		t.Fatalf("bind echo failed: %v", err)
	}

	loop := NewLoop(&scriptedGateway{}, dispatcher)
	emitter := events.NewEmitter("trace")

	eventCh := make(chan events.RunEvent, 16)
	errCh := make(chan error, 1)
	go func() {
		errCh <- loop.Run(
			context.Background(),
			RunContext{
				RunID:               uuid.New(),
				TraceID:             "trace",
				InputJSON:           map[string]any{},
				ReasoningIterations: 3,
				ToolExecutor:        dispatcher,
				ToolTimeoutMs:       intPtr(1000),
				ToolBudget:          map[string]any{},
				CancelSignal:        func() bool { return false },
			},
			llm.Request{Model: "stub"},
			emitter,
			func(ev events.RunEvent) error {
				eventCh <- ev
				return nil
			},
		)
		close(eventCh)
	}()

	select {
	case <-blocking.started:
	case <-time.After(2 * time.Second):
		t.Fatal("tool executor did not start")
	}

	var seen []string
loopScan:
	for {
		select {
		case ev, ok := <-eventCh:
			if !ok {
				t.Fatalf("event stream closed before tool.call, seen=%v", seen)
			}
			seen = append(seen, ev.Type)
			if ev.Type == "tool.call" {
				break loopScan
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("expected tool.call before executor finished, seen=%v", seen)
		}
	}

	close(blocking.release)
	if err := <-errCh; err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}

	toolCallCount := 0
	toolResultCount := 0
	for _, eventType := range seen {
		if eventType == "tool.call" {
			toolCallCount++
		}
		if eventType == "tool.result" {
			toolResultCount++
		}
	}
	for ev := range eventCh {
		if ev.Type == "tool.call" {
			toolCallCount++
		}
		if ev.Type == "tool.result" {
			toolResultCount++
		}
	}
	if toolCallCount != 1 {
		t.Fatalf("expected exactly 1 tool.call, got %d", toolCallCount)
	}
	if toolResultCount != 1 {
		t.Fatalf("expected exactly 1 tool.result, got %d", toolResultCount)
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
			RunID:               uuid.New(),
			TraceID:             "trace",
			InputJSON:           map[string]any{},
			ReasoningIterations: 3,
			ToolExecutor:        executor,
			CancelSignal:        func() bool { return false },
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

func TestAgentLoopAggregatesUsageIntoRunFailed(t *testing.T) {
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

	gateway := &scriptedTurnsGateway{turns: [][]llm.StreamEvent{
		{
			llm.ToolCall{ToolCallID: "call_1", ToolName: "echo", ArgumentsJSON: map[string]any{"text": "hi"}},
			llm.StreamRunCompleted{
				Usage: &llm.Usage{InputTokens: intPtr(10), OutputTokens: intPtr(3), CachedTokens: intPtr(4)},
				Cost:  &llm.Cost{Currency: "USD", AmountMicros: 1200},
			},
		},
		{
			llm.StreamRunFailed{
				Error: llm.GatewayError{ErrorClass: llm.ErrorClassProviderNonRetryable, Message: "upstream failed"},
				Usage: &llm.Usage{InputTokens: intPtr(20), OutputTokens: intPtr(5), CachedTokens: intPtr(6)},
				Cost:  &llm.Cost{Currency: "USD", AmountMicros: 3400},
			},
		},
	}}

	loop := NewLoop(gateway, executor)
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(
		context.Background(),
		RunContext{
			RunID:               uuid.New(),
			TraceID:             "trace",
			InputJSON:           map[string]any{},
			ReasoningIterations: 3,
			ToolExecutor:        executor,
			CancelSignal:        func() bool { return false },
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

	var failed events.RunEvent
	found := false
	for _, ev := range got {
		if ev.Type == "run.failed" {
			failed = ev
			found = true
		}
	}
	if !found {
		t.Fatalf("expected run.failed")
	}
	usage, ok := failed.DataJSON["usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected usage payload in run.failed")
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
	cost, ok := failed.DataJSON["cost"].(map[string]any)
	if !ok {
		t.Fatalf("expected cost payload in run.failed")
	}
	if value := mustInt64(t, cost["amount_micros"]); value != 4600 {
		t.Fatalf("expected amount_micros=4600, got %d", value)
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
			RunID:               uuid.New(),
			TraceID:             "trace",
			AgentID:             "search",
			InputJSON:           map[string]any{},
			ReasoningIterations: 3,
			ToolExecutor:        executor,
			CancelSignal:        func() bool { return false },
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
			RunID:               uuid.New(),
			TraceID:             "trace",
			InputJSON:           map[string]any{},
			ReasoningIterations: 4,
			ToolExecutor:        executor,
			CancelSignal:        func() bool { return false },
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

func TestAgentLoopDoesNotDedupErrorToolResultMessageInjection(t *testing.T) {
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
		// echo tool 会对全空白参数返回 args_invalid
		text: " ",
	}
	loop := NewLoop(gateway, executor)
	emitter := events.NewEmitter("trace")

	err := loop.Run(
		context.Background(),
		RunContext{
			RunID:               uuid.New(),
			TraceID:             "trace",
			InputJSON:           map[string]any{},
			ReasoningIterations: 4,
			ToolExecutor:        executor,
			CancelSignal:        func() bool { return false },
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

	second := toolMsgs[1].Content[0].Text
	if strings.Contains(second, "same_args_as_previous") {
		t.Fatalf("expected error tool message not to be deduped, got %q", second)
	}
	if !strings.Contains(second, "tool.args_invalid") {
		t.Fatalf("expected args_invalid error to be present, got %q", second)
	}
}

func TestAgentLoopPureContinuationDoesNotConsumeReasoningBudget(t *testing.T) {
	loop := NewLoop(&scriptedTurnsGateway{turns: [][]llm.StreamEvent{
		{llm.ToolCall{ToolCallID: "call_1", ToolName: "exec_command", ArgumentsJSON: map[string]any{"command": "sleep 1"}}, llm.StreamRunCompleted{}},
		{llm.ToolCall{ToolCallID: "call_2", ToolName: "write_stdin", ArgumentsJSON: map[string]any{"session_ref": "sess-1"}}, llm.StreamRunCompleted{}},
		{llm.ToolCall{ToolCallID: "call_3", ToolName: "write_stdin", ArgumentsJSON: map[string]any{"session_ref": "sess-1"}}, llm.StreamRunCompleted{}},
		{llm.StreamMessageDelta{ContentDelta: "done", Role: "assistant"}, llm.StreamRunCompleted{}},
	}}, buildContinuationDispatcher(t, []bool{true, false}))
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(context.Background(), RunContext{
		RunID:                  uuid.New(),
		TraceID:                "trace",
		InputJSON:              map[string]any{},
		ReasoningIterations:    2,
		ToolContinuationBudget: 2,
		PerToolSoftLimits:      tools.DefaultPerToolSoftLimits(),
		ToolExecutor:           buildContinuationDispatcher(t, []bool{true, false}),
		CancelSignal:           func() bool { return false },
	}, llm.Request{Model: "stub"}, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}
	assertHasEvent(t, got, "run.completed")
	assertNoErrorClass(t, got, ErrorClassAgentReasoningIterationsExceeded)
}

func TestAgentLoopZeroReasoningIterationsMeansUnlimited(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.EchoAgentSpec); err != nil {
		t.Fatalf("register echo failed: %v", err)
	}

	allowlist := tools.AllowlistFromNames([]string{"echo"})
	policy := tools.NewPolicyEnforcer(registry, allowlist)
	dispatcher := tools.NewDispatchingExecutor(registry, policy)
	if err := dispatcher.Bind("echo", builtin.EchoExecutor{}); err != nil {
		t.Fatalf("bind echo failed: %v", err)
	}

	loop := NewLoop(&scriptedTurnsGateway{turns: [][]llm.StreamEvent{
		{llm.ToolCall{ToolCallID: "call_1", ToolName: "echo", ArgumentsJSON: map[string]any{"text": "one"}}, llm.StreamRunCompleted{}},
		{llm.ToolCall{ToolCallID: "call_2", ToolName: "echo", ArgumentsJSON: map[string]any{"text": "two"}}, llm.StreamRunCompleted{}},
		{llm.ToolCall{ToolCallID: "call_3", ToolName: "echo", ArgumentsJSON: map[string]any{"text": "three"}}, llm.StreamRunCompleted{}},
		{llm.StreamMessageDelta{ContentDelta: "done", Role: "assistant"}, llm.StreamRunCompleted{}},
	}}, dispatcher)
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(context.Background(), RunContext{
		RunID:               uuid.New(),
		TraceID:             "trace",
		InputJSON:           map[string]any{},
		ReasoningIterations: 0,
		ToolExecutor:        dispatcher,
		ToolTimeoutMs:       intPtr(1000),
		CancelSignal:        func() bool { return false },
	}, llm.Request{Model: "stub"}, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}
	assertHasEvent(t, got, "run.completed")
	assertNoErrorClass(t, got, ErrorClassAgentReasoningIterationsExceeded)
}

func TestAgentLoopContinuationBudgetExceededReturnsToolResultError(t *testing.T) {
	dispatcher := buildContinuationDispatcher(t, []bool{true})
	loop := NewLoop(&scriptedTurnsGateway{turns: [][]llm.StreamEvent{
		{llm.ToolCall{ToolCallID: "call_1", ToolName: "exec_command", ArgumentsJSON: map[string]any{"command": "sleep 1"}}, llm.StreamRunCompleted{}},
		{llm.ToolCall{ToolCallID: "call_2", ToolName: "write_stdin", ArgumentsJSON: map[string]any{"session_ref": "sess-1"}}, llm.StreamRunCompleted{}},
		{llm.ToolCall{ToolCallID: "call_3", ToolName: "write_stdin", ArgumentsJSON: map[string]any{"session_ref": "sess-1"}}, llm.StreamRunCompleted{}},
		{llm.StreamMessageDelta{ContentDelta: "done", Role: "assistant"}, llm.StreamRunCompleted{}},
	}}, dispatcher)
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(context.Background(), RunContext{
		RunID:                  uuid.New(),
		TraceID:                "trace",
		InputJSON:              map[string]any{},
		ReasoningIterations:    3,
		ToolContinuationBudget: 1,
		PerToolSoftLimits:      tools.DefaultPerToolSoftLimits(),
		ToolExecutor:           dispatcher,
		CancelSignal:           func() bool { return false },
	}, llm.Request{Model: "stub"}, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}
	assertHasToolResultError(t, got, ErrorClassToolContinuationBudgetExceeded)
	assertNoErrorClass(t, got, ErrorClassAgentReasoningIterationsExceeded)
}

func TestAgentLoopMixedTurnConsumesContinuationBudget(t *testing.T) {
	dispatcher := buildContinuationDispatcher(t, []bool{true})
	loop := NewLoop(&scriptedTurnsGateway{turns: [][]llm.StreamEvent{
		{llm.ToolCall{ToolCallID: "call_1", ToolName: "exec_command", ArgumentsJSON: map[string]any{"command": "sleep 1"}}, llm.StreamRunCompleted{}},
		{
			llm.ToolCall{ToolCallID: "call_2", ToolName: "echo", ArgumentsJSON: map[string]any{"text": "hi"}},
			llm.ToolCall{ToolCallID: "call_3", ToolName: "write_stdin", ArgumentsJSON: map[string]any{"session_ref": "sess-1"}},
			llm.StreamRunCompleted{},
		},
		{llm.ToolCall{ToolCallID: "call_4", ToolName: "write_stdin", ArgumentsJSON: map[string]any{"session_ref": "sess-1"}}, llm.StreamRunCompleted{}},
		{llm.StreamMessageDelta{ContentDelta: "done", Role: "assistant"}, llm.StreamRunCompleted{}},
	}}, dispatcher)
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(context.Background(), RunContext{
		RunID:                  uuid.New(),
		TraceID:                "trace",
		InputJSON:              map[string]any{},
		ReasoningIterations:    4,
		ToolContinuationBudget: 1,
		PerToolSoftLimits:      tools.DefaultPerToolSoftLimits(),
		ToolExecutor:           dispatcher,
		CancelSignal:           func() bool { return false },
	}, llm.Request{Model: "stub"}, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}
	assertHasToolResultError(t, got, ErrorClassToolContinuationBudgetExceeded)
}

func TestAgentLoopIterHookOnlyRunsOnReasoningTurns(t *testing.T) {
	dispatcher := buildContinuationDispatcher(t, []bool{false})
	loop := NewLoop(&scriptedTurnsGateway{turns: [][]llm.StreamEvent{
		{llm.ToolCall{ToolCallID: "call_1", ToolName: "exec_command", ArgumentsJSON: map[string]any{"command": "sleep 1"}}, llm.StreamRunCompleted{}},
		{llm.ToolCall{ToolCallID: "call_2", ToolName: "write_stdin", ArgumentsJSON: map[string]any{"session_ref": "sess-1"}}, llm.StreamRunCompleted{}},
		{llm.ToolCall{ToolCallID: "call_3", ToolName: "echo", ArgumentsJSON: map[string]any{"text": "hi"}}, llm.StreamRunCompleted{}},
		{llm.StreamMessageDelta{ContentDelta: "done", Role: "assistant"}, llm.StreamRunCompleted{}},
	}}, dispatcher)
	emitter := events.NewEmitter("trace")

	hooks := []int{}
	err := loop.Run(context.Background(), RunContext{
		RunID:                  uuid.New(),
		TraceID:                "trace",
		InputJSON:              map[string]any{},
		ReasoningIterations:    3,
		ToolContinuationBudget: 1,
		PerToolSoftLimits:      tools.DefaultPerToolSoftLimits(),
		ToolExecutor:           dispatcher,
		CancelSignal:           func() bool { return false },
		IterHook: func(_ context.Context, iter int) (string, bool, error) {
			hooks = append(hooks, iter)
			return "", false, nil
		},
	}, llm.Request{Model: "stub"}, emitter, func(ev events.RunEvent) error { return nil })
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}
	if len(hooks) != 2 || hooks[0] != 1 || hooks[1] != 2 {
		t.Fatalf("unexpected hook iterations: %v", hooks)
	}
}

func TestAgentLoopContinuationLimitExceededReturnsToolResultError(t *testing.T) {
	limits := tools.DefaultPerToolSoftLimits()
	writeLimit := limits["write_stdin"]
	writeLimit.MaxContinuations = intPtr(1)
	limits["write_stdin"] = writeLimit
	dispatcher := buildContinuationDispatcher(t, []bool{true})
	loop := NewLoop(&scriptedTurnsGateway{turns: [][]llm.StreamEvent{
		{llm.ToolCall{ToolCallID: "call_1", ToolName: "exec_command", ArgumentsJSON: map[string]any{"command": "sleep 1"}}, llm.StreamRunCompleted{}},
		{llm.ToolCall{ToolCallID: "call_2", ToolName: "write_stdin", ArgumentsJSON: map[string]any{"session_ref": "sess-1"}}, llm.StreamRunCompleted{}},
		{llm.ToolCall{ToolCallID: "call_3", ToolName: "write_stdin", ArgumentsJSON: map[string]any{"session_ref": "sess-1"}}, llm.StreamRunCompleted{}},
		{llm.StreamMessageDelta{ContentDelta: "done", Role: "assistant"}, llm.StreamRunCompleted{}},
	}}, dispatcher)
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(context.Background(), RunContext{
		RunID:                  uuid.New(),
		TraceID:                "trace",
		InputJSON:              map[string]any{},
		ReasoningIterations:    3,
		ToolContinuationBudget: 3,
		PerToolSoftLimits:      limits,
		ToolExecutor:           dispatcher,
		CancelSignal:           func() bool { return false },
	}, llm.Request{Model: "stub"}, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}
	assertHasToolResultError(t, got, ErrorClassToolContinuationLimitExceeded)
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

type blockingToolExecutor struct {
	started chan struct{}
	release chan struct{}
}

func (e *blockingToolExecutor) Execute(
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
	e.started <- struct{}{}
	<-e.release
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

type scriptedTurnsGateway struct {
	turns [][]llm.StreamEvent
	calls int
}

func (g *scriptedTurnsGateway) Stream(ctx context.Context, request llm.Request, yield func(llm.StreamEvent) error) error {
	_ = ctx
	_ = request
	if g.calls >= len(g.turns) {
		return fmt.Errorf("unexpected turn %d", g.calls)
	}
	turn := g.turns[g.calls]
	g.calls++
	for _, event := range turn {
		if err := yield(event); err != nil {
			return err
		}
	}
	return nil
}

type continuationExecutor struct {
	writeRunning []bool
	writeCalls   int32
}

func (e *continuationExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	_ = ctx
	_ = execCtx
	_ = toolCallID
	switch toolName {
	case "exec_command":
		return tools.ExecutionResult{ResultJSON: map[string]any{"session_ref": "sess-1", "running": true}}
	case "write_stdin":
		idx := int(atomic.AddInt32(&e.writeCalls, 1)) - 1
		running := false
		if idx >= 0 && idx < len(e.writeRunning) {
			running = e.writeRunning[idx]
		}
		return tools.ExecutionResult{ResultJSON: map[string]any{"session_ref": args["session_ref"], "running": running}}
	case "echo":
		return tools.ExecutionResult{ResultJSON: map[string]any{"text": args["text"]}}
	default:
		return tools.ExecutionResult{Error: &tools.ExecutionError{ErrorClass: "tool.unknown", Message: toolName}}
	}
}

func buildContinuationDispatcher(t *testing.T, writeRunning []bool) *tools.DispatchingExecutor {
	t.Helper()
	registry := tools.NewRegistry()
	for _, spec := range []tools.AgentToolSpec{
		{Name: "exec_command", Version: "1", Description: "exec", RiskLevel: tools.RiskLevelHigh, SideEffects: true},
		{Name: "write_stdin", Version: "1", Description: "stdin", RiskLevel: tools.RiskLevelHigh, SideEffects: true},
		builtin.EchoAgentSpec,
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatalf("register spec failed: %v", err)
		}
	}
	allowlist := tools.AllowlistFromNames([]string{"exec_command", "write_stdin", "echo"})
	dispatcher := tools.NewDispatchingExecutor(registry, tools.NewPolicyEnforcer(registry, allowlist))
	executor := &continuationExecutor{writeRunning: append([]bool{}, writeRunning...)}
	for _, name := range []string{"exec_command", "write_stdin", "echo"} {
		if err := dispatcher.Bind(name, executor); err != nil {
			t.Fatalf("bind %s failed: %v", name, err)
		}
	}
	return dispatcher
}

func assertHasEvent(t *testing.T, eventsIn []events.RunEvent, eventType string) {
	t.Helper()
	for _, event := range eventsIn {
		if event.Type == eventType {
			return
		}
	}
	t.Fatalf("expected event %s, got %#v", eventType, eventsIn)
}

func assertNoErrorClass(t *testing.T, eventsIn []events.RunEvent, errorClass string) {
	t.Helper()
	for _, event := range eventsIn {
		if event.ErrorClass != nil && *event.ErrorClass == errorClass {
			t.Fatalf("unexpected error class %s in event %#v", errorClass, event)
		}
	}
}

func assertHasToolResultError(t *testing.T, eventsIn []events.RunEvent, errorClass string) {
	t.Helper()
	for _, event := range eventsIn {
		if event.Type == "tool.result" && event.ErrorClass != nil && *event.ErrorClass == errorClass {
			return
		}
	}
	t.Fatalf("expected tool.result error_class=%s, got %#v", errorClass, eventsIn)
}

// --- ask_user loop interception tests ---

type askUserGateway struct {
	calls int
}

func (g *askUserGateway) Stream(_ context.Context, _ llm.Request, yield func(llm.StreamEvent) error) error {
	g.calls++
	if g.calls == 1 {
		if err := yield(llm.ToolCall{
			ToolCallID: "call_askuser",
			ToolName:   "ask_user",
			ArgumentsJSON: map[string]any{
				"message": "Pick a database",
				"fields": []any{
					map[string]any{
						"key":      "db",
						"type":     "string",
						"title":    "Database",
						"enum":     []any{"postgres", "mysql"},
						"required": true,
					},
				},
			},
		}); err != nil {
			return err
		}
		return yield(llm.StreamRunCompleted{})
	}
	if err := yield(llm.StreamMessageDelta{ContentDelta: "got it", Role: "assistant"}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{})
}

func TestAskUserLoopIntercept(t *testing.T) {
	gateway := &askUserGateway{}
	loop := NewLoop(gateway, nil)
	emitter := events.NewEmitter("trace")

	userAnswer := `{"db":"postgres"}`

	var got []events.RunEvent
	err := loop.Run(
		context.Background(),
		RunContext{
			RunID:               uuid.New(),
			TraceID:             "trace",
			InputJSON:           map[string]any{},
			ReasoningIterations: 5,
			CancelSignal:        func() bool { return false },
			WaitForInput: func(_ context.Context) (string, bool) {
				return userAnswer, true
			},
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

	var hasInputRequested, hasToolCall, hasToolResult, hasCompleted bool
	for _, ev := range got {
		switch ev.Type {
		case "run.input_requested":
			hasInputRequested = true
			if ev.DataJSON["request_id"] != "call_askuser" {
				t.Fatalf("unexpected request_id: %v", ev.DataJSON["request_id"])
			}
		case "tool.call":
			if ev.DataJSON["tool_name"] == "ask_user" {
				hasToolCall = true
			}
		case "tool.result":
			if ev.ToolName != nil && *ev.ToolName == "ask_user" {
				hasToolResult = true
			}
		case "run.completed":
			hasCompleted = true
		}
	}

	if !hasToolCall {
		t.Fatal("expected tool.call for ask_user")
	}
	if !hasInputRequested {
		t.Fatal("expected run.input_requested event")
	}
	if !hasToolResult {
		t.Fatal("expected tool.result for ask_user")
	}
	if !hasCompleted {
		t.Fatal("expected run.completed")
	}
}

func TestAskUserNoWaitForInput(t *testing.T) {
	gateway := &askUserGateway{}
	loop := NewLoop(gateway, nil)
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(
		context.Background(),
		RunContext{
			RunID:               uuid.New(),
			TraceID:             "trace",
			InputJSON:           map[string]any{},
			ReasoningIterations: 5,
			CancelSignal:        func() bool { return false },
			// WaitForInput is nil
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

	var hasToolResultError bool
	for _, ev := range got {
		if ev.Type == "tool.result" && ev.ToolName != nil && *ev.ToolName == "ask_user" {
			if ev.ErrorClass == nil {
				t.Fatal("expected error_class on ask_user tool.result when WaitForInput is nil")
			}
			hasToolResultError = true
		}
	}
	if !hasToolResultError {
		t.Fatal("expected ask_user tool.result with error")
	}
}

type askUserMixedGateway struct {
	calls int
}

func (g *askUserMixedGateway) Stream(_ context.Context, _ llm.Request, yield func(llm.StreamEvent) error) error {
	g.calls++
	if g.calls == 1 {
		if err := yield(llm.ToolCall{
			ToolCallID:    "call_echo",
			ToolName:      "echo",
			ArgumentsJSON: map[string]any{"text": "hello"},
		}); err != nil {
			return err
		}
		if err := yield(llm.ToolCall{
			ToolCallID: "call_ask",
			ToolName:   "ask_user",
			ArgumentsJSON: map[string]any{
				"message": "Pick one",
				"fields": []any{
					map[string]any{
						"key":   "choice",
						"type":  "string",
						"title": "Choice",
						"enum":  []any{"a", "b"},
					},
				},
			},
		}); err != nil {
			return err
		}
		return yield(llm.StreamRunCompleted{})
	}
	if err := yield(llm.StreamMessageDelta{ContentDelta: "ok", Role: "assistant"}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{})
}

func TestAskUserMixedWithRegularTools(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.EchoAgentSpec); err != nil {
		t.Fatalf("register echo failed: %v", err)
	}
	allowlist := tools.AllowlistFromNames([]string{"echo", "ask_user"})
	policy := tools.NewPolicyEnforcer(registry, allowlist)
	executor := tools.NewDispatchingExecutor(registry, policy)
	if err := executor.Bind("echo", builtin.EchoExecutor{}); err != nil {
		t.Fatalf("bind echo failed: %v", err)
	}

	gateway := &askUserMixedGateway{}
	loop := NewLoop(gateway, executor)
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(
		context.Background(),
		RunContext{
			RunID:               uuid.New(),
			TraceID:             "trace",
			InputJSON:           map[string]any{},
			ReasoningIterations: 5,
			ToolExecutor:        executor,
			CancelSignal:        func() bool { return false },
			WaitForInput: func(_ context.Context) (string, bool) {
				return `{"choice":"b"}`, true
			},
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

	var echoResultIdx, askUserResultIdx int
	for i, ev := range got {
		if ev.Type == "tool.result" {
			if ev.ToolName != nil && *ev.ToolName == "echo" {
				echoResultIdx = i
			}
			if ev.ToolName != nil && *ev.ToolName == "ask_user" {
				askUserResultIdx = i
			}
		}
	}

	if echoResultIdx == 0 {
		t.Fatal("expected echo tool.result")
	}
	if askUserResultIdx == 0 {
		t.Fatal("expected ask_user tool.result")
	}
	if echoResultIdx >= askUserResultIdx {
		t.Fatal("echo should be processed before ask_user")
	}
}

func int64Ptr(v int64) *int64 { return &v }

func TestAgentLoopCostBudgetExceeded(t *testing.T) {
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

	gateway := &scriptedTurnsGateway{turns: [][]llm.StreamEvent{
		{
			llm.ToolCall{ToolCallID: "call_1", ToolName: "echo", ArgumentsJSON: map[string]any{"text": "hi"}},
			llm.StreamRunCompleted{
				Cost:  &llm.Cost{Currency: "USD", AmountMicros: 6000},
				Usage: &llm.Usage{OutputTokens: intPtr(100)},
			},
		},
		{
			llm.StreamMessageDelta{ContentDelta: "done", Role: "assistant"},
			llm.StreamRunCompleted{},
		},
	}}

	loop := NewLoop(gateway, executor)
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(context.Background(), RunContext{
		RunID:               uuid.New(),
		TraceID:             "trace",
		InputJSON:           map[string]any{},
		ReasoningIterations: 3,
		ToolExecutor:        executor,
		MaxCostMicros:       int64Ptr(5000),
		CancelSignal:        func() bool { return false },
	}, llm.Request{Model: "stub"}, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}

	found := false
	for _, ev := range got {
		if ev.Type == "run.failed" && ev.ErrorClass != nil && *ev.ErrorClass == llm.ErrorClassBudgetExceeded {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected run.failed with error_class=%s", llm.ErrorClassBudgetExceeded)
	}
}

func TestAgentLoopOutputTokenBudgetExceeded(t *testing.T) {
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

	gateway := &scriptedTurnsGateway{turns: [][]llm.StreamEvent{
		{
			llm.ToolCall{ToolCallID: "call_1", ToolName: "echo", ArgumentsJSON: map[string]any{"text": "hi"}},
			llm.StreamRunCompleted{
				Usage: &llm.Usage{OutputTokens: intPtr(60)},
			},
		},
		{
			llm.StreamMessageDelta{ContentDelta: "done", Role: "assistant"},
			llm.StreamRunCompleted{},
		},
	}}

	loop := NewLoop(gateway, executor)
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(context.Background(), RunContext{
		RunID:                uuid.New(),
		TraceID:              "trace",
		InputJSON:            map[string]any{},
		ReasoningIterations:  3,
		ToolExecutor:         executor,
		MaxTotalOutputTokens: int64Ptr(50),
		CancelSignal:         func() bool { return false },
	}, llm.Request{Model: "stub"}, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}

	found := false
	for _, ev := range got {
		if ev.Type == "run.failed" && ev.ErrorClass != nil && *ev.ErrorClass == llm.ErrorClassBudgetExceeded {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected run.failed with error_class=%s", llm.ErrorClassBudgetExceeded)
	}
}

func TestAgentLoopCostBudgetNotExceeded(t *testing.T) {
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

	gateway := &scriptedTurnsGateway{turns: [][]llm.StreamEvent{
		{
			llm.ToolCall{ToolCallID: "call_1", ToolName: "echo", ArgumentsJSON: map[string]any{"text": "hi"}},
			llm.StreamRunCompleted{
				Cost:  &llm.Cost{Currency: "USD", AmountMicros: 500},
				Usage: &llm.Usage{OutputTokens: intPtr(10)},
			},
		},
		{
			llm.StreamMessageDelta{ContentDelta: "done", Role: "assistant"},
			llm.StreamRunCompleted{},
		},
	}}

	loop := NewLoop(gateway, executor)
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := loop.Run(context.Background(), RunContext{
		RunID:               uuid.New(),
		TraceID:             "trace",
		InputJSON:           map[string]any{},
		ReasoningIterations: 3,
		ToolExecutor:        executor,
		MaxCostMicros:       int64Ptr(100000),
		CancelSignal:        func() bool { return false },
	}, llm.Request{Model: "stub"}, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}

	assertHasEvent(t, got, "run.completed")
	for _, ev := range got {
		if ev.Type == "run.failed" {
			t.Fatalf("unexpected run.failed event")
		}
	}
}

func TestCollectToolOutputScanTextUsesDecodedLeafStrings(t *testing.T) {
	text := collectToolOutputScanText(map[string]any{
		"stdout": "\u001b[?2004h",
		"output": "\u001b[?2004h",
		"stderr": "<string>:81: warning kaleido>=1.0.0",
		"artifacts": []map[string]any{
			{"filename": "integral_plot.png"},
		},
	})

	if strings.Contains(text, `\u001b`) {
		t.Fatalf("scan text should not keep JSON unicode escapes: %q", text)
	}
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) {
		t.Fatalf("scan text should decode HTML-safe JSON escapes: %q", text)
	}
	if strings.Count(text, "\u001b[?2004h") != 1 {
		t.Fatalf("expected deduped escape sequence once, got %q", text)
	}
	if !strings.Contains(text, "<string>:81: warning kaleido>=1.0.0") {
		t.Fatalf("expected decoded stderr in scan text, got %q", text)
	}
	if !strings.Contains(text, "integral_plot.png") {
		t.Fatalf("expected nested string values in scan text, got %q", text)
	}
}

func TestScanToolOutputPassesDecodedTextToScanner(t *testing.T) {
	result := &llm.StreamToolResult{
		ToolName: "python_execute",
		ResultJSON: map[string]any{
			"stderr": "<string>:81: warning kaleido>=1.0.0",
		},
	}
	emitter := events.NewEmitter("trace")

	var scanned string
	err := scanToolOutput(result, func(_ string, text string) (string, bool) {
		scanned = text
		return "", false
	}, emitter, func(events.RunEvent) error { return nil })
	if err != nil {
		t.Fatalf("scanToolOutput failed: %v", err)
	}
	if scanned == "" {
		t.Fatal("expected scanner to receive tool output text")
	}
	if strings.Contains(scanned, `\u003c`) || strings.Contains(scanned, `\u003e`) {
		t.Fatalf("scanner should see decoded text, got %q", scanned)
	}
	if !strings.Contains(scanned, "<string>:81: warning kaleido>=1.0.0") {
		t.Fatalf("expected decoded stderr, got %q", scanned)
	}
}
