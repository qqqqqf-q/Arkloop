package accountapi

import (
	"testing"
)

func TestTelegramCommandBaseWorksForQQ(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantCmd string
		wantOK  bool
	}{
		{"simple command", "/help", "/help", true},
		{"command with args", "/bind abc123", "/bind", true},
		{"start command", "/start", "/start", true},
		{"new command", "/new", "/new", true},
		{"stop command", "/stop", "/stop", true},
		{"heartbeat command", "/heartbeat on", "/heartbeat", true},
		{"not a command", "hello world", "", false},
		{"empty string", "", "", false},
		{"slash only", "/", "/", true},
		{"uppercase command", "/HELP", "/HELP", true},
		{"command with at-sign but no bot", "/new@somebot", "", false},
		{"command without at-sign", "/new", "/new", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, ok := telegramCommandBase(tt.text, "")
			if ok != tt.wantOK {
				t.Fatalf("telegramCommandBase(%q, \"\") ok = %v, want %v", tt.text, ok, tt.wantOK)
			}
			if cmd != tt.wantCmd {
				t.Fatalf("telegramCommandBase(%q, \"\") cmd = %q, want %q", tt.text, cmd, tt.wantCmd)
			}
		})
	}
}

func TestTelegramLinkBootstrapAllowedCoversQQCommands(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"/help", true},
		{"/bind abc", true},
		{"/start", true},
		{"/start bind_xyz", true},
		{"/new", false},
		{"/stop", false},
		{"/heartbeat", false},
		{"hello", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := telegramLinkBootstrapAllowed(tt.text)
			if got != tt.want {
				t.Fatalf("telegramLinkBootstrapAllowed(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestQQUserAllowed(t *testing.T) {
	t.Run("allow all users when no allowlist", func(t *testing.T) {
		cfg := qqChannelConfig{AllowAllUsers: true}
		if !qqUserAllowed(cfg, "12345", "") {
			t.Fatal("expected allowed")
		}
	})

	t.Run("reject user not in allowlist", func(t *testing.T) {
		cfg := qqChannelConfig{AllowedUserIDs: []string{"111"}}
		if qqUserAllowed(cfg, "999", "") {
			t.Fatal("expected rejected")
		}
	})

	t.Run("allow user in allowlist", func(t *testing.T) {
		cfg := qqChannelConfig{AllowedUserIDs: []string{"111", "222"}}
		if !qqUserAllowed(cfg, "222", "") {
			t.Fatal("expected allowed")
		}
	})

	t.Run("allow group in allowlist", func(t *testing.T) {
		cfg := qqChannelConfig{AllowedGroupIDs: []string{"100001"}}
		if !qqUserAllowed(cfg, "999", "100001") {
			t.Fatal("expected allowed by group")
		}
	})

	t.Run("reject when group not in allowlist", func(t *testing.T) {
		cfg := qqChannelConfig{AllowedGroupIDs: []string{"100001"}}
		if qqUserAllowed(cfg, "999", "200001") {
			t.Fatal("expected rejected")
		}
	})
}

func TestResolveQQChannelConfig(t *testing.T) {
	t.Run("nil config allows all", func(t *testing.T) {
		cfg, err := resolveQQChannelConfig(nil)
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.AllowAllUsers {
			t.Fatal("expected AllowAllUsers=true for nil config")
		}
	})

	t.Run("empty lists allows all", func(t *testing.T) {
		cfg, err := resolveQQChannelConfig([]byte(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.AllowAllUsers {
			t.Fatal("expected AllowAllUsers=true for empty lists")
		}
	})

	t.Run("with allowlist", func(t *testing.T) {
		cfg, err := resolveQQChannelConfig([]byte(`{"allowed_user_ids":["123"]}`))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.AllowAllUsers {
			t.Fatal("expected AllowAllUsers=false when allowlist present")
		}
		if len(cfg.AllowedUserIDs) != 1 || cfg.AllowedUserIDs[0] != "123" {
			t.Fatalf("unexpected AllowedUserIDs: %v", cfg.AllowedUserIDs)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := resolveQQChannelConfig([]byte(`{invalid}`))
		if err == nil {
			t.Fatal("expected error for invalid json")
		}
	})
}

func TestQQIncomingShouldCreateRun(t *testing.T) {
	t.Run("private always creates run", func(t *testing.T) {
		m := qqIncomingMessage{ChatType: "private"}
		if !m.ShouldCreateRun() {
			t.Fatal("expected true for private")
		}
	})

	t.Run("group without mention or reply does not create run", func(t *testing.T) {
		m := qqIncomingMessage{ChatType: "group"}
		if m.ShouldCreateRun() {
			t.Fatal("expected false for group without triggers")
		}
	})

	t.Run("group with mention creates run", func(t *testing.T) {
		m := qqIncomingMessage{ChatType: "group", MentionsBot: true}
		if !m.ShouldCreateRun() {
			t.Fatal("expected true for group with mention")
		}
	})

	t.Run("group with reply to bot creates run", func(t *testing.T) {
		m := qqIncomingMessage{ChatType: "group", IsReplyToBot: true}
		if !m.ShouldCreateRun() {
			t.Fatal("expected true for group with reply to bot")
		}
	})
}

func TestBuildQQEnvelopeText(t *testing.T) {
	t.Run("private message envelope", func(t *testing.T) {
		incoming := qqIncomingMessage{
			PlatformMsgID: "12345",
			ChatType:      "private",
		}
		result := buildQQEnvelopeText(
			[16]byte{1}, "TestUser", "private", "hello", 1710000000, incoming,
		)
		if result == "" {
			t.Fatal("expected non-empty envelope")
		}
		for _, expected := range []string{
			`display-name: "TestUser"`,
			`channel: "qq"`,
			`conversation-type: "private"`,
			`message-id: "12345"`,
			"hello",
		} {
			if !contains(result, expected) {
				t.Fatalf("envelope missing %q, got:\n%s", expected, result)
			}
		}
	})

	t.Run("group message with mentions-bot", func(t *testing.T) {
		incoming := qqIncomingMessage{
			PlatformMsgID: "67890",
			ChatType:      "group",
			MentionsBot:   true,
		}
		result := buildQQEnvelopeText(
			[16]byte{2}, "GroupUser", "group", "hi bot", 0, incoming,
		)
		if !contains(result, `mentions-bot: true`) {
			t.Fatalf("expected mentions-bot in envelope, got:\n%s", result)
		}
	})

	t.Run("message with reply-to", func(t *testing.T) {
		replyID := "11111"
		incoming := qqIncomingMessage{
			PlatformMsgID: "22222",
			ChatType:      "group",
			ReplyToMsgID:  &replyID,
		}
		result := buildQQEnvelopeText(
			[16]byte{3}, "User", "group", "reply", 0, incoming,
		)
		if !contains(result, `reply-to-message-id: "11111"`) {
			t.Fatalf("expected reply-to-message-id in envelope, got:\n%s", result)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
