package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
)

func TestBuildRuntimeContextBlock_IncludesTimeContextWithoutChannel(t *testing.T) {
	block := buildRuntimeContextBlock(context.Background(), &RunContext{
		Run: data.Run{AccountID: uuid.New()},
	})
	if !strings.Contains(block, "User Timezone: UTC") {
		t.Fatalf("expected timezone line, got %q", block)
	}
	if !strings.Contains(block, "User Local Now: ") {
		t.Fatalf("expected local now line, got %q", block)
	}
}

func TestFormatRuntimeLocalNow_FormatsOffset(t *testing.T) {
	got := formatRuntimeLocalNow(time.Date(2026, time.April, 9, 14, 11, 19, 0, time.UTC), "Asia/Shanghai")
	if got != "2026-04-09 22:11:19 [UTC+8]" {
		t.Fatalf("unexpected local now: %q", got)
	}
}

func TestFormatRuntimeLocalNowAmericaLosAngelesDST(t *testing.T) {
	got := formatRuntimeLocalNow(time.Date(2024, time.July, 4, 12, 0, 0, 0, time.UTC), "America/Los_Angeles")
	if got != "2024-07-04 05:00:00 [UTC-7]" {
		t.Fatalf("unexpected local now: %q", got)
	}
}

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
