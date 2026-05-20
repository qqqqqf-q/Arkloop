package pipeline

import (
	"regexp"
	"strings"

	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/data"
)

var stickerPlaceholderPattern = regexp.MustCompile(`\[sticker:([^\]]+)\]`)

func containsStickerPlaceholderText(text string) bool {
	return stickerPlaceholderPattern.MatchString(text)
}

func containsStickerPlaceholderOutputs(outputs []string) bool {
	for _, output := range outputs {
		if containsStickerPlaceholderText(output) {
			return true
		}
	}
	return false
}

func PrepareStickerDeliveryOutputs(outputs []string) ([]string, []data.OutboxSegment) {
	return prepareStickerDeliveryOutputs(outputs)
}

func StripStickerPlaceholders(text string) string {
	return stripStickerPlaceholders(text)
}

func IsStickerRegisterRun(rc *RunContext) bool {
	return isStickerRegisterRun(rc)
}

func isStickerRegisterRun(rc *RunContext) bool {
	if rc == nil {
		return false
	}
	if s, ok := stringField(rc.InputJSON, "run_kind"); ok && strings.EqualFold(s, runkind.StickerRegister) {
		return true
	}
	if s, ok := stringField(rc.JobPayload, "run_kind"); ok && strings.EqualFold(s, runkind.StickerRegister) {
		return true
	}
	return false
}

func prepareStickerDeliveryOutputs(outputs []string) (cleanOutputs []string, segments []data.OutboxSegment) {
	if len(outputs) == 0 {
		return nil, nil
	}
	cleanOutputs = make([]string, 0, len(outputs))
	for _, output := range outputs {
		if !stickerPlaceholderPattern.MatchString(output) {
			if trimmed := strings.TrimSpace(output); trimmed != "" {
				cleanOutputs = append(cleanOutputs, trimmed)
			}
			continue
		}
		seg, cleanText := parseStickerSegments(output)
		if len(seg) > 0 {
			segments = append(segments, seg...)
		}
		if trimmed := strings.TrimSpace(cleanText); trimmed != "" {
			cleanOutputs = append(cleanOutputs, trimmed)
		}
	}
	if len(cleanOutputs) == 0 {
		cleanOutputs = nil
	}
	if len(segments) == 0 {
		segments = nil
	}
	return cleanOutputs, segments
}

func stripStickerPlaceholders(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return strings.TrimSpace(buildStickerCleanText(text))
}

func parseStickerSegments(text string) ([]data.OutboxSegment, string) {
	matches := stickerPlaceholderPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return nil, ""
		}
		return []data.OutboxSegment{{Kind: "text", Text: trimmed}}, trimmed
	}

	segments := make([]data.OutboxSegment, 0, len(matches)*2+1)
	last := 0
	for _, match := range matches {
		start, end := match[0], match[1]
		idStart, idEnd := match[2], match[3]
		if start > last {
			rawChunk := text[last:start]
			trimmedChunk := strings.TrimSpace(rawChunk)
			if trimmedChunk != "" {
				segments = append(segments, data.OutboxSegment{Kind: "text", Text: trimmedChunk})
			}
		}
		stickerID := strings.TrimSpace(text[idStart:idEnd])
		if stickerID != "" {
			segments = append(segments, data.OutboxSegment{Kind: "sticker", StickerID: stickerID})
		}
		last = end
	}
	if last < len(text) {
		rawChunk := text[last:]
		trimmedChunk := strings.TrimSpace(rawChunk)
		if trimmedChunk != "" {
			segments = append(segments, data.OutboxSegment{Kind: "text", Text: trimmedChunk})
		}
	}
	return segments, strings.TrimSpace(buildStickerCleanText(text))
}

func buildStickerCleanText(text string) string {
	matches := stickerPlaceholderPattern.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text
	}
	cleaned := ""
	last := 0
	for _, match := range matches {
		cleaned = appendStickerCleanChunk(cleaned, text[last:match[0]])
		last = match[1]
	}
	return appendStickerCleanChunk(cleaned, text[last:])
}

func appendStickerCleanChunk(current string, chunk string) string {
	if chunk == "" {
		return current
	}
	if current == "" {
		return chunk
	}
	if endsWithHorizontalWhitespace(current) && startsWithNewline(chunk) {
		current = strings.TrimRight(current, " \t")
	}
	if endsWithHorizontalWhitespace(current) && startsWithHorizontalWhitespace(chunk) {
		chunk = strings.TrimLeft(chunk, " \t")
	}
	if endsWithNewline(current) && startsWithNewline(chunk) {
		chunk = trimLeadingNewlines(chunk)
	}
	return current + chunk
}

func endsWithHorizontalWhitespace(text string) bool {
	if text == "" {
		return false
	}
	last := text[len(text)-1]
	return last == ' ' || last == '\t'
}

func startsWithHorizontalWhitespace(text string) bool {
	if text == "" {
		return false
	}
	first := text[0]
	return first == ' ' || first == '\t'
}

func endsWithNewline(text string) bool {
	if text == "" {
		return false
	}
	last := text[len(text)-1]
	return last == '\n' || last == '\r'
}

func startsWithNewline(text string) bool {
	if text == "" {
		return false
	}
	first := text[0]
	return first == '\n' || first == '\r'
}

func trimLeadingNewlines(text string) string {
	return strings.TrimLeft(text, "\r\n")
}
