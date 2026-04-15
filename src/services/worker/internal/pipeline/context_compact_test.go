package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pkoukk/tiktoken-go"
)

func testProviderRequestEstimate(req llm.Request) (int, error) {
	raw, err := json.Marshal(req.ToJSON())
	if err != nil {
		return 0, err
	}
	return len(raw), nil
}

type stubCompactGateway struct {
	summary string
	calls   int
	lastReq llm.Request
}

func (g *stubCompactGateway) Stream(_ context.Context, request llm.Request, yield func(llm.StreamEvent) error) error {
	g.calls++
	g.lastReq = request
	if err := yield(llm.StreamLlmRequest{
		LlmCallID:    "compact-call-1",
		ProviderKind: "stub",
		APIMode:      "responses",
		InputJSON: map[string]any{
			"messages": request.Messages,
		},
		PayloadJSON: request.ToJSON(),
	}); err != nil {
		return err
	}
	if err := yield(llm.StreamMessageDelta{ContentDelta: g.summary, Role: "assistant"}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{
		LlmCallID: "compact-call-1",
		Usage: &llm.Usage{
			InputTokens:  ptrInt(12),
			OutputTokens: ptrInt(4),
		},
	})
}

type flakyCompactGateway struct {
	calls   int
	summary string
}

func (g *flakyCompactGateway) Stream(_ context.Context, _ llm.Request, yield func(llm.StreamEvent) error) error {
	g.calls++
	if g.calls == 1 {
		return fmt.Errorf("sse stream interrupted")
	}
	if err := yield(llm.StreamMessageDelta{ContentDelta: g.summary, Role: "assistant"}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{})
}

type shrinkRetryGateway struct {
	summary string
}

func (g *shrinkRetryGateway) Stream(_ context.Context, request llm.Request, yield func(llm.StreamEvent) error) error {
	if len(request.Messages) > 1 && len(request.Messages[1].Content) > 0 {
		text := request.Messages[1].Content[0].Text
		if strings.Contains(text, "first") && strings.Contains(text, "second") {
			return errors.New("sse stream interrupted")
		}
	}
	if err := yield(llm.StreamLlmRequest{
		LlmCallID:    "compact-call-retry",
		ProviderKind: "stub",
		APIMode:      "responses",
		InputJSON: map[string]any{
			"messages": request.Messages,
		},
		PayloadJSON: request.ToJSON(),
	}); err != nil {
		return err
	}
	if err := yield(llm.StreamMessageDelta{ContentDelta: g.summary, Role: "assistant"}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{LlmCallID: "compact-call-retry"})
}

type captureCompactEventAppender struct {
	events []events.RunEvent
}

func (c *captureCompactEventAppender) AppendRunEvent(_ context.Context, _ pgx.Tx, _ uuid.UUID, ev events.RunEvent) (int64, error) {
	c.events = append(c.events, ev)
	return int64(len(c.events)), nil
}

func ptrInt(v int) *int { return &v }

type captureCompactionAdvisor struct {
	outputs []CompactOutput
}

func (a *captureCompactionAdvisor) HookProviderName() string { return "capture_compaction" }

func (a *captureCompactionAdvisor) BeforeCompact(context.Context, *RunContext, CompactInput) (CompactHints, error) {
	return nil, nil
}

func (a *captureCompactionAdvisor) AfterCompact(_ context.Context, _ *RunContext, output CompactOutput) (PostCompactActions, error) {
	a.outputs = append(a.outputs, output)
	return nil, nil
}

func TestCompactThreadMessages_trimCount(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "a"}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "b"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "c"}}},
	}
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	cfg := ContextCompactSettings{Enabled: true, MaxMessages: 2}
	out, outIDs, dropped := CompactThreadMessages(msgs, ids, cfg, nil)
	if dropped != 0 || len(out) != len(msgs) {
		t.Fatalf("expected delete-based trim retired, got dropped=%d len=%d", dropped, len(out))
	}
	if out[0].Role != "user" || outIDs[0] != ids[0] {
		t.Fatalf("expected no-op compact result, got role=%q ids=%#v", out[0].Role, outIDs)
	}
}

func TestCompactThreadMessages_userTokenBudget(t *testing.T) {
	long := strings.Repeat("a", 600) // 150 近似 tokens，与尾部 user 合计超预算
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: long}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "ok"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "tail"}}},
	}
	cfg := ContextCompactSettings{Enabled: true, MaxUserMessageTokens: 120}
	out, _, dropped := CompactThreadMessages(msgs, nil, cfg, nil)
	if dropped != 0 || len(out) != len(msgs) {
		t.Fatalf("expected delete-based trim retired, dropped=%d len=%d", dropped, len(out))
	}
	if out[len(out)-1].Role != "user" {
		t.Fatal("expected tail preserved")
	}
}

func TestContextCompactHasActiveBudget(t *testing.T) {
	if ContextCompactHasActiveBudget(ContextCompactSettings{Enabled: true}) {
		t.Fatal("expected false when all budgets zero")
	}
	if !ContextCompactHasActiveBudget(ContextCompactSettings{Enabled: true, MaxMessages: 1}) {
		t.Fatal("expected true")
	}
}

func TestTrimPrefixMessagesForCompactLLM_keepsNewestUnderCap(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: strings.Repeat("x", 8000)}}},
		{Role: "user", Content: []llm.TextPart{{Text: "tail-marker"}}},
	}
	out := TrimPrefixMessagesForCompactLLM(enc, msgs, 80)
	if len(out) != 1 {
		t.Fatalf("expected single message kept, got %d", len(out))
	}
	if messageText(out[0]) != "tail-marker" {
		t.Fatalf("expected newest segment kept")
	}
}

func TestSelectCompactFrontierWindowCapsTargetTokens(t *testing.T) {
	nodes := make([]FrontierNode, 0, 73)
	for i := 0; i < 72; i++ {
		nodes = append(nodes, FrontierNode{
			ApproxTokens: 1000,
			AtomSeq:      i + 1,
			AtomType:     compactAtomUserText,
			SourceText:   fmt.Sprintf("chunk-%d", i),
		})
	}
	nodes = append(nodes, FrontierNode{
		ApproxTokens: 1000,
		AtomSeq:      73,
		AtomType:     compactAtomUserText,
		SourceText:   "protected-tail",
	})

	selection := selectCompactFrontierWindow(nodes, 200_000, contextCompactMaxLLMInputTokens)
	if selection.TargetTokens != contextCompactMaxLLMInputTokens/2 {
		t.Fatalf("expected target cap %d, got %d", contextCompactMaxLLMInputTokens/2, selection.TargetTokens)
	}
	if selection.SelectedTokens < selection.TargetTokens {
		t.Fatalf("expected selected tokens >= target, got selected=%d target=%d", selection.SelectedTokens, selection.TargetTokens)
	}
	if selection.SelectedTokens > contextCompactMaxLLMInputTokens {
		t.Fatalf("expected selected tokens <= max input %d, got %d", contextCompactMaxLLMInputTokens, selection.SelectedTokens)
	}
}

func TestSelectCompactFrontierWindowEnforcesMinimumTargetTokens(t *testing.T) {
	nodes := []FrontierNode{
		{ApproxTokens: 200, AtomSeq: 1, AtomType: compactAtomUserText, SourceText: "a"},
		{ApproxTokens: 200, AtomSeq: 2, AtomType: compactAtomUserText, SourceText: "b"},
		{ApproxTokens: 200, AtomSeq: 3, AtomType: compactAtomUserText, SourceText: "tail"},
	}

	selection := selectCompactFrontierWindow(nodes, 1, contextCompactMaxLLMInputTokens)
	if selection.TargetTokens != 1024 {
		t.Fatalf("expected minimum target 1024, got %d", selection.TargetTokens)
	}
}

func TestBuildCompactFrontierAtomsFromMessages_BasicGrouping(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "user-1"}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "assistant-1"}}},
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ToolCallID:    "call_1",
				ToolName:      "read_file",
				ArgumentsJSON: map[string]any{"path": "/tmp/demo.txt"},
			}},
		},
		{Role: "tool", Content: []llm.TextPart{{Text: `{"tool_call_id":"call_1","tool_name":"read_file","result":{"ok":true}}`}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "assistant-2"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "user-2"}}},
	}

	nodes := buildCompactFrontierAtomsFromMessages(enc, msgs)
	if len(nodes) != 5 {
		t.Fatalf("expected 5 atom nodes, got %d", len(nodes))
	}
	if nodes[0].AtomType != compactAtomUserText || nodes[0].Role != "user" || nodes[0].MsgStart != 0 || nodes[0].MsgEnd != 0 {
		t.Fatalf("unexpected first atom: %#v", nodes[0])
	}
	if nodes[1].AtomType != compactAtomAssistantText || nodes[1].Role != "assistant" || nodes[1].MsgStart != 1 || nodes[1].MsgEnd != 1 {
		t.Fatalf("unexpected second atom: %#v", nodes[1])
	}
	if nodes[2].AtomType != compactAtomToolEpisode || nodes[2].Role != "assistant" || nodes[2].MsgStart != 2 || nodes[2].MsgEnd != 3 {
		t.Fatalf("unexpected tool episode atom: %#v", nodes[2])
	}
	if !strings.Contains(nodes[2].SourceText, "Tool result: read_file") {
		t.Fatalf("expected serialized tool episode, got %q", nodes[2].SourceText)
	}
	for i, node := range nodes {
		wantSeq := int64(i + 1)
		if node.StartContextSeq != wantSeq || node.EndContextSeq != wantSeq || node.AtomSeq != i+1 {
			t.Fatalf("unexpected sequence at %d: %#v", i, node)
		}
	}
}

func TestBuildCompactFrontierAtomsFromMessages_NoChunkSplitting(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("x", 10000)
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: huge}}},
	}

	nodes := buildCompactFrontierAtomsFromMessages(enc, msgs)
	if len(nodes) != 1 {
		t.Fatalf("expected exactly one atom node, got %d", len(nodes))
	}
	if nodes[0].SourceText != huge {
		t.Fatalf("expected full raw text preserved, got %d chars", len(nodes[0].SourceText))
	}
	if nodes[0].ApproxTokens <= compactChunkTokenLimit {
		t.Fatalf("expected unsplit atom to exceed chunk token limit, got %d", nodes[0].ApproxTokens)
	}
}

