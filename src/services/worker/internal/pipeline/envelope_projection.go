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
	QuoteText      string
	ForwardFrom    string
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
		QuoteText:      meta["quote-text"],
		ForwardFrom:    meta["forward-from"],
		Body:           body,
	}
}

func quoteSpeakerFromPreview(replyToPreview string) string {
	replyToPreview = strings.TrimSpace(replyToPreview)
	if replyToPreview == "" {
		return ""
	}
	head, _, ok := strings.Cut(replyToPreview, ":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(head)
}

func formatReplyQuoteBlock(replyToMsgID string, replyToPreview string, quoteText string) string {
	replyToMsgID = strings.TrimSpace(replyToMsgID)
	if replyToMsgID == "" {
		return ""
	}
	replyToPreview = strings.TrimSpace(replyToPreview)
	quoteText = strings.TrimSpace(quoteText)
	if quoteText != "" {
		if speaker := quoteSpeakerFromPreview(replyToPreview); speaker != "" {
			quoteText = speaker + ": " + quoteText
		}
		return "[引用 #" + replyToMsgID + "] " + quoteText + " [/引用]"
	}
	if replyToPreview == "" {
		return "[引用 #" + replyToMsgID + "]"
	}
	return "[引用 #" + replyToMsgID + "] " + replyToPreview + " [/引用]"
}

// formatNaturalPrefix 将 envelopeFields 格式化为简洁的聊天记录前缀。
//
//	Alice (#42):
//	[引用 #38] Bob: 昨天的方案不错 [/引用]
//	消息正文
func formatNaturalPrefix(f *envelopeFields) string {
	name := f.DisplayName
	if name == "" {
		name = "?"
	}
	var prefix string
	if f.MessageID != "" {
		prefix = "[#" + f.MessageID + "] " + name + ":"
	} else {
		prefix = name + ":"
	}
	parts := []string{prefix}
	if replyBlock := formatReplyQuoteBlock(f.ReplyToMsgID, f.ReplyToPreview, f.QuoteText); replyBlock != "" {
		parts = append(parts, replyBlock)
	}
	parts = append(parts, f.Body)
	return strings.Join(parts, "\n")
}

// projectGroupEnvelopes 遍历 rc.Messages，将 user 消息中的 YAML envelope 替换为自然语言前缀。
// 就地修改 rc.Messages 的 Content，对所有含 Telegram envelope 的消息生效（含非 channel run）。
func projectGroupEnvelopes(rc *RunContext) (projected, skipped int) {
	if rc == nil {
		return 0, 0
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
			skipped++
			continue
		}
		first.Text = formatNaturalPrefix(fields)
		rc.Messages[i].Content = parts
		projected++
	}
	return projected, skipped
}
