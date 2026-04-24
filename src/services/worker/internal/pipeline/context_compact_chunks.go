package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/llm"

	"github.com/pkoukk/tiktoken-go"
)

type compactAtomType string

const (
	compactAtomUserText      compactAtomType = "user_text_atom"
	compactAtomAssistantText compactAtomType = "assistant_text_atom"
	compactAtomToolEpisode   compactAtomType = "tool_episode_atom"
)

const (
	compactChunkTokenLimit      = 320
	compactChunkFallbackRunes   = 1200
	compactToolPayloadMaxRunes  = 900
	compactToolArgsPreviewRunes = 320
	compactChunkFastPathRunes   = 24000
)

type compactChunk struct {
	ContextSeq int64
	AtomSeq    int
	AtomType   compactAtomType
	Role       string
	Text       string
}

func buildCanonicalCompactChunks(enc *tiktoken.Tiktoken, msgs []llm.Message) []compactChunk {
	if len(msgs) == 0 {
		return nil
	}
	chunks := make([]compactChunk, 0, len(msgs))
	nextContextSeq := int64(1)
	nextAtomSeq := 1
	for i := 0; i < len(msgs); {
		msg := msgs[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			end := i + 1
			for end < len(msgs) && strings.TrimSpace(msgs[end].Role) == "tool" {
				end++
			}
			payload := serializeToolEpisodeForCompact(msgs[i:end])
			for _, part := range splitCompactPayload(enc, payload) {
				chunks = append(chunks, compactChunk{
					ContextSeq: nextContextSeq,
					AtomSeq:    nextAtomSeq,
					AtomType:   compactAtomToolEpisode,
					Role:       "assistant",
					Text:       part,
				})
				nextContextSeq++
			}
			nextAtomSeq++
			i = end
			continue
		}
		if strings.TrimSpace(msg.Role) == "tool" {
			payload := serializeSingleToolForCompact(msg)
			for _, part := range splitCompactPayload(enc, payload) {
				chunks = append(chunks, compactChunk{
					ContextSeq: nextContextSeq,
					AtomSeq:    nextAtomSeq,
					AtomType:   compactAtomToolEpisode,
					Role:       "tool",
					Text:       part,
				})
				nextContextSeq++
			}
			nextAtomSeq++
			i++
			continue
		}

		rawText := strings.TrimSpace(messageText(msg))
		if rawText == "" {
			rawText = compactFallbackContentText(msg)
		}
		if rawText == "" {
			i++
			continue
		}
		atomType := compactAtomAssistantText
		if strings.TrimSpace(msg.Role) == "user" {
			atomType = compactAtomUserText
		}
		for _, part := range splitCompactPayload(enc, rawText) {
			chunks = append(chunks, compactChunk{
				ContextSeq: nextContextSeq,
				AtomSeq:    nextAtomSeq,
				AtomType:   atomType,
				Role:       msg.Role,
				Text:       part,
			})
			nextContextSeq++
		}
		nextAtomSeq++
		i++
	}
	return chunks
}

func splitCompactPayload(enc *tiktoken.Tiktoken, payload string) []string {
	payload = strings.TrimSpace(strings.ReplaceAll(payload, "\r\n", "\n"))
	if payload == "" {
		return nil
	}
	blocks := strings.Split(payload, "\n\n")
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		if shouldUseCompactRuneFallback(block) {
			out = append(out, splitCompactBlockByRunes(block)...)
			continue
		}
		if compactTokenCount(enc, block) <= compactChunkTokenLimit {
			out = append(out, block)
			continue
		}
		out = append(out, splitCompactBlockByToken(enc, block)...)
	}
	if len(out) == 0 {
		return []string{payload}
	}
	return out
}

func splitCompactBlockByToken(enc *tiktoken.Tiktoken, block string) []string {
	block = strings.TrimSpace(block)
	if block == "" {
		return nil
	}
	if enc == nil || shouldUseCompactRuneFallback(block) {
		return splitCompactBlockByRunes(block)
	}

	encoded := enc.Encode(block, nil, nil)
	if len(encoded) <= compactChunkTokenLimit {
		return []string{block}
	}
	out := make([]string, 0, (len(encoded)/compactChunkTokenLimit)+1)
	for start := 0; start < len(encoded); start += compactChunkTokenLimit {
		end := start + compactChunkTokenLimit
		if end > len(encoded) {
			end = len(encoded)
		}
		part := strings.TrimSpace(enc.Decode(encoded[start:end]))
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return []string{block}
	}
	return out
}

func compactTokenCount(enc *tiktoken.Tiktoken, text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	if enc == nil || shouldUseCompactRuneFallback(text) {
		return approxTokensFromText(text)
	}
	return len(enc.Encode(text, nil, nil))
}

func splitCompactBlockByRunes(block string) []string {
	runes := []rune(strings.TrimSpace(block))
	if len(runes) == 0 {
		return nil
	}
	out := make([]string, 0, (len(runes)/compactChunkFallbackRunes)+1)
	for start := 0; start < len(runes); start += compactChunkFallbackRunes {
		end := start + compactChunkFallbackRunes
		if end > len(runes) {
			end = len(runes)
		}
		part := strings.TrimSpace(string(runes[start:end]))
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return []string{strings.TrimSpace(block)}
	}
	return out
}