func TestSelectCompactAtomWindow_MaxAtomsCap(t *testing.T) {
	// token 预算是主要约束，50 个极小 atom 全部在预算内
	nodes := make([]FrontierNode, 0, 50)
	for i := 0; i < 50; i++ {
		nodes = append(nodes, FrontierNode{
			Kind:         FrontierNodeChunk,
			ApproxTokens: 10,
			AtomSeq:      i + 1,
			AtomType:     compactAtomUserText,
			Role:         "user",
			SourceText:   fmt.Sprintf("atom-%d", i+1),
		})
	}

	selection := selectCompactAtomWindow(nodes, 999999, contextCompactMaxLLMInputTokens)
	// protected = atom 50, eligible = 49, 49*10=490 远低于 token 预算，全选
	expectedCount := 49
	if len(selection.Nodes) != expectedCount {
		t.Fatalf("expected %d selected (all eligible), got %d", expectedCount, len(selection.Nodes))
	}
	if selection.EndNodeIndex != expectedCount-1 {
		t.Fatalf("unexpected end node index %d", selection.EndNodeIndex)
	}
	if selection.PartialTail {
		t.Fatal("expected atom selection to never set partial tail")
	}
	if selection.Nodes[len(selection.Nodes)-1].AtomSeq != expectedCount {
		t.Fatalf("expected last selected atom %d, got %d", expectedCount, selection.Nodes[len(selection.Nodes)-1].AtomSeq)
	}
}

func TestSelectCompactAtomWindow_TokenCap(t *testing.T) {
	nodes := []FrontierNode{
		{Kind: FrontierNodeChunk, ApproxTokens: 2000, AtomSeq: 1, AtomType: compactAtomUserText, Role: "user", SourceText: "a"},
		{Kind: FrontierNodeChunk, ApproxTokens: 2000, AtomSeq: 2, AtomType: compactAtomUserText, Role: "user", SourceText: "b"},
		{Kind: FrontierNodeChunk, ApproxTokens: 2000, AtomSeq: 3, AtomType: compactAtomUserText, Role: "user", SourceText: "c"},
		{Kind: FrontierNodeChunk, ApproxTokens: 2000, AtomSeq: 4, AtomType: compactAtomUserText, Role: "user", SourceText: "d"},
		{Kind: FrontierNodeChunk, ApproxTokens: 2000, AtomSeq: 5, AtomType: compactAtomUserText, Role: "user", SourceText: "tail"},
	}

	selection := selectCompactAtomWindow(nodes, 1, contextCompactMaxLLMInputTokens)
	if len(selection.Nodes) != 1 {
		t.Fatalf("expected minimal deficit-driven selection, got %d", len(selection.Nodes))
	}
	if selection.SelectedTokens != 2000 {
		t.Fatalf("expected 2000 selected tokens, got %d", selection.SelectedTokens)
	}
	if selection.TargetTokens != 1024 {
		t.Fatalf("expected minimum target 1024, got %d", selection.TargetTokens)
	}
}

func TestSelectCompactAtomWindow_ProtectsLastAtom(t *testing.T) {
	nodes := []FrontierNode{
		{Kind: FrontierNodeChunk, ApproxTokens: 10, AtomSeq: 1, AtomType: compactAtomUserText, Role: "user", SourceText: "head"},
		{Kind: FrontierNodeChunk, ApproxTokens: 10, AtomSeq: 2, AtomType: compactAtomAssistantText, Role: "assistant", SourceText: "middle"},
		{Kind: FrontierNodeChunk, ApproxTokens: 10, AtomSeq: 3, AtomType: compactAtomUserText, Role: "user", SourceText: "tail"},
	}

	selection := selectCompactAtomWindow(nodes, 1, contextCompactMaxLLMInputTokens)
	if len(selection.Nodes) != 2 {
		t.Fatalf("expected last atom protected, got %d selected atoms", len(selection.Nodes))
	}
	for _, node := range selection.Nodes {
		if node.AtomSeq == 3 {
			t.Fatalf("protected tail atom should not be selected: %#v", selection.Nodes)
		}
	}
}

func TestSelectCompactAtomWindow_SingleReplacementExtended(t *testing.T) {
	nodes := []FrontierNode{
		{Kind: FrontierNodeReplacement, ApproxTokens: 5000, AtomSeq: 1, SourceText: "old summary"},
		{Kind: FrontierNodeChunk, ApproxTokens: 100, AtomSeq: 2, AtomType: compactAtomUserText, Role: "user", SourceText: "chunk1"},
		{Kind: FrontierNodeChunk, ApproxTokens: 100, AtomSeq: 3, AtomType: compactAtomUserText, Role: "user", SourceText: "tail"},
	}
	// deficit small enough that selector would normally pick only R1
	selection := selectCompactAtomWindow(nodes, 1, contextCompactMaxLLMInputTokens)
	if len(selection.Nodes) < 2 {
		t.Fatalf("expected guard to extend single replacement, got %d nodes", len(selection.Nodes))
	}
	if selection.Nodes[0].Kind != FrontierNodeReplacement {
		t.Fatal("expected replacement as first node")
	}
	if selection.Nodes[1].Kind != FrontierNodeChunk {
		t.Fatal("expected chunk as second node (rightward progress)")
	}
}

func TestSelectCompactAtomWindow_SingleReplacementAloneReturnsEmpty(t *testing.T) {
	nodes := []FrontierNode{
		{Kind: FrontierNodeReplacement, ApproxTokens: 5000, AtomSeq: 1, SourceText: "old summary"},
		{Kind: FrontierNodeChunk, ApproxTokens: 100, AtomSeq: 2, AtomType: compactAtomUserText, Role: "user", SourceText: "tail"},
	}
	// Only 2 nodes: replacement + protected tail. Eligible = [R] only.
	selection := selectCompactAtomWindow(nodes, 1, contextCompactMaxLLMInputTokens)
	if len(selection.Nodes) != 0 {
		t.Fatalf("expected empty selection for lone replacement, got %d nodes", len(selection.Nodes))
	}
}

func TestSelectCompactAtomWindow_MultipleReplacementsAllowed(t *testing.T) {
	nodes := []FrontierNode{
		{Kind: FrontierNodeReplacement, ApproxTokens: 3000, AtomSeq: 1, SourceText: "summary1"},
		{Kind: FrontierNodeReplacement, ApproxTokens: 3000, AtomSeq: 2, SourceText: "summary2"},
		{Kind: FrontierNodeChunk, ApproxTokens: 100, AtomSeq: 3, AtomType: compactAtomUserText, Role: "user", SourceText: "chunk"},
		{Kind: FrontierNodeChunk, ApproxTokens: 100, AtomSeq: 4, AtomType: compactAtomUserText, Role: "user", SourceText: "tail"},
	}
	selection := selectCompactAtomWindow(nodes, 999999, contextCompactMaxLLMInputTokens)
	if len(selection.Nodes) < 2 {
		t.Fatalf("expected at least 2 nodes for multi-replacement selection, got %d", len(selection.Nodes))
	}
	// Guard should NOT trigger since we have 2+ nodes
}

func TestSelectCompactAtomWindow_SingleReplacementNextNodeTooBig(t *testing.T) {
	nodes := []FrontierNode{
		{Kind: FrontierNodeReplacement, ApproxTokens: 100000, AtomSeq: 1, SourceText: "huge summary"},
		{Kind: FrontierNodeChunk, ApproxTokens: 30000, AtomSeq: 2, AtomType: compactAtomUserText, Role: "user", SourceText: "big chunk"},
		{Kind: FrontierNodeChunk, ApproxTokens: 100, AtomSeq: 3, AtomType: compactAtomUserText, Role: "user", SourceText: "tail"},
	}
	// R(100k) + next(30k) = 130k > maxInputTokens(120k)
	selection := selectCompactAtomWindow(nodes, 1, contextCompactMaxLLMInputTokens)
	if len(selection.Nodes) != 0 {
		t.Fatalf("expected empty when single replacement + next exceeds maxInputTokens, got %d", len(selection.Nodes))
	}
}

func TestBuildCompactSummaryInputFromAtoms_StructuredFormat(t *testing.T) {
	nodes := []FrontierNode{
		{Kind: FrontierNodeChunk, AtomType: compactAtomUserText, Role: "user", SourceText: "user text"},
		{Kind: FrontierNodeChunk, AtomType: compactAtomAssistantText, Role: "assistant", SourceText: "assistant text"},
		{Kind: FrontierNodeChunk, AtomType: compactAtomToolEpisode, Role: "assistant", SourceText: "tool trace"},
		{Kind: FrontierNodeReplacement, SourceText: "older summary"},
	}

	input := buildCompactSummaryInputFromAtoms(nodes)
	for _, want := range []string{"[user]\nuser text", "[assistant]\nassistant text", "[tool_episode]\ntool trace", "[summary]\nolder summary"} {
		if !strings.Contains(input, want) {
			t.Fatalf("expected structured segment %q in %q", want, input)
		}
	}
}

func TestBuildCompactSummaryInputFromAtoms_ProjectsTelegramEnvelopeBurst(t *testing.T) {
	nodes := []FrontierNode{
		{
			Kind:     FrontierNodeChunk,
			AtomType: compactAtomUserText,
			Role:     "user",
			SourceText: "---\n" +
				"display-name: \"清凤\"\n" +
				"channel: \"telegram\"\n" +
				"conversation-type: \"supergroup\"\n" +
				"sender-ref: \"cf842dbb-a8e5-4d0c-876d-533c8d0d1b11\"\n" +
				"platform-username: \"chiffoncha\"\n" +
				"conversation-title: \"Arkloop\"\n" +
				"message-id: \"7158\"\n" +
				"time: \"2026-04-06T15:28:38Z\"\n" +
				"---\n" +
				"[Telegram in Arkloop] 也就是，质量和热度缺一不可？",
		},
		{
			Kind:     FrontierNodeChunk,
			AtomType: compactAtomUserText,
			Role:     "user",
			SourceText: "---\n" +
				"display-name: \"清凤\"\n" +
				"channel: \"telegram\"\n" +
				"conversation-type: \"supergroup\"\n" +
				"sender-ref: \"cf842dbb-a8e5-4d0c-876d-533c8d0d1b11\"\n" +
				"platform-username: \"chiffoncha\"\n" +
				"conversation-title: \"Arkloop\"\n" +
				"message-id: \"7159\"\n" +
				"time: \"2026-04-06T15:28:41Z\"\n" +
				"---\n" +
				"[Telegram in Arkloop] 草洛",
		},
	}

	input := buildCompactSummaryInputFromAtoms(nodes)
	for _, bad := range []string{"display-name:", "sender-ref:", "platform-username:", "conversation-title:"} {
		if strings.Contains(input, bad) {
			t.Fatalf("expected projected compact input without raw metadata %q, got %q", bad, input)
		}
	}
	for _, want := range []string{"[user]\nTelegram supergroup \"Arkloop\"", "清凤", "质量和热度缺一不可", "草洛"} {
		if !strings.Contains(input, want) {
			t.Fatalf("expected projected compact input to contain %q, got %q", want, input)
		}
	}
}

