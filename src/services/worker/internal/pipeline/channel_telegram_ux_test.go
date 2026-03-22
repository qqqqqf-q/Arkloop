package pipeline

import (
	"testing"
)

func TestParseTelegramChannelUXDefaults(t *testing.T) {
	ux := ParseTelegramChannelUX(nil)
	if !ux.TypingIndicator || ux.ReactionEmoji != "" {
		t.Fatalf("got %+v", ux)
	}
	ux = ParseTelegramChannelUX([]byte(`{}`))
	if !ux.TypingIndicator || ux.ReactionEmoji != "" {
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

