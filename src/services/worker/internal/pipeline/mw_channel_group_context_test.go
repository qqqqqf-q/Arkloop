package pipeline

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
)

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

func TestNewChannelGroupContextTrimMiddleware_skipsNonGroup(t *testing.T) {
	mw := NewChannelGroupContextTrimMiddleware()
	rc := &RunContext{
		ChannelContext: &ChannelContext{ConversationType: "private"},
		Messages:       []llm.Message{{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: strings.Repeat("a", 5000)}}}},
	}
	called := false
	err := mw(context.Background(), rc, func(context.Context, *RunContext) error {
		called = true
		if len(rc.Messages) != 1 {
			t.Fatalf("messages should not be trimmed for DM")
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
