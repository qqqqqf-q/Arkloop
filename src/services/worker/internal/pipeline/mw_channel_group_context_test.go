package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type capturedGroupTrimEventAppender struct {
	events []events.RunEvent
}

func (c *capturedGroupTrimEventAppender) AppendRunEvent(_ context.Context, _ pgx.Tx, _ uuid.UUID, ev events.RunEvent) (int64, error) {
	c.events = append(c.events, ev)
	return int64(len(c.events)), nil
}

func TestTrimRunContextMessagesToApproxTokens_keepsSuffixWithinBudget(t *testing.T) {
	const budget = 50
	msgs := make([]llm.Message, 0, 20)
	ids := make([]uuid.UUID, 0, 20)
	for i := 0; i < 20; i++ {
		msgs = append(msgs, llm.Message{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: strings.Repeat("x", 120)}}})
		ids = append(ids, uuid.New())
	}
	rc := &RunContext{
		Messages:         msgs,
		ThreadMessageIDs: ids,
	}
	trimRunContextMessagesToApproxTokens(rc, budget)
	if len(rc.Messages) >= 20 {
		t.Fatalf("expected trimming, got %d", len(rc.Messages))
	}
	if len(rc.ThreadMessageIDs) != len(rc.Messages) {
		t.Fatalf("ThreadMessageIDs should stay aligned, msgs=%d ids=%d", len(rc.Messages), len(rc.ThreadMessageIDs))
	}
}

func TestNewChannelGroupContextTrimMiddleware_projectsButSkipsTrimForPrivate(t *testing.T) {
	mw := NewChannelGroupContextTrimMiddleware()
	rc := &RunContext{
		ChannelContext: &ChannelContext{ConversationType: "private"},
		Messages:       []llm.Message{{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "---\ndisplay-name: \"Alice\"\nchannel: \"telegram\"\nconversation-type: \"private\"\ntime: \"2026-04-03T10:00:00Z\"\n---\nhello"}}}},
	}
	called := false
	err := mw(context.Background(), rc, func(context.Context, *RunContext) error {
		called = true
		if len(rc.Messages) != 1 {
			t.Fatalf("messages should not be trimmed for DM")
		}
		text := llm.PartPromptText(rc.Messages[0].Content[0])
		if strings.Contains(text, "---") {
			t.Fatalf("envelope should be projected for DM, got %q", text)
		}
		if !strings.Contains(text, "Alice") {
			t.Fatalf("projected text should contain display name, got %q", text)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("next not invoked")
	}
}

func TestNewChannelGroupContextTrimMiddleware_skipsProjectionWithoutChannelContext(t *testing.T) {
	mw := NewChannelGroupContextTrimMiddleware()
	original := "---\ndisplay-name: \"Alice\"\nchannel: \"telegram\"\nconversation-type: \"private\"\ntime: \"2026-04-03T10:00:00Z\"\n---\nhello"
	rc := &RunContext{
		Messages: []llm.Message{{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: original}}}},
	}

	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })

	if got := llm.PartPromptText(rc.Messages[0].Content[0]); got != original {
		t.Fatalf("expected envelope to stay untouched without channel context, got %q", got)
	}
}

func TestNewChannelGroupContextTrimMiddleware_trimsSupergroup(t *testing.T) {
	t.Setenv("ARKLOOP_CHANNEL_GROUP_MAX_CONTEXT_TOKENS", "40")
	mw := NewChannelGroupContextTrimMiddleware()
	long := strings.Repeat("w", 400)
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: long}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: long}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "tail"}}},
	}
	rc := &RunContext{
		ChannelContext: &ChannelContext{ConversationType: "supergroup"},
		Messages:       msgs,
	}
	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })
	if len(rc.Messages) != 1 {
		t.Fatalf("expected only tail message kept, got %d", len(rc.Messages))
	}
	if rc.Messages[0].Content[0].Text != "tail" {
		t.Fatalf("unexpected kept content")
	}
}

