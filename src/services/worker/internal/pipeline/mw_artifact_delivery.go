package pipeline

import (
	"regexp"
	"strings"

	"arkloop/services/worker/internal/data"
)

// artifactRefPattern 匹配模型输出里的 artifact 引用（Markdown 链接或图片形式），捕获组为裸 key。
// 形如 [label](artifact:acct/path) 或 ![alt](artifact:acct/path)。
var artifactRefPattern = regexp.MustCompile(`!?\[[^\]]*\]\(artifact:([^)\s]+)\)`)

func containsArtifactReferenceText(text string) bool {
	return artifactRefPattern.MatchString(text)
}

func containsArtifactReferenceOutputs(outputs []string) bool {
	for _, output := range outputs {
		if artifactRefPattern.MatchString(output) {
			return true
		}
	}
	return false
}

// applyArtifactDeliverySegments 在 sticker 解析之后运行，把文本中的 artifact 引用拆成独立的 artifact segment，
// 供渠道投递层作为文件外发。返回更新后的 (cleanOutputs, segments)；无 artifact 引用时原样返回。
func applyArtifactDeliverySegments(cleanOutputs []string, segments []data.OutboxSegment) ([]string, []data.OutboxSegment) {
	base := segments
	if len(base) == 0 {
		base = make([]data.OutboxSegment, 0, len(cleanOutputs))
		for _, output := range cleanOutputs {
			if trimmed := strings.TrimSpace(output); trimmed != "" {
				base = append(base, data.OutboxSegment{Kind: "text", Text: trimmed})
			}
		}
	}

	hasArtifact := false
	for _, seg := range base {
		if seg.Kind == "text" && containsArtifactReferenceText(seg.Text) {
			hasArtifact = true
			break
		}
	}
	if !hasArtifact {
		return cleanOutputs, segments
	}

	out := make([]data.OutboxSegment, 0, len(base)+2)
	var texts []string
	for _, seg := range base {
		if seg.Kind != "text" {
			out = append(out, seg)
			continue
		}
		for _, split := range splitArtifactSegments(seg.Text) {
			out = append(out, split)
			if split.Kind == "text" {
				texts = append(texts, split.Text)
			}
		}
	}
	if len(texts) == 0 {
		texts = nil
	}
	return texts, out
}

// splitArtifactSegments 按 artifact 引用把单段文本拆为有序的 text / artifact 片段。
func splitArtifactSegments(text string) []data.OutboxSegment {
	matches := artifactRefPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			return []data.OutboxSegment{{Kind: "text", Text: trimmed}}
		}
		return nil
	}

	segments := make([]data.OutboxSegment, 0, len(matches)*2+1)
	last := 0
	for _, match := range matches {
		start, end := match[0], match[1]
		keyStart, keyEnd := match[2], match[3]
		if start > last {
			if chunk := strings.TrimSpace(text[last:start]); chunk != "" {
				segments = append(segments, data.OutboxSegment{Kind: "text", Text: chunk})
			}
		}
		key := strings.TrimSpace(text[keyStart:keyEnd])
		if key != "" {
			segments = append(segments, data.OutboxSegment{Kind: "artifact", ArtifactKey: key})
		}
		last = end
	}
	if last < len(text) {
		if chunk := strings.TrimSpace(text[last:]); chunk != "" {
			segments = append(segments, data.OutboxSegment{Kind: "text", Text: chunk})
		}
	}
	return segments
}
