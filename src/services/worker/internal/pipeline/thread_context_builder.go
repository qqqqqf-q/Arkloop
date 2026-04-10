package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type canonicalThreadContext struct {
	VisibleMessages          []data.ThreadMessage
	Entries                  []canonicalThreadContextEntry
	Messages                 []llm.Message
	ThreadMessageIDs         []uuid.UUID
	HasLeadingCompactSummary bool
	LeadingCompactSummary    string
}

type canonicalThreadContextEntry struct {
	AnchorKey       string
	Message         llm.Message
	ThreadMessageID uuid.UUID
	StartThreadSeq  int64
	EndThreadSeq    int64
	IsReplacement   bool
}

func buildCanonicalThreadContext(
	ctx context.Context,
	tx pgx.Tx,
	run data.Run,
	messagesRepo data.MessagesRepository,
	attachmentStore MessageAttachmentStore,
	upperBoundMessageID *uuid.UUID,
	messageLimit int,
) (*canonicalThreadContext, error) {
	fetchLimit := canonicalHistoryFetchLimit(messageLimit)

	var (
		visibleMessages []data.ThreadMessage
		err             error
		upperBoundSeq   *int64
	)
	if upperBoundMessageID != nil && *upperBoundMessageID != uuid.Nil {
		seq, seqErr := messagesRepo.GetThreadSeqByMessageID(ctx, tx, run.AccountID, run.ThreadID, *upperBoundMessageID)
		if seqErr != nil {
			return nil, seqErr
		}
		upperBoundSeq = &seq
		visibleMessages, err = messagesRepo.ListByThreadUpToID(ctx, tx, run.AccountID, run.ThreadID, *upperBoundMessageID, fetchLimit)
	} else {
		visibleMessages, err = messagesRepo.ListByThread(ctx, tx, run.AccountID, run.ThreadID, fetchLimit)
		if len(visibleMessages) > 0 {
			lastSeq := visibleMessages[len(visibleMessages)-1].ThreadSeq
			upperBoundSeq = &lastSeq
		}
	}
	if err != nil {
		return nil, err
	}

	renderableMessages := filterPromptRenderableThreadMessages(visibleMessages)

	replacements, err := (data.ThreadContextReplacementsRepository{}).ListActiveByThreadUpToSeq(
		ctx,
		tx,
		run.AccountID,
		run.ThreadID,
		upperBoundSeq,
	)
	if err != nil {
		return nil, err
	}

	legacySnapshot, legacyErr := (data.ThreadCompactionSnapshotsRepository{}).GetActiveByThread(ctx, tx, run.AccountID, run.ThreadID)
	if legacyErr != nil {
		return nil, legacyErr
	}
	if legacy := legacySnapshotAsReplacement(legacySnapshot, renderableMessages, upperBoundSeq, replacements); legacy != nil {
		replacements = append(replacements, *legacy)
	}

	selected := selectRenderableReplacements(replacements)
	entries, leadingSummary, err := renderCanonicalThreadMessages(ctx, attachmentStore, renderableMessages, selected)
	if err != nil {
		return nil, err
	}
	renderedMessages := make([]llm.Message, 0, len(entries))
	renderedIDs := make([]uuid.UUID, 0, len(entries))
	for _, entry := range entries {
		renderedMessages = append(renderedMessages, entry.Message)
		renderedIDs = append(renderedIDs, entry.ThreadMessageID)
	}
	if messageLimit > 0 {
		entries = trimEntriesToMessageLimit(entries, messageLimit)
		renderedMessages = renderedMessages[:0]
		renderedIDs = renderedIDs[:0]
		for _, entry := range entries {
			renderedMessages = append(renderedMessages, entry.Message)
			renderedIDs = append(renderedIDs, entry.ThreadMessageID)
		}
	}

	return &canonicalThreadContext{
		VisibleMessages:          renderableMessages,
		Entries:                  entries,
		Messages:                 renderedMessages,
		ThreadMessageIDs:         renderedIDs,
		HasLeadingCompactSummary: strings.TrimSpace(leadingSummary) != "",
		LeadingCompactSummary:    strings.TrimSpace(leadingSummary),
	}, nil
}

func canonicalHistoryFetchLimit(messageLimit int) int {
	if messageLimit <= 0 {
		messageLimit = 200
	}
	return messageLimit
}

func filterPromptRenderableThreadMessages(messages []data.ThreadMessage) []data.ThreadMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]data.ThreadMessage, 0, len(messages))
	for _, msg := range messages {
		if isPromptExcludedThreadMessage(msg) {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func isPromptExcludedThreadMessage(msg data.ThreadMessage) bool {
	if len(msg.MetadataJSON) == 0 {
		return false
	}
	var metadata map[string]any
	if err := json.Unmarshal(msg.MetadataJSON, &metadata); err != nil {
		return false
	}
	return metadataBool(metadata["exclude_from_prompt"])
}

func metadataBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		trimmed := strings.TrimSpace(strings.ToLower(typed))
		return trimmed == "true" || trimmed == "1"
	case float64:
		return typed != 0
	default:
		return false
	}
}

