package pipeline

import (
	"encoding/json"
	"strings"
)

// TelegramChannelUX is parsed from channels.config_json (telegram-specific UX flags).
type TelegramChannelUX struct {
	TypingIndicator bool
	ReactionEmoji   string
}

// ParseTelegramChannelUX reads optional keys:
//   - telegram_typing_indicator (bool, default true if absent)
//   - telegram_reaction_emoji (string, empty = off)
func ParseTelegramChannelUX(configJSON []byte) TelegramChannelUX {
	out := TelegramChannelUX{TypingIndicator: true}
	if len(configJSON) == 0 {
		return out
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(configJSON, &m); err != nil {
		return out
	}
	if raw, ok := m["telegram_typing_indicator"]; ok {
		var v bool
		if err := json.Unmarshal(raw, &v); err == nil {
			out.TypingIndicator = v
		}
	}
	if raw, ok := m["telegram_reaction_emoji"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			out.ReactionEmoji = strings.TrimSpace(s)
		}
	}
	return out
}
