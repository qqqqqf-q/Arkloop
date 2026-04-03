package pipeline

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/shared/messagecontent"
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
	if got := len(rc.Messages[1].Content); got != 1 {
		t.Fatalf("expected merged burst to collapse to 1 text part, got %d", got)
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
	tailVariants := userMessageScanTextVariants(llm.Message{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "three"}}})
	if len(rc.InjectionScanUserTexts) != len(tailVariants) {
		t.Fatalf("InjectionScanUserTexts len=%d want %d (%#v)", len(rc.InjectionScanUserTexts), len(tailVariants), rc.InjectionScanUserTexts)
	}
	for i := range tailVariants {
		if rc.InjectionScanUserTexts[i] != tailVariants[i] {
			t.Fatalf("InjectionScanUserTexts[%d]=%q want %q", i, rc.InjectionScanUserTexts[i], tailVariants[i])
		}
	}
}

func TestNewChannelTelegramGroupUserMergeMiddleware_compactsTelegramEnvelopeBurst(t *testing.T) {
	mw := NewChannelTelegramGroupUserMergeMiddleware()
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.ContentPart{{Type: "text", Text: "bot"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "A ck"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "3e4496b5-9544-4669-b4a7-790b11224c3e"
platform-username: "kilockok"
conversation-title: "Arkloop"
time: "2026-03-28T13:31:00Z"
---
[Telegram in Arkloop] xhelogo`}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "A ck"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "3e4496b5-9544-4669-b4a7-790b11224c3e"
platform-username: "kilockok"
conversation-title: "Arkloop"
time: "2026-03-28T13:31:05Z"
---
[Telegram in Arkloop] 怎么那么像`}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "清凤"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "509cb603-ae05-43f1-be4b-a8728a68e16f"
platform-username: "chiffoncha"
conversation-title: "Arkloop"
time: "2026-03-28T13:31:16Z"
---
[Telegram in Arkloop] 哈`}}},
	}
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	rc := tgGroupRC(msgs, ids)

	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })

	if len(rc.Messages) != 2 {
		t.Fatalf("expected assistant + 1 merged user, got %d", len(rc.Messages))
	}
	if got := len(rc.Messages[1].Content); got != 1 {
		t.Fatalf("expected compacted envelope burst to be 1 text part, got %d", got)
	}
	text := llm.PartPromptText(rc.Messages[1].Content[0])
	if strings.Contains(text, "---") {
		t.Fatalf("expected compacted burst to omit yaml separators, got %q", text)
	}
	for _, forbidden := range []string{`platform-username:`, `sender-ref:`, `[Telegram in Arkloop]`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("expected compacted burst to omit %q, got %q", forbidden, text)
		}
	}
	for _, want := range []string{
		`Telegram supergroup "Arkloop"`,
		`[13:31:00-13:31:05] A ck:`,
		`  xhelogo`,
		`  怎么那么像`,
		`[13:31:16] 清凤: 哈`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected compacted burst to contain %q, got %q", want, text)
		}
	}
}

func TestCompactTelegramGroupEnvelopeBurst_mergesConsecutiveMessagesFromSameSpeaker(t *testing.T) {
	tail := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "A ck"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "3e4496b5-9544-4669-b4a7-790b11224c3e"
conversation-title: "Arkloop"
time: "2026-03-28T13:31:00Z"
---
[Telegram in Arkloop] 第一条`}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "A ck"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "3e4496b5-9544-4669-b4a7-790b11224c3e"
conversation-title: "Arkloop"
time: "2026-03-28T13:31:05Z"
---
[Telegram in Arkloop] 第二条

换行`}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "清凤"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "509cb603-ae05-43f1-be4b-a8728a68e16f"
conversation-title: "Arkloop"
time: "2026-03-28T13:31:16Z"
---
[Telegram in Arkloop] 第三条`}}},
	}

	text, _, ok := compactTelegramGroupEnvelopeBurst(tail)
	if !ok {
		t.Fatal("expected telegram burst to compact")
	}
	for _, want := range []string{
		`Telegram supergroup "Arkloop"`,
		`[13:31:00-13:31:05] A ck:`,
		`  第一条`,
		`  第二条`,
		`  换行`,
		`[13:31:16] 清凤: 第三条`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected compacted burst to contain %q, got %q", want, text)
		}
	}
	if strings.Contains(text, `[13:31:05] A ck:`) {
		t.Fatalf("expected same speaker lines to merge, got %q", text)
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

func TestCompactTelegramGroupEnvelopeBurst_withImageParts(t *testing.T) {
	t.Parallel()
	tail := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{
			{Type: "text", Text: "---\ndisplay-name: \"A ck\"\nchannel: \"telegram\"\nconversation-type: \"supergroup\"\nsender-ref: \"3e4496b5-9544-4669-b4a7-790b11224c3e\"\nconversation-title: \"Arkloop\"\ntime: \"2026-03-28T13:31:00Z\"\n---\n[Telegram in Arkloop] look at this\n\n[图片: image.jpg]"},
			{Type: "image", Attachment: &messagecontent.AttachmentRef{Key: "k1", Filename: "image.jpg", MimeType: "image/jpeg"}, Data: []byte("fake")},
		}},
		{Role: "user", Content: []llm.ContentPart{
			{Type: "text", Text: "---\ndisplay-name: \"清凤\"\nchannel: \"telegram\"\nconversation-type: \"supergroup\"\nsender-ref: \"509cb603-ae05-43f1-be4b-a8728a68e16f\"\nconversation-title: \"Arkloop\"\ntime: \"2026-03-28T13:31:10Z\"\n---\n[Telegram in Arkloop] nice"},
		}},
	}

	text, extras, ok := compactTelegramGroupEnvelopeBurst(tail)
	if !ok {
		t.Fatal("expected compact to succeed for mixed text+image burst")
	}
	for _, want := range []string{
		"Telegram supergroup \"Arkloop\"",
		"[13:31:00] A ck:",
		"[图片: image.jpg]",
		"[13:31:10] 清凤: nice",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected compacted burst to contain %q, got %q", want, text)
		}
	}
	if len(extras) != 1 {
		t.Fatalf("expected 1 extra image part, got %d", len(extras))
	}
	if extras[0].Type != "image" || extras[0].Attachment.Key != "k1" {
		t.Fatalf("unexpected extra part: %+v", extras[0])
	}
}

