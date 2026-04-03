package pipeline

import (
	"testing"
)

func TestParseTelegramChannelUXDefaults(t *testing.T) {
	ux := ParseTelegramChannelUX(nil)
	if !ux.TypingIndicator || ux.ReactionEmoji != "" || ux.BotUsername != "" || ux.BotFirstName != "" {
		t.Fatalf("got %+v", ux)
	}
	ux = ParseTelegramChannelUX([]byte(`{}`))
	if !ux.TypingIndicator || ux.ReactionEmoji != "" || ux.BotUsername != "" || ux.BotFirstName != "" {
		t.Fatalf("got %+v", ux)
	}
}

func TestParseTelegramChannelUXExplicit(t *testing.T) {
	raw := []byte(`{"telegram_typing_indicator":false,"telegram_reaction_emoji":"👍"}`)
	ux := ParseTelegramChannelUX(raw)
	if ux.TypingIndicator {
		t.Fatal("typing should be off")
	}
	if ux.ReactionEmoji != "👍" {
		t.Fatalf("emoji: %q", ux.ReactionEmoji)
	}
}

func TestParseTelegramChannelUXBotIdentity(t *testing.T) {
	raw := []byte(`{"bot_username":"kira_bot","bot_first_name":"Kira"}`)
	ux := ParseTelegramChannelUX(raw)
	if ux.BotUsername != "kira_bot" {
		t.Fatalf("bot_username: %q", ux.BotUsername)
	}
	if ux.BotFirstName != "Kira" {
		t.Fatalf("bot_first_name: %q", ux.BotFirstName)
	}
	if !ux.TypingIndicator {
		t.Fatal("typing default should be true")
	}
}

