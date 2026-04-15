package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
	"github.com/pkoukk/tiktoken-go"
)

type FrontierNodeKind string

const (
	FrontierNodeChunk       FrontierNodeKind = "chunk"
	FrontierNodeReplacement FrontierNodeKind = "replacement"
)

type FrontierNode struct {
	Kind            FrontierNodeKind
	NodeID          uuid.UUID
	Layer           int
	StartContextSeq int64
	EndContextSeq   int64
	StartThreadSeq  int64
	EndThreadSeq    int64
	SourceText      string
	ApproxTokens    int

	MsgStart  int
	MsgEnd    int
	AtomSeq   int
	AtomType  compactAtomType
	Role      string
	atomKey   string
	chunkSeq  int64
	chunkKind string
	role      string
}

func compactFrontierNodeID(kind FrontierNodeKind, atomSeq int, msgStart int, msgEnd int, part int) uuid.UUID {
	return uuid.NewSHA1(uuid.Nil, []byte(fmt.Sprintf("%s:%d:%d:%d:%d", kind, atomSeq, msgStart, msgEnd, part)))
}

type compactFrontierSelection struct {
	Nodes          []FrontierNode
	EndNodeIndex   int
	TargetTokens   int
	PartialTail    bool
	SelectedTokens int
}

const compactMaxAtomsPerRound = 20

type compactProgressRecorder struct {
	base             map[string]any
	emitFn           func(context.Context, *RunContext, map[string]any) error
	appendStandardFn func(context.Context, *RunContext, events.RunEvent) error
}

func newCompactProgressRecorder(
	pool CompactPersistDB,
	eventsRepo CompactRunEventAppender,
	base map[string]any,
) compactProgressRecorder {
	return compactProgressRecorder{
		base: cloneContextCompactEventData(base),
		emitFn: func(ctx context.Context, rc *RunContext, data map[string]any) error {
			return appendContextCompactRunEvent(ctx, pool, eventsRepo, rc, data)
		},
		appendStandardFn: func(ctx context.Context, rc *RunContext, ev events.RunEvent) error {
			return appendScopedCompactStandardEvent(ctx, pool, eventsRepo, rc, ev)
		},
	}
}

func (r compactProgressRecorder) emit(ctx context.Context, rc *RunContext, phase string, data map[string]any) {
	if rc == nil || r.emitFn == nil {
		return
	}
	payload := cloneContextCompactEventData(r.base)
	payload["phase"] = phase
	for key, value := range data {
		payload[key] = value
	}
	if err := r.emitFn(ctx, rc, payload); err != nil {
		runID := uuid.Nil
		if rc != nil {
			runID = rc.Run.ID
		}
		slog.WarnContext(ctx, "context_compact", "phase", phase, "err", err.Error(), "run_id", runID.String())
	}
}

func (r compactProgressRecorder) emitStandard(ctx context.Context, rc *RunContext, eventType string, data map[string]any) {
	if rc == nil || r.appendStandardFn == nil {
		return
	}
	payload := cloneContextCompactEventData(r.base)
	payload["event_scope"] = "context_compact"
	payload["compact_event_type"] = eventType
	for key, value := range data {
		payload[key] = value
	}
	ev := rc.Emitter.Emit(eventType, payload, nil, nil)
	if err := r.appendStandardFn(ctx, rc, ev); err != nil {
		slog.WarnContext(ctx, "context_compact_standard_event", "event_type", eventType, "err", err.Error(), "run_id", rc.Run.ID.String())
	}
}

