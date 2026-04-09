package pipeline

import (
	"context"
	"math"
	"strings"
	"testing"

	"arkloop/services/shared/creditpolicy"
	"arkloop/services/worker/internal/llm"
)

func TestCalcPlatformCost_AnthropicCacheFamily(t *testing.T) {
	w := &eventWriter{
		totalInputTokens:         3000,
		totalOutputTokens:        1000,
		totalCacheCreationTokens: 2000,
		totalCacheReadTokens:     5000,
		costPer1kInput:           f64ptr(1.0),
		costPer1kOutput:          f64ptr(2.0),
		costPer1kCacheWrite:      f64ptr(1.25),
		costPer1kCacheRead:       f64ptr(0.1),
	}

	cost := w.calcPlatformCost()
	// output: 1k*2 + input:3k*1 + cache_write:2k*1.25 + cache_read:5k*0.1 = 8.0
	if math.Abs(cost-8.0) > 1e-9 {
		t.Fatalf("expected cost=8.0, got %.10f", cost)
	}
}

func TestCalcPlatformCost_OpenAICacheFamily(t *testing.T) {
	w := &eventWriter{
		totalInputTokens:    10000,
		totalOutputTokens:   2000,
		totalCachedTokens:   4000,
		costPer1kInput:      f64ptr(1.0),
		costPer1kOutput:     f64ptr(2.0),
		costPer1kCacheRead:  nil,
		costPer1kCacheWrite: nil,
	}

	cost := w.calcPlatformCost()
	// output: 2k*2 + uncached:6k*1 + cached:4k*0.5 = 12.0
	if math.Abs(cost-12.0) > 1e-9 {
		t.Fatalf("expected cost=12.0, got %.10f", cost)
	}
}

func TestCalcCreditDeduction_AnthropicCacheFamilyFallback(t *testing.T) {
	w := &eventWriter{
		totalInputTokens:         3000,
		totalOutputTokens:        1000,
		totalCacheCreationTokens: 2000,
		totalCacheReadTokens:     5000,
		totalCostUSD:             0,
		multiplier:               1.0,
		policy: creditpolicy.CreditDeductionPolicy{
			Tiers: []creditpolicy.CreditTier{{Multiplier: 1.0}},
		},
	}

	r := w.calcCreditDeduction()
	// effective = 3000 + 2000*1.25 + 5000*0.1 + 1000 = 7000 => ceil(7.0) = 7
	if r.Credits != 7 {
		t.Fatalf("expected credits=7, got %d", r.Credits)
	}
	if r.Metadata["method"] != "token_fallback" {
		t.Fatalf("expected method=token_fallback, got %v", r.Metadata["method"])
	}
}

func TestCalcCreditDeduction_OpenAICacheFamilyFallback(t *testing.T) {
	w := &eventWriter{
		totalInputTokens:  10000,
		totalOutputTokens: 2000,
		totalCachedTokens: 4000,
		totalCostUSD:      0,
		multiplier:        1.0,
		policy: creditpolicy.CreditDeductionPolicy{
			Tiers: []creditpolicy.CreditTier{{Multiplier: 1.0}},
		},
	}

	r := w.calcCreditDeduction()
	// effective = (10000-4000) + 4000*0.5 + 2000 = 10000 => 10 credits
	if r.Credits != 10 {
		t.Fatalf("expected credits=10, got %d", r.Credits)
	}
}

func TestCalcCreditDeduction_MixedCacheFamilyKeepsLegacy(t *testing.T) {
	w := &eventWriter{
		totalInputTokens:         10000,
		totalOutputTokens:        1000,
		totalCacheCreationTokens: 2000,
		totalCacheReadTokens:     2000,
		totalCachedTokens:        3000,
		totalCostUSD:             0,
		multiplier:               1.0,
		policy: creditpolicy.CreditDeductionPolicy{
			Tiers: []creditpolicy.CreditTier{{Multiplier: 1.0}},
		},
	}

	r := w.calcCreditDeduction()
	// legacy: nonCached=10000-2000-3000=5000
	// effective=5000 + 2000*1.25 + 2000*0.1 + 3000*0.5 + 1000 = 10200 => ceil(10.2)=11
	if r.Credits != 11 {
		t.Fatalf("expected legacy credits=11, got %d", r.Credits)
	}
}

func TestExtractAssistantDeltaFiltersHeartbeatTerminalToken(t *testing.T) {
	if got := extractAssistantDelta(map[string]any{
		"role":          "assistant",
		"content_delta": "<end_turn>",
	}); got != "" {
		t.Fatalf("expected empty delta, got %q", got)
	}
}