func shouldUseCompactRuneFallback(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	return utf8.RuneCountInString(text) > compactChunkFastPathRunes
}

func serializeToolEpisodeForCompact(msgs []llm.Message) string {
	if len(msgs) == 0 {
		return ""
	}
	var parts []string
	head := msgs[0]
	if strings.TrimSpace(head.Role) == "assistant" && len(head.ToolCalls) > 0 {
		calls := make([]string, 0, len(head.ToolCalls))
		for _, call := range head.ToolCalls {
			name := strings.TrimSpace(call.ToolName)
			if name == "" {
				name = "tool"
			}
			item := name
			if len(call.ArgumentsJSON) > 0 {
				if raw, err := json.Marshal(call.ArgumentsJSON); err == nil {
					item += "(" + compactRunePreview(string(raw), compactToolArgsPreviewRunes) + ")"
				}
			}
			calls = append(calls, item)
		}
		if len(calls) > 0 {
			parts = append(parts, "[Assistant tool calls]: "+strings.Join(calls, "; "))
		}
	}
	for _, toolMsg := range msgs[1:] {
		if strings.TrimSpace(toolMsg.Role) != "tool" {
			continue
		}
		parts = append(parts, serializeSingleToolForCompact(toolMsg))
	}
	if len(parts) == 0 {
		return strings.TrimSpace(serializeSingleToolForCompact(msgs[len(msgs)-1]))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func serializeSingleToolForCompact(msg llm.Message) string {
	text := strings.TrimSpace(messageText(msg))
	if text == "" {
		return ""
	}
	label := "[Tool result]"
	payload := text
	var envelope map[string]any
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		return label + ": " + compactRunePreview(text, compactToolPayloadMaxRunes)
	}
	if name, _ := envelope["tool_name"].(string); strings.TrimSpace(name) != "" {
		label = "[Tool result: " + strings.TrimSpace(name) + "]"
	}
	status := "ok"
	if envelope["error"] != nil {
		status = "error"
		if raw, err := json.Marshal(envelope["error"]); err == nil {
			payload = compactRunePreview(string(raw), compactToolPayloadMaxRunes)
		}
	} else if envelope["result"] != nil {
		if raw, err := json.Marshal(envelope["result"]); err == nil {
			payload = compactRunePreview(string(raw), compactToolPayloadMaxRunes)
		}
	}
	return fmt.Sprintf("%s status=%s payload=%s", label, status, payload)
}

func compactRunePreview(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxRunes <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + fmt.Sprintf(" ...[truncated %d runes]", len(runes)-maxRunes)
}

func compactFallbackContentText(msg llm.Message) string {
	if len(msg.Content) == 0 {
		return ""
	}
	parts := make([]string, 0, len(msg.Content))
	for _, part := range msg.Content {
		if part.Kind() == messagecontent.PartTypeImage {
			key := ""
			if part.Attachment != nil {
				key = strings.TrimSpace(part.Attachment.Key)
			}
			if key != "" {
				parts = append(parts, fmt.Sprintf(`[image attachment_key="%s"]`, key))
			} else {
				parts = append(parts, "[image]")
			}
			continue
		}
		text := strings.TrimSpace(llm.PartPromptText(part))
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func serializeCompactChunksForLLM(chunks []compactChunk) string {
	if len(chunks) == 0 {
		return ""
	}
	var b strings.Builder
	for _, chunk := range chunks {
		_, _ = fmt.Fprintf(&b, "[chunk #%d atom=%d type=%s role=%s]\n",
			chunk.ContextSeq,
			chunk.AtomSeq,
			chunk.AtomType,
			strings.TrimSpace(chunk.Role),
		)
		b.WriteString(strings.TrimSpace(chunk.Text))
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func compactHeadTailChunks(chunks []compactChunk, keepTail int) (head []compactChunk, tail []compactChunk) {
	if len(chunks) == 0 {
		return nil, nil
	}
	if keepTail < 1 {
		keepTail = 1
	}
	if keepTail >= len(chunks) {
		return nil, append([]compactChunk(nil), chunks...)
	}
	head = append([]compactChunk(nil), chunks[:len(chunks)-keepTail]...)
	tail = append([]compactChunk(nil), chunks[len(chunks)-keepTail:]...)
	return head, tail
}

func compactLeadingReplacementSummaries(msgs []llm.Message) []string {
	summaries := make([]string, 0, 2)
	for _, msg := range msgs {
		if msg.Phase == nil || strings.TrimSpace(*msg.Phase) != compactSyntheticPhase || len(msg.Content) == 0 {
			break
		}
		raw := strings.TrimSpace(msg.Content[0].Text)
		if raw == "" {
			break
		}
		s := extractCompactSnapshotSummary(raw)
		if s != "" {
			summaries = append(summaries, s)
		}
	}
	return summaries
}

func extractCompactSnapshotSummary(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	start := strings.Index(raw, "<state_snapshot>")
	end := strings.Index(raw, "</state_snapshot>")
	if start < 0 || end < 0 || end <= start {
		return raw
	}
	start += len("<state_snapshot>")
	return strings.TrimSpace(raw[start:end])
}