func TestMaterializeCompactedPrefixAtoms_NoPartialTail(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "head"}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "middle"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "tail"}}},
	}
	nodes := []FrontierNode{
		{Kind: FrontierNodeChunk, MsgStart: 0, MsgEnd: 0, AtomSeq: 1, AtomType: compactAtomUserText, Role: "user"},
		{Kind: FrontierNodeChunk, MsgStart: 1, MsgEnd: 1, AtomSeq: 2, AtomType: compactAtomAssistantText, Role: "assistant"},
		{Kind: FrontierNodeChunk, MsgStart: 2, MsgEnd: 2, AtomSeq: 3, AtomType: compactAtomUserText, Role: "user"},
	}

	out := materializeCompactedPrefixAtoms(msgs, nodes, 1, "summary")
	if len(out) != 2 {
		t.Fatalf("expected replacement plus untouched tail, got %d messages", len(out))
	}
	if out[0].Role != "system" {
		t.Fatalf("expected compact replacement rendered as system, got %q", out[0].Role)
	}
	if out[0].Phase == nil || *out[0].Phase != compactSyntheticPhase {
		t.Fatalf("expected compact synthetic replacement, got %#v", out[0].Phase)
	}
	if got := messageText(out[0]); got != "summary" {
		t.Fatalf("unexpected replacement text %q", got)
	}
	if got := messageText(out[1]); got != "tail" {
		t.Fatalf("expected clean tail boundary, got %q", got)
	}
}

func TestInlineCompactAtomFrontierCarriesPriorReplacementAcrossRounds(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	working := make([]llm.Message, 0, 100)
	for i := 1; i <= 100; i++ {
		working = append(working, llm.Message{
			Role:    "user",
			Content: []llm.TextPart{{Text: fmt.Sprintf("atom-%d", i)}},
		})
	}

	// token 预算远大于全部 atom 总 token，每轮选全部 eligible
	// 第 1 轮: 99 eligible → compact → [R1, atom-100]
	// 第 2 轮: R1 是 replacement 且是唯一 eligible 以外的 protected atom-100
	//          单 replacement 扩展不可能（没有更多 eligible），返回 empty
	nodes := buildCompactFrontierAtomsFromMessagesWithOptions(enc, working, false)
	selection := selectCompactAtomWindow(nodes, 999999, contextCompactMaxLLMInputTokens)
	if len(selection.Nodes) == 0 {
		t.Fatal("round 1: expected non-empty selection")
	}
	// 验证 replacement head 在第一轮不存在
	working = materializeCompactedPrefixAtoms(working, nodes, len(selection.Nodes)-1, "R1")

	if len(working) != 2 {
		t.Fatalf("expected 2 messages after round 1 (summary + protected tail), got %d", len(working))
	}
	if got := messageText(working[0]); got != "R1" {
		t.Fatalf("expected head summary R1, got %q", got)
	}
	if got := messageText(working[len(working)-1]); got != "atom-100" {
		t.Fatalf("expected protected tail atom-100, got %q", got)
	}
}

func TestBuildCompactFrontierAtomsFromPersistFrontier_PreservesCanonicalSeqRanges(t *testing.T) {
	frontier := []FrontierNode{
		{
			Kind:            FrontierNodeReplacement,
			NodeID:          uuid.New(),
			StartContextSeq: 1,
			EndContextSeq:   20,
			StartThreadSeq:  1,
			EndThreadSeq:    20,
			SourceText:      "R1",
			ApproxTokens:    32,
		},
	}
	for seq := int64(21); seq <= 40; seq++ {
		frontier = append(frontier, FrontierNode{
			Kind:            FrontierNodeChunk,
			NodeID:          uuid.New(),
			StartContextSeq: seq,
			EndContextSeq:   seq,
			StartThreadSeq:  seq,
			EndThreadSeq:    seq,
			SourceText:      fmt.Sprintf("atom-%d", seq),
			ApproxTokens:    8,
			AtomSeq:         int(seq),
			AtomType:        compactAtomUserText,
			Role:            "user",
			atomKey:         fmt.Sprintf("atom:%d", seq),
		})
	}

	atoms := buildCompactFrontierAtomsFromPersistFrontier(frontier)
	selection := selectCompactAtomWindow(atoms, 999999, contextCompactMaxLLMInputTokens)
	// 所有 atom token 极小，全部 eligible 都会被选中（除 protected tail）
	expectedCount := len(atoms) - 1 // protected tail excluded
	if len(selection.Nodes) != expectedCount {
		t.Fatalf("expected %d selected atoms, got %d", expectedCount, len(selection.Nodes))
	}
	if selection.Nodes[0].StartContextSeq != 1 || selection.Nodes[0].EndContextSeq != 20 {
		t.Fatalf("expected replacement seq 1..20, got %#v", selection.Nodes[0])
	}
	if selection.Nodes[1].StartContextSeq != 21 || selection.Nodes[1].EndContextSeq != 21 {
		t.Fatalf("expected next atom seq 21..21, got %#v", selection.Nodes[1])
	}
	if selection.Nodes[len(selection.Nodes)-1].StartContextSeq != 39 || selection.Nodes[len(selection.Nodes)-1].EndContextSeq != 39 {
		t.Fatalf("expected last selected atom seq 39..39, got %#v", selection.Nodes[len(selection.Nodes)-1])
	}
}

func TestMapSelectedAtomsToPersistFrontierNodes_UsesCanonicalSeqRanges(t *testing.T) {
	selectedAtoms := []FrontierNode{
		{
			Kind:            FrontierNodeReplacement,
			StartContextSeq: 1,
			EndContextSeq:   20,
			StartThreadSeq:  1,
			EndThreadSeq:    20,
			AtomSeq:         1,
		},
		{
			Kind:            FrontierNodeChunk,
			StartContextSeq: 21,
			EndContextSeq:   21,
			StartThreadSeq:  21,
			EndThreadSeq:    21,
			AtomSeq:         21,
		},
	}
	frontier := []FrontierNode{
		{
			Kind:            FrontierNodeReplacement,
			NodeID:          uuid.New(),
			StartContextSeq: 1,
			EndContextSeq:   20,
			StartThreadSeq:  1,
			EndThreadSeq:    20,
		},
		{
			Kind:            FrontierNodeChunk,
			NodeID:          uuid.New(),
			StartContextSeq: 21,
			EndContextSeq:   21,
			StartThreadSeq:  21,
			EndThreadSeq:    21,
		},
		{
			Kind:            FrontierNodeChunk,
			NodeID:          uuid.New(),
			StartContextSeq: 22,
			EndContextSeq:   22,
			StartThreadSeq:  22,
			EndThreadSeq:    22,
		},
	}

	got := mapSelectedAtomsToPersistFrontierNodes(selectedAtoms, frontier)
	if len(got) != 2 {
		t.Fatalf("expected replacement plus first raw chunk, got %#v", got)
	}
	if got[0].StartContextSeq != 1 || got[0].EndContextSeq != 20 {
		t.Fatalf("unexpected first mapped node %#v", got[0])
	}
	if got[1].StartContextSeq != 21 || got[1].EndContextSeq != 21 {
		t.Fatalf("unexpected second mapped node %#v", got[1])
	}
}

func TestMapSelectedAtomsToPersistFrontierNodes_ExpandsMergedChunkAtom(t *testing.T) {
	selectedAtoms := []FrontierNode{
		{
			Kind:            FrontierNodeChunk,
			StartContextSeq: 21,
			EndContextSeq:   23,
			StartThreadSeq:  21,
			EndThreadSeq:    23,
			AtomSeq:         21,
			atomKey:         "atom:21-23",
		},
	}
	chunk1 := uuid.New()
	chunk2 := uuid.New()
	chunk3 := uuid.New()
	frontier := []FrontierNode{
		{
			Kind:            FrontierNodeChunk,
			NodeID:          chunk1,
			StartContextSeq: 21,
			EndContextSeq:   21,
			StartThreadSeq:  21,
			EndThreadSeq:    21,
			atomKey:         "atom:21-23",
		},
		{
			Kind:            FrontierNodeChunk,
			NodeID:          chunk2,
			StartContextSeq: 22,
			EndContextSeq:   22,
			StartThreadSeq:  22,
			EndThreadSeq:    22,
			atomKey:         "atom:21-23",
		},
		{
			Kind:            FrontierNodeChunk,
			NodeID:          chunk3,
			StartContextSeq: 23,
			EndContextSeq:   23,
			StartThreadSeq:  23,
			EndThreadSeq:    23,
			atomKey:         "atom:21-23",
		},
	}

	got := mapSelectedAtomsToPersistFrontierNodes(selectedAtoms, frontier)
	if len(got) != 3 {
		t.Fatalf("expected merged atom to expand to all chunks, got %#v", got)
	}
	if got[0].NodeID != chunk1 || got[1].NodeID != chunk2 || got[2].NodeID != chunk3 {
		t.Fatalf("unexpected mapped chunks %#v", got)
	}
}

func TestCompactNodesWithShrinkRetryRetriesOnNonWindowError(t *testing.T) {
	gateway := &flakyCompactGateway{summary: "summary"}
	nodes := []FrontierNode{
		{AtomSeq: 1, AtomType: compactAtomUserText, SourceText: "one"},
		{AtomSeq: 2, AtomType: compactAtomUserText, SourceText: "two"},
		{AtomSeq: 3, AtomType: compactAtomUserText, SourceText: "three"},
	}

	summary, usedNodes, err := compactNodesWithShrinkRetry(context.Background(), &RunContext{}, gateway, "stub", nodes, compactProgressRecorder{})
	if err != nil {
		t.Fatalf("compactNodesWithShrinkRetry: %v", err)
	}
	if strings.TrimSpace(summary) != "summary" {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if gateway.calls != 2 {
		t.Fatalf("expected shrink retry to call gateway twice, got %d", gateway.calls)
	}
	if len(usedNodes) != 2 {
		t.Fatalf("expected one node to be dropped on retry, got %d nodes", len(usedNodes))
	}
}

func TestHistoryThreadPromptTokens_CountsImageCost(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	textOnly := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "hello"}}},
	}
	withImage := []llm.Message{
		{
			Role: "user",
			Content: []llm.ContentPart{
				{Type: messagecontent.PartTypeText, Text: "hello"},
				{Type: messagecontent.PartTypeImage},
			},
		},
	}
	base := HistoryThreadPromptTokens(enc, textOnly)
	got := HistoryThreadPromptTokens(enc, withImage)
	if got-base != contextCompactVisionTokensPerImage {
		t.Fatalf("expected image cost %d, got delta %d", contextCompactVisionTokensPerImage, got-base)
	}
}