func TestMergeUserBurstContent_compactsWithImageParts(t *testing.T) {
	t.Parallel()
	tail := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{
			{Type: "text", Text: "---\ndisplay-name: \"A ck\"\nchannel: \"telegram\"\nconversation-type: \"supergroup\"\nsender-ref: \"3e4496b5-9544-4669-b4a7-790b11224c3e\"\nconversation-title: \"Arkloop\"\ntime: \"2026-03-28T13:31:00Z\"\n---\n[Telegram in Arkloop] hello\n\n[图片: img.png]"},
			{Type: "image", Attachment: &messagecontent.AttachmentRef{Key: "k2", Filename: "img.png", MimeType: "image/png"}, Data: []byte("fake")},
		}},
		{Role: "user", Content: []llm.ContentPart{
			{Type: "text", Text: "---\ndisplay-name: \"A ck\"\nchannel: \"telegram\"\nconversation-type: \"supergroup\"\nsender-ref: \"3e4496b5-9544-4669-b4a7-790b11224c3e\"\nconversation-title: \"Arkloop\"\ntime: \"2026-03-28T13:31:05Z\"\n---\n[Telegram in Arkloop] world"},
		}},
	}

	parts := mergeUserBurstContent(tail)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (text + image), got %d", len(parts))
	}
	if parts[0].Type != "text" {
		t.Fatalf("expected first part to be text, got %q", parts[0].Type)
	}
	if !strings.Contains(parts[0].Text, "Telegram supergroup") {
		t.Fatalf("expected compact timeline in text, got %q", parts[0].Text)
	}
	if parts[1].Type != "image" || parts[1].Attachment.Key != "k2" {
		t.Fatalf("expected image part preserved, got %+v", parts[1])
	}
}

