package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
)

// NewChannelTelegramGroupUserMergeMiddleware 将 Telegram 群聊线程尾部、自最后一条 assistant 起的连续多条 user
// 合并为单条 user 再交给后续中间件与 LLM。入库仍为每人一条；合并后 ThreadMessageIDs 仅保留尾段最后一条 user 的 id，
// 中间几条 id 不再出现在数组中（与 context compact 的 id 对齐语义一致）。
func NewChannelTelegramGroupUserMergeMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		_ = ctx
		if rc == nil || rc.ChannelContext == nil {
			return next(ctx, rc)
		}
		if strings.ToLower(strings.TrimSpace(rc.ChannelContext.ChannelType)) != "telegram" {
			return next(ctx, rc)
		}
		if !IsTelegramGroupLikeConversation(rc.ChannelContext.ConversationType) {
			return next(ctx, rc)
		}
		msgs, ids, lastTailScan := mergeTelegramGroupTrailingUserBurst(rc.Messages, rc.ThreadMessageIDs)
		rc.Messages = msgs
		rc.ThreadMessageIDs = ids
		if len(lastTailScan) > 0 {
			rc.InjectionScanUserTexts = lastTailScan
		}
		return next(ctx, rc)
	}
}

func mergeTelegramGroupTrailingUserBurst(msgs []llm.Message, ids []uuid.UUID) ([]llm.Message, []uuid.UUID, []string) {
	if len(msgs) != len(ids) || len(msgs) < 2 {
		return msgs, ids, nil
	}
	lastAsst := -1
	for i := range msgs {
		if strings.EqualFold(strings.TrimSpace(msgs[i].Role), "assistant") {
			lastAsst = i
		}
	}
	tailStart := lastAsst + 1
	tail := msgs[tailStart:]
	tailIDs := ids[tailStart:]
	if len(tail) < 2 {
		return msgs, ids, nil
	}
	for _, m := range tail {
		if !strings.EqualFold(strings.TrimSpace(m.Role), "user") {
			return msgs, ids, nil
		}
		if len(m.ToolCalls) > 0 {
			return msgs, ids, nil
		}
	}
	lastTailScan := userMessageScanTextVariants(tail[len(tail)-1])
	mergedContent := mergeUserBurstContent(tail)
	merged := llm.Message{
		Role:    "user",
		Content: mergedContent,
	}
	outMsgs := make([]llm.Message, 0, len(msgs)-len(tail)+1)
	outMsgs = append(outMsgs, msgs[:tailStart]...)
	outMsgs = append(outMsgs, merged)
	outIDs := make([]uuid.UUID, 0, len(ids)-len(tail)+1)
	outIDs = append(outIDs, ids[:tailStart]...)
	outIDs = append(outIDs, tailIDs[len(tailIDs)-1])
	return outMsgs, outIDs, lastTailScan
}