func TestTrimEntriesToMessageLimitPreservesAllLeadingReplacements(t *testing.T) {
	entries := []canonicalThreadContextEntry{
		{IsReplacement: true, StartThreadSeq: 1, EndThreadSeq: 10, ThreadMessageID: uuid.Nil},
		{IsReplacement: true, StartThreadSeq: 11, EndThreadSeq: 20, ThreadMessageID: uuid.Nil},
		{IsReplacement: false, StartThreadSeq: 21, EndThreadSeq: 21, ThreadMessageID: uuid.New()},
		{IsReplacement: false, StartThreadSeq: 22, EndThreadSeq: 22, ThreadMessageID: uuid.New()},
		{IsReplacement: false, StartThreadSeq: 23, EndThreadSeq: 23, ThreadMessageID: uuid.New()},
	}

	out := trimEntriesToMessageLimit(entries, 2)
	if len(out) != 4 {
		t.Fatalf("expected 4 entries after trim, got %d", len(out))
	}
	if !out[0].IsReplacement || out[0].StartThreadSeq != 1 || !out[1].IsReplacement || out[1].StartThreadSeq != 11 {
		t.Fatalf("expected both leading replacements preserved, got %#v", out[:2])
	}
	if out[2].StartThreadSeq != 22 || out[3].StartThreadSeq != 23 {
		t.Fatalf("expected tail real messages preserved, got %#v", out[2:])
	}
}

func TestTrimEntriesToMessageLimitDoesNotSplitAtom(t *testing.T) {
	atomA := "atom:a"
	atomB := "atom:b"
	entries := []canonicalThreadContextEntry{
		{IsReplacement: true, StartThreadSeq: 1, EndThreadSeq: 10, ThreadMessageID: uuid.Nil},
		{IsReplacement: false, AtomKey: atomA, StartThreadSeq: 21, EndThreadSeq: 21, ThreadMessageID: uuid.New()},
		{IsReplacement: false, AtomKey: atomA, StartThreadSeq: 22, EndThreadSeq: 22, ThreadMessageID: uuid.New()},
		{IsReplacement: false, AtomKey: atomB, StartThreadSeq: 23, EndThreadSeq: 23, ThreadMessageID: uuid.New()},
	}

	out := trimEntriesToMessageLimit(entries, 2)
	if len(out) != 4 {
		t.Fatalf("expected atom-preserving trim to keep 4 entries, got %d", len(out))
	}
	if out[1].AtomKey != atomA || out[2].AtomKey != atomA || out[3].AtomKey != atomB {
		t.Fatalf("unexpected atom layout after trim: %#v", out)
	}
}

func TestSelectRenderableReplacementSpansKeepsLastChunkOfLastAtomRaw(t *testing.T) {
	lastAtom := &canonicalAtom{
		Kind:            canonicalAtomUserText,
		StartContextSeq: 9,
		EndContextSeq:   12,
		Messages: []data.ThreadMessage{{
			ID:        uuid.New(),
			Role:      "user",
			Content:   "alpha\n\nbeta\n\ngamma",
			ThreadSeq: 1,
		}},
	}
	spans := []canonicalReplacementSpan{
		{
			Record: data.ThreadContextReplacementRecord{
				ID:          uuid.New(),
				SummaryText: "older summary",
				Layer:       3,
			},
			StartContextSeq: 1,
			EndContextSeq:   8,
		},
		{
			Record: data.ThreadContextReplacementRecord{
				ID:          uuid.New(),
				SummaryText: "prefix of last atom",
				Layer:       4,
			},
			StartContextSeq: 9,
			EndContextSeq:   10,
		},
		{
			Record: data.ThreadContextReplacementRecord{
				ID:          uuid.New(),
				SummaryText: "touches atom tail",
				Layer:       5,
			},
			StartContextSeq: 11,
			EndContextSeq:   12,
		},
	}

	out := selectRenderableReplacementSpans(spans, 1, lastAtom)
	if len(out) != 2 {
		t.Fatalf("expected two replacements after keeping last chunk raw, got %d", len(out))
	}
	if out[0].StartContextSeq != 1 || out[0].EndContextSeq != 8 {
		t.Fatalf("unexpected first surviving replacement: %#v", out[0])
	}
	if out[1].StartContextSeq != 9 || out[1].EndContextSeq != 10 {
		t.Fatalf("unexpected second surviving replacement: %#v", out[1])
	}
}

func TestCompactNodesWithShrinkRetryRetriesOnStreamInterrupt(t *testing.T) {
	nodes := []FrontierNode{
		{SourceText: "first", AtomSeq: 1, AtomType: compactAtomUserText},
		{SourceText: "second", AtomSeq: 2, AtomType: compactAtomUserText},
	}

	summary, usedNodes, err := compactNodesWithShrinkRetry(context.Background(), &RunContext{}, &shrinkRetryGateway{summary: "shrunk"}, "stub", nodes, compactProgressRecorder{})
	if err != nil {
		t.Fatalf("compactNodesWithShrinkRetry: %v", err)
	}
	if summary != "shrunk" {
		t.Fatalf("unexpected summary %q", summary)
	}
	if len(usedNodes) != 1 || usedNodes[0].SourceText != "first" {
		t.Fatalf("expected shrink retry to keep only first node, got %#v", usedNodes)
	}
}

func TestCompactNodesWithShrinkRetryEmitsAttemptProgress(t *testing.T) {
	nodes := []FrontierNode{
		{SourceText: "first", AtomSeq: 1, AtomType: compactAtomUserText, ApproxTokens: 10},
		{SourceText: "second", AtomSeq: 2, AtomType: compactAtomUserText, ApproxTokens: 12},
	}
	rc := &RunContext{}
	var phases []string
	var payloads []map[string]any
	progress := compactProgressRecorder{
		base: map[string]any{
			"op":    "persist",
			"mode":  "canonical_atoms",
			"round": 1,
		},
		emitFn: func(_ context.Context, _ *RunContext, data map[string]any) error {
			payloads = append(payloads, cloneContextCompactEventData(data))
			phase, _ := data["phase"].(string)
			phases = append(phases, phase)
			return nil
		},
	}

	summary, usedNodes, err := compactNodesWithShrinkRetry(context.Background(), rc, &shrinkRetryGateway{summary: "shrunk"}, "stub", nodes, progress)
	if err != nil {
		t.Fatalf("compactNodesWithShrinkRetry: %v", err)
	}
	if summary != "shrunk" {
		t.Fatalf("unexpected summary %q", summary)
	}
	if len(usedNodes) != 1 {
		t.Fatalf("expected one surviving node after retry, got %d", len(usedNodes))
	}
	wantPhases := []string{"llm_request_started", "llm_request_retrying", "llm_request_started", "llm_request_completed"}
	if strings.Join(phases, ",") != strings.Join(wantPhases, ",") {
		t.Fatalf("unexpected phases: got %v want %v", phases, wantPhases)
	}
	if got := payloads[0]["attempt"]; got != 1 {
		t.Fatalf("expected first attempt=1, got %#v", got)
	}
	if got := payloads[1]["atoms_dropped"]; got != 1 {
		t.Fatalf("expected retry to drop one atom, got %#v", got)
	}
	if got := payloads[1]["atoms_remaining"]; got != 1 {
		t.Fatalf("expected retry to keep one atom, got %#v", got)
	}
	if got := payloads[3]["attempt"]; got != 2 {
		t.Fatalf("expected completion on attempt 2, got %#v", got)
	}
}

func TestCompactNodesWithShrinkRetryEmitsStandardLLMEvents(t *testing.T) {
	nodes := []FrontierNode{
		{SourceText: "first", AtomSeq: 1, AtomType: compactAtomUserText, ApproxTokens: 10},
	}
	rc := &RunContext{
		Run: data.Run{ID: uuid.New()},
	}
	appender := &captureCompactEventAppender{}
	progress := compactProgressRecorder{
		base: map[string]any{
			"op": "persist",
		},
		appendStandardFn: func(_ context.Context, _ *RunContext, ev events.RunEvent) error {
			_, err := appender.AppendRunEvent(context.Background(), nil, uuid.Nil, ev)
			return err
		},
	}

	_, _, err := compactNodesWithShrinkRetry(context.Background(), rc, &stubCompactGateway{summary: "shrunk"}, "stub", nodes, progress)
	if err != nil {
		t.Fatalf("compactNodesWithShrinkRetry: %v", err)
	}
	if len(appender.events) != 2 {
		t.Fatalf("expected 2 standard compact llm events, got %d", len(appender.events))
	}
	if appender.events[0].Type != "llm.request" {
		t.Fatalf("expected first event llm.request, got %q", appender.events[0].Type)
	}
	if got := appender.events[0].DataJSON["event_scope"]; got != "context_compact" {
		t.Fatalf("expected llm.request event_scope=context_compact, got %#v", got)
	}
	if appender.events[1].Type != "llm.turn.completed" {
		t.Fatalf("expected second event llm.turn.completed, got %q", appender.events[1].Type)
	}
	if got := appender.events[1].DataJSON["llm_call_id"]; got != "compact-call-1" {
		t.Fatalf("expected llm_call_id to be propagated, got %#v", got)
	}
}

func TestCompactNodesWithShrinkRetryDropsWholeChunkedToolEpisode(t *testing.T) {
	nodes := []FrontierNode{
		{SourceText: "keep", AtomSeq: 1, AtomType: compactAtomUserText},
		{SourceText: "first", AtomSeq: 2, AtomType: compactAtomToolEpisode},
		{SourceText: "second", AtomSeq: 2, AtomType: compactAtomToolEpisode},
	}

	summary, usedNodes, err := compactNodesWithShrinkRetry(context.Background(), &RunContext{}, &shrinkRetryGateway{summary: "shrunk"}, "stub", nodes, compactProgressRecorder{})
	if err != nil {
		t.Fatalf("compactNodesWithShrinkRetry: %v", err)
	}
	if summary != "shrunk" {
		t.Fatalf("unexpected summary %q", summary)
	}
	if len(usedNodes) != 1 || usedNodes[0].SourceText != "keep" {
		t.Fatalf("expected shrink retry to drop the whole tool episode, got %#v", usedNodes)
	}
}