func legacySnapshotAsReplacement(
	snapshot *data.ThreadCompactionSnapshotRecord,
	renderableMessages []data.ThreadMessage,
	upperBoundSeq *int64,
	existingReplacements []data.ThreadContextReplacementRecord,
) *data.ThreadContextReplacementRecord {
	if snapshot == nil || strings.TrimSpace(snapshot.SummaryText) == "" {
		return nil
	}
	var endThreadSeq int64
	minStart := int64(0)
	for _, repl := range existingReplacements {
		if repl.StartThreadSeq <= 0 {
			continue
		}
		if minStart == 0 || repl.StartThreadSeq < minStart {
			minStart = repl.StartThreadSeq
		}
	}
	if minStart > 1 {
		endThreadSeq = minStart - 1
	} else if minStart == 0 {
		if len(renderableMessages) > 0 && renderableMessages[0].ThreadSeq > 1 {
			endThreadSeq = renderableMessages[0].ThreadSeq - 1
		} else if upperBoundSeq != nil && *upperBoundSeq > 0 {
			endThreadSeq = *upperBoundSeq
		}
	}
	if endThreadSeq <= 0 {
		return nil
	}
	return &data.ThreadContextReplacementRecord{
		ID:             snapshot.ID,
		AccountID:      snapshot.AccountID,
		ThreadID:       snapshot.ThreadID,
		StartThreadSeq: 1,
		EndThreadSeq:   endThreadSeq,
		SummaryText:    strings.TrimSpace(snapshot.SummaryText),
		Layer:          0,
		MetadataJSON:   snapshot.MetadataJSON,
		CreatedAt:      snapshot.CreatedAt,
	}
}

func selectRenderableReplacements(items []data.ThreadContextReplacementRecord) []data.ThreadContextReplacementRecord {
	if len(items) == 0 {
		return nil
	}
	candidates := append([]data.ThreadContextReplacementRecord(nil), items...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Layer != candidates[j].Layer {
			return candidates[i].Layer > candidates[j].Layer
		}
		if !candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
		}
		return candidates[i].StartThreadSeq < candidates[j].StartThreadSeq
	})

	selected := make([]data.ThreadContextReplacementRecord, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.SummaryText) == "" {
			continue
		}
		overlaps := false
		for _, existing := range selected {
			if candidate.StartThreadSeq <= existing.EndThreadSeq && candidate.EndThreadSeq >= existing.StartThreadSeq {
				overlaps = true
				break
			}
		}
		if overlaps {
			continue
		}
		selected = append(selected, candidate)
	}

	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].StartThreadSeq != selected[j].StartThreadSeq {
			return selected[i].StartThreadSeq < selected[j].StartThreadSeq
		}
		if selected[i].EndThreadSeq != selected[j].EndThreadSeq {
			return selected[i].EndThreadSeq < selected[j].EndThreadSeq
		}
		if selected[i].Layer != selected[j].Layer {
			return selected[i].Layer > selected[j].Layer
		}
		return selected[i].CreatedAt.Before(selected[j].CreatedAt)
	})
	return selected
}

func renderCanonicalThreadMessages(
	ctx context.Context,
	attachmentStore MessageAttachmentStore,
	messages []data.ThreadMessage,
	replacements []data.ThreadContextReplacementRecord,
) ([]canonicalThreadContextEntry, string, error) {
	entries := make([]canonicalThreadContextEntry, 0, len(messages)+len(replacements))
	leadingSummary := ""
	messageIndex := 0

	appendReplacement := func(replacement data.ThreadContextReplacementRecord) {
		entries = append(entries, canonicalThreadContextEntry{
			AnchorKey:       replacementAnchorKey(replacement.ID),
			Message:         makeCompactSnapshotMessage(replacement.SummaryText),
			ThreadMessageID: uuid.Nil,
			StartThreadSeq:  replacement.StartThreadSeq,
			EndThreadSeq:    replacement.EndThreadSeq,
			IsReplacement:   true,
		})
		if leadingSummary == "" && len(entries) == 1 {
			leadingSummary = strings.TrimSpace(replacement.SummaryText)
		}
	}

	appendThreadMessage := func(msg data.ThreadMessage) error {
		if strings.TrimSpace(msg.Role) == "" {
			return nil
		}
		parts, err := BuildMessageParts(ctx, attachmentStore, msg)
		if err != nil {
			return err
		}
		if msg.Role == "tool" {
			parts = canonicalizeToolMessageParts(parts)
		}
		lm := llm.Message{
			Role:         msg.Role,
			Content:      parts,
			OutputTokens: msg.OutputTokens,
		}
		if msg.Role == "assistant" && len(msg.ContentJSON) > 0 {
			lm.ToolCalls = parseToolCallsFromContentJSON(msg.ContentJSON)
		}
		var keep bool
		lm, keep = filterLongTermHeartbeatDecision(lm)
		if !keep {
			return nil
		}
		entries = append(entries, canonicalThreadContextEntry{
			AnchorKey:       messageAnchorKey(msg.ID),
			Message:         lm,
			ThreadMessageID: msg.ID,
			StartThreadSeq:  msg.ThreadSeq,
			EndThreadSeq:    msg.ThreadSeq,
			IsReplacement:   false,
		})
		return nil
	}

	for _, replacement := range replacements {
		for messageIndex < len(messages) && messages[messageIndex].ThreadSeq < replacement.StartThreadSeq {
			if err := appendThreadMessage(messages[messageIndex]); err != nil {
				return nil, "", err
			}
			messageIndex++
		}
		appendReplacement(replacement)
		for messageIndex < len(messages) && messages[messageIndex].ThreadSeq <= replacement.EndThreadSeq {
			messageIndex++
		}
	}
	for messageIndex < len(messages) {
		if err := appendThreadMessage(messages[messageIndex]); err != nil {
			return nil, "", err
		}
		messageIndex++
	}
	return entries, leadingSummary, nil
}