func TestApproxLLMMessageTokens_imageDoesNotScaleWithBytes(t *testing.T) {
	huge := make([]byte, 4*1024*1024)
	m := llm.Message{
		Content: []llm.ContentPart{{
			Type:       messagecontent.PartTypeImage,
			Attachment: &messagecontent.AttachmentRef{Filename: "huge.png"},
			Data:       huge,
		}},
	}
	tok := approxLLMMessageTokens(m)
	if tok > 5000 {
		t.Fatalf("image trim estimate should not grow with raw bytes, got %d", tok)
	}
}

func TestMessageTokens_outputTokensOnlyForAssistant(t *testing.T) {
	fiveHundred := int64(500)
	u := &llm.Message{
		Role:         "user",
		OutputTokens: &fiveHundred,
		Content:      []llm.ContentPart{{Type: "text", Text: strings.Repeat("x", 120)}},
	}
	if got := messageTokens(u); got == 500 {
		t.Fatalf("user message must not use output_tokens as length, got %d", got)
	}
	a := &llm.Message{
		Role:         "assistant",
		OutputTokens: &fiveHundred,
		Content:      []llm.ContentPart{{Type: "text", Text: "short"}},
	}
	if got := messageTokens(a); got != 500 {
		t.Fatalf("assistant should use output_tokens, got %d want 500", got)
	}
}

func TestNewChannelGroupContextTrimMiddleware_emitsDebugTrimEvent(t *testing.T) {
	t.Setenv("ARKLOOP_CHANNEL_GROUP_MAX_CONTEXT_TOKENS", "40")
	appender := &capturedGroupTrimEventAppender{}
	mw := NewChannelGroupContextTrimMiddleware(GroupContextTrimDeps{
		Pool:            noopCompactPersistDB{},
		EventsRepo:      appender,
		EmitDebugEvents: true,
	})
	long := strings.Repeat("w", 400)
	runID := uuid.New()
	rc := &RunContext{
		Run:            dataRunForTest(runID),
		Emitter:        events.NewEmitter("trace-group-trim"),
		ChannelContext: &ChannelContext{ConversationType: "supergroup"},
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: long}}},
			{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: long}}},
			{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "tail"}}},
		},
		ThreadMessageIDs: []uuid.UUID{uuid.New(), uuid.New(), uuid.New()},
	}
	if err := mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(appender.events) != 1 {
		t.Fatalf("expected 1 debug event, got %d", len(appender.events))
	}
	ev := appender.events[0]
	if ev.Type != "run.context_compact" {
		t.Fatalf("unexpected event type: %s", ev.Type)
	}
	if ev.DataJSON["op"] != "group_trim" || ev.DataJSON["phase"] != "completed" {
		t.Fatalf("unexpected event payload: %#v", ev.DataJSON)
	}
	if ev.DataJSON["dropped_count"] != 2 {
		t.Fatalf("expected dropped_count=2, got %#v", ev.DataJSON["dropped_count"])
	}
	if ev.DataJSON["estimated_text_tokens_before"] == nil || ev.DataJSON["estimated_image_tokens_before"] == nil {
		t.Fatalf("expected token diagnostics, got %#v", ev.DataJSON)
	}
}

func TestNewChannelGroupContextTrimMiddleware_skipsDebugEventWhenDisabled(t *testing.T) {
	t.Setenv("ARKLOOP_CHANNEL_GROUP_MAX_CONTEXT_TOKENS", "40")
	appender := &capturedGroupTrimEventAppender{}
	mw := NewChannelGroupContextTrimMiddleware(GroupContextTrimDeps{
		Pool:            noopCompactPersistDB{},
		EventsRepo:      appender,
		EmitDebugEvents: false,
	})
	long := strings.Repeat("w", 400)
	rc := &RunContext{
		Run:              dataRunForTest(uuid.New()),
		Emitter:          events.NewEmitter("trace-group-trim"),
		ChannelContext:   &ChannelContext{ConversationType: "supergroup"},
		Messages:         []llm.Message{{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: long}}}, {Role: "user", Content: []llm.ContentPart{{Type: "text", Text: long}}}, {Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "tail"}}}},
		ThreadMessageIDs: []uuid.UUID{uuid.New(), uuid.New(), uuid.New()},
	}
	if err := mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(appender.events) != 0 {
		t.Fatalf("expected no debug events, got %d", len(appender.events))
	}
}