func TestMaybeInlineCompactMessagesAfterCompactReceivesRealOutput(t *testing.T) {
	advisor := &captureCompactionAdvisor{}
	registry := NewHookRegistry()
	registry.RegisterCompactionAdvisor(advisor)
	gateway := &stubCompactGateway{summary: "summary"}

	rc := &RunContext{
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			PersistTriggerApproxTokens:  1,
			PersistTriggerContextPct:    0,
			TargetContextPct:            1,
			FallbackContextWindowTokens: 1_000_000,
			PersistKeepLastMessages:     1,
		},
		Gateway: gateway,
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{Model: "stub"},
		},
		HookRuntime: NewHookRuntime(registry, NewDefaultHookResultApplier()),
	}
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "first"}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "second"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "tail"}}},
	}

	out, _, changed, err := MaybeInlineCompactMessages(context.Background(), rc, msgs, nil, false)
	if err != nil {
		t.Fatalf("MaybeInlineCompactMessages: %v", err)
	}
	if changed {
		t.Fatal("expected normal inline compact retired")
	}
	if len(advisor.outputs) != 0 {
		t.Fatalf("expected no compact callback, got %d", len(advisor.outputs))
	}
	if len(out) != len(msgs) {
		t.Fatalf("expected messages unchanged, got %d", len(out))
	}
	if gateway.calls != 0 {
		t.Fatalf("expected no compact gateway call, got %d", gateway.calls)
	}
}

func TestRenderCanonicalThreadMessagesFromGraphRendersPartialTailForLastTextAtom(t *testing.T) {
	msg := data.ThreadMessage{
		ID:        uuid.New(),
		Role:      "user",
		Content:   "alpha\n\nbeta\n\ngamma",
		ThreadSeq: 1,
	}
	atoms, chunks := buildCanonicalAtomGraph([]data.ThreadMessage{msg})
	if len(atoms) != 1 || len(chunks) < 3 {
		t.Fatalf("expected one atom and chunked text, got atoms=%d chunks=%d", len(atoms), len(chunks))
	}

	entries, _, err := renderCanonicalThreadMessagesFromGraph(context.Background(), nil, atoms, chunks, []canonicalReplacementSpan{
		{
			Record: data.ThreadContextReplacementRecord{
				ID:          uuid.New(),
				SummaryText: "summary",
				Layer:       1,
			},
			StartContextSeq: chunks[0].ContextSeq,
			EndContextSeq:   chunks[1].ContextSeq,
		},
	})
	if err != nil {
		t.Fatalf("render graph: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected replacement plus tail entry, got %d", len(entries))
	}
	if !entries[0].IsReplacement {
		t.Fatalf("expected first entry replacement, got %#v", entries[0])
	}
	if entries[1].IsReplacement {
		t.Fatalf("expected second entry to be raw tail, got %#v", entries[1])
	}
	if got := messageText(entries[1].Message); got != "gamma" {
		t.Fatalf("expected tail text gamma, got %q", got)
	}
}

func TestSelectRenderableReplacementSpansKeepsWholeRichLastAtomRaw(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"parts": []map[string]any{
			{"type": "text", "text": "caption"},
			{"type": "file", "attachment": map[string]any{"key": "docs/a.txt"}, "extracted_text": "file body"},
		},
	})
	if err != nil {
		t.Fatalf("marshal content json: %v", err)
	}
	lastAtom := &canonicalAtom{
		Kind:            canonicalAtomUserText,
		StartContextSeq: 9,
		EndContextSeq:   12,
		Messages: []data.ThreadMessage{{
			ID:          uuid.New(),
			Role:        "user",
			Content:     "caption",
			ContentJSON: raw,
			ThreadSeq:   99,
		}},
	}
	spans := []canonicalReplacementSpan{
		{
			Record: data.ThreadContextReplacementRecord{
				ID:          uuid.New(),
				SummaryText: "older summary",
				Layer:       3,
			},
			StartContextSeq: 1,
			EndContextSeq:   8,
		},
		{
			Record: data.ThreadContextReplacementRecord{
				ID:          uuid.New(),
				SummaryText: "should be skipped",
				Layer:       4,
			},
			StartContextSeq: 9,
			EndContextSeq:   10,
		},
	}

	out := selectRenderableReplacementSpans(spans, 1, lastAtom)
	if len(out) != 1 {
		t.Fatalf("expected only non-overlapping older replacement, got %#v", out)
	}
	if out[0].StartContextSeq != 1 || out[0].EndContextSeq != 8 {
		t.Fatalf("unexpected surviving replacement: %#v", out[0])
	}
}

func TestMapReplacementsToContextSpansPrefersCurrentThreadSeqMapping(t *testing.T) {
	chunks := []canonicalChunk{
		{ContextSeq: 1, StartThreadSeq: 10, EndThreadSeq: 10},
		{ContextSeq: 2, StartThreadSeq: 11, EndThreadSeq: 11},
		{ContextSeq: 3, StartThreadSeq: 12, EndThreadSeq: 12},
	}
	replacements := []data.ThreadContextReplacementRecord{
		{
			ID:              uuid.New(),
			StartThreadSeq:  10,
			EndThreadSeq:    11,
			StartContextSeq: 100,
			EndContextSeq:   200,
			SummaryText:     "summary",
			Layer:           1,
		},
	}

	spans := mapReplacementsToContextSpans(replacements, chunks, nil)
	if len(spans) != 1 {
		t.Fatalf("expected one mapped replacement, got %#v", spans)
	}
	if spans[0].StartContextSeq != 1 || spans[0].EndContextSeq != 2 {
		t.Fatalf("expected thread-seq remap to current chunks, got %#v", spans[0])
	}
}

func TestContextCompactMiddlewareStripsImagesEvenWhenDisabled(t *testing.T) {
	messages := make([]llm.Message, 0, 12)
	for i := 0; i < 12; i++ {
		messages = append(messages, llm.Message{
			Role: "user",
			Content: []llm.ContentPart{{
				Type: messagecontent.PartTypeImage,
				Attachment: &messagecontent.AttachmentRef{
					Key: fmt.Sprintf("attachments/%d.png", i),
				},
			}},
		})
	}
	rc := &RunContext{
		Messages: messages,
		ContextCompact: ContextCompactSettings{
			Enabled:        false,
			PersistEnabled: false,
		},
	}

	mw := NewContextCompactMiddleware(nil, data.MessagesRepository{}, nil, nil, false)
	if err := mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	realImages := 0
	placeholders := 0
	for _, msg := range rc.Messages {
		for _, part := range msg.Content {
			switch {
			case part.Kind() == messagecontent.PartTypeImage:
				realImages++
			case strings.HasPrefix(part.Text, "[image"):
				placeholders++
			}
		}
	}
	if realImages != defaultGroupKeepImageTail {
		t.Fatalf("expected %d real images kept, got %d", defaultGroupKeepImageTail, realImages)
	}
	if placeholders != 2 {
		t.Fatalf("expected 2 placeholders, got %d", placeholders)
	}
}

// ---------------------------------------------------------------------------
// computeTailKeepByTokenBudget
// ---------------------------------------------------------------------------

func TestComputeTailKeepByTokenBudget_AllShortMessages(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	msgs := make([]llm.Message, 10)
	for i := range msgs {
		msgs[i] = llm.Message{Role: "user", Content: []llm.TextPart{{Text: "hi"}}}
	}
	got := computeTailKeepByTokenBudget(enc, msgs, 100000, 0)
	if got != 10 {
		t.Fatalf("expected 10, got %d", got)
	}
}

func TestComputeTailKeepByTokenBudget_AllShortMessages_MaxMessagesLimit(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	msgs := make([]llm.Message, 10)
	for i := range msgs {
		msgs[i] = llm.Message{Role: "user", Content: []llm.TextPart{{Text: "hi"}}}
	}
	got := computeTailKeepByTokenBudget(enc, msgs, 100000, 5)
	if got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
}

func TestComputeTailKeepByTokenBudget_MixedSizes(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("x", 40000)
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: huge}}},
		{Role: "user", Content: []llm.TextPart{{Text: huge}}},
		{Role: "user", Content: []llm.TextPart{{Text: huge}}},
		{Role: "user", Content: []llm.TextPart{{Text: "hi"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "hi"}}},
	}
	got := computeTailKeepByTokenBudget(enc, msgs, 5000, 0)
	if got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
}

func TestComputeTailKeepByTokenBudget_SingleHugeMessage(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: strings.Repeat("a", 200000)}}},
	}
	got := computeTailKeepByTokenBudget(enc, msgs, 100, 0)
	if got != 1 {
		t.Fatalf("expected 1 (at-least-one guarantee), got %d", got)
	}
}

func TestComputeTailKeepByTokenBudget_EmptyMessages(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	got := computeTailKeepByTokenBudget(enc, nil, 1000, 0)
	if got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestComputeTailKeepByTokenBudget_ZeroBudget(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "a"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "b"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "c"}}},
	}
	got := computeTailKeepByTokenBudget(enc, msgs, 0, 0)
	if got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestMaybeInlineCompactMessages_SingleOversizedTextAtomRetired(t *testing.T) {
	gateway := &stubCompactGateway{summary: "compacted head"}
	huge := strings.Repeat("alpha beta gamma delta\n\n", 240)
	rc := &RunContext{
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			PersistTriggerApproxTokens:  1,
			PersistTriggerContextPct:    0,
			TargetContextPct:            1,
			FallbackContextWindowTokens: 1_000_000,
			PersistKeepLastMessages:     1,
		},
		Gateway: gateway,
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{Model: "gpt-4o", ID: "route-1"},
			Credential: routing.ProviderCredential{
				ProviderKind: routing.ProviderKindOpenAI,
			},
		},
	}
	msgs := []llm.Message{{Role: "user", Content: []llm.TextPart{{Text: huge}}}}
	out, stats, changed, err := MaybeInlineCompactMessages(context.Background(), rc, msgs, nil, false)
	if err != nil {
		t.Fatalf("MaybeInlineCompactMessages: %v", err)
	}
	if changed {
		t.Fatal("expected inline compact path retired")
	}
	if len(out) != 1 {
		t.Fatalf("expected message unchanged, got %d", len(out))
	}
	if strings.TrimSpace(messageText(out[0])) != strings.TrimSpace(huge) {
		t.Fatalf("expected original message preserved")
	}
	if stats.SingleAtomPartial {
		t.Fatalf("expected no single atom partial flag, got %#v", stats)
	}
	if stats.TargetChunkCount != 0 {
		t.Fatalf("expected no chunk-driven compact stats, got %#v", stats)
	}
	if gateway.calls != 0 {
		t.Fatalf("expected no compact gateway call, got %d", gateway.calls)
	}
}