func TestMergeAll_compactsMiddleBurst(t *testing.T) {
	mw := NewChannelTelegramGroupUserMergeMiddleware()
	idA := uuid.New()
	idU1, idU2 := uuid.New(), uuid.New()
	idB := uuid.New()
	idU3 := uuid.New()
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.ContentPart{{Type: "text", Text: "bot1"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "x"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "y"}}},
		{Role: "assistant", Content: []llm.ContentPart{{Type: "text", Text: "bot2"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "z"}}},
	}
	rc := tgGroupRC(msgs, []uuid.UUID{idA, idU1, idU2, idB, idU3})
	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })
	// assistant + merged(x,y) + assistant + user(z)
	if len(rc.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(rc.Messages))
	}
	if rc.ThreadMessageIDs[1] != idU2 {
		t.Fatalf("middle burst should keep last id, got %v", rc.ThreadMessageIDs[1])
	}
	if rc.ThreadMessageIDs[3] != idU3 {
		t.Fatalf("tail single user should keep its id, got %v", rc.ThreadMessageIDs[3])
	}
}

func TestCompactSingleEnvelopeMessage(t *testing.T) {
	tail := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "清凤"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "509cb603-ae05-43f1-be4b-a8728a68e16f"
conversation-title: "Arkloop"
time: "2026-03-28T06:20:00Z"
admin: "true"
---
[Telegram in Arkloop] hello world`}}},
	}
	text, _, ok := compactTelegramGroupEnvelopeBurst(tail)
	if !ok {
		t.Fatal("expected single envelope message to compact")
	}
	for _, want := range []string{
		`Telegram supergroup "Arkloop"`,
		`[06:20:00] 清凤 [admin]: hello world`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in output, got %q", want, text)
		}
	}
	if strings.Contains(text, "---") {
		t.Fatalf("should not contain yaml separator, got %q", text)
	}
}

func TestCompactWithMessageIDs(t *testing.T) {
	tail := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "清凤"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "509cb603"
conversation-title: "Arkloop"
time: "2026-03-28T07:38:23Z"
admin: "true"
message-id: "4814"
---
[Telegram in Arkloop] first`}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "清凤"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "509cb603"
conversation-title: "Arkloop"
time: "2026-03-28T07:38:29Z"
admin: "true"
message-id: "4815"
---
[Telegram in Arkloop] second`}}},
	}
	text, _, ok := compactTelegramGroupEnvelopeBurst(tail)
	if !ok {
		t.Fatal("expected burst to compact")
	}
	if !strings.Contains(text, `[07:38:23-07:38:29 #4814,#4815] 清凤 [admin]:`) {
		t.Fatalf("expected message-ids in output, got %q", text)
	}
}

func TestCompactSingleMessageID(t *testing.T) {
	tail := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "k ilock"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "abc12345"
conversation-title: "Arkloop"
time: "2026-03-28T07:03:04Z"
message-id: "4812"
---
[Telegram in Arkloop] hello`}}},
	}
	text, _, ok := compactTelegramGroupEnvelopeBurst(tail)
	if !ok {
		t.Fatal("expected single msg to compact")
	}
	if !strings.Contains(text, `[07:03:04 #4812] k ilock: hello`) {
		t.Fatalf("expected message-id in output, got %q", text)
	}
}

func TestCompactNoMessageID(t *testing.T) {
	tail := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "old user"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "abc12345"
conversation-title: "Arkloop"
time: "2026-03-28T07:03:04Z"
---
[Telegram in Arkloop] legacy msg`}}},
	}
	text, _, ok := compactTelegramGroupEnvelopeBurst(tail)
	if !ok {
		t.Fatal("expected single msg to compact")
	}
	if !strings.Contains(text, `[07:03:04] old user: legacy msg`) {
		t.Fatalf("expected no message-id prefix, got %q", text)
	}
	if strings.Contains(text, "#") {
		t.Fatalf("should not contain # when no message-id, got %q", text)
	}
}

func TestCompactDifferentSpeakersEachGetMessageID(t *testing.T) {
	tail := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "A"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "aaa11111"
conversation-title: "Arkloop"
time: "2026-03-28T10:00:00Z"
message-id: "100"
---
[Telegram in Arkloop] msg a`}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "B"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "bbb22222"
conversation-title: "Arkloop"
time: "2026-03-28T10:00:05Z"
message-id: "101"
---
[Telegram in Arkloop] msg b`}}},
	}
	text, _, ok := compactTelegramGroupEnvelopeBurst(tail)
	if !ok {
		t.Fatal("expected burst to compact")
	}
	if !strings.Contains(text, `[10:00:00 #100] A: msg a`) {
		t.Fatalf("expected A's message-id, got %q", text)
	}
	if !strings.Contains(text, `[10:00:05 #101] B: msg b`) {
		t.Fatalf("expected B's message-id, got %q", text)
	}
}