func TestEventWriterPendingTelegramFlushChunk(t *testing.T) {
	w := &eventWriter{
		assistantOutputs:          []string{"第一段输出", "第二段输出", "第三段输出"},
		telegramSentOutputCount:   1,
		telegramToolBoundaryFlush: func(_ context.Context, _ string) error { return nil },
	}

	got := w.pendingTelegramFlushChunk()
	// 期望返回 index=1 之后的 "第二段输出\n第三段输出"
	if !strings.Contains(got, "第二段输出") || !strings.Contains(got, "第三段输出") {
		t.Fatalf("expected pending flush chunk to include 第二段输出 and 第三段输出, got %q", got)
	}
	if strings.Contains(got, "第一段输出") {
		t.Fatalf("expected pending flush chunk to NOT include 第一段输出, got %q", got)
	}
}

func TestEventWriterPendingTelegramFlushChunkFromAssistantMessage(t *testing.T) {
	// 当 turn 通过 assistantMessage 完成（无 delta）时，captureAssistantTurnOutput 依然能追加到 assistantOutputs
	// 然后 pendingTelegramFlushChunk 应该返回该内容
	w := &eventWriter{
		assistantOutputs:          []string{},
		telegramSentOutputCount:   0,
		telegramToolBoundaryFlush: func(_ context.Context, _ string) error { return nil },
	}
	// 模拟 LLM 直接给完整 message（而非 delta）
	msg := llm.Message{Role: "assistant", Content: []llm.ContentPart{{Type: "text", Text: "来自 assistantMessage 的内容"}}}
	w.assistantMessage = &msg
	w.assistantMessageFresh = true
	w.captureAssistantTurnOutput()

	got := w.pendingTelegramFlushChunk()
	if !strings.Contains(got, "来自 assistantMessage 的内容") {
		t.Fatalf("expected flush chunk to contain assistantMessage content, got %q", got)
	}
}

func TestEventWriterTelegramUnsentOutputsMixedScenario(t *testing.T) {
	// Turn 1：有 delta，已通过 mid-stream flush 发出（telegramSentOutputCount=1）
	// Turn 2：LLM 直接给完整 assistantMessage，无 delta
	// 期望 telegramUnsentOutputs() 只返回 Turn 2 的内容
	w := &eventWriter{
		assistantOutputs:          []string{"Turn1 内容"},
		telegramSentOutputCount:   1,
		telegramToolBoundaryFlush: func(_ context.Context, _ string) error { return nil },
	}

	// 模拟 Turn 2：无 delta，通过 assistantMessage 到达
	msg := llm.Message{Role: "assistant", Content: []llm.ContentPart{{Type: "text", Text: "Turn2 内容"}}}
	w.assistantMessage = &msg
	w.assistantMessageFresh = true
	w.captureAssistantTurnOutput()

	unsent := w.telegramUnsentOutputs()
	if len(unsent) != 1 || unsent[0] != "Turn2 内容" {
		t.Fatalf("expected unsent=[Turn2 内容], got %v", unsent)
	}
	remainder := w.telegramStreamRemainder()
	if remainder != "Turn2 内容" {
		t.Fatalf("expected remainder=Turn2 内容, got %q", remainder)
	}
}