func TestBuildCanonicalCompactChunks_ToolEpisodePreservesSkeleton(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	hugePayload := strings.Repeat("z", compactToolPayloadMaxRunes+300)
	msgs := []llm.Message{
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ToolCallID:    "call_1",
				ToolName:      "read_file",
				ArgumentsJSON: map[string]any{"path": "/tmp/huge.txt"},
			}},
		},
		{Role: "tool", Content: []llm.TextPart{{Text: fmt.Sprintf(`{"tool_call_id":"call_1","tool_name":"read_file","result":{"data":"%s"}}`, hugePayload)}}},
	}
	chunks := buildCanonicalCompactChunks(enc, msgs)
	if len(chunks) == 0 {
		t.Fatal("expected chunks for tool episode")
	}
	if chunks[0].AtomType != compactAtomToolEpisode {
		t.Fatalf("expected tool episode atom, got %s", chunks[0].AtomType)
	}
	serialized := serializeCompactChunksForLLM(chunks)
	if !strings.Contains(serialized, "Tool result: read_file") {
		t.Fatalf("expected tool skeleton in serialized chunks, got %q", serialized)
	}
	if !strings.Contains(serialized, "truncated") {
		t.Fatalf("expected oversized payload to be compacted, got %q", serialized)
	}
}

func TestComputeTailKeepByTokenBudget_CountsImageCost(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{
		{
			Role: "user",
			Content: []llm.ContentPart{
				{Type: messagecontent.PartTypeText, Text: "with image"},
				{Type: messagecontent.PartTypeImage},
			},
		},
		{Role: "user", Content: []llm.TextPart{{Text: "tail"}}},
	}
	got := computeTailKeepByTokenBudget(enc, msgs, 512, 0)
	if got != 1 {
		t.Fatalf("expected only tail kept when image cost exceeds budget, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// truncateLargeTailMessages
// ---------------------------------------------------------------------------

func TestTruncateLargeTailMessages_NoTruncation(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "short"}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "reply"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "also short"}}},
	}
	out := truncateLargeTailMessages(enc, msgs)
	for i, m := range out {
		if messageText(m) != messageText(msgs[i]) {
			t.Fatalf("msg[%d] changed unexpectedly", i)
		}
	}
}

func TestTruncateLargeTailMessages_TruncatesOldLargeUser(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("x", 40000) // ~10K tokens
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: large}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "ok"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "hi"}}},
	}
	out := truncateLargeTailMessages(enc, msgs)
	if !strings.Contains(messageText(out[0]), "[... content truncated") {
		t.Fatal("expected first user message to be truncated")
	}
	if messageText(out[2]) != "hi" {
		t.Fatal("last user message should be untouched")
	}
}

func TestTruncateLargeTailMessages_SkipsLastUser(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("x", 40000)
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "small"}}},
		{Role: "user", Content: []llm.TextPart{{Text: large}}},
	}
	out := truncateLargeTailMessages(enc, msgs)
	if messageText(out[1]) != large {
		t.Fatal("last user message must not be truncated")
	}
}

func TestTruncateLargeTailMessages_SkipsAssistant(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("x", 40000)
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.TextPart{{Text: large}}},
	}
	out := truncateLargeTailMessages(enc, msgs)
	if messageText(out[0]) != large {
		t.Fatal("assistant messages must not be truncated")
	}
}

func TestTruncateLargeTailMessages_OriginalUnmodified(t *testing.T) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("x", 40000)
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: large}}},
		{Role: "user", Content: []llm.TextPart{{Text: "tail"}}},
	}
	origText := messageText(msgs[0])
	_ = truncateLargeTailMessages(enc, msgs)
	if messageText(msgs[0]) != origText {
		t.Fatal("original slice must not be modified")
	}
}

// ---------------------------------------------------------------------------
// microcompactToolResults
// ---------------------------------------------------------------------------

func TestMicrocompactToolResults_NoTools(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "hello"}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "world"}}},
	}
	out := microcompactToolResults(msgs, 3)
	if len(out) != len(msgs) {
		t.Fatalf("expected same length, got %d", len(out))
	}
	for i := range out {
		if messageText(out[i]) != messageText(msgs[i]) {
			t.Fatalf("msg[%d] changed unexpectedly", i)
		}
	}
}

func TestRewriteOversizeRequest_StripsOlderImagePartsBeyondTailBudget(t *testing.T) {
	rc := &RunContext{
		ContextCompact: ContextCompactSettings{
			PersistKeepLastMessages:     1,
			MicrocompactKeepRecentTools: 1,
		},
	}
	messages := make([]llm.Message, 0, 12)
	messages = append(messages, llm.Message{
		Role: "user",
		Content: []llm.ContentPart{{
			Type: messagecontent.PartTypeImage,
			Attachment: &messagecontent.AttachmentRef{
				Key: "attachments/old.png",
			},
		}},
	})
	for i := 0; i < 9; i++ {
		messages = append(messages, llm.Message{
			Role: "user",
			Content: []llm.ContentPart{{
				Type: messagecontent.PartTypeImage,
				Data: []byte("img"),
				Attachment: &messagecontent.AttachmentRef{
					Key:      fmt.Sprintf("attachments/mid-%d.png", i),
					MimeType: "image/png",
				},
			}},
		})
	}
	messages = append(messages,
		llm.Message{
			Role: "tool",
			Content: []llm.TextPart{{
				Text: `{"tool_call_id":"call_old","tool_name":"read","result":{"huge":true}}`,
			}},
		},
		llm.Message{
			Role: "user",
			Content: []llm.ContentPart{{
				Type: messagecontent.PartTypeImage,
				Data: []byte("latest"),
				Attachment: &messagecontent.AttachmentRef{
					Key:      "attachments/latest.png",
					MimeType: "image/png",
				},
			}},
		},
	)
	request := llm.Request{Messages: messages}

	rewritten, stats, err := RewriteOversizeRequest(context.Background(), rc, request, nil, testProviderRequestEstimate)
	if err != nil {
		t.Fatalf("RewriteOversizeRequest failed: %v", err)
	}
	if !stats.RewriteApplied {
		t.Fatal("expected rewrite to apply")
	}
	if stats.ImagesStripped != 1 {
		t.Fatalf("expected 1 stripped image, got %d", stats.ImagesStripped)
	}
	if stats.ToolResultsMicrocompacted != 0 {
		t.Fatalf("expected no tool microcompact when only one tool result remains recent, got %d", stats.ToolResultsMicrocompacted)
	}
	if got := rewritten.Messages[0].Content[0].Text; got != "[image attachment_key=\"attachments/old.png\"]" {
		t.Fatalf("unexpected old image placeholder: %q", got)
	}
	if rewritten.Messages[2].Content[0].Kind() != messagecontent.PartTypeImage {
		t.Fatalf("expected latest user image to remain image, got %#v", rewritten.Messages[2].Content[0])
	}
}

func TestRewriteOversizeRequest_MicrocompactsOldToolResults(t *testing.T) {
	rc := &RunContext{
		ContextCompact: ContextCompactSettings{
			PersistKeepLastMessages:     10,
			MicrocompactKeepRecentTools: 1,
		},
	}
	request := llm.Request{
		Messages: []llm.Message{
			{
				Role: "tool",
				Content: []llm.TextPart{{
					Text: `{"tool_call_id":"call_old","tool_name":"read","result":{"old":true}}`,
				}},
			},
			{
				Role: "tool",
				Content: []llm.TextPart{{
					Text: `{"tool_call_id":"call_new","tool_name":"read","result":{"new":true}}`,
				}},
			},
		},
	}

	rewritten, stats, err := RewriteOversizeRequest(context.Background(), rc, request, nil, testProviderRequestEstimate)
	if err != nil {
		t.Fatalf("RewriteOversizeRequest failed: %v", err)
	}
	if stats.ToolResultsMicrocompacted != 1 {
		t.Fatalf("expected 1 microcompacted tool result, got %d", stats.ToolResultsMicrocompacted)
	}
	if !strings.Contains(messageText(rewritten.Messages[0]), `"cleared":true`) {
		t.Fatalf("expected old tool result stub, got %q", messageText(rewritten.Messages[0]))
	}
	if !strings.Contains(messageText(rewritten.Messages[1]), `"new":true`) {
		t.Fatalf("expected latest tool result preserved, got %q", messageText(rewritten.Messages[1]))
	}
}

func TestRewriteOversizeRequest_CurrentInputNodeTooLarge(t *testing.T) {
	rc := &RunContext{
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			MicrocompactKeepRecentTools: 1,
		},
	}
	request := llm.Request{
		Model: "stub",
		Messages: []llm.Message{
			{Role: "system", Content: []llm.TextPart{{Text: "sys"}}},
			{Role: "user", Content: []llm.TextPart{{Text: strings.Repeat("x", llm.RequestPayloadLimitBytes+2048)}}},
		},
	}

	rewritten, stats, err := RewriteOversizeRequest(context.Background(), rc, request, nil, testProviderRequestEstimate)
	if err == nil {
		t.Fatal("expected current input oversize error")
	}
	inputErr, ok := IsCurrentInputOversizeError(err)
	if !ok || inputErr == nil {
		t.Fatalf("expected CurrentInputOversizeError, got %T", err)
	}
	if !stats.CurrentInputTooLarge {
		t.Fatal("expected CurrentInputTooLarge=true")
	}
	if stats.MinimalRequestBytes <= llm.RequestPayloadLimitBytes {
		t.Fatalf("expected minimal request to exceed limit, got %d", stats.MinimalRequestBytes)
	}
	if rewritten.Messages[1].Role != "user" {
		t.Fatalf("rewrite should not mutate messages when current input is oversize, got %#v", rewritten.Messages)
	}
}

func TestRewriteOversizeRequest_CurrentInputTokenWindowTooLarge(t *testing.T) {
	rc := &RunContext{
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			FallbackContextWindowTokens: 12,
		},
		ContextWindowTokens: 12,
		SelectedRoute: &routing.SelectedProviderRoute{
			Route:      routing.ProviderRouteRule{Model: "gpt-4o", ID: "route-1"},
			Credential: routing.ProviderCredential{ProviderKind: routing.ProviderKindOpenAI},
		},
	}
	request := llm.Request{
		Model: "stub",
		Messages: []llm.Message{
			{Role: "system", Content: []llm.TextPart{{Text: "sys"}}},
			{Role: "user", Content: []llm.TextPart{{Text: "one two three four five six seven eight nine ten eleven twelve thirteen"}}},
		},
	}

	rewritten, stats, err := RewriteOversizeRequest(context.Background(), rc, request, nil, testProviderRequestEstimate)
	if err == nil {
		t.Fatal("expected current input token window error")
	}
	inputErr, ok := IsCurrentInputOversizeError(err)
	if !ok || inputErr == nil {
		t.Fatalf("expected CurrentInputOversizeError, got %T", err)
	}
	if !stats.CurrentInputTooLarge {
		t.Fatal("expected CurrentInputTooLarge=true")
	}
	if stats.MinimalRequestBytes >= llm.RequestPayloadLimitBytes {
		t.Fatalf("expected minimal request bytes to stay below bytes limit, got %d", stats.MinimalRequestBytes)
	}
	if inputErr.MinimalRequestTokens <= rc.ContextWindowTokens {
		t.Fatalf("expected minimal request tokens to exceed context window, got %d <= %d", inputErr.MinimalRequestTokens, rc.ContextWindowTokens)
	}
	if rewritten.Messages[1].Role != "user" {
		t.Fatalf("rewrite should not mutate messages when token window is exceeded, got %#v", rewritten.Messages)
	}
}

