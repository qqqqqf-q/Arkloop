package accountapi

import (
	"encoding/json"
	"testing"
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

func TestTelegramMessageRepliesToBot_fallsBackWhenBotIDUnset(t *testing.T) {
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
	if !telegramMessageRepliesToBot(&msg, 0) {
		t.Fatal("expected IsBot fallback when telegram_bot_user_id missing")
	}
}

func jsonInt(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
