package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type canonicalThreadContext struct {
	VisibleMessages  []data.ThreadMessage
	Atoms            []canonicalAtom
	Chunks           []canonicalChunk
	Frontier         []FrontierNode
	Entries          []canonicalThreadContextEntry
	Messages         []llm.Message
	ThreadMessageIDs []uuid.UUID
}

type canonicalThreadContextEntry struct {
	AnchorKey       string
	AtomKey         string
	Message         llm.Message
	ThreadMessageID uuid.UUID
	StartThreadSeq  int64
	EndThreadSeq    int64
	StartContextSeq int64
	EndContextSeq   int64
	IsReplacement   bool
	SummaryText     string
}

const compactSyntheticPhase = "system_compact"

func buildCanonicalThreadContext(
	ctx context.Context,
	tx pgx.Tx,
	run data.Run,
	messagesRepo data.MessagesRepository,
	attachmentStore MessageAttachmentStore,
	upperBoundMessageID *uuid.UUID,
	messageLimit int,
	partOptions ...MessagePartBuildOptions,
) (*canonicalThreadContext, error) {
	return buildCanonicalThreadContextWithTrace(ctx, tx, run, messagesRepo, attachmentStore, upperBoundMessageID, messageLimit, nil, partOptions...)
}

func buildCanonicalThreadContextWithTrace(
	ctx context.Context,
	tx pgx.Tx,
	run data.Run,
	messagesRepo data.MessagesRepository,
	attachmentStore MessageAttachmentStore,
	upperBoundMessageID *uuid.UUID,
	messageLimit int,
	trace InputLoaderTraceFunc,
	partOptions ...MessagePartBuildOptions,
) (*canonicalThreadContext, error) {
	fetchLimit := canonicalHistoryFetchLimit(messageLimit)

	var (
		visibleMessages []data.ThreadMessage
		err             error
		upperBoundSeq   *int64
	)
	stageStart := time.Now()
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
	traceInputLoaderStage(trace, "history_query", stageStart, map[string]any{
		"message_count": len(visibleMessages),
		"fetch_limit":   fetchLimit,
	})

	stageStart = time.Now()
	renderableMessages := filterPromptRenderableThreadMessages(visibleMessages)
	traceInputLoaderStage(trace, "filter_renderable", stageStart, map[string]any{
		"message_count":     len(visibleMessages),
		"renderable_count":  len(renderableMessages),
		"filtered_messages": len(visibleMessages) - len(renderableMessages),
	})
	stageStart = time.Now()
	atoms, chunks := buildCanonicalAtomGraph(renderableMessages)
	traceInputLoaderStage(trace, "build_atom_graph", stageStart, map[string]any{
		"atom_count":  len(atoms),
		"chunk_count": len(chunks),
	})
	var upperBoundContextSeq *int64
	if len(chunks) > 0 {
		lastContextSeq := chunks[len(chunks)-1].ContextSeq
		upperBoundContextSeq = &lastContextSeq
	}
	stageStart = time.Now()
	graph, err := ensureCanonicalThreadGraphPersistedFromGraph(ctx, tx, run.AccountID, run.ThreadID, atoms, chunks)
	if err != nil {
		return nil, err
	}
	traceInputLoaderStage(trace, "graph_persist", stageStart, map[string]any{
		"atom_count":  len(atoms),
		"chunk_count": len(chunks),
	})

	replacementsRepo := data.ThreadContextReplacementsRepository{}
	var replacements []data.ThreadContextReplacementRecord
	stageStart = time.Now()
	if upperBoundContextSeq != nil {
		replacements, err = replacementsRepo.ListActiveByThreadUpToContextSeq(
			ctx,
			tx,
			run.AccountID,
			run.ThreadID,
			upperBoundContextSeq,
		)
	} else {
		replacements, err = replacementsRepo.ListActiveByThreadUpToSeq(
			ctx,
			tx,
			run.AccountID,
			run.ThreadID,
			upperBoundSeq,
		)
	}
	if err != nil {
		return nil, err
	}
	traceInputLoaderStage(trace, "replacement_query", stageStart, map[string]any{
		"replacement_count": len(replacements),
	})

	stageStart = time.Now()
	frontier, err := buildThreadContextFrontier(ctx, tx, graph, run.AccountID, run.ThreadID, replacements, upperBoundContextSeq)
	if err != nil {
		return nil, err
	}
	traceInputLoaderStage(trace, "frontier_build", stageStart, map[string]any{
		"frontier_count": len(frontier),
	})
	stageStart = time.Now()
	entries, err := renderCanonicalThreadMessagesFromFrontier(ctx, attachmentStore, atoms, chunks, frontier, partOptions...)
	if err != nil {
		return nil, err
	}
	traceInputLoaderStage(trace, "render_messages", stageStart, map[string]any{
		"entry_count":                len(entries),
		"selected_replacement_count": countFrontierReplacements(frontier),
	})
	renderedMessages := make([]llm.Message, 0, len(entries))
	renderedIDs := make([]uuid.UUID, 0, len(entries))
	for _, entry := range entries {
		renderedMessages = append(renderedMessages, entry.Message)
		renderedIDs = append(renderedIDs, entry.ThreadMessageID)
	}
	if messageLimit > 0 {
		stageStart = time.Now()
		entries = trimEntriesToMessageLimit(entries, messageLimit)
		frontier = reindexFrontierToEntries(frontier, entries)
		renderedMessages = renderedMessages[:0]
		renderedIDs = renderedIDs[:0]
		for _, entry := range entries {
			renderedMessages = append(renderedMessages, entry.Message)
			renderedIDs = append(renderedIDs, entry.ThreadMessageID)
		}
		traceInputLoaderStage(trace, "trim_message_limit", stageStart, map[string]any{
			"message_limit": messageLimit,
			"entry_count":   len(entries),
		})
	}

	return &canonicalThreadContext{
		VisibleMessages:  renderableMessages,
		Atoms:            atoms,
		Chunks:           chunks,
		Frontier:         frontier,
		Entries:          entries,
		Messages:         renderedMessages,
		ThreadMessageIDs: renderedIDs,
	}, nil
}