func TestRewriteOversizeRequest_ForceCompactsWhenOnlyBytesOversize(t *testing.T) {
	gateway := &compactSummaryGateway{summary: "summary"}
	rc := &RunContext{
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			PersistTriggerContextPct:    85,
			TargetContextPct:            50,
			FallbackContextWindowTokens: 4096,
			PersistKeepLastMessages:     1,
		},
		ContextWindowTokens: 4096,
		Gateway:             gateway,
		SelectedRoute: &routing.SelectedProviderRoute{
			Route:      routing.ProviderRouteRule{Model: "gpt-4o", ID: "route-1"},
			Credential: routing.ProviderCredential{ProviderKind: routing.ProviderKindOpenAI},
		},
	}
	request := llm.Request{
		Model: "stub",
		Messages: []llm.Message{
			{Role: "user", Content: []llm.TextPart{{Text: "small"}}},
			{Role: "assistant", Content: []llm.TextPart{{Text: "tiny"}}},
			{Role: "user", Content: []llm.TextPart{{Text: "tail"}}},
		},
	}
	requestEstimateCalls := 0
	requestEstimate := func(req llm.Request) (int, error) {
		requestEstimateCalls++
		if len(req.Messages) >= 3 {
			return llm.RequestPayloadLimitBytes + 1024, nil
		}
		return llm.RequestPayloadLimitBytes - 1024, nil
	}

	rewritten, stats, err := RewriteOversizeRequest(context.Background(), rc, request, nil, requestEstimate)
	if err != nil {
		t.Fatalf("RewriteOversizeRequest failed: %v", err)
	}
	if stats.CompactApplied {
		t.Fatalf("expected no emergency persist without DB context, got %#v", stats)
	}
	if len(gateway.requests) != 0 {
		t.Fatalf("expected compact gateway idle without DB context, got %d", len(gateway.requests))
	}
	if requestEstimateCalls < 2 {
		t.Fatalf("expected estimator used during rewrite checks, got %d calls", requestEstimateCalls)
	}
	if len(rewritten.Messages) != len(request.Messages) {
		t.Fatalf("expected request unchanged without persistence substrate, got %d != %d", len(rewritten.Messages), len(request.Messages))
	}
}

func TestMicrocompactToolResults_KeepAll(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "q"}}},
		{Role: "tool", Content: []llm.TextPart{{Text: "r1"}}},
		{Role: "tool", Content: []llm.TextPart{{Text: "r2"}}},
	}
	out := microcompactToolResults(msgs, 5)
	for i := range out {
		if messageText(out[i]) != messageText(msgs[i]) {
			t.Fatalf("msg[%d] changed unexpectedly", i)
		}
	}
}

func TestMicrocompactToolResults_ClearOld(t *testing.T) {
	msgs := make([]llm.Message, 0, 20)
	for i := 0; i < 10; i++ {
		msgs = append(msgs, llm.Message{Role: "user", Content: []llm.TextPart{{Text: "q"}}})
		msgs = append(msgs, llm.Message{Role: "tool", Content: []llm.TextPart{{Text: "result-" + strings.Repeat("x", i)}}})
	}
	out := microcompactToolResults(msgs, 3)
	if len(out) != len(msgs) {
		t.Fatalf("expected same length, got %d", len(out))
	}
	toolCount := 0
	clearedCount := 0
	preservedCount := 0
	for _, m := range out {
		if m.Role != "tool" {
			continue
		}
		toolCount++
		if strings.Contains(messageText(m), `"cleared":true`) {
			clearedCount++
		} else {
			preservedCount++
		}
	}
	if toolCount != 10 {
		t.Fatalf("expected 10 tool messages, got %d", toolCount)
	}
	if clearedCount != 7 {
		t.Fatalf("expected 7 cleared, got %d", clearedCount)
	}
	if preservedCount != 3 {
		t.Fatalf("expected 3 preserved, got %d", preservedCount)
	}
}

func TestMicrocompactToolResults_PreservesNonTool(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "u1"}}},
		{Role: "tool", Content: []llm.TextPart{{Text: "old-tool"}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "a1"}}},
		{Role: "tool", Content: []llm.TextPart{{Text: "new-tool"}}},
	}
	out := microcompactToolResults(msgs, 1)
	if messageText(out[0]) != "u1" {
		t.Fatal("user message should be unchanged")
	}
	if !strings.Contains(messageText(out[1]), `"cleared":true`) {
		t.Fatal("old tool should be cleared")
	}
	if messageText(out[2]) != "a1" {
		t.Fatal("assistant message should be unchanged")
	}
	if messageText(out[3]) != "new-tool" {
		t.Fatal("recent tool should be preserved")
	}
}

func TestMicrocompactToolResults_OriginalUnmodified(t *testing.T) {
	msgs := []llm.Message{
		{Role: "tool", Content: []llm.TextPart{{Text: "old"}}},
		{Role: "tool", Content: []llm.TextPart{{Text: "new"}}},
	}
	origText := messageText(msgs[0])
	out := microcompactToolResults(msgs, 1)
	if messageText(msgs[0]) != origText {
		t.Fatal("original slice must not be modified")
	}
	if !strings.Contains(messageText(out[0]), `"cleared":true`) {
		t.Fatal("output should be cleared")
	}
}

// ---------------------------------------------------------------------------
// compactConsecutiveFailures
// ---------------------------------------------------------------------------

func TestCompactConsecutiveFailures_NilPool(t *testing.T) {
	got := compactConsecutiveFailures(t.Context(), nil, uuid.New(), uuid.New())
	if got != 0 {
		t.Fatalf("expected 0 for nil pool, got %d", got)
	}
}

func TestCompactConsecutiveFailures_NilIDs(t *testing.T) {
	got := compactConsecutiveFailures(t.Context(), nil, uuid.Nil, uuid.New())
	if got != 0 {
		t.Fatalf("expected 0 for nil accountID, got %d", got)
	}
	got = compactConsecutiveFailures(t.Context(), nil, uuid.New(), uuid.Nil)
	if got != 0 {
		t.Fatalf("expected 0 for nil threadID, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// ensureToolPairIntegrity
// ---------------------------------------------------------------------------

func TestEnsureToolPairIntegrity_NotOnTool(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "a"}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "b"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "c"}}},
	}
	if got := ensureToolPairIntegrity(msgs, 1); got != 1 {
		t.Fatalf("expected 1, got %d", got)
	}
}

func TestEnsureToolPairIntegrity_ToolWithAssistantBefore(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "a"}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "b"}}},
		{Role: "tool", Content: []llm.TextPart{{Text: "c"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "d"}}},
	}
	if got := ensureToolPairIntegrity(msgs, 2); got != 1 {
		t.Fatalf("expected 1, got %d", got)
	}
}

func TestEnsureToolPairIntegrity_ConsecutiveTools(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.TextPart{{Text: "a"}}},
		{Role: "tool", Content: []llm.TextPart{{Text: "b"}}},
		{Role: "tool", Content: []llm.TextPart{{Text: "c"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "d"}}},
	}
	if got := ensureToolPairIntegrity(msgs, 2); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestEnsureToolPairIntegrity_StartZeroTool(t *testing.T) {
	msgs := []llm.Message{
		{Role: "tool", Content: []llm.TextPart{{Text: "a"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "b"}}},
	}
	if got := ensureToolPairIntegrity(msgs, 0); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestEnsureToolPairIntegrity_Empty(t *testing.T) {
	if got := ensureToolPairIntegrity(nil, 0); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// sanitizeToolPairs
// ---------------------------------------------------------------------------

func toolMsg(callID string) llm.Message {
	return llm.Message{
		Role:    "tool",
		Content: []llm.TextPart{{Text: `{"tool_call_id":"` + callID + `","tool_name":"test","result":{}}`}},
	}
}

func assistantWithCalls(ids ...string) llm.Message {
	calls := make([]llm.ToolCall, len(ids))
	for i, id := range ids {
		calls[i] = llm.ToolCall{ToolCallID: id, ToolName: "test"}
	}
	return llm.Message{Role: "assistant", Content: []llm.TextPart{{Text: "ok"}}, ToolCalls: calls}
}

func TestSanitizeToolPairs_NormalPair(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "hi"}}},
		assistantWithCalls("c1"),
		toolMsg("c1"),
		{Role: "user", Content: []llm.TextPart{{Text: "next"}}},
	}
	out, _ := sanitizeToolPairs(msgs, nil)
	if len(out) != 4 {
		t.Fatalf("expected 4, got %d", len(out))
	}
}

func TestSanitizeToolPairs_OrphanAfterUser(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "hi"}}},
		toolMsg("c1"),
		{Role: "user", Content: []llm.TextPart{{Text: "next"}}},
	}
	out, _ := sanitizeToolPairs(msgs, nil)
	if len(out) != 2 {
		t.Fatalf("expected 2, got %d", len(out))
	}
	if out[0].Role != "user" || out[1].Role != "user" {
		t.Fatal("expected only user messages")
	}
}

func TestSanitizeToolPairs_OrphanAfterAssistantNoToolCalls(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.TextPart{{Text: "plain"}}},
		toolMsg("c1"),
	}
	out, _ := sanitizeToolPairs(msgs, nil)
	if len(out) != 1 {
		t.Fatalf("expected 1, got %d", len(out))
	}
	if out[0].Role != "assistant" {
		t.Fatal("expected assistant kept")
	}
}

func TestSanitizeToolPairs_OrphanMismatchedID(t *testing.T) {
	msgs := []llm.Message{
		assistantWithCalls("c1"),
		toolMsg("c2"),
	}
	out, _ := sanitizeToolPairs(msgs, nil)
	if len(out) != 0 {
		t.Fatalf("expected 0 (both removed), got %d", len(out))
	}
}

func TestSanitizeToolPairs_EmptySlice(t *testing.T) {
	out, _ := sanitizeToolPairs(nil, nil)
	if len(out) != 0 {
		t.Fatalf("expected empty, got %d", len(out))
	}
}

