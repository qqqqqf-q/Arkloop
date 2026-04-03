package pipeline

import "testing"

func TestFormatBotIdentity(t *testing.T) {
	tests := []struct {
		name     string
		cc       *ChannelContext
		expected string
	}{
		{"both", &ChannelContext{BotDisplayName: "Kira", BotUsername: "kira_bot"}, "Kira (@kira_bot)"},
		{"username only", &ChannelContext{BotUsername: "kira_bot"}, "@kira_bot"},
		{"display name only", &ChannelContext{BotDisplayName: "Kira"}, "Kira"},
		{"empty", &ChannelContext{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBotIdentity(tt.cc)
			if got != tt.expected {
				t.Fatalf("got %q, want %q", got, tt.expected)
			}
		})
	}
}
