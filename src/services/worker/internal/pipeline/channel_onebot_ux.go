package pipeline

import (
	"encoding/json"
	"strings"
)

// OneBotChannelUX is parsed from channels.config_json (QQ-specific UX flags).
type OneBotChannelUX struct {
	ReactionEmojiID string
}

// ParseOneBotChannelUX reads optional keys from channel config:
//   - reaction_emoji_id (string, empty = off) — QQ face ID for auto-reaction
func ParseOneBotChannelUX(configJSON []byte) OneBotChannelUX {
	var out OneBotChannelUX
	if len(configJSON) == 0 {
		return out
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(configJSON, &m); err != nil {
		return out
	}
	if raw, ok := m["reaction_emoji_id"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			out.ReactionEmojiID = strings.TrimSpace(s)
		}
	}
	return out
}
