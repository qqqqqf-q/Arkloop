package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	canonicalChunkTargetApproxTokens = 1200
	canonicalChunkTargetApproxRunes  = canonicalChunkTargetApproxTokens * 4
	canonicalPersistFetchLimit       = 1000000
)

type canonicalAtomKind string

const (
	canonicalAtomUserText      canonicalAtomKind = "user_text_atom"
	canonicalAtomAssistantText canonicalAtomKind = "assistant_text_atom"
	canonicalAtomToolEpisode   canonicalAtomKind = "tool_episode_atom"
)

type canonicalAtom struct {
	Key             string
	Kind            canonicalAtomKind
	Messages        []data.ThreadMessage
	StartThreadSeq  int64
	EndThreadSeq    int64
	StartContextSeq int64
	EndContextSeq   int64
}

type canonicalChunk struct {
	ContextSeq     int64
	AtomKey        string
	StartThreadSeq int64
	EndThreadSeq   int64
	Content        string
}

type canonicalReplacementSpan struct {
	Record          data.ThreadContextReplacementRecord
	StartContextSeq int64
	EndContextSeq   int64
}

type persistedCanonicalThreadGraph struct {
	Atoms                    []canonicalAtom
	Chunks                   []canonicalChunk
	AtomRecordsByKey         map[string]*data.ProtocolAtomRecord
	ChunkRecordsByContextSeq map[int64]*data.ContextChunkRecord
}

func buildCanonicalAtomGraph(messages []data.ThreadMessage) ([]canonicalAtom, []canonicalChunk) {
	if len(messages) == 0 {
		return nil, nil
	}
	atoms := buildProtocolAtoms(messages)
	if len(atoms) == 0 {
		return nil, nil
	}

	chunks := make([]canonicalChunk, 0, len(messages))
	nextContextSeq := int64(1)
	for i := range atoms {
		pieces := atomPayloadChunks(atoms[i])
		if len(pieces) == 0 {
			pieces = []string{atomFallbackChunk(atoms[i])}
		}
		start := nextContextSeq
		for _, piece := range pieces {
			chunks = append(chunks, canonicalChunk{
				ContextSeq:     nextContextSeq,
				AtomKey:        atoms[i].Key,
				StartThreadSeq: atoms[i].StartThreadSeq,
				EndThreadSeq:   atoms[i].EndThreadSeq,
				Content:        strings.TrimSpace(piece),
			})
			nextContextSeq++
		}
		atoms[i].StartContextSeq = start
		atoms[i].EndContextSeq = nextContextSeq - 1
	}
	return atoms, chunks
}

func buildProtocolAtoms(messages []data.ThreadMessage) []canonicalAtom {
	atoms := make([]canonicalAtom, 0, len(messages))
	for i := 0; i < len(messages); {
		msg := messages[i]
		role := strings.TrimSpace(msg.Role)
		start := i
		kind := canonicalAtomUserText

		switch role {
		case "assistant":
			if assistantMessageHasToolCalls(msg) {
				kind = canonicalAtomToolEpisode
				i++
				for i < len(messages) && strings.TrimSpace(messages[i].Role) == "tool" {
					i++
				}
			} else {
				kind = canonicalAtomAssistantText
				i++
			}
		case "tool":
			kind = canonicalAtomToolEpisode
			i++
			for i < len(messages) && strings.TrimSpace(messages[i].Role) == "tool" {
				i++
			}
		default:
			kind = canonicalAtomUserText
			i++
		}

		block := append([]data.ThreadMessage(nil), messages[start:i]...)
		if len(block) == 0 {
			continue
		}
		startSeq := block[0].ThreadSeq
		endSeq := block[len(block)-1].ThreadSeq
		atoms = append(atoms, canonicalAtom{
			Key:            fmt.Sprintf("atom:%d-%d", startSeq, endSeq),
			Kind:           kind,
			Messages:       block,
			StartThreadSeq: startSeq,
			EndThreadSeq:   endSeq,
		})
	}
	return atoms
}

