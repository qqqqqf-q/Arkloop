package pipeline

import (
	"strings"
)

// envelopeFields 从 YAML front-matter 中提取的字段。
type envelopeFields struct {
	DisplayName    string
	MessageID      string
	ReplyToMsgID   string
	ReplyToPreview string
	Body           string
}

// parseEnvelope 解析 "---\n...\n---\n" 包裹的 YAML front-matter，提取关键字段和消息正文。
// 复用 parseTelegramEnvelopeText 做底层解析，避免重复。
// 非 envelope 格式的文本返回 nil。
func parseEnvelope(text string) *envelopeFields {
	meta, body, ok := parseTelegramEnvelopeText(text)
	if !ok {
		return nil
	}
	return &envelopeFields{
		DisplayName:    meta["display-name"],
		MessageID:      meta["message-id"],
		ReplyToMsgID:   meta["reply-to-message-id"],
		ReplyToPreview: meta["reply-to-preview"],
		Body:           body,
	}
}

// formatNaturalPrefix 将 envelopeFields 格式化为简洁的聊天记录前缀。
//
//	Alice (#42, > #38 "Bob: 昨天的方案..."):
//	消息正文
func formatNaturalPrefix(f *envelopeFields) string {
	name := f.DisplayName
	if name == "" {
		name = "?"
	}
	var meta []string
	if f.MessageID != "" {
		meta = append(meta, "#"+f.MessageID)
	}
	if f.ReplyToMsgID != "" {
		replyPart := "> #" + f.ReplyToMsgID
		if f.ReplyToPreview != "" {
			replyPart += ` "` + f.ReplyToPreview + `"`
		}
		meta = append(meta, replyPart)
	}

	var prefix string
	if len(meta) > 0 {
		prefix = name + " (" + strings.Join(meta, ", ") + "):"
	} else {
		prefix = name + ":"
	}
	return prefix + "\n" + f.Body
}

// projectGroupEnvelopes 遍历 rc.Messages，将 user 消息中的 YAML envelope 替换为自然语言前缀。
// 仅在群聊中调用，就地修改 rc.Messages 的 Content。
// envelope 信息总是位于 Content[0]（buildTelegramEnvelopeText 的产出格式保证）。
func projectGroupEnvelopes(rc *RunContext) {
	if rc == nil {
		return
	}
	for i := range rc.Messages {
		if rc.Messages[i].Role != "user" {
			continue
		}
		parts := rc.Messages[i].Content
		if len(parts) == 0 {
			continue
		}
		first := &parts[0]
		if first.Kind() != "text" || !strings.HasPrefix(first.Text, "---\n") {
			continue
		}
		fields := parseEnvelope(first.Text)
		if fields == nil {
			continue
		}
		first.Text = formatNaturalPrefix(fields)
		rc.Messages[i].Content = parts
	}
}
