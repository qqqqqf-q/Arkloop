package accountapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
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
			cmd, ok := slashCommandBase(tt.text, "")
			if ok != tt.wantOK {
				t.Fatalf("slashCommandBase(%q, \"\") ok = %v, want %v", tt.text, ok, tt.wantOK)
			}
			if cmd != tt.wantCmd {
				t.Fatalf("slashCommandBase(%q, \"\") cmd = %q, want %q", tt.text, cmd, tt.wantCmd)
			}
		})
	}
}

func TestAdaptTelegramGroupCommandTextRequiresBotMention(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		bot      string
		wantCmd  string
		wantText string
		wantOK   bool
	}{
		{"bare command ignored", "/help", "arkloopbot", "", "", false},
		{"matching mention accepted", "/help@arkloopbot", "arkloopbot", "/help", "/help", true},
		{"attached mention with args accepted", "/model@arkloopbot gpt-5", "arkloopbot", "/model", "/model gpt-5", true},
		{"space mention accepted", "/help @arkloopbot", "arkloopbot", "/help", "/help", true},
		{"space mention with args accepted", "/model @arkloopbot gpt-5", "arkloopbot", "/model", "/model gpt-5", true},
		{"bind command strips space mention", "/bind @arkloopbot abc123", "arkloopbot", "/bind", "/bind abc123", true},
		{"matching mention with at prefix", "/models@arkloopbot", "@arkloopbot", "/models", "/models", true},
		{"other bot ignored", "/help@otherbot", "arkloopbot", "", "", false},
		{"space mention other bot ignored", "/help @otherbot", "arkloopbot", "", "", false},
		{"missing bot username ignored", "/help@arkloopbot", "", "", "", false},
		{"full width slash accepted", "／new@arkloopbot", "arkloopbot", "/new", "/new", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, commandText, ok := adaptTelegramGroupCommandText(tt.text, tt.bot)
			if ok != tt.wantOK {
				t.Fatalf("adaptTelegramGroupCommandText(%q, %q) ok = %v, want %v", tt.text, tt.bot, ok, tt.wantOK)
			}
			if cmd != tt.wantCmd {
				t.Fatalf("adaptTelegramGroupCommandText(%q, %q) cmd = %q, want %q", tt.text, tt.bot, cmd, tt.wantCmd)
			}
			if commandText != tt.wantText {
				t.Fatalf("adaptTelegramGroupCommandText(%q, %q) commandText = %q, want %q", tt.text, tt.bot, commandText, tt.wantText)
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

func TestChannelCommandHelpTextUsesGroupMentions(t *testing.T) {
	groupHelp := channelCommandHelpText(false)
	if !strings.Contains(groupHelp, "/help@bot") || !strings.Contains(groupHelp, "/models@bot") {
		t.Fatalf("group help should show targeted commands: %q", groupHelp)
	}
	if strings.Contains(groupHelp, "\n/models —") {
		t.Fatalf("group help should not show bare management commands: %q", groupHelp)
	}

	privateHelp := channelCommandHelpText(true)
	if strings.Contains(privateHelp, "@bot") {
		t.Fatalf("private help should not show targeted commands: %q", privateHelp)
	}
	if strings.Contains(privateHelp, "/heartbeat") {
		t.Fatalf("private help should not include heartbeat: %q", privateHelp)
	}
}

func TestQQGroupCommandAuthorizationUsesArkloopOwner(t *testing.T) {
	t.Run("qq group role is not a fallback", func(t *testing.T) {
		handled, replyText, _, _, _, err := DispatchChannelCommand(
			context.Background(),
			nil,
			data.Channel{},
			data.Persona{},
			data.ChannelIdentity{},
			"/new",
			false,
			"20002",
			nil,
			ChannelCommandResolver{
				IsBoundAdmin: func(context.Context) bool { return false },
				IsGroupAdmin: func(context.Context) bool { return true },
			},
			ChannelCommandDeps{},
			"QQ",
		)
		if err != nil {
			t.Fatal(err)
		}
		if !handled || replyText != "无权限。" {
			t.Fatalf("expected Arkloop binding denial, handled=%v reply=%q", handled, replyText)
		}
	})

	t.Run("owner identity can use group command", func(t *testing.T) {
		handled, replyText, _, _, _, err := DispatchChannelCommand(
			context.Background(),
			nil,
			data.Channel{},
			data.Persona{},
			data.ChannelIdentity{},
			"/new",
			false,
			"20002",
			nil,
			ChannelCommandResolver{
				IsBoundAdmin: func(context.Context) bool { return true },
				IsGroupAdmin: func(context.Context) bool { return false },
			},
			ChannelCommandDeps{},
			"QQ",
		)
		if err != nil {
			t.Fatal(err)
		}
		if !handled || replyText == "无权限。" {
			t.Fatalf("expected owner identity to pass authorization, handled=%v reply=%q", handled, replyText)
		}
	})
}

func TestQQPrivateCommandDoesNotRequireArkloopOwner(t *testing.T) {
	personaID := uuid.New()
	handled, replyText, _, _, _, err := DispatchChannelCommand(
		context.Background(),
		nil,
		data.Channel{PersonaID: &personaID},
		data.Persona{},
		data.ChannelIdentity{},
		"/new",
		true,
		"10001",
		nil,
		ChannelCommandResolver{
			IsBoundAdmin: func(context.Context) bool { return false },
		},
		ChannelCommandDeps{},
		"QQ",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || replyText == "无权限。" {
		t.Fatalf("private QQ command should not require owner binding, handled=%v reply=%q", handled, replyText)
	}
}

func TestQQChannelIdentityIsOwnerRejectsNonOwner(t *testing.T) {
	ownerUserID := uuid.New()
	otherUserID := uuid.New()
	identityID := uuid.New()
	channelID := uuid.New()

	if qqChannelIdentityIsOwner(context.Background(), nil, data.Channel{
		ID:          channelID,
		OwnerUserID: &ownerUserID,
	}, data.ChannelIdentity{
		ID:     identityID,
		UserID: &otherUserID,
	}, nil) {
		t.Fatal("non-owner identity must not be treated as QQ channel owner")
	}
}

func TestQQChannelIdentityIsOwnerRejectsUnownedChannel(t *testing.T) {
	identityUserID := uuid.New()
	if qqChannelIdentityIsOwner(context.Background(), nil, data.Channel{
		ID: uuid.New(),
	}, data.ChannelIdentity{
		ID:     uuid.New(),
		UserID: &identityUserID,
	}, nil) {
		t.Fatal("unowned channel must not authorize QQ owner commands")
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

func TestQQAccessDeniedReply(t *testing.T) {
	if qqAccessDeniedReplyText != "此用户不在白名单中。" {
		t.Fatalf("unexpected reply text: %q", qqAccessDeniedReplyText)
	}

	t.Run("private", func(t *testing.T) {
		msgType, target := qqAccessDeniedReplyDestination("10001", "")
		if msgType != "private" || target != "10001" {
			t.Fatalf("unexpected destination: %s %s", msgType, target)
		}
	})

	t.Run("group", func(t *testing.T) {
		msgType, target := qqAccessDeniedReplyDestination("10001", "20002")
		if msgType != "group" || target != "20002" {
			t.Fatalf("unexpected destination: %s %s", msgType, target)
		}
	})
}

func TestQQAccessDeniedReplyTrigger(t *testing.T) {
	tests := []struct {
		name     string
		incoming qqIncomingMessage
		want     bool
	}{
		{
			name:     "private replies",
			incoming: qqIncomingMessage{ChatType: "private", Text: "hello"},
			want:     true,
		},
		{
			name:     "group ordinary message is silent",
			incoming: qqIncomingMessage{ChatType: "group", Text: "hello"},
			want:     false,
		},
		{
			name:     "group mention replies",
			incoming: qqIncomingMessage{ChatType: "group", Text: "hello", MentionsBot: true},
			want:     true,
		},
		{
			name:     "group keyword replies",
			incoming: qqIncomingMessage{ChatType: "group", Text: "草洛", MatchesKeyword: true},
			want:     true,
		},
		{
			name:     "group command replies",
			incoming: qqIncomingMessage{ChatType: "group", Text: "/new"},
			want:     true,
		},
		{
			name:     "unknown slash is silent",
			incoming: qqIncomingMessage{ChatType: "group", Text: "/unknown"},
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qqShouldReplyAccessDenied(tt.incoming); got != tt.want {
				t.Fatalf("qqShouldReplyAccessDenied() = %v, want %v", got, tt.want)
			}
		})
	}
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

	t.Run("allow all follows normalized allowlists", func(t *testing.T) {
		cfg, err := resolveQQChannelConfig([]byte(`{"allow_all_users":true,"allowed_user_ids":[" 123 ","123","456\n789"],"allowed_group_ids":[" 20001 "]}`))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.AllowAllUsers {
			t.Fatal("expected AllowAllUsers=false when allowlist present")
		}
		if got, want := strings.Join(cfg.AllowedUserIDs, ","), "123,456,789"; got != want {
			t.Fatalf("AllowedUserIDs = %q, want %q", got, want)
		}
		if got, want := strings.Join(cfg.AllowedGroupIDs, ","), "20001"; got != want {
			t.Fatalf("AllowedGroupIDs = %q, want %q", got, want)
		}
	})

	t.Run("normalizes persisted config", func(t *testing.T) {
		normalized, _, err := normalizeChannelConfigJSON("qq", []byte(`{"allow_all_users":true,"allowed_user_ids":["123"]}`))
		if err != nil {
			t.Fatal(err)
		}
		var cfg qqChannelConfig
		if err := json.Unmarshal(normalized, &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.AllowAllUsers {
			t.Fatal("expected stale allow_all_users to be cleared")
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
		m := InboundMessage{ChatType: "private"}
		if !m.ShouldCreateRun() {
			t.Fatal("expected true for private")
		}
	})

	t.Run("group without mention or reply does not create run", func(t *testing.T) {
		m := InboundMessage{ChatType: "group"}
		if m.ShouldCreateRun() {
			t.Fatal("expected false for group without triggers")
		}
	})

	t.Run("group with mention creates run", func(t *testing.T) {
		m := InboundMessage{ChatType: "group", MentionsBot: true}
		if !m.ShouldCreateRun() {
			t.Fatal("expected true for group with mention")
		}
	})

	t.Run("group with reply to bot creates run", func(t *testing.T) {
		m := InboundMessage{ChatType: "group", IsReplyToBot: true}
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