func canonicalHistoryFetchLimit(messageLimit int) int {
	if messageLimit <= 0 {
		return 0
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
		return candidates[i].StartContextSeq < candidates[j].StartContextSeq
	})

	selected := make([]data.ThreadContextReplacementRecord, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.SummaryText) == "" {
			continue
		}
		overlaps := false
		for _, existing := range selected {
			if candidate.StartContextSeq <= existing.EndContextSeq && candidate.EndContextSeq >= existing.StartContextSeq {
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
		if selected[i].StartContextSeq != selected[j].StartContextSeq {
			return selected[i].StartContextSeq < selected[j].StartContextSeq
		}
		if selected[i].EndContextSeq != selected[j].EndContextSeq {
			return selected[i].EndContextSeq < selected[j].EndContextSeq
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
	atoms, chunks := buildCanonicalAtomGraph(messages)
	frontier := make([]FrontierNode, 0, len(chunks)+len(replacements))
	for _, chunk := range chunks {
		frontier = append(frontier, FrontierNode{
			Kind:            FrontierNodeChunk,
			StartContextSeq: chunk.ContextSeq,
			EndContextSeq:   chunk.ContextSeq,
			StartThreadSeq:  chunk.StartThreadSeq,
			EndThreadSeq:    chunk.EndThreadSeq,
			SourceText:      strings.TrimSpace(chunk.Content),
			ApproxTokens:    approxTokensFromText(chunk.Content),
			atomKey:         chunk.AtomKey,
			chunkSeq:        chunk.ContextSeq,
		})
	}
	for _, replacement := range replacements {
		if strings.TrimSpace(replacement.SummaryText) == "" {
			continue
		}
		frontier = append(frontier, FrontierNode{
			Kind:            FrontierNodeReplacement,
			NodeID:          replacement.ID,
			Layer:           replacement.Layer,
			StartContextSeq: replacement.StartContextSeq,
			EndContextSeq:   replacement.EndContextSeq,
			StartThreadSeq:  replacement.StartThreadSeq,
			EndThreadSeq:    replacement.EndThreadSeq,
			SourceText:      strings.TrimSpace(replacement.SummaryText),
			ApproxTokens:    approxTokensFromText(replacement.SummaryText),
			role:            "system",
		})
	}
	sort.SliceStable(frontier, func(i, j int) bool {
		if frontier[i].StartContextSeq != frontier[j].StartContextSeq {
			return frontier[i].StartContextSeq < frontier[j].StartContextSeq
		}
		if frontier[i].Kind != frontier[j].Kind {
			return frontier[i].Kind == FrontierNodeReplacement
		}
		return frontier[i].EndContextSeq < frontier[j].EndContextSeq
	})
	entries, err := renderCanonicalThreadMessagesFromFrontier(ctx, attachmentStore, atoms, chunks, frontier)
	return entries, "", err
}

func renderCanonicalThreadMessagesFromGraph(
	ctx context.Context,
	attachmentStore MessageAttachmentStore,
	atoms []canonicalAtom,
	chunks []canonicalChunk,
	replacements []canonicalReplacementSpan,
	partOptions ...MessagePartBuildOptions,
) ([]canonicalThreadContextEntry, string, error) {
	frontier := make([]FrontierNode, 0, len(chunks)+len(replacements))
	skipped := make(map[int64]struct{})
	for _, replacement := range replacements {
		summary := strings.TrimSpace(replacement.Record.SummaryText)
		if summary == "" {
			continue
		}
		for seq := replacement.StartContextSeq; seq <= replacement.EndContextSeq; seq++ {
			skipped[seq] = struct{}{}
		}
		frontier = append(frontier, FrontierNode{
			Kind:            FrontierNodeReplacement,
			NodeID:          replacement.Record.ID,
			Layer:           replacement.Record.Layer,
			StartContextSeq: replacement.StartContextSeq,
			EndContextSeq:   replacement.EndContextSeq,
			StartThreadSeq:  replacement.Record.StartThreadSeq,
			EndThreadSeq:    replacement.Record.EndThreadSeq,
			SourceText:      summary,
			ApproxTokens:    approxTokensFromText(summary),
			role:            "system",
		})
	}
	for _, chunk := range chunks {
		if _, ok := skipped[chunk.ContextSeq]; ok {
			continue
		}
		frontier = append(frontier, FrontierNode{
			Kind:            FrontierNodeChunk,
			StartContextSeq: chunk.ContextSeq,
			EndContextSeq:   chunk.ContextSeq,
			StartThreadSeq:  chunk.StartThreadSeq,
			EndThreadSeq:    chunk.EndThreadSeq,
			SourceText:      strings.TrimSpace(chunk.Content),
			ApproxTokens:    approxTokensFromText(chunk.Content),
			atomKey:         chunk.AtomKey,
			chunkSeq:        chunk.ContextSeq,
		})
	}
	sort.SliceStable(frontier, func(i, j int) bool {
		if frontier[i].StartContextSeq != frontier[j].StartContextSeq {
			return frontier[i].StartContextSeq < frontier[j].StartContextSeq
		}
		if frontier[i].Kind != frontier[j].Kind {
			return frontier[i].Kind == FrontierNodeReplacement
		}
		return frontier[i].EndContextSeq < frontier[j].EndContextSeq
	})
	entries, err := renderCanonicalThreadMessagesFromFrontier(ctx, attachmentStore, atoms, chunks, frontier, partOptions...)
	return entries, "", err
}

func renderCanonicalThreadMessagesFromFrontier(
	ctx context.Context,
	attachmentStore MessageAttachmentStore,
	atoms []canonicalAtom,
	chunks []canonicalChunk,
	frontier []FrontierNode,
	partOptions ...MessagePartBuildOptions,
) ([]canonicalThreadContextEntry, error) {
	opts := resolveMessagePartBuildOptions(partOptions...)
	entries := make([]canonicalThreadContextEntry, 0, len(frontier))
	if len(frontier) == 0 {
		return entries, nil
	}
	atomByKey := make(map[string]canonicalAtom, len(atoms))
	chunkBySeq := make(map[int64]canonicalChunk, len(chunks))
	for _, atom := range atoms {
		atomByKey[atom.Key] = atom
	}
	for _, chunk := range chunks {
		chunkBySeq[chunk.ContextSeq] = chunk
	}

	var prevUserTime time.Time
	appendThreadMessage := func(atom canonicalAtom, msg data.ThreadMessage) error {
		if strings.TrimSpace(msg.Role) == "" {
			return nil
		}
		parts, err := BuildMessagePartsWithOptions(ctx, attachmentStore, msg, opts)
		if err != nil {
			if attachmentStore == nil {
				parts = fallbackTextParts(msg.Content)
			} else {
				return err
			}
		}
		if msg.Role == "tool" {
			parts = canonicalizeToolMessageParts(parts)
		}
		if msg.Role == "user" && !msg.CreatedAt.IsZero() {
			parts = prependUserMessageTimestamp(parts, msg.CreatedAt, prevUserTime, opts.UserLocation)
			prevUserTime = msg.CreatedAt
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
			AtomKey:         atom.Key,
			Message:         lm,
			ThreadMessageID: msg.ID,
			StartThreadSeq:  msg.ThreadSeq,
			EndThreadSeq:    msg.ThreadSeq,
			StartContextSeq: atom.StartContextSeq,
			EndContextSeq:   atom.EndContextSeq,
			IsReplacement:   false,
		})
		return nil
	}

	appendReplacement := func(node FrontierNode) {
		summary := strings.TrimSpace(node.SourceText)
		if summary == "" {
			return
		}
		entries = append(entries, canonicalThreadContextEntry{
			AnchorKey:       replacementAnchorKey(node.NodeID),
			Message:         makeThreadContextReplacementMessageWithHandle(summary, node),
			ThreadMessageID: uuid.Nil,
			StartThreadSeq:  node.StartThreadSeq,
			EndThreadSeq:    node.EndThreadSeq,
			StartContextSeq: node.StartContextSeq,
			EndContextSeq:   node.EndContextSeq,
			IsReplacement:   true,
			SummaryText:     summary,
		})
	}

	appendChunkRange := func(atom canonicalAtom, startContextSeq, endContextSeq int64) error {
		if startContextSeq == atom.StartContextSeq && endContextSeq == atom.EndContextSeq {
			for _, msg := range atom.Messages {
				if err := appendThreadMessage(atom, msg); err != nil {
					return err
				}
			}
			return nil
		}
		if !atomSupportsPartialTail(atom) {
			for _, msg := range atom.Messages {
				if err := appendThreadMessage(atom, msg); err != nil {
					return err
				}
			}
			return nil
		}
		textParts := make([]string, 0, endContextSeq-startContextSeq+1)
		for seq := startContextSeq; seq <= endContextSeq; seq++ {
			chunk, ok := chunkBySeq[seq]
			if !ok || chunk.AtomKey != atom.Key {
				continue
			}
			textParts = append(textParts, strings.TrimSpace(chunk.Content))
		}
		visibleText := strings.TrimSpace(strings.Join(textParts, "\n\n"))
		if visibleText == "" {
			return nil
		}
		msg := atom.Messages[0]
		entries = append(entries, canonicalThreadContextEntry{
			AnchorKey:       messageAnchorKey(msg.ID),
			AtomKey:         atom.Key,
			Message:         llm.Message{Role: msg.Role, Content: []llm.TextPart{{Text: visibleText}}, OutputTokens: msg.OutputTokens},
			ThreadMessageID: msg.ID,
			StartThreadSeq:  msg.ThreadSeq,
			EndThreadSeq:    msg.ThreadSeq,
			StartContextSeq: startContextSeq,
			EndContextSeq:   endContextSeq,
			IsReplacement:   false,
		})
		return nil
	}

	for i := 0; i < len(frontier); {
		node := frontier[i]
		if node.Kind == FrontierNodeReplacement {
			msgStart := len(entries)
			appendReplacement(node)
			if len(entries) > msgStart {
				frontier[i].MsgStart = msgStart
				frontier[i].MsgEnd = len(entries) - 1
				frontier[i].Role = "system"
			}
			i++
			continue
		}
		atom, ok := atomByKey[node.atomKey]
		if !ok {
			i++
			continue
		}
		startContextSeq := node.StartContextSeq
		endContextSeq := node.EndContextSeq
		j := i + 1
		for j < len(frontier) {
			next := frontier[j]
			if next.Kind != FrontierNodeChunk || next.atomKey != node.atomKey || next.StartContextSeq != endContextSeq+1 {
				break
			}
			endContextSeq = next.EndContextSeq
			j++
		}
		msgStart := len(entries)
		if err := appendChunkRange(atom, startContextSeq, endContextSeq); err != nil {
			return nil, err
		}
		if len(entries) > msgStart {
			msgEnd := len(entries) - 1
			for k := i; k < j; k++ {
				frontier[k].MsgStart = msgStart
				frontier[k].MsgEnd = msgEnd
				frontier[k].Role = atom.Messages[0].Role
			}
		}
		i = j
	}
	return entries, nil
}

func countFrontierReplacements(frontier []FrontierNode) int {
	count := 0
	for _, node := range frontier {
		if node.Kind == FrontierNodeReplacement {
			count++
		}
	}
	return count
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
	// Never split a protocol atom. If cutoff lands inside an atom, move to the atom head.
	if cutoff > 0 && cutoff < len(entries) && !entries[cutoff].IsReplacement && entries[cutoff].AtomKey != "" {
		atomKey := entries[cutoff].AtomKey
		for cutoff > 0 {
			prev := entries[cutoff-1]
			if prev.IsReplacement || prev.AtomKey != atomKey {
				break
			}
			cutoff--
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

func reindexFrontierToEntries(frontier []FrontierNode, entries []canonicalThreadContextEntry) []FrontierNode {
	if len(frontier) == 0 || len(entries) == 0 {
		return frontier
	}
	out := make([]FrontierNode, 0, len(frontier))
	for _, node := range frontier {
		start, end, ok := frontierEntryRange(node, entries)
		if !ok {
			continue
		}
		node.MsgStart = start
		node.MsgEnd = end
		out = append(out, node)
	}
	return out
}

func frontierEntryRange(node FrontierNode, entries []canonicalThreadContextEntry) (int, int, bool) {
	if len(entries) == 0 {
		return 0, 0, false
	}
	if node.Kind == FrontierNodeReplacement {
		anchor := replacementAnchorKey(node.NodeID)
		for i, entry := range entries {
			if entry.IsReplacement && entry.AnchorKey == anchor {
				return i, i, true
			}
		}
		return 0, 0, false
	}
	start := -1
	end := -1
	for i, entry := range entries {
		if entry.IsReplacement || entry.AtomKey != node.atomKey {
			continue
		}
		if entry.StartContextSeq > node.StartContextSeq || entry.EndContextSeq < node.EndContextSeq {
			continue
		}
		if start < 0 {
			start = i
		}
		end = i
	}
	if start < 0 || end < start {
		return 0, 0, false
	}
	return start, end, true
}

func compactReplacementMetadata(kind string) json.RawMessage {
	payload, _ := json.Marshal(map[string]string{"kind": kind})
	if len(payload) == 0 {
		return json.RawMessage(`{}`)
	}
	return payload
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

func makeThreadContextReplacementMessage(summary string) llm.Message {
	phase := compactSyntheticPhase
	return llm.Message{
		Role:    "system",
		Phase:   &phase,
		Content: []llm.TextPart{{Text: strings.TrimSpace(summary)}},
	}
}

func makeThreadContextReplacementMessageWithHandle(summary string, node FrontierNode) llm.Message {
	trimmed := strings.TrimSpace(summary)
	if node.NodeID == uuid.Nil {
		return makeThreadContextReplacementMessage(trimmed)
	}
	return makeThreadContextReplacementMessage(fmt.Sprintf(
		"<conversation_summary replacement_id=\"%s\" layer=\"%d\" start_context_seq=\"%d\" end_context_seq=\"%d\" start_thread_seq=\"%d\" end_thread_seq=\"%d\">\n%s\n</conversation_summary>",
		node.NodeID.String(),
		node.Layer,
		node.StartContextSeq,
		node.EndContextSeq,
		node.StartThreadSeq,
		node.EndThreadSeq,
		escapeCompactSummaryXML(trimmed),
	))
}

func escapeCompactSummaryXML(summary string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(summary)); err != nil {
		return strings.ReplaceAll(summary, "]]>", "]]&gt;")
	}
	return buf.String()
}
