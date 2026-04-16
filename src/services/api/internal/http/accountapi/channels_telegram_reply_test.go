package accountapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestTelegramMessageRepliesToBot_usesNumericBotID(t *testing.T) {
	otherBot := int64(999001)
	ourBot := int64(777002)
	raw := json.RawMessage(`{
		"message_id": 1,
		"date": 1,
		"text": "hi",
		"chat": {"id": 1, "type": "supergroup"},
		"from": {"id": 100, "is_bot": false, "first_name": "U"},
		"reply_to_message": {
			"message_id": 9,
			"date": 1,
			"text": "old",
			"chat": {"id": 1, "type": "supergroup"},
			"from": {"id": ` + jsonInt(otherBot) + `, "is_bot": true, "first_name": "Other"}
		}
	}`)
	var msg telegramMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatal(err)
	}
	if telegramMessageRepliesToBot(&msg, ourBot) {
		t.Fatal("reply to another bot should not count")
	}
	msg.ReplyToMessage.From.ID = ourBot
	if !telegramMessageRepliesToBot(&msg, ourBot) {
		t.Fatal("reply to this bot should count")
	}
}

func TestTelegramMessageRepliesToBot_requiresBotIDWhenUnset(t *testing.T) {
	raw := json.RawMessage(`{
		"message_id": 1,
		"date": 1,
		"text": "hi",
		"chat": {"id": 1, "type": "supergroup"},
		"from": {"id": 100, "is_bot": false},
		"reply_to_message": {
			"message_id": 9,
			"date": 1,
			"text": "old",
			"chat": {"id": 1, "type": "supergroup"},
			"from": {"id": 50, "is_bot": true}
		}
	}`)
	var msg telegramMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatal(err)
	}
	if telegramMessageRepliesToBot(&msg, 0) {
		t.Fatal("reply should not count when telegram_bot_user_id is missing")
	}
}

func TestNormalizeTelegramIncomingMessage_extractsQuote(t *testing.T) {
	update := telegramUpdate{
		Message: &telegramMessage{
			MessageID: 14,
			Date:      1710000002,
			Text:      "继续说",
			Quote:     &telegramTextQuote{Text: "ENTP解析", Position: 3, IsManual: true},
			ReplyToMessage: &telegramMessage{
				MessageID: 11,
				Date:      1710000001,
				Text:      "bot old message with more context",
				Chat: telegramChat{
					ID:   -20001,
					Type: "supergroup",
				},
				From: &telegramUser{
					ID:        777002,
					IsBot:     true,
					FirstName: strPtr("Arkloop"),
				},
			},
			Chat: telegramChat{
				ID:   -20001,
				Type: "supergroup",
			},
			From: &telegramUser{
				ID:        10001,
				IsBot:     false,
				FirstName: strPtr("Alice"),
			},
		},
	}

	incoming, err := normalizeTelegramIncomingMessage(uuid.New(), "telegram", []byte(`{}`), update, "arkloopbot", 777002, nil)
	if err != nil {
		t.Fatalf("normalizeTelegramIncomingMessage error: %v", err)
	}
	if incoming == nil {
		t.Fatal("expected incoming message")
	}
	if incoming.QuoteText != "ENTP解析" {
		t.Fatalf("QuoteText = %q", incoming.QuoteText)
	}
	if incoming.QuotePosition == nil || *incoming.QuotePosition != 3 {
		t.Fatalf("QuotePosition = %#v", incoming.QuotePosition)
	}
	if !incoming.QuoteIsManual {
		t.Fatal("expected QuoteIsManual=true")
	}
}

func TestBuildTelegramEnvelopeText_includesQuoteFields(t *testing.T) {
	replyToID := "11"
	position := 3
	text := buildTelegramEnvelopeText(
		uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		telegramIncomingMessage{
			PlatformUsername:  "alice",
			ChatType:          "supergroup",
			ConversationTitle: "Arkloop",
			ReplyToMsgID:      &replyToID,
			ReplyToPreview:    "Arkloop: 很长的预览",
			QuoteText:         "ENTP解析",
			QuotePosition:     &position,
			QuoteIsManual:     true,
			PlatformMsgID:     "14",
		},
		"Alice",
		"[Telegram in Arkloop] 继续说",
		inboundTimeContext{Local: "2024-03-09 00:00:01 [UTC+8]", UTC: "2024-03-08T16:00:01Z", TimeZone: "Asia/Singapore"},
	)
	if !strings.Contains(text, `quote-text: "ENTP解析"`) {
		t.Fatalf("expected quote-text in envelope, got %s", text)
	}
	if !strings.Contains(text, `quote-position: "3"`) {
		t.Fatalf("expected quote-position in envelope, got %s", text)
	}
	if !strings.Contains(text, `quote-is-manual: "true"`) {
		t.Fatalf("expected quote-is-manual in envelope, got %s", text)
	}
}

func jsonInt(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func strPtr(v string) *string {
	return &v
}

func TestTelegramPrivateChatAllowed_emptyAllowlistOpens(t *testing.T) {
	t.Helper()
	if !telegramPrivateChatAllowed(telegramChannelConfig{PrivateAllowedUserIDs: nil}, "5957030043") {
		t.Fatal("nil allowlist should allow any user id")
	}
	if !telegramPrivateChatAllowed(telegramChannelConfig{PrivateAllowedUserIDs: []string{}}, "123") {
		t.Fatal("empty allowlist should allow")
	}
	if telegramPrivateChatAllowed(telegramChannelConfig{PrivateAllowedUserIDs: []string{"0"}}, "5957030043") {
		t.Fatal("\"0\" is a literal Telegram user id, not a wildcard")
	}
	if !telegramPrivateChatAllowed(telegramChannelConfig{PrivateAllowedUserIDs: []string{"5957030043"}}, "5957030043") {
		t.Fatal("explicit id should match")
	}
}

func TestTelegramPrivateChatAllowed_legacyFallback(t *testing.T) {
	t.Helper()
	cfg := telegramChannelConfig{AllowedUserIDs: []string{"111", "222"}, PrivateAllowedUserIDs: nil}
	if !telegramPrivateChatAllowed(cfg, "111") {
		t.Fatal("should fall back to allowed_user_ids")
	}
	if telegramPrivateChatAllowed(cfg, "333") {
		t.Fatal("should deny when not in legacy list")
	}
}

func TestTelegramGroupChatAllowed_emptyAllowlistOpens(t *testing.T) {
	t.Helper()
	if !telegramGroupChatAllowed(telegramChannelConfig{AllowedGroupIDs: nil}, "-20001") {
		t.Fatal("nil group allowlist should allow any group")
	}
	if !telegramGroupChatAllowed(telegramChannelConfig{AllowedGroupIDs: []string{}}, "-20001") {
		t.Fatal("empty group allowlist should allow")
	}
}

func TestTelegramGroupChatAllowed_explicitGroupMatch(t *testing.T) {
	t.Helper()
	cfg := telegramChannelConfig{AllowedGroupIDs: []string{"-20001", "-20002"}}
	if !telegramGroupChatAllowed(cfg, "-20001") {
		t.Fatal("explicit group id should match")
	}
	if telegramGroupChatAllowed(cfg, "-20003") {
		t.Fatal("unlisted group should be denied")
	}
}