func mergeUserBurstContent(tail []llm.Message) []llm.ContentPart {
	if compacted, extras, ok := compactTelegramGroupEnvelopeBurst(tail); ok {
		parts := []llm.ContentPart{{Type: messagecontent.PartTypeText, Text: compacted}}
		parts = append(parts, extras...)
		return parts
	}
	if mergedText, ok := mergePureTextBurst(tail); ok {
		return []llm.ContentPart{{Type: messagecontent.PartTypeText, Text: mergedText}}
	}
	const sep = "\n\n"
	var parts []llm.ContentPart
	for i := range tail {
		if i > 0 {
			parts = append(parts, llm.ContentPart{Type: messagecontent.PartTypeText, Text: sep})
		}
		for _, p := range tail[i].Content {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return []llm.ContentPart{{Type: messagecontent.PartTypeText, Text: ""}}
	}
	return parts
}

type telegramEnvelopeMessage struct {
	meta map[string]string
	body string
}

type telegramCompactBurstBlock struct {
	startTime string
	endTime   string
	speaker   string
	bodies    []string
}

// compactTelegramGroupEnvelopeBurst 将多条 telegram envelope 消息合并为紧凑时间线。
// 返回 compact 文本、非 text parts（图片/文件等）和成功标志。
func compactTelegramGroupEnvelopeBurst(tail []llm.Message) (string, []llm.ContentPart, bool) {
	if len(tail) < 2 {
		return "", nil, false
	}
	items := make([]telegramEnvelopeMessage, 0, len(tail))
	var extraParts []llm.ContentPart
	for _, msg := range tail {
		text, nonTextParts, ok := extractEnvelopeText(msg)
		if !ok {
			return "", nil, false
		}
		meta, body, ok := parseTelegramEnvelopeText(text)
		if !ok {
			return "", nil, false
		}
		if !strings.EqualFold(strings.TrimSpace(meta["channel"]), "telegram") {
			return "", nil, false
		}
		body = compactTelegramEnvelopeBody(meta, body)
		if strings.TrimSpace(body) == "" && len(nonTextParts) == 0 {
			return "", nil, false
		}
		items = append(items, telegramEnvelopeMessage{meta: meta, body: body})
		extraParts = append(extraParts, nonTextParts...)
	}

	channel := commonEnvelopeValue(items, "channel")
	conversationType := commonEnvelopeValue(items, "conversation-type")
	if channel == "" || conversationType == "" {
		return "", nil, false
	}
	conversationTitle := commonEnvelopeValue(items, "conversation-title")
	messageThreadID := commonEnvelopeValue(items, "message-thread-id")

	nameRefs := map[string]map[string]struct{}{}
	for _, item := range items {
		name := strings.TrimSpace(item.meta["display-name"])
		ref := strings.TrimSpace(item.meta["sender-ref"])
		if name == "" {
			continue
		}
		bucket := nameRefs[name]
		if bucket == nil {
			bucket = map[string]struct{}{}
			nameRefs[name] = bucket
		}
		bucket[ref] = struct{}{}
	}

	lines := []string{
		fmt.Sprintf("Telegram %s", conversationType),
	}
	if conversationTitle != "" {
		lines = append(lines, fmt.Sprintf("title: %s", conversationTitle))
	}
	if messageThreadID != "" {
		lines = append(lines, fmt.Sprintf("thread: %s", messageThreadID))
	}

	blocks := make([]telegramCompactBurstBlock, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.meta["display-name"])
		duplicateDisplay := false
		if refs := nameRefs[name]; len(refs) > 1 {
			duplicateDisplay = true
		}
		speaker := compactTelegramBurstSpeaker(item.meta, duplicateDisplay)
		ts := compactTelegramBurstTime(item.meta["time"])
		if len(blocks) > 0 && blocks[len(blocks)-1].speaker == speaker {
			blocks[len(blocks)-1].endTime = ts
			blocks[len(blocks)-1].bodies = append(blocks[len(blocks)-1].bodies, item.body)
			continue
		}
		blocks = append(blocks, telegramCompactBurstBlock{
			startTime: ts,
			endTime:   ts,
			speaker:   speaker,
			bodies:    []string{item.body},
		})
	}

	bodyLines := make([]string, 0, len(blocks))
	for _, block := range blocks {
		bodyLines = append(bodyLines, renderCompactTelegramBurstBlock(block))
	}

	return strings.Join(append(lines, "", strings.Join(bodyLines, "\n")), "\n"), extraParts, true
}

func mergePureTextBurst(tail []llm.Message) (string, bool) {
	if len(tail) == 0 {
		return "", false
	}
	texts := make([]string, 0, len(tail))
	for _, msg := range tail {
		text, ok := singleTextMessage(msg)
		if !ok {
			return "", false
		}
		texts = append(texts, text)
	}
	return strings.Join(texts, "\n\n"), true
}

func singleTextMessage(msg llm.Message) (string, bool) {
	if len(msg.Content) == 0 {
		return "", false
	}
	var sb strings.Builder
	for _, part := range msg.Content {
		if !strings.EqualFold(strings.TrimSpace(part.Type), messagecontent.PartTypeText) {
			return "", false
		}
		sb.WriteString(part.Text)
	}
	return sb.String(), true
}

// extractEnvelopeText 从消息中提取 text 部分和非 text parts。
// text 部分用于 envelope 解析，非 text parts（图片/文件）原样保留。
func extractEnvelopeText(msg llm.Message) (string, []llm.ContentPart, bool) {
	if len(msg.Content) == 0 {
		return "", nil, false
	}
	var sb strings.Builder
	var nonText []llm.ContentPart
	for _, part := range msg.Content {
		if strings.EqualFold(strings.TrimSpace(part.Type), messagecontent.PartTypeText) {
			sb.WriteString(part.Text)
		} else {
			nonText = append(nonText, part)
		}
	}
	text := sb.String()
	if strings.TrimSpace(text) == "" && len(nonText) == 0 {
		return "", nil, false
	}
	return text, nonText, true
}

