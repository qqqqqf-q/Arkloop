//go:build !desktop

package pipeline

import (
	"context"
	"strings"
	"testing"
)

func TestToolBrief(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		argsJSON string
		want     string
	}{
		{"memory_search extracts query", "memory_search", `{"query":"hello world"}`, "hello world"},
		{"web_search.tavily extracts first query", "web_search.tavily", `{"queries":["first","second"]}`, "first"},
		{"notebook_write extracts key", "notebook_write", `{"key":"my-note","content":"..."}`, "my-note"},
		{"code_interpreter", "code_interpreter", `{"code":"print(1)"}`, "Python"},
		{"read_file extracts path", "read_file", `{"path":"/tmp/foo.txt"}`, "/tmp/foo.txt"},
		{"unknown tool returns empty", "some_custom_tool", `{"x":1}`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toolBrief(tc.toolName, tc.argsJSON)
			if got != tc.want {
				t.Fatalf("toolBrief(%q) = %q, want %q", tc.toolName, got, tc.want)
			}
		})
	}
}

func TestDisplayToolName_NormalizesProviderSuffix(t *testing.T) {
	if got := displayToolName("web_search.tavily"); got != "Web Search" {
		t.Fatalf("displayToolName = %q, want %q", got, "Web Search")
	}
}

func TestProgressTracker_TimelineTitleUsesLabel(t *testing.T) {
	tracker, fake := newTestTelegramProgressTracker(t)
	ctx := context.Background()

	tracker.OnRunSegmentStart(ctx, "seg-1", "planning_round", "collapsed", "第 1 轮规划")
	tracker.OnToolCall(ctx, "call-title", "timeline_title", `{"label":"搜索 vLLM meetup 信息"}`)
	tracker.OnToolCall(ctx, "call-1", "web_search.tavily", `{"queries":["vLLM 北京 meetup"]}`)

	sends, _ := fake.stats()
	if sends != 1 {
		t.Fatalf("expected one initial send, got %d", sends)
	}
	if tracker.current == nil {
		t.Fatal("expected current segment")
	}
	if tracker.current.Title != "" {
		t.Fatalf("expected timeline_title to stay hidden, got %q", tracker.current.Title)
	}
}

func TestProgressTracker_TimelineTitleStartsNewSegmentAfterExistingTools(t *testing.T) {
	tracker, _ := newTestTelegramProgressTracker(t)
	ctx := context.Background()

	tracker.OnToolCall(ctx, "call-1", "web_search.tavily", `{"query":"first"}`)
	resetTelegramTrackerThrottle(tracker)
	tracker.OnToolCall(ctx, "call-title", "timeline_title", `{"label":"第二段"}`)
	resetTelegramTrackerThrottle(tracker)
	tracker.OnToolCall(ctx, "call-2", "web_search.tavily", `{"query":"second"}`)

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if len(tracker.segments) != 0 {
		t.Fatalf("expected hidden timeline_title to avoid splitting segments, got %d closed segments", len(tracker.segments))
	}
	if tracker.current == nil || len(tracker.current.Entries) != 2 {
		t.Fatalf("expected both visible tools to stay in one segment, got %#v", tracker.current)
	}
}

func TestProgressTracker_HidesTelegramInternalTools(t *testing.T) {
	tracker, fake := newTestTelegramProgressTracker(t)
	ctx := context.Background()

	tracker.OnToolCall(ctx, "call-reply", "telegram_reply", `{"reply_to_message_id":"42"}`)
	tracker.OnToolResult(ctx, "call-reply", "telegram_reply", "")
	tracker.OnToolCall(ctx, "call-react", "telegram_react", `{"emoji":"👍"}`)
	tracker.OnToolResult(ctx, "call-react", "telegram_react", "")
	tracker.OnToolCall(ctx, "call-1", "web_search.tavily", `{"query":"first"}`)

	sends, _ := fake.stats()
	if sends != 1 {
		t.Fatalf("expected only visible tool to trigger progress send, got %d", sends)
	}

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.current == nil || len(tracker.current.Entries) != 1 {
		t.Fatalf("expected only one visible progress entry, got %#v", tracker.current)
	}
	if tracker.current.Entries[0].DisplayName != "Web Search" {
		t.Fatalf("unexpected visible progress entry: %#v", tracker.current.Entries[0])
	}
}