func trimEntriesToMessageLimit(entries []canonicalThreadContextEntry, messageLimit int) []canonicalThreadContextEntry {
	if messageLimit <= 0 || len(entries) == 0 {
		return entries
	}
	realCount := 0
	for _, entry := range entries {
		if !entry.IsReplacement {
			realCount++
		}
	}
	if realCount <= messageLimit {
		return entries
	}

	keptReal := 0
	cutoff := 0
	for i := len(entries) - 1; i >= 0; i-- {
		if !entries[i].IsReplacement {
			keptReal++
			if keptReal >= messageLimit {
				cutoff = i
				break
			}
		}
	}
	tail := entries[cutoff:]
	prefix := make([]canonicalThreadContextEntry, 0, cutoff)
	for i := 0; i < cutoff; i++ {
		if entries[i].IsReplacement {
			prefix = append(prefix, entries[i])
		}
	}
	if len(prefix) == 0 {
		return tail
	}
	return append(prefix, tail...)
}

func compactReplacementMetadata(kind string) json.RawMessage {
	payload, _ := json.Marshal(map[string]string{"kind": kind})
	if len(payload) == 0 {
		return json.RawMessage(`{}`)
	}
	return payload
}

func summaryTextOrEmpty(snapshot *data.ThreadCompactionSnapshotRecord) string {
	if snapshot == nil {
		return ""
	}
	return strings.TrimSpace(snapshot.SummaryText)
}

func messageAnchorKey(messageID uuid.UUID) string {
	if messageID == uuid.Nil {
		return ""
	}
	return "message:" + messageID.String()
}

func replacementAnchorKey(replacementID uuid.UUID) string {
	if replacementID == uuid.Nil {
		return ""
	}
	return "replacement:" + replacementID.String()
}

func leadingCompactEntryCount(entries []canonicalThreadContextEntry) int {
	count := 0
	for _, entry := range entries {
		if !entry.IsReplacement {
			break
		}
		count++
	}
	return count
}

func renderedMessageAnchorKey(entries []canonicalThreadContextEntry, messageID uuid.UUID) string {
	for _, entry := range entries {
		if entry.ThreadMessageID == messageID && !entry.IsReplacement {
			return entry.AnchorKey
		}
	}
	return ""
}

func replacementAnchorKeyForThreadSeq(entries []canonicalThreadContextEntry, threadSeq int64) string {
	for _, entry := range entries {
		if entry.IsReplacement && entry.StartThreadSeq <= threadSeq && entry.EndThreadSeq >= threadSeq {
			return entry.AnchorKey
		}
	}
	return ""
}

func isLastRenderedMessage(entries []canonicalThreadContextEntry, messageID uuid.UUID) bool {
	if len(entries) == 0 {
		return false
	}
	last := entries[len(entries)-1]
	return !last.IsReplacement && last.ThreadMessageID == messageID
}

func mergedLeadingReplacementRange(
	existing *data.ThreadContextReplacementRecord,
	legacySummary string,
	startThreadSeq int64,
	endThreadSeq int64,
) (int64, int64, int, error) {
	if startThreadSeq <= 0 || endThreadSeq <= 0 || startThreadSeq > endThreadSeq {
		return 0, 0, 0, fmt.Errorf("invalid thread seq range")
	}
	layer := 1
	if existing != nil {
		if existing.StartThreadSeq < startThreadSeq {
			startThreadSeq = existing.StartThreadSeq
		}
		if existing.EndThreadSeq > endThreadSeq {
			endThreadSeq = existing.EndThreadSeq
		}
		layer = existing.Layer + 1
	} else if strings.TrimSpace(legacySummary) != "" {
		startThreadSeq = 1
		layer = 2
	}
	return startThreadSeq, endThreadSeq, layer, nil
}