func TestBuildGroupTrimEventIncludesSnapshotFlag(t *testing.T) {
	before := groupTrimStats{
		MessageCount:         4,
		RealMessageCount:     3,
		HasSnapshotPrefix:    true,
		EstimatedTrimWeight:  100,
		EstimatedTextTokens:  80,
		EstimatedImageTokens: 20,
	}
	after := groupTrimStats{
		MessageCount:         3,
		RealMessageCount:     2,
		HasSnapshotPrefix:    true,
		EstimatedTrimWeight:  60,
		EstimatedTextTokens:  50,
		EstimatedImageTokens: 10,
	}
	ev := buildGroupTrimEvent(before, after, 32768, false)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev["has_snapshot_prefix"] != true {
		t.Fatalf("expected snapshot flag, got %#v", ev["has_snapshot_prefix"])
	}
	if ev["dropped_count"] != 1 {
		t.Fatalf("expected dropped_count=1, got %#v", ev["dropped_count"])
	}
}

type noopCompactPersistDB struct{}

func (noopCompactPersistDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return noopTx{}, nil
}
func (noopCompactPersistDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}
func (noopCompactPersistDB) QueryRow(context.Context, string, ...any) pgx.Row { return noopRow{} }

type noopTx struct{}

func (noopTx) Begin(context.Context) (pgx.Tx, error) { return noopTx{}, nil }
func (noopTx) Commit(context.Context) error          { return nil }
func (noopTx) Rollback(context.Context) error        { return nil }
func (noopTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (noopTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return noopBatchResults{} }
func (noopTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (noopTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (noopTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (noopTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return noopRows{}, nil }
func (noopTx) QueryRow(context.Context, string, ...any) pgx.Row        { return noopRow{} }
func (noopTx) Conn() *pgx.Conn                                         { return nil }

type noopRow struct{}

func (noopRow) Scan(...any) error { return nil }

type noopRows struct{}

func (noopRows) Close()                                       {}
func (noopRows) Err() error                                   { return nil }
func (noopRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (noopRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (noopRows) Next() bool                                   { return false }
func (noopRows) Scan(...any) error                            { return nil }
func (noopRows) Values() ([]any, error)                       { return nil, nil }
func (noopRows) RawValues() [][]byte                          { return nil }
func (noopRows) Conn() *pgx.Conn                              { return nil }

type noopBatchResults struct{}

func (noopBatchResults) Exec() (pgconn.CommandTag, error) { return pgconn.CommandTag{}, nil }
func (noopBatchResults) Query() (pgx.Rows, error)         { return noopRows{}, nil }
func (noopBatchResults) QueryRow() pgx.Row                { return noopRow{} }
func (noopBatchResults) Close() error                     { return nil }

func dataRunForTest(runID uuid.UUID) data.Run {
	return data.Run{ID: runID, AccountID: uuid.New(), ThreadID: uuid.New()}
}

func TestGroupTrimEventJSONStable(t *testing.T) {
	ev := buildGroupTrimEvent(
		groupTrimStats{MessageCount: 3, RealMessageCount: 3, EstimatedTrimWeight: 90, EstimatedTextTokens: 80, EstimatedImageTokens: 10},
		groupTrimStats{MessageCount: 2, RealMessageCount: 2, EstimatedTrimWeight: 60, EstimatedTextTokens: 50, EstimatedImageTokens: 10},
		100,
		false,
	)
	if ev == nil {
		t.Fatal("expected event")
	}
	if _, err := json.Marshal(ev); err != nil {
		t.Fatalf("marshal event: %v", err)
	}
}
