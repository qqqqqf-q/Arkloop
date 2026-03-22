package pipeline

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
)

func tgGroupRC(msgs []llm.Message, ids []uuid.UUID) *RunContext {
	return &RunContext{
		ChannelContext: &ChannelContext{
			ChannelType:      "telegram",
			ConversationType: "supergroup",
		},
		Messages:         msgs,
		ThreadMessageIDs: ids,
	}
}

func TestNewChannelTelegramGroupUserMergeMiddleware_skipsNonTelegram(t *testing.T) {
	mw := NewChannelTelegramGroupUserMergeMiddleware()
	id1, id2 := uuid.New(), uuid.New()
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.ContentPart{{Type: "text", Text: "hi"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "a"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "b"}}},
	}
	rc := &RunContext{
		ChannelContext: &ChannelContext{
			ChannelType:      "discord",
			ConversationType: "supergroup",
		},
		Messages:         append([]llm.Message(nil), msgs...),
		ThreadMessageIDs: []uuid.UUID{id1, id2, uuid.New()},
	}
	err := mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(rc.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(rc.Messages))
	}
}

func TestNewChannelTelegramGroupUserMergeMiddleware_skipsPrivate(t *testing.T) {
	mw := NewChannelTelegramGroupUserMergeMiddleware()
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "a"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "b"}}},
	}
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	rc := &RunContext{
		ChannelContext: &ChannelContext{
			ChannelType:      "telegram",
			ConversationType: "private",
		},
		Messages:         append([]llm.Message(nil), msgs...),
		ThreadMessageIDs: append([]uuid.UUID(nil), ids...),
	}
	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })
	if len(rc.Messages) != 2 {
		t.Fatalf("expected 2 messages for DM, got %d", len(rc.Messages))
	}
}

func TestNewChannelTelegramGroupUserMergeMiddleware_mergesThreeUsersAfterAssistant(t *testing.T) {
	mw := NewChannelTelegramGroupUserMergeMiddleware()
	idA, id1, id2, id3 := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.ContentPart{{Type: "text", Text: "bot"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "one"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "two"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "three"}}},
	}
	rc := tgGroupRC(append([]llm.Message(nil), msgs...), []uuid.UUID{idA, id1, id2, id3})
	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })
	if len(rc.Messages) != 2 {
		t.Fatalf("expected assistant + 1 merged user, got %d", len(rc.Messages))
	}
	if len(rc.ThreadMessageIDs) != 2 || rc.ThreadMessageIDs[1] != id3 {
		t.Fatalf("expected last tail id preserved, got %#v", rc.ThreadMessageIDs)
	}
	var got strings.Builder
	for _, p := range rc.Messages[1].Content {
		got.WriteString(llm.PartPromptText(p))
	}
	s := got.String()
	for _, want := range []string{"one", "two", "three"} {
		if !strings.Contains(s, want) {
			t.Fatalf("merged text missing %q: %q", want, s)
		}
	}
}

func TestNewChannelTelegramGroupUserMergeMiddleware_mergesTwoUsersNoAssistant(t *testing.T) {
	mw := NewChannelTelegramGroupUserMergeMiddleware()
	id1, id2 := uuid.New(), uuid.New()
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "x"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "y"}}},
	}
	rc := tgGroupRC(msgs, []uuid.UUID{id1, id2})
	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })
	if len(rc.Messages) != 1 {
		t.Fatalf("expected 1 merged user, got %d", len(rc.Messages))
	}
	if rc.ThreadMessageIDs[0] != id2 {
		t.Fatalf("expected last id, got %v", rc.ThreadMessageIDs[0])
	}
}

func TestNewChannelTelegramGroupUserMergeMiddleware_singleUserAfterAssistantNoOp(t *testing.T) {
	mw := NewChannelTelegramGroupUserMergeMiddleware()
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.ContentPart{{Type: "text", Text: "a"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "only"}}},
	}
	rc := tgGroupRC(append([]llm.Message(nil), msgs...), append([]uuid.UUID(nil), ids...))
	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })
	if len(rc.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(rc.Messages))
	}
}

func TestNewChannelTelegramGroupUserMergeMiddleware_skipsWhenTailHasTool(t *testing.T) {
	mw := NewChannelTelegramGroupUserMergeMiddleware()
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.ContentPart{{Type: "text", Text: "a"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "u1"}}},
		{Role: "tool", Content: []llm.ContentPart{{Type: "text", Text: "{}"}}},
	}
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	rc := tgGroupRC(msgs, ids)
	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })
	if len(rc.Messages) != 3 {
		t.Fatalf("expected no merge, got %d messages", len(rc.Messages))
	}
}

func TestNewChannelTelegramGroupUserMergeMiddleware_skipsMisalignedIDs(t *testing.T) {
	mw := NewChannelTelegramGroupUserMergeMiddleware()
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "a"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "b"}}},
	}
	rc := tgGroupRC(msgs, []uuid.UUID{uuid.New()})
	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })
	if len(rc.Messages) != 2 {
		t.Fatalf("expected unchanged count, got %d", len(rc.Messages))
	}
}

func TestNewChannelTelegramGroupUserMergeMiddleware_skipsUserWithToolCalls(t *testing.T) {
	mw := NewChannelTelegramGroupUserMergeMiddleware()
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.ContentPart{{Type: "text", Text: "a"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "u1"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "u2"}}, ToolCalls: []llm.ToolCall{{ToolName: "x"}}},
	}
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	rc := tgGroupRC(msgs, ids)
	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })
	if len(rc.Messages) != 3 {
		t.Fatalf("expected no merge, got %d", len(rc.Messages))
	}
}
