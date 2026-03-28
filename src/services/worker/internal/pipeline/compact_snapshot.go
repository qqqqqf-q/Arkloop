package pipeline

import (
	"strings"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/llm"
)

const compactSnapshotHeader = "[Context summary for continuation]"

func formatCompactSnapshotText(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	return compactSnapshotHeader + "\n<state_snapshot>\n" + summary + "\n</state_snapshot>"
}

func makeCompactSnapshotMessage(summary string) llm.Message {
	return llm.Message{
		Role:    "user",
		Content: []llm.TextPart{{Type: messagecontent.PartTypeText, Text: formatCompactSnapshotText(summary)}},
	}
}
