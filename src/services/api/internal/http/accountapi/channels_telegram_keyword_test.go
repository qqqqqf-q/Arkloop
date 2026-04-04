package accountapi

import (
	"strings"
	"testing"
)

func TestTelegramMessageMatchesKeyword(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		if telegramMessageMatchesKeyword(nil, []string{"hello"}) {
			t.Fatal("expected false for nil message")
		}
	})

	t.Run("empty keywords", func(t *testing.T) {
		msg := &telegramMessage{Text: "hello world"}
		if telegramMessageMatchesKeyword(msg, nil) {
			t.Fatal("expected false for nil keywords")
		}
		if telegramMessageMatchesKeyword(msg, []string{}) {
			t.Fatal("expected false for empty keywords")
		}
	})

	t.Run("empty text", func(t *testing.T) {
		msg := &telegramMessage{}
		if telegramMessageMatchesKeyword(msg, []string{"hello"}) {
			t.Fatal("expected false for empty text")
		}
	})

	t.Run("match text field", func(t *testing.T) {
		msg := &telegramMessage{Text: "hello world"}
		if !telegramMessageMatchesKeyword(msg, []string{"hello"}) {
			t.Fatal("expected match on text field")
		}
	})

	t.Run("match caption when text empty", func(t *testing.T) {
		msg := &telegramMessage{Caption: "check this photo"}
		if !telegramMessageMatchesKeyword(msg, []string{"photo"}) {
			t.Fatal("expected match on caption field")
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		msg := &telegramMessage{Text: "Hello World"}
		if !telegramMessageMatchesKeyword(msg, []string{"HELLO"}) {
			t.Fatal("expected case-insensitive match")
		}
	})

	t.Run("partial match", func(t *testing.T) {
		msg := &telegramMessage{Text: "playground fun"}
		if !telegramMessageMatchesKeyword(msg, []string{"ground"}) {
			t.Fatal("expected partial match")
		}
	})

	t.Run("no match", func(t *testing.T) {
		msg := &telegramMessage{Text: "hello world"}
		if telegramMessageMatchesKeyword(msg, []string{"foo", "bar"}) {
			t.Fatal("expected no match")
		}
	})
}

func TestBuildTelegramTriggerKeywords(t *testing.T) {
	t.Run("empty config", func(t *testing.T) {
		kw := buildTelegramTriggerKeywords(telegramChannelConfig{})
		if len(kw) != 0 {
			t.Fatalf("expected empty, got %v", kw)
		}
	})

	t.Run("trigger keywords only", func(t *testing.T) {
		kw := buildTelegramTriggerKeywords(telegramChannelConfig{
			TriggerKeywords: []string{"hello", "ping"},
		})
		if len(kw) != 2 {
			t.Fatalf("expected 2 keywords, got %v", kw)
		}
	})

	t.Run("bot first name appended", func(t *testing.T) {
		kw := buildTelegramTriggerKeywords(telegramChannelConfig{
			TriggerKeywords: []string{"hello"},
			BotFirstName:    "Alice",
		})
		if len(kw) != 2 {
			t.Fatalf("expected 2 keywords, got %v", kw)
		}
		if kw[1] != "alice" {
			t.Fatalf("expected bot name lowercase, got %q", kw[1])
		}
	})

	t.Run("dedup with bot first name", func(t *testing.T) {
		kw := buildTelegramTriggerKeywords(telegramChannelConfig{
			TriggerKeywords: []string{"Alice"},
			BotFirstName:    "alice",
		})
		if len(kw) != 1 {
			t.Fatalf("expected 1 keyword after dedup, got %v", kw)
		}
	})

	t.Run("case normalization", func(t *testing.T) {
		kw := buildTelegramTriggerKeywords(telegramChannelConfig{
			TriggerKeywords: []string{"HELLO"},
		})
		if kw[0] != "hello" {
			t.Fatalf("expected lowercase, got %q", kw[0])
		}
	})
}

func TestNormalizeTelegramTriggerKeywords(t *testing.T) {
	t.Run("filter empty values", func(t *testing.T) {
		kw := normalizeTelegramTriggerKeywords([]string{"hello", "", "  ", "world"})
		if len(kw) != 2 {
			t.Fatalf("expected 2, got %v", kw)
		}
	})

	t.Run("dedup", func(t *testing.T) {
		kw := normalizeTelegramTriggerKeywords([]string{"hello", "Hello", "HELLO"})
		if len(kw) != 1 {
			t.Fatalf("expected 1 after dedup, got %v", kw)
		}
	})

	t.Run("lowercase", func(t *testing.T) {
		kw := normalizeTelegramTriggerKeywords([]string{"FOO"})
		if kw[0] != "foo" {
			t.Fatalf("expected lowercase, got %q", kw[0])
		}
	})
}

func TestShouldCreateRun_MatchesKeyword(t *testing.T) {
	t.Run("group chat with keyword match", func(t *testing.T) {
		m := telegramIncomingMessage{
			ChatType:       "group",
			MatchesKeyword: true,
		}
		if !m.ShouldCreateRun() {
			t.Fatal("expected ShouldCreateRun=true when MatchesKeyword=true")
		}
	})

	t.Run("group chat without any trigger", func(t *testing.T) {
		m := telegramIncomingMessage{
			ChatType: "group",
		}
		if m.ShouldCreateRun() {
			t.Fatal("expected ShouldCreateRun=false with no triggers")
		}
	})
}

// 快速健全性检查：resolveTelegramMessageBody 优先 text，fallback caption
func TestResolveTelegramMessageBody(t *testing.T) {
	t.Run("prefer text over caption", func(t *testing.T) {
		body := resolveTelegramMessageBody(&telegramMessage{Text: "txt", Caption: "cap"})
		if !strings.Contains(body, "txt") {
			t.Fatalf("expected text priority, got %q", body)
		}
	})

	t.Run("fallback to caption", func(t *testing.T) {
		body := resolveTelegramMessageBody(&telegramMessage{Caption: "cap"})
		if body != "cap" {
			t.Fatalf("expected caption fallback, got %q", body)
		}
	})
}