func parseTelegramEnvelopeText(text string) (map[string]string, string, bool) {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return nil, "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(normalized, "---\n"), "\n---\n", 2)
	if len(parts) != 2 {
		return nil, "", false
	}
	meta := map[string]string{}
	for _, line := range strings.Split(parts[0], "\n") {
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		if key == "" || value == "" {
			continue
		}
		meta[key] = strings.Trim(value, `"`)
	}
	body := strings.TrimSpace(parts[1])
	if len(meta) == 0 || body == "" {
		return nil, "", false
	}
	return meta, body, true
}

func commonEnvelopeValue(items []telegramEnvelopeMessage, key string) string {
	if len(items) == 0 {
		return ""
	}
	first := strings.TrimSpace(items[0].meta[key])
	if first == "" {
		return ""
	}
	for _, item := range items[1:] {
		if strings.TrimSpace(item.meta[key]) != first {
			return ""
		}
	}
	return first
}

func compactTelegramEnvelopeBody(meta map[string]string, body string) string {
	cleaned := strings.TrimSpace(body)
	title := strings.TrimSpace(meta["conversation-title"])
	if title != "" {
		prefix := "[Telegram in " + title + "]"
		if strings.HasPrefix(cleaned, prefix) {
			cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, prefix))
		}
	}
	if strings.HasPrefix(cleaned, "[Telegram]") {
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "[Telegram]"))
	}
	return cleaned
}

func compactTelegramBurstSpeaker(meta map[string]string, duplicateDisplay bool) string {
	displayName := strings.TrimSpace(meta["display-name"])
	shortRef := compactTelegramSenderRef(meta["sender-ref"])
	isAdmin := strings.TrimSpace(meta["admin"]) == "true"
	var speaker string
	switch {
	case displayName == "" && shortRef == "":
		speaker = "user"
	case displayName == "":
		speaker = shortRef
	case duplicateDisplay && shortRef != "":
		speaker = displayName + " <" + shortRef + ">"
	default:
		speaker = displayName
	}
	if isAdmin {
		speaker += " [admin]"
	}
	return speaker
}

func compactTelegramSenderRef(ref string) string {
	cleaned := strings.TrimSpace(ref)
	if cleaned == "" {
		return ""
	}
	if len(cleaned) > 8 {
		return cleaned[:8]
	}
	return cleaned
}

func compactTelegramBurstTime(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return "time?"
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, cleaned); err == nil {
			return parsed.UTC().Format("15:04:05")
		}
	}
	return cleaned
}

func renderCompactTelegramBurstLine(ts, speaker, body string) string {
	text := strings.TrimSpace(body)
	if text == "" {
		return fmt.Sprintf("[%s] %s", ts, speaker)
	}
	lines := strings.Split(text, "\n")
	var sb strings.Builder
	sb.WriteString("[")
	sb.WriteString(ts)
	sb.WriteString("] ")
	sb.WriteString(strings.TrimSpace(speaker))
	sb.WriteString(": ")
	sb.WriteString(strings.TrimSpace(lines[0]))
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		sb.WriteString("\n  ")
		sb.WriteString(trimmed)
	}
	return sb.String()
}

func renderCompactTelegramBurstBlock(block telegramCompactBurstBlock) string {
	bodies := make([]string, 0, len(block.bodies))
	for _, body := range block.bodies {
		text := strings.TrimSpace(body)
		if text != "" {
			bodies = append(bodies, text)
		}
	}
	if len(bodies) == 0 {
		return renderCompactTelegramBurstLine(compactTelegramBurstRange(block.startTime, block.endTime), block.speaker, "")
	}
	if len(bodies) == 1 {
		return renderCompactTelegramBurstLine(compactTelegramBurstRange(block.startTime, block.endTime), block.speaker, bodies[0])
	}
	var sb strings.Builder
	sb.WriteString("[")
	sb.WriteString(compactTelegramBurstRange(block.startTime, block.endTime))
	sb.WriteString("] ")
	sb.WriteString(strings.TrimSpace(block.speaker))
	sb.WriteString(":")
	for _, body := range bodies {
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			sb.WriteString("\n  ")
			sb.WriteString(trimmed)
		}
	}
	return sb.String()
}

func compactTelegramBurstRange(start, end string) string {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	switch {
	case start == "":
		return end
	case end == "", start == end:
		return start
	default:
		return start + "-" + end
	}
}