func TestSanitizeToolPairs_NoToolMessages(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "a"}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "b"}}},
	}
	out, _ := sanitizeToolPairs(msgs, nil)
	if &out[0] != &msgs[0] {
		t.Fatal("expected same slice returned when nothing to remove")
	}
}

func TestSanitizeToolPairs_MultiToolCallsSameAssistant(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "go"}}},
		assistantWithCalls("c1", "c2", "c3"),
		toolMsg("c1"),
		toolMsg("c2"),
		toolMsg("c3"),
		{Role: "user", Content: []llm.TextPart{{Text: "next"}}},
	}
	out, _ := sanitizeToolPairs(msgs, nil)
	if len(out) != 6 {
		t.Fatalf("expected 6, got %d", len(out))
	}
}

func TestSanitizeToolPairs_ToolAtIndex0(t *testing.T) {
	msgs := []llm.Message{
		toolMsg("c1"),
		{Role: "user", Content: []llm.TextPart{{Text: "hi"}}},
	}
	out, _ := sanitizeToolPairs(msgs, nil)
	if len(out) != 1 {
		t.Fatalf("expected 1, got %d", len(out))
	}
	if out[0].Role != "user" {
		t.Fatal("expected user only")
	}
}

func TestSanitizeToolPairs_OriginalUnmodified(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "hi"}}},
		toolMsg("c1"),
		{Role: "user", Content: []llm.TextPart{{Text: "bye"}}},
	}
	origLen := len(msgs)
	origRole1 := msgs[1].Role
	_, _ = sanitizeToolPairs(msgs, nil)
	if len(msgs) != origLen {
		t.Fatal("original slice length changed")
	}
	if msgs[1].Role != origRole1 {
		t.Fatal("original slice content changed")
	}
}

func TestSanitizeToolPairs_BadJSON(t *testing.T) {
	msgs := []llm.Message{
		assistantWithCalls("c1"),
		{Role: "tool", Content: []llm.TextPart{{Text: "not json"}}},
	}
	out, _ := sanitizeToolPairs(msgs, nil)
	if len(out) != 0 {
		t.Fatalf("expected 0 (both removed), got %d", len(out))
	}
}

func TestSanitizeToolPairs_EmptyToolCallID(t *testing.T) {
	msgs := []llm.Message{
		assistantWithCalls("c1"),
		{Role: "tool", Content: []llm.TextPart{{Text: `{"tool_name":"test","result":{}}`}}},
	}
	out, _ := sanitizeToolPairs(msgs, nil)
	if len(out) != 0 {
		t.Fatalf("expected 0 (both removed), got %d", len(out))
	}
}

func TestSanitizeToolPairs_IDsAligned(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "hi"}}},
		toolMsg("orphan"),
		assistantWithCalls("c1"),
		toolMsg("c1"),
	}
	outMsgs, outIDs := sanitizeToolPairs(msgs, ids)
	if len(outMsgs) != 3 {
		t.Fatalf("expected 3 msgs, got %d", len(outMsgs))
	}
	if len(outIDs) != 3 {
		t.Fatalf("expected 3 ids, got %d", len(outIDs))
	}
	if outIDs[0] != ids[0] || outIDs[1] != ids[2] || outIDs[2] != ids[3] {
		t.Fatal("ids not properly aligned")
	}
}

func TestSanitizeToolPairs_OrphanAssistantToolUseRemoved(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "hi"}}},
		assistantWithCalls("c1"),
		// tool for c1 is missing -> orphan tool call should be removed, but visible text should remain
		{Role: "user", Content: []llm.TextPart{{Text: "next"}}},
	}
	out, _ := sanitizeToolPairs(msgs, nil)
	if len(out) != 3 {
		t.Fatalf("expected 3, got %d", len(out))
	}
	if out[1].Role != "assistant" || len(out[1].ToolCalls) != 0 {
		t.Fatalf("expected visible assistant text without orphan tool calls, got %#v", out[1])
	}
}

func TestSanitizeToolPairs_PartialToolCallsPrunesAssistant(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "hi"}}},
		assistantWithCalls("c1", "c2"),
		toolMsg("c1"),
		// c2 tool is missing, but c1 is present -> assistant kept
		{Role: "user", Content: []llm.TextPart{{Text: "next"}}},
	}
	out, _ := sanitizeToolPairs(msgs, nil)
	if len(out) != 4 {
		t.Fatalf("expected 4, got %d", len(out))
	}
	if out[1].Role != "assistant" || len(out[1].ToolCalls) != 1 {
		t.Fatalf("assistant should keep only the surviving tool call, got %#v", out[1].ToolCalls)
	}
	if out[1].ToolCalls[0].ToolCallID != "c1" {
		t.Fatalf("expected c1 to survive, got %#v", out[1].ToolCalls)
	}
}

func TestSanitizeToolPairs_EmptyAssistantToolCallsKeepsVisibleText(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "hi"}}},
		{
			Role:      "assistant",
			Content:   []llm.TextPart{{Text: "done"}},
			ToolCalls: []llm.ToolCall{{ToolCallID: "c1", ToolName: "test"}},
		},
		{Role: "user", Content: []llm.TextPart{{Text: "next"}}},
	}
	out, _ := sanitizeToolPairs(msgs, nil)
	if len(out) != 3 {
		t.Fatalf("expected 3, got %d", len(out))
	}
	if out[1].Role != "assistant" || len(out[1].ToolCalls) != 0 {
		t.Fatalf("expected visible assistant text without orphan tool calls, got %#v", out[1])
	}
}

func TestSelectPersistFrontierWindowForPressure_CompactZoneUnderBudget(t *testing.T) {
	// compact zone under budget → selection includes both replacements and chunks
	frontier := []FrontierNode{
		{Kind: FrontierNodeReplacement, StartContextSeq: 1, EndContextSeq: 5, StartThreadSeq: 1, EndThreadSeq: 5, SourceText: "small R", ApproxTokens: 100, AtomSeq: 1},
	}
	for seq := int64(6); seq <= 30; seq++ {
		frontier = append(frontier, FrontierNode{
			Kind: FrontierNodeChunk, StartContextSeq: seq, EndContextSeq: seq, StartThreadSeq: seq, EndThreadSeq: seq,
			SourceText: fmt.Sprintf("c%d", seq), ApproxTokens: 2000, AtomSeq: int(seq), AtomType: compactAtomUserText, Role: "user",
			atomKey: fmt.Sprintf("a:%d", seq),
		})
	}
	rc := &RunContext{
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			TargetContextPct:            65,
			CompactZoneBudgetPct:        25,
			FallbackContextWindowTokens: 128000,
		},
		ContextWindowTokens: 128000,
	}
	selected, ok := selectPersistFrontierWindowForPressure(rc, frontier, 100000)
	if !ok || len(selected) == 0 {
		t.Fatal("expected non-empty selection when compact zone under budget")
	}
	hasChunk := false
	for _, n := range selected {
		if n.Kind == FrontierNodeChunk {
			hasChunk = true
			break
		}
	}
	if !hasChunk {
		t.Fatal("expected selection to include chunks when compact zone is under budget")
	}
}

func TestSelectPersistFrontierWindowForPressure_CompactZoneOverBudget(t *testing.T) {
	// compact zone over budget -> selection restricted to replacements only (compact-of-compact).
	// 6 replacements at 4000 tokens each, plus chunks at the tail.
	// window=128000, targetTokens=65%=83200, rawBudget=44800
	// raw zone scans from end: 15 chunks(30000) + R6(4000) = 34000 < 44800, + R5(4000) = 38000, + R4(4000) = 42000, + R3(4000) = 46000 > 44800
	// -> rawStart = index of R3 (=2), eligible = [R1, R2, R3]
	// compact zone budget = 25% of 128000 = 32000, replacement tokens in eligible = R1+R2+R3 = 12000
	// hmm, that's under budget. Use lower window so budget is smaller.
	frontier := []FrontierNode{
		{Kind: FrontierNodeReplacement, StartContextSeq: 1, EndContextSeq: 3, StartThreadSeq: 1, EndThreadSeq: 3, SourceText: "R1", ApproxTokens: 4000, AtomSeq: 1},
		{Kind: FrontierNodeReplacement, StartContextSeq: 4, EndContextSeq: 6, StartThreadSeq: 4, EndThreadSeq: 6, SourceText: "R2", ApproxTokens: 4000, AtomSeq: 2},
		{Kind: FrontierNodeReplacement, StartContextSeq: 7, EndContextSeq: 9, StartThreadSeq: 7, EndThreadSeq: 9, SourceText: "R3", ApproxTokens: 4000, AtomSeq: 3},
		{Kind: FrontierNodeReplacement, StartContextSeq: 10, EndContextSeq: 12, StartThreadSeq: 10, EndThreadSeq: 12, SourceText: "R4", ApproxTokens: 4000, AtomSeq: 4},
	}
	for seq := int64(13); seq <= 22; seq++ {
		frontier = append(frontier, FrontierNode{
			Kind: FrontierNodeChunk, StartContextSeq: seq, EndContextSeq: seq, StartThreadSeq: seq, EndThreadSeq: seq,
			SourceText: fmt.Sprintf("c%d", seq), ApproxTokens: 2000, AtomSeq: int(seq), AtomType: compactAtomUserText, Role: "user",
			atomKey: fmt.Sprintf("a:%d", seq),
		})
	}
	// window=40000, targetTokens=65%=26000, rawBudget=14000
	// raw zone from end: chunks 13-22 = 10*2000 = 20000 > 14000
	// -> scans backwards until sum > 14000: c22(2000)..c16(2000) = 7*2000 = 14000, c15 would be 16000 > 14000
	// rawStart is at R4+c13..c15 boundary area. eligible includes R1..R3 plus some chunks.
	// compact zone budget = 10% of 40000 = 4000, replacement tokens in eligible >= 12000 > 4000
	// -> filter to replacements only
	// selectCompactAtomWindow: protects last AtomSeq in eligible, selects from front
	// R1(4000) + R2(4000) = 8000 <= maxInputTokens(10000) -> selects R1+R2
	rc := &RunContext{
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			TargetContextPct:            65,
			CompactZoneBudgetPct:        10,
			FallbackContextWindowTokens: 40000,
		},
		ContextWindowTokens: 40000,
	}
	selected, ok := selectPersistFrontierWindowForPressure(rc, frontier, 35000)
	if !ok || len(selected) == 0 {
		t.Fatal("expected non-empty selection for compact-of-compact")
	}
	for _, n := range selected {
		if n.Kind == FrontierNodeChunk {
			t.Fatalf("expected only replacement nodes when compact zone over budget, got chunk: %+v", n)
		}
	}
}
