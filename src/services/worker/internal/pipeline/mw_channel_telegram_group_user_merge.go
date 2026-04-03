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

// NewChannelTelegramGroupUserMergeMiddleware 遍历整个消息列表，将每一段连续 user 消息
// compact 为单条 user 再交给后续中间件与 LLM。每段 burst 的 ThreadMessageIDs 仅保留该段最后一条 id。
// InjectionScanUserTexts 仍取物理上最后一条 user 输入。
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
		msgs, ids, lastScan := mergeAllTelegramGroupUserBursts(rc.Messages, rc.ThreadMessageIDs)
		rc.Messages = msgs
		rc.ThreadMessageIDs = ids
		if len(lastScan) > 0 {
			rc.InjectionScanUserTexts = lastScan
		}
		return next(ctx, rc)
	}
}

// mergeAllTelegramGroupUserBursts 遍历全部消息，对每一段连续 user 消息都做 compact。
// 每段 burst 的 ThreadMessageIDs 只保留该段最后一条 id。
// lastScan 取物理上最后一条 user 消息的 scan text。
func mergeAllTelegramGroupUserBursts(msgs []llm.Message, ids []uuid.UUID) ([]llm.Message, []uuid.UUID, []string) {
	if len(msgs) != len(ids) {
		return msgs, ids, nil
	}

	outMsgs := make([]llm.Message, 0, len(msgs))
	outIDs := make([]uuid.UUID, 0, len(ids))
	var lastScan []string

	i := 0
	for i < len(msgs) {
		if !isPlainUserMessage(msgs[i]) {
			outMsgs = append(outMsgs, msgs[i])
			outIDs = append(outIDs, ids[i])
			i++
			continue
		}
		// 收集连续 user burst
		burstStart := i
		for i < len(msgs) && isPlainUserMessage(msgs[i]) {
			i++
		}
		burst := msgs[burstStart:i]
		burstIDs := ids[burstStart:i]

		lastScan = userMessageScanTextVariants(burst[len(burst)-1])

		if len(burst) == 1 {
			content := compactSingleUserMessage(burst[0])
			if content != nil {
				outMsgs = append(outMsgs, llm.Message{Role: "user", Content: content})
			} else {
				outMsgs = append(outMsgs, burst[0])
			}
			outIDs = append(outIDs, burstIDs[0])
			continue
		}

		mergedContent := mergeUserBurstContent(burst)
		outMsgs = append(outMsgs, llm.Message{Role: "user", Content: mergedContent})
		outIDs = append(outIDs, burstIDs[len(burstIDs)-1])
	}

	return outMsgs, outIDs, lastScan
}

func isPlainUserMessage(m llm.Message) bool {
	return strings.EqualFold(strings.TrimSpace(m.Role), "user") && len(m.ToolCalls) == 0
}