func cloneContextCompactEventData(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func compactNodesApproxTokens(nodes []FrontierNode) int {
	total := 0
	for _, node := range nodes {
		if node.ApproxTokens > 0 {
			total += node.ApproxTokens
			continue
		}
		total += approxTokensFromText(node.SourceText)
	}
	return total
}

func contextCompactTargetTokens(cfg ContextCompactSettings, window int) int {
	targetPct := cfg.TargetContextPct
	if targetPct <= 0 {
		targetPct = 65
	}
	if targetPct > 100 {
		targetPct = 100
	}
	if window <= 0 {
		window = cfg.FallbackContextWindowTokens
	}
	if window <= 0 {
		return 0
	}
	target := window * targetPct / 100
	if target < 1 {
		return 1
	}
	return target
}

func buildCompactFrontierNodesFromMessages(enc *tiktoken.Tiktoken, msgs []llm.Message) []FrontierNode {
	return buildCompactFrontierNodesFromMessagesWithOptions(enc, msgs, false)
}

func buildCompactFrontierNodesFromMessagesWithOptions(enc *tiktoken.Tiktoken, msgs []llm.Message, skipSynthetic bool) []FrontierNode {
	if len(msgs) == 0 {
		return nil
	}
	nodes := make([]FrontierNode, 0, len(msgs))
	nextContextSeq := int64(1)
	nextAtomSeq := 1
	for i := 0; i < len(msgs); {
		msg := msgs[i]
		if skipSynthetic && msg.Phase != nil && strings.TrimSpace(*msg.Phase) == compactSyntheticPhase {
			i++
			continue
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			end := i + 1
			for end < len(msgs) && strings.TrimSpace(msgs[end].Role) == "tool" {
				end++
			}
			payload := serializeToolEpisodeForCompact(msgs[i:end])
			parts := splitCompactPayload(enc, payload)
			for partIndex, part := range parts {
				nodes = append(nodes, FrontierNode{
					Kind:            FrontierNodeChunk,
					NodeID:          compactFrontierNodeID(FrontierNodeChunk, nextAtomSeq, i, end-1, partIndex),
					StartContextSeq: nextContextSeq,
					EndContextSeq:   nextContextSeq,
					SourceText:      part,
					ApproxTokens:    compactTokenCount(enc, part),
					MsgStart:        i,
					MsgEnd:          end - 1,
					AtomSeq:         nextAtomSeq,
					AtomType:        compactAtomToolEpisode,
					Role:            "assistant",
				})
				nextContextSeq++
			}
			nextAtomSeq++
			i = end
			continue
		}
		if strings.TrimSpace(msg.Role) == "tool" {
			payload := serializeSingleToolForCompact(msg)
			parts := splitCompactPayload(enc, payload)
			for partIndex, part := range parts {
				nodes = append(nodes, FrontierNode{
					Kind:            FrontierNodeChunk,
					NodeID:          compactFrontierNodeID(FrontierNodeChunk, nextAtomSeq, i, i, partIndex),
					StartContextSeq: nextContextSeq,
					EndContextSeq:   nextContextSeq,
					SourceText:      part,
					ApproxTokens:    compactTokenCount(enc, part),
					MsgStart:        i,
					MsgEnd:          i,
					AtomSeq:         nextAtomSeq,
					AtomType:        compactAtomToolEpisode,
					Role:            "tool",
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
		parts := splitCompactPayload(enc, rawText)
		for partIndex, part := range parts {
			nodes = append(nodes, FrontierNode{
				Kind:            FrontierNodeChunk,
				NodeID:          compactFrontierNodeID(FrontierNodeChunk, nextAtomSeq, i, i, partIndex),
				StartContextSeq: nextContextSeq,
				EndContextSeq:   nextContextSeq,
				SourceText:      part,
				ApproxTokens:    compactTokenCount(enc, part),
				MsgStart:        i,
				MsgEnd:          i,
				AtomSeq:         nextAtomSeq,
				AtomType:        atomType,
				Role:            msg.Role,
			})
			nextContextSeq++
		}
		nextAtomSeq++
		i++
	}
	return nodes
}

func buildCompactFrontierAtomsFromMessages(enc *tiktoken.Tiktoken, msgs []llm.Message) []FrontierNode {
	return buildCompactFrontierAtomsFromMessagesWithOptions(enc, msgs, false)
}

func buildCompactFrontierAtomsFromMessagesWithOptions(enc *tiktoken.Tiktoken, msgs []llm.Message, skipSynthetic bool) []FrontierNode {
	if len(msgs) == 0 {
		return nil
	}
	nodes := make([]FrontierNode, 0, len(msgs))
	nextContextSeq := int64(1)
	nextAtomSeq := 1
	for i := 0; i < len(msgs); {
		msg := msgs[i]
		if skipSynthetic && msg.Phase != nil && strings.TrimSpace(*msg.Phase) == compactSyntheticPhase {
			i++
			continue
		}
		// replacement messages from previous compact rounds → FrontierNodeReplacement
		if msg.Phase != nil && strings.TrimSpace(*msg.Phase) == compactSyntheticPhase {
			rawText := strings.TrimSpace(messageText(msg))
			if rawText == "" {
				rawText = compactFallbackContentText(msg)
			}
			if rawText != "" {
				nodes = append(nodes, FrontierNode{
					Kind:            FrontierNodeReplacement,
					NodeID:          compactFrontierNodeID(FrontierNodeReplacement, nextAtomSeq, i, i, 0),
					StartContextSeq: nextContextSeq,
					EndContextSeq:   nextContextSeq,
					SourceText:      rawText,
					ApproxTokens:    compactTokenCount(enc, rawText),
					MsgStart:        i,
					MsgEnd:          i,
					AtomSeq:         nextAtomSeq,
					Role:            "system",
				})
				nextContextSeq++
				nextAtomSeq++
			}
			i++
			continue
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			end := i + 1
			for end < len(msgs) && strings.TrimSpace(msgs[end].Role) == "tool" {
				end++
			}
			sourceText := serializeToolEpisodeForCompact(msgs[i:end])
			if strings.TrimSpace(sourceText) != "" {
				nodes = append(nodes, FrontierNode{
					Kind:            FrontierNodeChunk,
					NodeID:          compactFrontierNodeID(FrontierNodeChunk, nextAtomSeq, i, end-1, 0),
					StartContextSeq: nextContextSeq,
					EndContextSeq:   nextContextSeq,
					SourceText:      sourceText,
					ApproxTokens:    compactTokenCount(enc, sourceText),
					MsgStart:        i,
					MsgEnd:          end - 1,
					AtomSeq:         nextAtomSeq,
					AtomType:        compactAtomToolEpisode,
					Role:            "assistant",
				})
				nextContextSeq++
				nextAtomSeq++
			}
			i = end
			continue
		}
		if strings.TrimSpace(msg.Role) == "tool" {
			sourceText := serializeSingleToolForCompact(msg)
			if strings.TrimSpace(sourceText) != "" {
				nodes = append(nodes, FrontierNode{
					Kind:            FrontierNodeChunk,
					NodeID:          compactFrontierNodeID(FrontierNodeChunk, nextAtomSeq, i, i, 0),
					StartContextSeq: nextContextSeq,
					EndContextSeq:   nextContextSeq,
					SourceText:      sourceText,
					ApproxTokens:    compactTokenCount(enc, sourceText),
					MsgStart:        i,
					MsgEnd:          i,
					AtomSeq:         nextAtomSeq,
					AtomType:        compactAtomToolEpisode,
					Role:            "tool",
				})
				nextContextSeq++
				nextAtomSeq++
			}
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
		nodes = append(nodes, FrontierNode{
			Kind:            FrontierNodeChunk,
			NodeID:          compactFrontierNodeID(FrontierNodeChunk, nextAtomSeq, i, i, 0),
			StartContextSeq: nextContextSeq,
			EndContextSeq:   nextContextSeq,
			SourceText:      rawText,
			ApproxTokens:    compactTokenCount(enc, rawText),
			MsgStart:        i,
			MsgEnd:          i,
			AtomSeq:         nextAtomSeq,
			AtomType:        atomType,
			Role:            msg.Role,
		})
		nextContextSeq++
		nextAtomSeq++
		i++
	}
	return nodes
}

func buildCompactFrontierAtomsFromPersistFrontier(frontier []FrontierNode) []FrontierNode {
	if len(frontier) == 0 {
		return nil
	}
	nodes := make([]FrontierNode, 0, len(frontier))
	nextMsgIndex := 0
	for i := 0; i < len(frontier); {
		current := frontier[i]
		if current.Kind == FrontierNodeReplacement {
			text := strings.TrimSpace(current.SourceText)
			if text != "" {
				current.SourceText = text
				if current.ApproxTokens <= 0 {
					current.ApproxTokens = approxTokensFromText(text)
				}
				if strings.TrimSpace(current.Role) == "" {
					current.Role = "system"
				}
				if current.AtomSeq <= 0 {
					current.AtomSeq = nextMsgIndex + 1
				}
				current.MsgStart = nextMsgIndex
				current.MsgEnd = nextMsgIndex
				nodes = append(nodes, current)
				nextMsgIndex++
			}
			i++
			continue
		}

		merged := current
		textParts := make([]string, 0, 4)
		if text := strings.TrimSpace(current.SourceText); text != "" {
			textParts = append(textParts, text)
		}
		approxTokens := current.ApproxTokens
		j := i + 1
		for j < len(frontier) {
			next := frontier[j]
			if next.Kind != FrontierNodeChunk {
				break
			}
			if merged.atomKey != "" {
				if next.atomKey != merged.atomKey {
					break
				}
			} else if next.AtomSeq != merged.AtomSeq {
				break
			}
			if next.StartContextSeq != merged.EndContextSeq+1 {
				break
			}
			if text := strings.TrimSpace(next.SourceText); text != "" {
				textParts = append(textParts, text)
			}
			if next.ApproxTokens > 0 {
				approxTokens += next.ApproxTokens
			}
			if next.EndContextSeq > merged.EndContextSeq {
				merged.EndContextSeq = next.EndContextSeq
			}
			if next.EndThreadSeq > merged.EndThreadSeq {
				merged.EndThreadSeq = next.EndThreadSeq
			}
			j++
		}

		merged.SourceText = strings.TrimSpace(strings.Join(textParts, "\n\n"))
		if merged.SourceText != "" {
			if approxTokens <= 0 {
				approxTokens = approxTokensFromText(merged.SourceText)
			}
			merged.ApproxTokens = approxTokens
			if strings.TrimSpace(merged.Role) == "" {
				merged.Role = strings.TrimSpace(merged.role)
			}
			if merged.AtomSeq <= 0 {
				merged.AtomSeq = nextMsgIndex + 1
			}
			merged.MsgStart = nextMsgIndex
			merged.MsgEnd = nextMsgIndex
			nodes = append(nodes, merged)
			nextMsgIndex++
		}
		i = j
	}
	return nodes
}

func mapSelectedAtomsToPersistFrontierNodes(selectedAtoms []FrontierNode, frontier []FrontierNode) []FrontierNode {
	if len(selectedAtoms) == 0 || len(frontier) == 0 {
		return nil
	}
	out := make([]FrontierNode, 0, len(selectedAtoms))
	seen := make(map[uuid.UUID]struct{}, len(selectedAtoms))
	for _, selected := range selectedAtoms {
		if selected.Kind == FrontierNodeReplacement {
			for _, node := range frontier {
				if node.Kind != FrontierNodeReplacement {
					continue
				}
				if selected.NodeID != uuid.Nil {
					if node.NodeID == uuid.Nil || node.NodeID != selected.NodeID {
						continue
					}
				} else if node.StartContextSeq != selected.StartContextSeq ||
					node.EndContextSeq != selected.EndContextSeq ||
					node.StartThreadSeq != selected.StartThreadSeq ||
					node.EndThreadSeq != selected.EndThreadSeq {
					continue
				}
				if _, ok := seen[node.NodeID]; ok {
					break
				}
				seen[node.NodeID] = struct{}{}
				out = append(out, node)
				break
			}
			continue
		}
		for _, node := range frontier {
			if node.Kind != FrontierNodeChunk || node.NodeID == uuid.Nil {
				continue
			}
			if node.atomKey != selected.atomKey {
				continue
			}
			if node.StartContextSeq < selected.StartContextSeq || node.EndContextSeq > selected.EndContextSeq {
				continue
			}
			if node.StartThreadSeq < selected.StartThreadSeq || node.EndThreadSeq > selected.EndThreadSeq {
				continue
			}
			if _, ok := seen[node.NodeID]; ok {
				continue
			}
			seen[node.NodeID] = struct{}{}
			out = append(out, node)
		}
	}
	return out
}

func selectCompactFrontierWindow(nodes []FrontierNode, deficitTokens int, maxInputTokens int) compactFrontierSelection {
	if len(nodes) == 0 {
		return compactFrontierSelection{}
	}
	protectedAtomSeq := nodes[len(nodes)-1].AtomSeq
	eligibleEnd := len(nodes)
	for i := range nodes {
		if nodes[i].AtomSeq == protectedAtomSeq {
			eligibleEnd = i
			break
		}
	}
	if eligibleEnd <= 0 {
		return compactFrontierSelection{}
	}

	targetTokens := int(math.Ceil(float64(deficitTokens) / 0.8))
	if maxTargetTokens := maxInputTokens / 2; maxTargetTokens > 0 && targetTokens > maxTargetTokens {
		targetTokens = maxTargetTokens
	}
	if targetTokens < 1024 {
		targetTokens = 1024
	}
	selection := compactFrontierSelection{
		EndNodeIndex: -1,
		TargetTokens: targetTokens,
	}
	for i := 0; i < eligibleEnd; i++ {
		selection.Nodes = append(selection.Nodes, nodes[i])
		selection.EndNodeIndex = i
		selection.SelectedTokens += nodes[i].ApproxTokens
		if selection.SelectedTokens >= targetTokens {
			break
		}
	}
	if selection.EndNodeIndex < 0 {
		return compactFrontierSelection{}
	}
	if last := selection.Nodes[len(selection.Nodes)-1]; last.AtomType == compactAtomToolEpisode {
		for i := selection.EndNodeIndex + 1; i < eligibleEnd && nodes[i].AtomSeq == last.AtomSeq; i++ {
			selection.Nodes = append(selection.Nodes, nodes[i])
			selection.EndNodeIndex = i
			selection.SelectedTokens += nodes[i].ApproxTokens
		}
	}
	for selection.EndNodeIndex >= 0 && selection.SelectedTokens > maxInputTokens {
		last := selection.Nodes[len(selection.Nodes)-1]
		cut := 1
		if last.AtomType == compactAtomToolEpisode {
			for cut < len(selection.Nodes) && selection.Nodes[len(selection.Nodes)-1-cut].AtomSeq == last.AtomSeq {
				cut++
			}
		}
		for i := 0; i < cut; i++ {
			selection.SelectedTokens -= selection.Nodes[len(selection.Nodes)-1].ApproxTokens
			selection.Nodes = selection.Nodes[:len(selection.Nodes)-1]
			selection.EndNodeIndex--
		}
	}
	if len(selection.Nodes) == 0 {
		return compactFrontierSelection{}
	}
	if selection.EndNodeIndex+1 < len(nodes) && nodes[selection.EndNodeIndex+1].AtomSeq == nodes[selection.EndNodeIndex].AtomSeq {
		selection.PartialTail = nodes[selection.EndNodeIndex].AtomType != compactAtomToolEpisode
	}
	return selection
}

func selectCompactAtomWindow(nodes []FrontierNode, deficitTokens int, maxInputTokens int) compactFrontierSelection {
	if len(nodes) == 0 {
		return compactFrontierSelection{}
	}
	protectedAtomSeq := nodes[len(nodes)-1].AtomSeq
	eligibleEnd := len(nodes)
	for i := range nodes {
		if nodes[i].AtomSeq == protectedAtomSeq {
			eligibleEnd = i
			break
		}
	}
	if eligibleEnd <= 0 {
		return compactFrontierSelection{}
	}

	targetTokens := deficitTokens
	if targetTokens < 1024 {
		targetTokens = 1024
	}
	if maxTargetTokens := maxInputTokens / 2; maxTargetTokens > 0 && targetTokens > maxTargetTokens {
		targetTokens = maxTargetTokens
	}
	if maxInputTokens > 0 && targetTokens > maxInputTokens {
		targetTokens = maxInputTokens
	}
	selection := compactFrontierSelection{
		EndNodeIndex: -1,
		TargetTokens: targetTokens,
	}
	for i := 0; i < eligibleEnd; i++ {
		selection.Nodes = append(selection.Nodes, nodes[i])
		selection.EndNodeIndex = i
		selection.SelectedTokens += nodes[i].ApproxTokens
		if len(selection.Nodes) >= compactMaxAtomsPerRound || (targetTokens > 0 && selection.SelectedTokens >= targetTokens) {
			break
		}
	}
	if selection.EndNodeIndex < 0 {
		return compactFrontierSelection{}
	}
	if maxInputTokens > 0 && selection.SelectedTokens > maxInputTokens {
		selection.SelectedTokens -= selection.Nodes[len(selection.Nodes)-1].ApproxTokens
		selection.Nodes = selection.Nodes[:len(selection.Nodes)-1]
		selection.EndNodeIndex--
	}
	if len(selection.Nodes) == 0 {
		return compactFrontierSelection{}
	}
	// 禁止单独 compact 一个 replacement，无法推进覆盖范围
	if len(selection.Nodes) == 1 && selection.Nodes[0].Kind == FrontierNodeReplacement {
		return compactFrontierSelection{}
	}
	return selection
}

func buildCompactSummaryInputFromNodes(nodes []FrontierNode) string {
	if len(nodes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(nodes))
	for _, node := range nodes {
		text := strings.TrimSpace(node.SourceText)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func buildCompactSummaryInputFromAtoms(nodes []FrontierNode) string {
	if len(nodes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(nodes))
	for i := 0; i < len(nodes); {
		if block, next := buildCompactEnvelopeBurstBlock(nodes, i); next > i {
			parts = append(parts, block)
			i = next
			continue
		}
		node := nodes[i]
		text := strings.TrimSpace(projectCompactNodeText(node))
		if text == "" {
			i++
			continue
		}
		tag := "[assistant]"
		switch {
		case node.Kind == FrontierNodeReplacement:
			tag = "[summary]"
		case node.AtomType == compactAtomToolEpisode:
			tag = "[tool_episode]"
		case strings.TrimSpace(node.Role) == "user" || node.AtomType == compactAtomUserText:
			tag = "[user]"
		case strings.TrimSpace(node.Role) == "assistant" || node.AtomType == compactAtomAssistantText:
			tag = "[assistant]"
		}
		parts = append(parts, tag+"\n"+text)
		i++
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func buildCompactEnvelopeBurstBlock(nodes []FrontierNode, start int) (string, int) {
	if start < 0 || start >= len(nodes) {
		return "", start
	}
	msgs := make([]llm.Message, 0, len(nodes)-start)
	for i := start; i < len(nodes); i++ {
		node := nodes[i]
		if !compactNodeUsesEnvelopeProjection(node) {
			break
		}
		text := strings.TrimSpace(node.SourceText)
		if text == "" {
			break
		}
		msgs = append(msgs, llm.Message{
			Role:    "user",
			Content: []llm.ContentPart{{Type: messagecontent.PartTypeText, Text: text}},
		})
	}
	if len(msgs) == 0 {
		return "", start
	}
	compacted, _, ok := compactTelegramGroupEnvelopeBurst(msgs)
	if !ok {
		return "", start
	}
	compacted = strings.TrimSpace(compacted)
	if compacted == "" {
		return "", start
	}
	return "[user]\n" + compacted, start + len(msgs)
}

func compactNodeUsesEnvelopeProjection(node FrontierNode) bool {
	if node.Kind != FrontierNodeChunk {
		return false
	}
	if strings.TrimSpace(node.Role) != "user" && node.AtomType != compactAtomUserText {
		return false
	}
	text := strings.TrimSpace(node.SourceText)
	if text == "" {
		return false
	}
	meta, _, ok := parseTelegramEnvelopeText(text)
	if !ok {
		return false
	}
	return isGroupMergeEligibleChannel(strings.TrimSpace(meta["channel"]))
}

func projectCompactNodeText(node FrontierNode) string {
	text := strings.TrimSpace(node.SourceText)
	if text == "" {
		return ""
	}
	if node.Kind == FrontierNodeReplacement {
		return text
	}
	if fields := parseEnvelope(text); fields != nil {
		return formatNaturalPrefix(fields)
	}
	return text
}

func runContextCompactLLMForNodes(
	ctx context.Context,
	rc *RunContext,
	gateway llm.Gateway,
	model string,
	nodes []FrontierNode,
	progress compactProgressRecorder,
	attempt int,
) (string, error) {
	if gateway == nil || strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("gateway or model missing")
	}
	targetText := buildCompactSummaryInputFromAtoms(nodes)
	if targetText == "" {
		return "", nil
	}
	runes := []rune(targetText)
	if len(runes) > contextCompactMaxLLMInputRunes {
		targetText = string(runes[:contextCompactMaxLLMInputRunes])
	}
	var userBlock strings.Builder
	userBlock.WriteString("<target-chunks>\n")
	userBlock.WriteString(targetText)
	userBlock.WriteString("\n</target-chunks>\n\n")
	userBlock.WriteString(contextCompactInitialPrompt)

	maxTok := contextCompactMaxOut
	req := llm.Request{
		Model: model,
		Messages: []llm.Message{
			{Role: "system", Content: []llm.TextPart{{Text: compactSystemPromptForRun(ctx, rc, contextCompactSystemPrompt, nil)}}},
			{Role: "user", Content: []llm.TextPart{{Text: userBlock.String()}}},
		},
		MaxOutputTokens: &maxTok,
	}
	streamCtx, cancel := context.WithTimeout(ctx, contextCompactStreamTimeout)
	defer cancel()

	progress.emit(ctx, rc, "llm_request_started", map[string]any{
		"attempt":                attempt,
		"atoms_attempted":        len(nodes),
		"input_tokens":           compactNodesApproxTokens(nodes),
		"input_runes":            len([]rune(targetText)),
		"model":                  model,
		"stream_timeout_seconds": int(contextCompactStreamTimeout / time.Second),
	})

	var chunks []string
	startedAt := time.Now()
	err := gateway.Stream(streamCtx, req, func(ev llm.StreamEvent) error {
		switch typed := ev.(type) {
		case llm.StreamLlmRequest:
			progress.emitStandard(ctx, rc, "llm.request", typed.ToDataJSON())
		case llm.StreamLlmResponseChunk:
			progress.emitStandard(ctx, rc, "llm.response.chunk", typed.ToDataJSON())
		case llm.StreamMessageDelta:
			if typed.Channel != nil && *typed.Channel == "thinking" {
				return nil
			}
			if typed.ContentDelta != "" {
				chunks = append(chunks, typed.ContentDelta)
			}
		case llm.StreamRunCompleted:
			progress.emitStandard(ctx, rc, "llm.turn.completed", typed.ToDataJSON())
			return errContextCompactStreamDone
		case llm.StreamRunFailed:
			return fmt.Errorf("stream failed: %s", typed.Error.Message)
		}
		return nil
	})
	if err != nil && !errors.Is(err, errContextCompactStreamDone) {
		return "", err
	}
	summary := strings.TrimSpace(strings.Join(chunks, ""))
	progress.emit(ctx, rc, "llm_request_completed", map[string]any{
		"attempt":         attempt,
		"atoms_attempted": len(nodes),
		"summary_tokens":  approxTokensFromText(summary),
		"elapsed_ms":      time.Since(startedAt).Milliseconds(),
	})
	return summary, nil
}

func compactNodesWithShrinkRetry(
	ctx context.Context,
	rc *RunContext,
	gateway llm.Gateway,
	model string,
	nodes []FrontierNode,
	progress compactProgressRecorder,
) (string, []FrontierNode, error) {
	if len(nodes) == 0 {
		return "", nil, nil
	}
	current := append([]FrontierNode(nil), nodes...)
	attempt := 1
	for len(current) > 0 {
		summary, err := runContextCompactLLMForNodes(ctx, rc, gateway, model, current, progress, attempt)
		if err == nil {
			return summary, current, nil
		}
		if !shouldShrinkCompactNodes(err) || len(current) == 1 {
			return "", current, err
		}
		last := current[len(current)-1]
		cut := 1
		if last.AtomType == compactAtomToolEpisode {
			for cut < len(current) && current[len(current)-1-cut].AtomSeq == last.AtomSeq {
				cut++
			}
		}
		progress.emit(ctx, rc, "llm_request_retrying", map[string]any{
			"attempt":         attempt,
			"atoms_attempted": len(current),
			"atoms_dropped":   cut,
			"atoms_remaining": len(current) - cut,
			"llm_error":       err.Error(),
		})
		current = current[:len(current)-cut]
		attempt++
	}
	return "", nil, nil
}

func shouldShrinkCompactNodes(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	errMsg := strings.ToLower(strings.TrimSpace(err.Error()))
	return !strings.Contains(errMsg, "unauthorized") && !strings.Contains(errMsg, "forbidden")
}

func shrinkFrontierNodesToMessageBoundary(nodes []FrontierNode) []FrontierNode {
	current := append([]FrontierNode(nil), nodes...)
	for len(current) > 0 {
		last := current[len(current)-1]
		if last.AtomType == compactAtomToolEpisode {
			return current
		}
		if len(current) == 1 {
			return current
		}
		if current[len(current)-2].MsgStart != last.MsgStart {
			return current
		}
		current = current[:len(current)-1]
	}
	return nil
}

func compactNodesWithPersistRetry(
	ctx context.Context,
	rc *RunContext,
	gateway llm.Gateway,
	model string,
	nodes []FrontierNode,
	progress compactProgressRecorder,
) (string, []FrontierNode, error) {
	current := shrinkFrontierNodesToMessageBoundary(nodes)
	for len(current) > 0 {
		summary, usedNodes, err := compactNodesWithShrinkRetry(ctx, rc, gateway, model, current, progress)
		if err != nil {
			return "", usedNodes, err
		}
		bounded := shrinkFrontierNodesToMessageBoundary(usedNodes)
		if len(bounded) == 0 {
			return "", nil, nil
		}
		if len(bounded) != len(usedNodes) {
			current = bounded
			continue
		}
		return summary, bounded, nil
	}
	return "", nil, nil
}

func isContextWindowExceeded(errMsg string) bool {
	for _, kw := range []string{"context_length_exceeded", "max_tokens", "too many tokens", "maximum context length", "token limit"} {
		if strings.Contains(errMsg, kw) {
			return true
		}
	}
	return false
}

func materializeCompactedPrefixAtoms(
	msgs []llm.Message,
	nodes []FrontierNode,
	endNodeIndex int,
	summary string,
) []llm.Message {
	if len(msgs) == 0 || len(nodes) == 0 || endNodeIndex < 0 || endNodeIndex >= len(nodes) {
		return msgs
	}
	endNode := nodes[endNodeIndex]
	out := make([]llm.Message, 0, len(msgs)+1)
	out = append(out, makeThreadContextReplacementMessage(summary))
	out = append(out, msgs[endNode.MsgEnd+1:]...)
	return out
}

func materializeCompactedPrefixMessages(
	msgs []llm.Message,
	nodes []FrontierNode,
	endNodeIndex int,
	summary string,
) []llm.Message {
	if len(msgs) == 0 || len(nodes) == 0 || endNodeIndex < 0 || endNodeIndex >= len(nodes) {
		return msgs
	}
	out := make([]llm.Message, 0, len(msgs)+1)
	out = append(out, makeThreadContextReplacementMessage(summary))

	endNode := nodes[endNodeIndex]
	nextNodeIndex := endNodeIndex + 1
	if nextNodeIndex < len(nodes) && nodes[nextNodeIndex].MsgStart == endNode.MsgStart && endNode.AtomType != compactAtomToolEpisode {
		tailParts := make([]string, 0, 4)
		for i := nextNodeIndex; i < len(nodes) && nodes[i].MsgStart == endNode.MsgStart; i++ {
			tailParts = append(tailParts, strings.TrimSpace(nodes[i].SourceText))
		}
		tailText := strings.TrimSpace(strings.Join(tailParts, "\n\n"))
		if tailText != "" {
			tailMsg := llm.Message{
				Role:    msgs[endNode.MsgStart].Role,
				Phase:   msgs[endNode.MsgStart].Phase,
				Content: []llm.ContentPart{{Type: messagecontent.PartTypeText, Text: tailText}},
			}
			out = append(out, tailMsg)
		}
		out = append(out, msgs[endNode.MsgStart+1:]...)
		return out
	}

	out = append(out, msgs[endNode.MsgEnd+1:]...)
	return out
}

func materializeCompactedPrefixIDs(ids []uuid.UUID, nodes []FrontierNode, endNodeIndex int) []uuid.UUID {
	if len(ids) == 0 || len(nodes) == 0 || endNodeIndex < 0 || endNodeIndex >= len(nodes) {
		return ids
	}
	out := make([]uuid.UUID, 0, len(ids)+1)
	out = append(out, uuid.Nil)
	endNode := nodes[endNodeIndex]
	nextNodeIndex := endNodeIndex + 1
	if nextNodeIndex < len(nodes) && nodes[nextNodeIndex].MsgStart == endNode.MsgStart && endNode.AtomType != compactAtomToolEpisode {
		out = append(out, ids[endNode.MsgStart])
		out = append(out, ids[endNode.MsgStart+1:]...)
		return out
	}
	out = append(out, ids[endNode.MsgEnd+1:]...)
	return out
}