func assistantMessageHasToolCalls(msg data.ThreadMessage) bool {
	if strings.TrimSpace(msg.Role) != "assistant" || len(msg.ContentJSON) == 0 {
		return false
	}
	return len(parseToolCallsFromContentJSON(msg.ContentJSON)) > 0
}

func atomPayloadChunks(atom canonicalAtom) []string {
	var base []string
	switch atom.Kind {
	case canonicalAtomToolEpisode:
		base = toolEpisodePayloadBlocks(atom.Messages)
	default:
		base = textPayloadBlocks(atom.Messages)
	}
	if len(base) == 0 {
		return nil
	}
	out := make([]string, 0, len(base))
	for _, block := range base {
		out = append(out, splitByApproxTokenBudget(block, canonicalChunkTargetApproxTokens)...)
	}
	filtered := make([]string, 0, len(out))
	for _, piece := range out {
		trimmed := strings.TrimSpace(piece)
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	return filtered
}

func atomFallbackChunk(atom canonicalAtom) string {
	if len(atom.Messages) == 0 {
		return ""
	}
	parts := make([]string, 0, len(atom.Messages))
	for _, msg := range atom.Messages {
		text := strings.TrimSpace(msg.Content)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n")
}

func textPayloadBlocks(messages []data.ThreadMessage) []string {
	combined := make([]string, 0, len(messages))
	for _, msg := range messages {
		text := strings.TrimSpace(msg.Content)
		if text == "" {
			continue
		}
		combined = append(combined, text)
	}
	if len(combined) == 0 {
		return nil
	}
	return splitLogicalBlocks(strings.Join(combined, "\n\n"))
}

func toolEpisodePayloadBlocks(messages []data.ThreadMessage) []string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		switch strings.TrimSpace(msg.Role) {
		case "assistant":
			text := strings.TrimSpace(msg.Content)
			if text != "" {
				parts = append(parts, "[assistant] "+text)
			}
			calls := parseToolCallsFromContentJSON(msg.ContentJSON)
			if len(calls) == 0 {
				continue
			}
			names := make([]string, 0, len(calls))
			for _, call := range calls {
				name := strings.TrimSpace(call.ToolName)
				if name == "" {
					name = "unknown_tool"
				}
				names = append(names, name)
			}
			parts = append(parts, "[assistant.tool_calls] "+strings.Join(names, ", "))
		case "tool":
			parts = append(parts, toolEnvelopePayloadBlock(msg.Content))
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func toolEnvelopePayloadBlock(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return "[tool.result] " + trimmed
	}
	name, _ := envelope["tool_name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "unknown_tool"
	}
	body := ""
	if errBody, ok := envelope["error"]; ok && errBody != nil {
		encoded, _ := json.Marshal(errBody)
		body = strings.TrimSpace(string(encoded))
	} else if result, ok := envelope["result"]; ok && result != nil {
		encoded, _ := json.Marshal(result)
		body = strings.TrimSpace(string(encoded))
	}
	if body == "" {
		body = trimmed
	}
	return "[tool.result:" + name + "] " + body
}

func splitLogicalBlocks(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")
	blocks := make([]string, 0, len(lines)/2+1)
	current := make([]string, 0, 8)
	inCodeFence := false

	flush := func() {
		if len(current) == 0 {
			return
		}
		block := strings.TrimSpace(strings.Join(current, "\n"))
		if block != "" {
			blocks = append(blocks, block)
		}
		current = current[:0]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if !inCodeFence {
				flush()
			}
			current = append(current, line)
			inCodeFence = !inCodeFence
			if !inCodeFence {
				flush()
			}
			continue
		}
		if inCodeFence {
			current = append(current, line)
			continue
		}
		if trimmed == "" {
			flush()
			continue
		}
		if len(current) > 0 && isLogicalBoundaryLine(trimmed) && !isLogicalBoundaryLine(strings.TrimSpace(current[len(current)-1])) {
			flush()
		}
		current = append(current, line)
	}
	flush()
	if len(blocks) == 0 {
		plain := strings.TrimSpace(normalized)
		if plain == "" {
			return nil
		}
		return []string{plain}
	}
	return blocks
}

func isLogicalBoundaryLine(trimmed string) bool {
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "#") ||
		strings.HasPrefix(trimmed, "- ") ||
		strings.HasPrefix(trimmed, "* ") ||
		strings.HasPrefix(trimmed, "+ ") ||
		strings.HasPrefix(trimmed, "> ") ||
		strings.HasPrefix(trimmed, "|") {
		return true
	}
	if len(trimmed) > 3 && trimmed[1] == '.' && trimmed[0] >= '0' && trimmed[0] <= '9' && trimmed[2] == ' ' {
		return true
	}
	return false
}