func TestFullScenarioFromSpec(t *testing.T) {
	mw := NewChannelTelegramGroupUserMergeMiddleware()
	ids := make([]uuid.UUID, 7)
	for i := range ids {
		ids[i] = uuid.New()
	}
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "清凤"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "509cb603"
conversation-title: "Arkloop"
time: "2026-03-28T06:20:00Z"
admin: "true"
message-id: "4810"
---
[Telegram in Arkloop]`}}},
		{Role: "assistant", Content: []llm.ContentPart{{Type: "text", Text: "回复1"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "k ilock"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "abc12345"
conversation-title: "Arkloop"
time: "2026-03-28T07:03:04Z"
message-id: "4812"
---
[Telegram in Arkloop]`}}},
		{Role: "assistant", Content: []llm.ContentPart{{Type: "text", Text: "回复2"}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: `---
display-name: "清凤"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "509cb603"
conversation-title: "Arkloop"
time: "2026-03-28T07:38:23Z"
admin: "true"
message-id: "4814"
---
[Telegram in Arkloop] 六个 cursor 同时干`}}},
		{Role: "user", Content: []llm.ContentPart{
			{Type: "text", Text: `---
display-name: "清凤"
channel: "telegram"
conversation-type: "supergroup"
sender-ref: "509cb603"
conversation-title: "Arkloop"
time: "2026-03-28T07:38:29Z"
admin: "true"
message-id: "4815"
---
[Telegram in Arkloop]`},
			{Type: "image", Attachment: &messagecontent.AttachmentRef{Key: "img1", Filename: "photo.jpg", MimeType: "image/jpeg"}, Data: []byte("fake")},
		}},
	}
	rc := tgGroupRC(msgs, ids[:6])
	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })

	// user(compact) + assistant + user(compact) + assistant + user(compact merge)
	if len(rc.Messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(rc.Messages))
	}

	// 第一条 user: 单条 compact，body 清理后为空
	u1text := llm.PartPromptText(rc.Messages[0].Content[0])
	if !strings.Contains(u1text, `Telegram supergroup "Arkloop"`) {
		t.Fatalf("u1 missing header, got %q", u1text)
	}
	if !strings.Contains(u1text, `#4810`) {
		t.Fatalf("u1 missing message-id, got %q", u1text)
	}
	if !strings.Contains(u1text, `清凤 [admin]`) {
		t.Fatalf("u1 missing speaker, got %q", u1text)
	}

	// 第三条 user: 单条 compact，body 清理后为空
	u2text := llm.PartPromptText(rc.Messages[2].Content[0])
	if !strings.Contains(u2text, `[07:03:04 #4812] k ilock`) {
		t.Fatalf("u2 missing content, got %q", u2text)
	}

	// 第五条 user: 两条 merged
	u3text := llm.PartPromptText(rc.Messages[4].Content[0])
	if !strings.Contains(u3text, `[07:38:23-07:38:29 #4814,#4815] 清凤 [admin]:`) {
		t.Fatalf("u3 missing merged header, got %q", u3text)
	}
	if !strings.Contains(u3text, `六个 cursor 同时干`) {
		t.Fatalf("u3 missing body, got %q", u3text)
	}
	// 图片 part 应保留
	hasImage := false
	for _, p := range rc.Messages[4].Content {
		if p.Type == "image" {
			hasImage = true
		}
	}
	if !hasImage {
		t.Fatal("u3 missing image part")
	}
}