// compactSingleUserMessage 尝试将单条 telegram envelope user 消息 compact 化。
func compactSingleUserMessage(msg llm.Message) []llm.ContentPart {
	compacted, extras, ok := compactTelegramGroupEnvelopeBurst([]llm.Message{msg})
	if !ok {
		return nil
	}
	parts := []llm.ContentPart{{Type: messagecontent.PartTypeText, Text: compacted}}
	parts = append(parts, extras...)
	return parts
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

// telegramCompactBurstEntry 存储单条消息在 burst block 中的内容和 reply 信息。
type telegramCompactBurstEntry struct {
	body         string
	time         string // 完整时间 "15:04:05"
	messageID    string
	replyToID    string
	replyPreview string
}

type telegramCompactBurstBlock struct {
	startTime  string
	endTime    string
	speaker    string
	entries    []telegramCompactBurstEntry
	messageIDs []string
}

// compactTelegramGroupEnvelopeBurst 将 telegram envelope 消息合并为紧凑时间线。
// 支持单条和多条消息。返回 compact 文本、非 text parts（图片/文件等）和成功标志。
func compactTelegramGroupEnvelopeBurst(tail []llm.Message) (string, []llm.ContentPart, bool) {
	if len(tail) == 0 {
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

	header := fmt.Sprintf("Telegram %s", conversationType)
	if conversationTitle != "" {
		header += fmt.Sprintf(" %q", conversationTitle)
	}
	lines := []string{header}
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
		msgID := strings.TrimSpace(item.meta["message-id"])
		entry := telegramCompactBurstEntry{
			body:         item.body,
			time:         ts,
			messageID:    msgID,
			replyToID:    strings.TrimSpace(item.meta["reply-to-message-id"]),
			replyPreview: strings.TrimSpace(item.meta["reply-to-preview"]),
		}
		if len(blocks) > 0 && blocks[len(blocks)-1].speaker == speaker {
			blocks[len(blocks)-1].endTime = ts
			blocks[len(blocks)-1].entries = append(blocks[len(blocks)-1].entries, entry)
			if msgID != "" {
				blocks[len(blocks)-1].messageIDs = append(blocks[len(blocks)-1].messageIDs, msgID)
			}
			continue
		}
		var mids []string
		if msgID != "" {
			mids = []string{msgID}
		}
		blocks = append(blocks, telegramCompactBurstBlock{
			startTime:  ts,
			endTime:    ts,
			speaker:    speaker,
			entries:    []telegramCompactBurstEntry{entry},
			messageIDs: mids,
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
	if len(meta) == 0 {
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

func renderCompactTelegramBurstLine(ts, msgIDSuffix, speaker string, entry telegramCompactBurstEntry) string {
	text := strings.TrimSpace(entry.body)
	replyLine := ""
	if entry.replyToID != "" {
		replyLine = "> #" + entry.replyToID
		if entry.replyPreview != "" {
			replyLine += ` "` + entry.replyPreview + `"`
		}
	}
	if text == "" && replyLine == "" {
		return fmt.Sprintf("[%s%s] %s", ts, msgIDSuffix, speaker)
	}
	var sb strings.Builder
	sb.WriteString("[")
	sb.WriteString(ts)
	sb.WriteString(msgIDSuffix)
	sb.WriteString("] ")
	sb.WriteString(strings.TrimSpace(speaker))
	sb.WriteString(": ")
	if replyLine != "" {
		sb.WriteString(replyLine)
		sb.WriteString("\n  ")
	}
	lines := strings.Split(text, "\n")
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
	entries := make([]telegramCompactBurstEntry, 0, len(block.entries))
	for _, e := range block.entries {
		trimmed := strings.TrimSpace(e.body)
		if trimmed != "" || e.replyToID != "" {
			entries = append(entries, telegramCompactBurstEntry{
				body: trimmed, time: e.time, messageID: e.messageID,
				replyToID: e.replyToID, replyPreview: e.replyPreview,
			})
		}
	}
	tsRange := compactTelegramBurstRange(block.startTime, block.endTime)
	idSuffix := formatMessageIDSuffix(block.messageIDs)
	if len(entries) == 0 {
		return fmt.Sprintf("[%s%s] %s", tsRange, idSuffix, strings.TrimSpace(block.speaker))
	}
	if len(entries) == 1 {
		return renderCompactTelegramBurstLine(tsRange, idSuffix, block.speaker, entries[0])
	}
	// 合并 block：头部只放时间范围和说话人，每条消息带分钟级时间 + id
	var sb strings.Builder
	sb.WriteString("[")
	sb.WriteString(tsRange)
	sb.WriteString("] ")
	sb.WriteString(strings.TrimSpace(block.speaker))
	sb.WriteString(":")
	for _, entry := range entries {
		sb.WriteString("\n  ")
		sb.WriteString(entryMinuteTime(entry.time))
		if entry.messageID != "" {
			sb.WriteString(" #")
			sb.WriteString(entry.messageID)
		}
		sb.WriteString(", ")
		if entry.replyToID != "" {
			sb.WriteString("> #")
			sb.WriteString(entry.replyToID)
			if entry.replyPreview != "" {
				sb.WriteString(` "`)
				sb.WriteString(entry.replyPreview)
				sb.WriteString(`"`)
			}
			sb.WriteString("\n  ")
		}
		for i, line := range strings.Split(entry.body, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if i > 0 {
				sb.WriteString("\n  ")
			}
			sb.WriteString(trimmed)
		}
	}
	return sb.String()
}

// entryMinuteTime 将 "15:04:05" 缩短为 "15:04"。
func entryMinuteTime(ts string) string {
	if len(ts) >= 5 {
		return ts[:5]
	}
	return ts
}

func formatMessageIDSuffix(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(" #")
	for i, id := range ids {
		if i > 0 {
			sb.WriteString(",#")
		}
		sb.WriteString(id)
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