func TestProgressTracker_TelegramReplyAndReactStayHidden(t *testing.T) {
	tracker, fake := newTestTelegramProgressTracker(t)
	ctx := context.Background()

	tracker.OnToolCall(ctx, "call-reply", "telegram_reply", `{"reply_to_message_id":"42"}`)
	tracker.OnToolResult(ctx, "call-reply", "telegram_reply", "")
	tracker.OnToolCall(ctx, "call-react", "telegram_react", `{"emoji":"👍"}`)
	tracker.OnToolResult(ctx, "call-react", "telegram_react", "")

	sends, edits := fake.stats()
	if sends != 0 || edits != 0 {
		t.Fatalf("expected hidden channel tools to skip Telegram progress output, got sends=%d edits=%d", sends, edits)
	}

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if len(tracker.segments) != 0 || tracker.current != nil {
		t.Fatalf("expected hidden channel tools to leave no progress state, got segments=%d current=%#v", len(tracker.segments), tracker.current)
	}
}

func TestProgressTracker_MessageDeltaClosesCurrentSegment(t *testing.T) {
	tracker, fake := newTestTelegramProgressTracker(t)
	ctx := context.Background()

	tracker.OnToolCall(ctx, "call-1", "web_search.tavily", `{"query":"first"}`)
	resetTelegramTrackerThrottle(tracker)
	tracker.OnMessageDelta(ctx, "assistant", "", "先给你一个结论。")
	resetTelegramTrackerThrottle(tracker)
	tracker.OnToolCall(ctx, "call-2", "read_file", `{"path":"/tmp/result.md"}`)

	sends, edits := fake.stats()
	if sends != 2 || edits < 1 {
		t.Fatalf("expected two sends and at least one edit, got sends=%d edits=%d", sends, edits)
	}

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if len(tracker.segments) != 1 {
		t.Fatalf("expected one closed segment, got %d", len(tracker.segments))
	}
	if tracker.segments[0].MessageID == 0 {
		t.Fatal("expected closed segment to keep its message id")
	}
	if tracker.current == nil || len(tracker.current.Entries) != 1 {
		t.Fatalf("expected one new current segment, got %#v", tracker.current)
	}
	if tracker.current.MessageID == 0 {
		t.Fatal("expected new current segment to create a new message")
	}
	if tracker.current.MessageID == tracker.segments[0].MessageID {
		t.Fatalf("expected second segment to use a new telegram message, got same id %d", tracker.current.MessageID)
	}
}

func TestProgressTracker_RunSegmentBoundaryProducesSeparateSummary(t *testing.T) {
	tracker, _ := newTestTelegramProgressTracker(t)
	ctx := context.Background()

	tracker.OnRunSegmentStart(ctx, "seg-1", "planning_round", "collapsed", "搜索第一轮")
	tracker.OnToolCall(ctx, "call-1", "web_search.tavily", `{"query":"first"}`)
	resetTelegramTrackerThrottle(tracker)
	tracker.OnRunSegmentEnd(ctx, "seg-1")
	resetTelegramTrackerThrottle(tracker)
	tracker.OnRunSegmentStart(ctx, "seg-2", "planning_round", "collapsed", "搜索第二轮")
	tracker.OnToolCall(ctx, "call-2", "web_search.tavily", `{"query":"second"}`)

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if len(tracker.segments) != 1 {
		t.Fatalf("expected first segment to be closed, got %d", len(tracker.segments))
	}
	if got := resolveSegmentTitle(tracker.segments[0], true); got != "搜索第一轮" {
		t.Fatalf("expected first closed segment label, got %q", got)
	}
	if tracker.current == nil {
		t.Fatal("expected current second segment")
	}
	if got := resolveSegmentTitle(*tracker.current, false); got != "搜索第二轮" {
		t.Fatalf("expected current segment label, got %q", got)
	}
}

func TestProgressTracker_LiveFallbackTitleUsesAdaptiveProgressLabel(t *testing.T) {
	tracker := &TelegramProgressTracker{
		current: &TelegramProgressSegment{
			ID: "seg-1",
			Entries: []ProgressEntry{
				{ToolCallID: "call-1", DisplayName: "Web Search"},
			},
		},
	}

	got := tracker.formatProgressLocked(false)
	if !strings.Contains(got, "… In process") {
		t.Fatalf("expected live adaptive title, got:\n%s", got)
	}
}