func TestShouldSuppressHeartbeatOutput(t *testing.T) {
	tests := []struct {
		name   string
		rc     *RunContext
		output string
		want   bool
	}{
		{
			name:   "non heartbeat never suppresses",
			rc:     &RunContext{},
			output: "hello",
			want:   false,
		},
		{
			name: "tool explicit silent suppresses",
			rc: &RunContext{
				HeartbeatRun: true,
				HeartbeatToolOutcome: &HeartbeatDecisionOutcome{
					Reply: false,
				},
			},
			output: "hello",
			want:   true,
		},
		{
			name: "tool explicit reply keeps output",
			rc: &RunContext{
				HeartbeatRun: true,
				HeartbeatToolOutcome: &HeartbeatDecisionOutcome{
					Reply: true,
				},
			},
			output: "hello",
			want:   false,
		},
		{
			name: "blank heartbeat output suppresses",
			rc: &RunContext{
				HeartbeatRun: true,
			},
			output: "",
			want:   true,
		},
		{
			name: "heartbeat ack suppresses",
			rc: &RunContext{
				HeartbeatRun: true,
			},
			output: "HEARTBEAT_OK",
			want:   true,
		},
		{
			name: "real heartbeat text still suppresses when tool missing",
			rc: &RunContext{
				HeartbeatRun: true,
			},
			output: "请关注今天 18:00 的发布窗口。",
			want:   true,
		},
		{
			name: "reply=true but no substantive content suppresses",
			rc: &RunContext{
				HeartbeatRun: true,
				HeartbeatToolOutcome: &HeartbeatDecisionOutcome{
					Reply: true,
				},
			},
			output: "[No substantive content to send]",
			want:   true,
		},
		{
			name: "reply=true with real text keeps output",
			rc: &RunContext{
				HeartbeatRun: true,
				HeartbeatToolOutcome: &HeartbeatDecisionOutcome{
					Reply: true,
				},
			},
			output: "请关注今天 18:00 的发布窗口。",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldSuppressHeartbeatOutput(tt.rc, tt.output); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldAccumulateUsageForEvent(t *testing.T) {
	tests := []struct {
		eventType string
		want      bool
	}{
		{eventType: "llm.turn.completed", want: true},
		{eventType: "tool.result", want: true},
		{eventType: "run.completed", want: false},
		{eventType: "run.failed", want: false},
		{eventType: "run.cancelled", want: false},
		{eventType: "run.interrupted", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			if got := shouldAccumulateUsageForEvent(tt.eventType); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func f64ptr(v float64) *float64 {
	return &v
}

func TestCaptureReplyOverride(t *testing.T) {
	w := &eventWriter{}
	w.captureReplyOverride(map[string]any{
		"tool_name":    "telegram_reply",
		"tool_call_id": "call-1",
		"arguments":    map[string]any{"reply_to_message_id": "6592"},
	})
	if w.pendingReplyOverride != "6592" {
		t.Fatalf("expected pendingReplyOverride=6592, got %q", w.pendingReplyOverride)
	}
}

func TestCaptureReplyOverride_IgnoresOtherTools(t *testing.T) {
	w := &eventWriter{}
	w.captureReplyOverride(map[string]any{
		"tool_name":    "telegram_react",
		"tool_call_id": "call-2",
		"arguments":    map[string]any{"emoji": "thumbs_up"},
	})
	if w.pendingReplyOverride != "" {
		t.Fatalf("expected empty pendingReplyOverride, got %q", w.pendingReplyOverride)
	}
}

func TestCaptureReplyOverride_OverwritesOnMultipleCalls(t *testing.T) {
	w := &eventWriter{}
	w.captureReplyOverride(map[string]any{
		"tool_name": "telegram_reply",
		"arguments": map[string]any{"reply_to_message_id": "100"},
	})
	w.captureReplyOverride(map[string]any{
		"tool_name": "telegram_reply",
		"arguments": map[string]any{"reply_to_message_id": "200"},
	})
	if w.pendingReplyOverride != "200" {
		t.Fatalf("expected last override=200, got %q", w.pendingReplyOverride)
	}
}

func TestEventWriterFlushPendingToolCallsDoesNotPersistProviderToolNames(t *testing.T) {
	w := &eventWriter{
		assistantMessage: &llm.Message{
			Role:    "assistant",
			Content: []llm.TextPart{{Text: "searching"}},
		},
	}

	w.collectToolCall(map[string]any{
		"tool_call_id": "call_1",
		"tool_name":    "web_search.tavily",
		"arguments":    map[string]any{"query": "hello"},
	})
	w.collectToolResult(map[string]any{
		"tool_call_id": "call_1",
		"tool_name":    "web_search.tavily",
		"result":       map[string]any{"items": []any{map[string]any{"title": "x"}}},
	})
	w.flushPendingToolCalls()

	if len(w.intermediateMessages) != 2 {
		t.Fatalf("expected assistant+tool intermediate messages, got %d", len(w.intermediateMessages))
	}

	assistantJSON := string(w.intermediateMessages[0].ContentJSON)
	if strings.Contains(assistantJSON, "web_search.tavily") {
		t.Fatalf("expected assistant intermediate message to hide provider tool name, got %s", assistantJSON)
	}
	if !strings.Contains(assistantJSON, `"tool_name":"web_search"`) {
		t.Fatalf("expected assistant intermediate message to keep canonical tool name, got %s", assistantJSON)
	}

	toolContent := w.intermediateMessages[1].Content
	if strings.Contains(toolContent, "web_search.tavily") {
		t.Fatalf("expected tool intermediate message to hide provider tool name, got %s", toolContent)
	}
	if !strings.Contains(toolContent, `"tool_name":"web_search"`) {
		t.Fatalf("expected tool intermediate message to keep canonical tool name, got %s", toolContent)
	}
}

func TestEventWriterFlushPendingToolCallsFiltersHeartbeatDecisionFromPersistentHistory(t *testing.T) {
	w := &eventWriter{
		heartbeatRun: true,
		assistantMessage: &llm.Message{
			Role:    "assistant",
			Content: []llm.TextPart{{Text: "replying"}},
		},
	}

	w.collectToolCall(map[string]any{
		"tool_call_id": "call_1",
		"tool_name":    "heartbeat_decision",
		"arguments":    map[string]any{"reply": true},
	})
	w.collectToolResult(map[string]any{
		"tool_call_id": "call_1",
		"tool_name":    "heartbeat_decision",
		"result":       map[string]any{"ok": true, "reply": true},
	})
	w.flushPendingToolCalls()

	if len(w.intermediateMessages) != 1 {
		t.Fatalf("expected assistant-only intermediate message, got %d", len(w.intermediateMessages))
	}
	if w.intermediateMessages[0].Role != "assistant" {
		t.Fatalf("expected assistant intermediate message, got %#v", w.intermediateMessages[0])
	}
	assistantJSON := string(w.intermediateMessages[0].ContentJSON)
	if strings.Contains(assistantJSON, "heartbeat_decision") {
		t.Fatalf("expected heartbeat_decision to be removed from persistent history, got %s", assistantJSON)
	}
}