func splitByApproxTokenBudget(block string, maxApproxTokens int) []string {
	trimmed := strings.TrimSpace(block)
	if trimmed == "" {
		return nil
	}
	if maxApproxTokens <= 0 || approxTokensFromText(trimmed) <= maxApproxTokens {
		return []string{trimmed}
	}
	runes := []rune(trimmed)
	step := canonicalChunkTargetApproxRunes
	if step <= 0 {
		step = 2048
	}
	out := make([]string, 0, len(runes)/step+1)
	for start := 0; start < len(runes); start += step {
		end := start + step
		if end > len(runes) {
			end = len(runes)
		}
		part := strings.TrimSpace(string(runes[start:end]))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func mapReplacementsToContextSpans(
	replacements []data.ThreadContextReplacementRecord,
	chunks []canonicalChunk,
	upperBoundContextSeq *int64,
) []canonicalReplacementSpan {
	if len(replacements) == 0 {
		return nil
	}
	out := make([]canonicalReplacementSpan, 0, len(replacements))
	for _, repl := range replacements {
		if strings.TrimSpace(repl.SummaryText) == "" {
			continue
		}
		startContextSeq, endContextSeq, ok := resolveContextSeqRangeForReplacement(chunks, repl)
		if !ok {
			continue
		}
		if upperBoundContextSeq != nil && endContextSeq > *upperBoundContextSeq {
			continue
		}
		out = append(out, canonicalReplacementSpan{
			Record:          repl,
			StartContextSeq: startContextSeq,
			EndContextSeq:   endContextSeq,
		})
	}
	return out
}

func resolveContextSeqRangeForReplacement(chunks []canonicalChunk, repl data.ThreadContextReplacementRecord) (int64, int64, bool) {
	if repl.StartThreadSeq > 0 && repl.EndThreadSeq > 0 && repl.StartThreadSeq <= repl.EndThreadSeq {
		if startContextSeq, endContextSeq, ok := resolveContextSeqRangeForThreadSeqRange(chunks, repl.StartThreadSeq, repl.EndThreadSeq); ok {
			return startContextSeq, endContextSeq, true
		}
	}
	startContextSeq := repl.StartContextSeq
	endContextSeq := repl.EndContextSeq
	if startContextSeq <= 0 || endContextSeq <= 0 || startContextSeq > endContextSeq {
		return 0, 0, false
	}
	return startContextSeq, endContextSeq, true
}

func resolveContextSeqRangeForThreadSeqRange(chunks []canonicalChunk, startThreadSeq, endThreadSeq int64) (int64, int64, bool) {
	if len(chunks) == 0 || startThreadSeq <= 0 || endThreadSeq <= 0 || startThreadSeq > endThreadSeq {
		return 0, 0, false
	}
	startContextSeq := int64(0)
	endContextSeq := int64(0)
	for _, chunk := range chunks {
		if chunk.EndThreadSeq < startThreadSeq {
			continue
		}
		if chunk.StartThreadSeq > endThreadSeq {
			break
		}
		if startContextSeq == 0 || chunk.ContextSeq < startContextSeq {
			startContextSeq = chunk.ContextSeq
		}
		if chunk.ContextSeq > endContextSeq {
			endContextSeq = chunk.ContextSeq
		}
	}
	if startContextSeq == 0 || endContextSeq == 0 || startContextSeq > endContextSeq {
		return 0, 0, false
	}
	return startContextSeq, endContextSeq, true
}

func selectRenderableReplacementSpans(items []canonicalReplacementSpan, lastAtom *canonicalAtom) []canonicalReplacementSpan {
	if len(items) == 0 {
		return nil
	}
	filtered := make([]canonicalReplacementSpan, 0, len(items))
	for _, item := range items {
		if item.StartContextSeq <= 0 || item.EndContextSeq <= 0 || item.StartContextSeq > item.EndContextSeq {
			continue
		}
		if lastAtom != nil && item.EndContextSeq >= lastAtom.EndContextSeq {
			continue
		}
		if lastAtom != nil &&
			rangesOverlap(item.StartContextSeq, item.EndContextSeq, lastAtom.StartContextSeq, lastAtom.EndContextSeq) &&
			!atomSupportsPartialTail(*lastAtom) {
			continue
		}
		filtered = append(filtered, item)
	}
	if len(filtered) == 0 {
		return nil
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Record.Layer != filtered[j].Record.Layer {
			return filtered[i].Record.Layer > filtered[j].Record.Layer
		}
		if !filtered[i].Record.CreatedAt.Equal(filtered[j].Record.CreatedAt) {
			return filtered[i].Record.CreatedAt.After(filtered[j].Record.CreatedAt)
		}
		return filtered[i].StartContextSeq < filtered[j].StartContextSeq
	})

	selected := make([]canonicalReplacementSpan, 0, len(filtered))
	for _, candidate := range filtered {
		overlaps := false
		for _, existing := range selected {
			if rangesOverlap(candidate.StartContextSeq, candidate.EndContextSeq, existing.StartContextSeq, existing.EndContextSeq) {
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
		if selected[i].Record.Layer != selected[j].Record.Layer {
			return selected[i].Record.Layer > selected[j].Record.Layer
		}
		return selected[i].Record.CreatedAt.Before(selected[j].Record.CreatedAt)
	})
	return selected
}

func rangesOverlap(aStart, aEnd, bStart, bEnd int64) bool {
	if aStart <= 0 || aEnd <= 0 || bStart <= 0 || bEnd <= 0 {
		return false
	}
	return aStart <= bEnd && bStart <= aEnd
}

func ensureCanonicalThreadGraphPersisted(
	ctx context.Context,
	tx pgx.Tx,
	messagesRepo data.MessagesRepository,
	accountID uuid.UUID,
	threadID uuid.UUID,
) (*persistedCanonicalThreadGraph, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return nil, fmt.Errorf("account_id and thread_id must not be empty")
	}
	messages, err := messagesRepo.ListRawByThread(ctx, tx, accountID, threadID, canonicalPersistFetchLimit)
	if err != nil {
		return nil, err
	}
	atoms, chunks := buildCanonicalAtomGraph(messages)

	atomsRepo := data.ThreadContextAtomsRepository{}
	chunksRepo := data.ThreadContextChunksRepository{}
	atomRecords := make(map[string]*data.ProtocolAtomRecord, len(atoms))
	chunkRecords := make(map[int64]*data.ContextChunkRecord, len(chunks))

	for i := range atoms {
		role := ""
		if len(atoms[i].Messages) > 0 {
			role = strings.TrimSpace(atoms[i].Messages[0].Role)
		}
		record, err := atomsRepo.Upsert(ctx, tx, data.ProtocolAtomInsertInput{
			AccountID:             accountID,
			ThreadID:              threadID,
			AtomSeq:               int64(i + 1),
			AtomKind:              string(atoms[i].Kind),
			Role:                  role,
			SourceMessageStartSeq: atoms[i].StartThreadSeq,
			SourceMessageEndSeq:   atoms[i].EndThreadSeq,
			PayloadText:           atomFallbackChunk(atoms[i]),
		})
		if err != nil {
			return nil, err
		}
		atomRecords[atoms[i].Key] = record
	}

	chunkSeqByAtomKey := make(map[string]int64, len(atoms))
	for i := range chunks {
		atomRecord := atomRecords[chunks[i].AtomKey]
		if atomRecord == nil {
			return nil, fmt.Errorf("atom record missing for %s", chunks[i].AtomKey)
		}
		chunkSeqByAtomKey[chunks[i].AtomKey]++
		record, err := chunksRepo.Upsert(ctx, tx, data.ContextChunkInsertInput{
			AccountID:   accountID,
			ThreadID:    threadID,
			AtomID:      atomRecord.ID,
			ChunkSeq:    chunkSeqByAtomKey[chunks[i].AtomKey],
			ContextSeq:  chunks[i].ContextSeq,
			ChunkKind:   "payload",
			PayloadText: chunks[i].Content,
		})
		if err != nil {
			return nil, err
		}
		chunkRecords[chunks[i].ContextSeq] = record
	}

	return &persistedCanonicalThreadGraph{
		Atoms:                    atoms,
		Chunks:                   chunks,
		AtomRecordsByKey:         atomRecords,
		ChunkRecordsByContextSeq: chunkRecords,
	}, nil
}

func (g *persistedCanonicalThreadGraph) chunkTargetsForThreadSeqRange(startThreadSeq, endThreadSeq int64) ([]uuid.UUID, int64, int64, bool) {
	if g == nil || len(g.Chunks) == 0 || startThreadSeq <= 0 || endThreadSeq <= 0 || startThreadSeq > endThreadSeq {
		return nil, 0, 0, false
	}
	chunkIDs := make([]uuid.UUID, 0, endThreadSeq-startThreadSeq+1)
	startContextSeq := int64(0)
	endContextSeq := int64(0)
	for _, chunk := range g.Chunks {
		if chunk.EndThreadSeq < startThreadSeq {
			continue
		}
		if chunk.StartThreadSeq > endThreadSeq {
			break
		}
		record := g.ChunkRecordsByContextSeq[chunk.ContextSeq]
		if record == nil {
			return nil, 0, 0, false
		}
		chunkIDs = append(chunkIDs, record.ID)
		if startContextSeq == 0 || chunk.ContextSeq < startContextSeq {
			startContextSeq = chunk.ContextSeq
		}
		if chunk.ContextSeq > endContextSeq {
			endContextSeq = chunk.ContextSeq
		}
	}
	if len(chunkIDs) == 0 || startContextSeq <= 0 || endContextSeq <= 0 {
		return nil, 0, 0, false
	}
	return chunkIDs, startContextSeq, endContextSeq, true
}

func atomSupportsPartialTail(atom canonicalAtom) bool {
	if atom.Kind == canonicalAtomToolEpisode || len(atom.Messages) != 1 {
		return false
	}
	role := strings.TrimSpace(atom.Messages[0].Role)
	if role != "user" && role != "assistant" {
		return false
	}
	if len(atom.Messages[0].ContentJSON) == 0 {
		return true
	}
	parsed, err := messagecontent.Parse(atom.Messages[0].ContentJSON)
	if err != nil {
		return false
	}
	normalized, err := messagecontent.Normalize(parsed.Parts)
	if err != nil {
		return false
	}
	if len(normalized.Parts) == 0 {
		return true
	}
	for _, part := range normalized.Parts {
		if part.Type != messagecontent.PartTypeText {
			return false
		}
	}
	return true
}
