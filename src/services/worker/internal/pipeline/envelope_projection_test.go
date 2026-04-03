package pipeline

import (
	"testing"

	"arkloop/services/worker/internal/llm"
)

func TestParseEnvelope_withReply(t *testing.T) {
	text := "---\n" +
		`display-name: "Alice"` + "\n" +
		`channel: "telegram"` + "\n" +
		`conversation-type: "supergroup"` + "\n" +
		`reply-to-message-id: "38"` + "\n" +
		`reply-to-preview: "Bob: 昨天的方案不错"` + "\n" +
		`message-id: "42"` + "\n" +
		`time: "2026-04-03T10:00:00Z"` + "\n" +
		"---\n" +
		"我同意"

	f := parseEnvelope(text)
	if f == nil {
		t.Fatal("parseEnvelope returned nil")
	}
	if f.DisplayName != "Alice" {
		t.Fatalf("DisplayName = %q, want Alice", f.DisplayName)
	}
	if f.MessageID != "42" {
		t.Fatalf("MessageID = %q, want 42", f.MessageID)
	}
	if f.ReplyToMsgID != "38" {
		t.Fatalf("ReplyToMsgID = %q, want 38", f.ReplyToMsgID)
	}
	if f.ReplyToPreview != "Bob: 昨天的方案不错" {
		t.Fatalf("ReplyToPreview = %q", f.ReplyToPreview)
	}
	if f.Body != "我同意" {
		t.Fatalf("Body = %q, want 我同意", f.Body)
	}
}

func TestParseEnvelope_noReply(t *testing.T) {
	text := "---\n" +
		`display-name: "Charlie"` + "\n" +
		`channel: "telegram"` + "\n" +
		`message-id: "99"` + "\n" +
		`time: "2026-04-03T11:00:00Z"` + "\n" +
		"---\n" +
		"Hello world"

	f := parseEnvelope(text)
	if f == nil {
		t.Fatal("parseEnvelope returned nil")
	}
	if f.DisplayName != "Charlie" {
		t.Fatalf("DisplayName = %q", f.DisplayName)
	}
	if f.MessageID != "99" {
		t.Fatalf("MessageID = %q", f.MessageID)
	}
	if f.ReplyToMsgID != "" {
		t.Fatalf("ReplyToMsgID should be empty, got %q", f.ReplyToMsgID)
	}
	if f.Body != "Hello world" {
		t.Fatalf("Body = %q", f.Body)
	}
}

func TestParseEnvelope_notEnvelope(t *testing.T) {
	if f := parseEnvelope("just plain text"); f != nil {
		t.Fatal("expected nil for non-envelope text")
	}
	if f := parseEnvelope("---\nno closing marker"); f != nil {
		t.Fatal("expected nil for malformed envelope")
	}
}

func TestFormatNaturalPrefix_withReply(t *testing.T) {
	f := &envelopeFields{
		DisplayName:    "Alice",
		MessageID:      "42",
		ReplyToMsgID:   "38",
		ReplyToPreview: "Bob: 昨天的方案不错",
		Body:           "我同意",
	}
	got := formatNaturalPrefix(f)
	want := "Alice (#42, > #38 \"Bob: 昨天的方案不错\"):\n我同意"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatNaturalPrefix_noReply(t *testing.T) {
	f := &envelopeFields{
		DisplayName: "Charlie",
		MessageID:   "99",
		Body:        "Hello",
	}
	got := formatNaturalPrefix(f)
	want := "Charlie (#99):\nHello"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatNaturalPrefix_noMeta(t *testing.T) {
	f := &envelopeFields{
		DisplayName: "Dave",
		Body:        "test",
	}
	got := formatNaturalPrefix(f)
	want := "Dave:\ntest"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestProjectGroupEnvelopes_mixedMessages(t *testing.T) {
	envelopeText := "---\n" +
		`display-name: "Alice"` + "\n" +
		`reply-to-message-id: "10"` + "\n" +
		`reply-to-preview: "Bob: hi"` + "\n" +
		`message-id: "12"` + "\n" +
		"---\n" +
		"回复内容"

	rc := &RunContext{
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: envelopeText}}},
			{Role: "assistant", Content: []llm.ContentPart{{Type: "text", Text: "ok"}}},
			{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "plain message no envelope"}}},
		},
	}
	projectGroupEnvelopes(rc)

	// 第一条 user 消息应被投影
	got := rc.Messages[0].Content[0].Text
	want := "Alice (#12, > #10 \"Bob: hi\"):\n回复内容"
	if got != want {
		t.Fatalf("projected msg[0]:\n%s\nwant:\n%s", got, want)
	}

	// assistant 不动
	if rc.Messages[1].Content[0].Text != "ok" {
		t.Fatal("assistant message should not be modified")
	}

	// 无 envelope 的 user 不动
	if rc.Messages[2].Content[0].Text != "plain message no envelope" {
		t.Fatal("non-envelope user message should not be modified")
	}
}

func TestProjectGroupEnvelopes_escapedQuotes(t *testing.T) {
	// parseTelegramEnvelopeText 用 strings.Trim 去引号，不做反转义，
	// 所以 escaped quote 会保留 backslash。
	envelopeText := "---\n" +
		`display-name: "O\"Brien"` + "\n" +
		`message-id: "5"` + "\n" +
		"---\n" +
		"body"

	rc := &RunContext{
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: envelopeText}}},
		},
	}
	projectGroupEnvelopes(rc)

	got := rc.Messages[0].Content[0].Text
	want := "O\\\"Brien (#5):\nbody"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseEnvelope_bodyContainsDashes(t *testing.T) {
	text := "---\n" +
		`display-name: "Eve"` + "\n" +
		`message-id: "77"` + "\n" +
		"---\n" +
		"first line\n---\nsecond line"

	f := parseEnvelope(text)
	if f == nil {
		t.Fatal("parseEnvelope returned nil")
	}
	if f.DisplayName != "Eve" {
		t.Fatalf("DisplayName = %q", f.DisplayName)
	}
	// body 应完整保留，包括内部的 "---"
	if f.Body != "first line\n---\nsecond line" {
		t.Fatalf("Body = %q", f.Body)
	}
}

func TestParseEnvelope_emptyBody(t *testing.T) {
	text := "---\n" +
		`display-name: "Frank"` + "\n" +
		`message-id: "88"` + "\n" +
		"---\n"

	f := parseEnvelope(text)
	if f == nil {
		t.Fatal("parseEnvelope returned nil")
	}
	if f.Body != "" {
		t.Fatalf("Body should be empty, got %q", f.Body)
	}
}