func TestProgressTracker_CompletedFallbackTitleUsesAdaptiveSummaryLabel(t *testing.T) {
	tracker := &TelegramProgressTracker{
		segments: []TelegramProgressSegment{
			{
				ID: "seg-1",
				Entries: []ProgressEntry{
					{ToolCallID: "call-1", DisplayName: "Web Search", Done: true},
				},
				Closed: true,
			},
		},
	}

	got := tracker.formatProgressLocked(true)
	if !strings.Contains(got, "✓ 1 step completed") {
		t.Fatalf("expected completed adaptive title, got:\n%s", got)
	}
}

func TestProgressTracker_FinalizeGroupsBySegment(t *testing.T) {
	tracker := &TelegramProgressTracker{
		segments: []TelegramProgressSegment{
			{
				ID:    "seg-1",
				Title: "搜索 vLLM meetup 信息",
				Entries: []ProgressEntry{
					{ToolCallID: "call-1", ToolName: "web_search", DisplayName: "Web Search", Done: true},
					{ToolCallID: "call-2", ToolName: "web_search", DisplayName: "Web Search", Done: true},
				},
				Closed: true,
			},
		},
		current: &TelegramProgressSegment{
			ID:    "seg-2",
			Title: "深入搜索 meetup 议程",
			Entries: []ProgressEntry{
				{ToolCallID: "call-3", ToolName: "read_file", DisplayName: "Read File", Done: true},
			},
		},
	}

	got := tracker.formatProgressLocked(true)
	if !strings.Contains(got, "✓ 搜索 vLLM meetup 信息") {
		t.Fatalf("expected closed segment title, got:\n%s", got)
	}
	if !strings.Contains(got, "Web Search x2") {
		t.Fatalf("expected grouped tool summary, got:\n%s", got)
	}
	if !strings.Contains(got, "✓ 深入搜索 meetup 议程") {
		t.Fatalf("expected second segment title, got:\n%s", got)
	}
	if strings.Contains(got, "web_search.tavily") {
		t.Fatalf("provider suffix leaked into summary:\n%s", got)
	}
}

func TestProgressTracker_LiveViewCollapsesHistoryButKeepsCurrentExpanded(t *testing.T) {
	tracker := &TelegramProgressTracker{
		segments: []TelegramProgressSegment{
			{
				ID:    "seg-1",
				Title: "第一段",
				Entries: []ProgressEntry{
					{ToolCallID: "call-1", DisplayName: "Web Search", Done: true},
				},
				Closed: true,
			},
		},
		current: &TelegramProgressSegment{
			ID:    "seg-2",
			Title: "当前段",
			Entries: []ProgressEntry{
				{ToolCallID: "call-2", DisplayName: "Read File", Brief: "/tmp/a.txt"},
			},
		},
	}

	got := tracker.formatProgressLocked(false)
	if !strings.Contains(got, "✓ 第一段\n  Web Search") {
		t.Fatalf("expected historical summary, got:\n%s", got)
	}
	if !strings.Contains(got, "… 当前段\n  … Read File: /tmp/a.txt") {
		t.Fatalf("expected active segment details, got:\n%s", got)
	}
}

func TestProgressTracker_FinalizeIgnoresThrottle(t *testing.T) {
	tracker, fake := newTestTelegramProgressTracker(t)
	ctx := context.Background()

	tracker.OnToolCall(ctx, "call-1", "memory_search", `{"query":"test"}`)
	tracker.Finalize(ctx)

	_, edits := fake.stats()
	if edits < 1 {
		t.Fatalf("expected finalize to force an edit, got %d", edits)
	}
}

func TestProgressTracker_FinalizeNoOpsWhenEmpty(t *testing.T) {
	tracker, fake := newTestTelegramProgressTracker(t)

	tracker.Finalize(context.Background())

	sends, edits := fake.stats()
	if sends != 0 || edits != 0 {
		t.Fatalf("expected no API calls for empty tracker, got sends=%d edits=%d", sends, edits)
	}
}

func TestProgressTracker_TimelineTitleOnlyStaysHidden(t *testing.T) {
	tracker, fake := newTestTelegramProgressTracker(t)
	ctx := context.Background()

	tracker.OnToolCall(ctx, "call-title", "timeline_title", `{"label":"隐藏标题"}`)
	tracker.Finalize(ctx)

	sends, edits := fake.stats()
	if sends != 0 || edits != 0 {
		t.Fatalf("expected timeline_title to stay hidden, got sends=%d edits=%d", sends, edits)
	}
}
