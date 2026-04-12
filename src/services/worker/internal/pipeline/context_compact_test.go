package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"

	"github.com/google/uuid"
	"github.com/pkoukk/tiktoken-go"
)

type stubCompactGateway struct {
	summary string
	calls   int
	lastReq llm.Request
}

func (g *stubCompactGateway) Stream(_ context.Context, request llm.Request, yield func(llm.StreamEvent) error) error {
	g.calls++
	g.lastReq = request
	if err := yield(llm.StreamMessageDelta{ContentDelta: g.summary, Role: "assistant"}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{})
}

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
	// stabilizeCompactStart skips past assistant to land on user
	if dropped != 2 || len(out) != 1 {
		t.Fatalf("expected drop 2, len 1, got dropped=%d len=%d", dropped, len(out))
	}
	if out[0].Role != "user" || outIDs[0] != ids[2] {
		t.Fatalf("expected user start, got %q", out[0].Role)
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
	if dropped == 0 || len(out) == len(msgs) {
		t.Fatalf("expected head dropped, dropped=%d len=%d", dropped, len(out))
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

	out := selectRenderableReplacementSpans(spans, lastAtom)
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

func TestMaybeInlineCompactSingleOversizedTextAtomRejectsToolCalls(t *testing.T) {
	rc := &RunContext{
		Gateway: &stubCompactGateway{summary: "summary"},
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{Model: "stub"},
		},
	}
	msg := llm.Message{
		Role: "assistant",
		Content: []llm.TextPart{{
			Text: strings.Repeat("tool payload ", 400),
		}},
		ToolCalls: []llm.ToolCall{{
			ToolCallID: "call_1",
			ToolName:   "search",
		}},
	}

	out, changed, err := maybeInlineCompactSingleOversizedTextAtom(context.Background(), rc, msg, nil)
	if err != nil {
		t.Fatalf("maybeInlineCompactSingleOversizedTextAtom returned error: %v", err)
	}
	if changed {
		t.Fatalf("expected tool episode message to stay intact, got %#v", out)
	}
}

func TestMaybeInlineCompactMessagesAfterCompactReceivesRealOutput(t *testing.T) {
	advisor := &captureCompactionAdvisor{}
	registry := NewHookRegistry()
	registry.RegisterCompactionAdvisor(advisor)

	rc := &RunContext{
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			PersistTriggerApproxTokens:  1,
			PersistTriggerContextPct:    0,
			FallbackContextWindowTokens: 1_000_000,
			PersistKeepLastMessages:     1,
		},
		Gateway: &stubCompactGateway{summary: "summary"},
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

	out, _, changed, err := MaybeInlineCompactMessages(context.Background(), rc, msgs, nil)
	if err != nil {
		t.Fatalf("MaybeInlineCompactMessages: %v", err)
	}
	if !changed {
		t.Fatal("expected inline compact change")
	}
	if len(advisor.outputs) != 1 {
		t.Fatalf("expected one after compact callback, got %d", len(advisor.outputs))
	}
	got := advisor.outputs[0]
	if !got.Changed {
		t.Fatal("expected compact output Changed=true")
	}
	if strings.TrimSpace(got.Summary) != "summary" {
		t.Fatalf("unexpected summary: %q", got.Summary)
	}
	if len(got.Messages) != len(out) {
		t.Fatalf("expected %d output messages, got %d", len(out), len(got.Messages))
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

	out := selectRenderableReplacementSpans(spans, lastAtom)
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

func TestSelectPromotionReplacementsUsesOldestSideOrder(t *testing.T) {
	now := time.Now().UTC()
	items := []data.ThreadContextReplacementRecord{
		{
			ID:              uuid.New(),
			StartContextSeq: 21,
			EndContextSeq:   30,
			SummaryText:     "newer high layer",
			Layer:           3,
			CreatedAt:       now.Add(2 * time.Minute),
		},
		{
			ID:              uuid.New(),
			StartContextSeq: 1,
			EndContextSeq:   10,
			SummaryText:     "first",
			Layer:           1,
			CreatedAt:       now,
		},
		{
			ID:              uuid.New(),
			StartContextSeq: 11,
			EndContextSeq:   20,
			SummaryText:     "second",
			Layer:           1,
			CreatedAt:       now.Add(30 * time.Second),
		},
	}

	selected := selectPromotionReplacements(items)
	if len(selected) != 3 {
		t.Fatalf("expected 3 promotion candidates, got %#v", selected)
	}
	if selected[0].SummaryText != "first" || selected[1].SummaryText != "second" || selected[2].SummaryText != "newer high layer" {
		t.Fatalf("expected oldest-side ordering, got %#v", selected)
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

func TestMaybeInlineCompactMessages_SingleOversizedTextAtom(t *testing.T) {
	gateway := &stubCompactGateway{summary: "compacted head"}
	huge := strings.Repeat("alpha beta gamma delta\n\n", 240)
	rc := &RunContext{
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			PersistTriggerApproxTokens:  1,
			PersistTriggerContextPct:    0,
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
	out, stats, changed, err := MaybeInlineCompactMessages(context.Background(), rc, msgs, nil)
	if err != nil {
		t.Fatalf("MaybeInlineCompactMessages: %v", err)
	}
	if !changed {
		t.Fatal("expected single oversized atom to be compacted")
	}
	if len(out) != 2 {
		t.Fatalf("expected snapshot + tail message, got %d", len(out))
	}
	if !strings.Contains(messageText(out[0]), compactSnapshotHeader) {
		t.Fatalf("expected leading snapshot header, got %q", messageText(out[0]))
	}
	if strings.TrimSpace(messageText(out[1])) == "" {
		t.Fatal("expected tail message kept")
	}
	if !stats.SingleAtomPartial {
		t.Fatalf("expected single atom partial flag in stats, got %#v", stats)
	}
	if stats.TargetChunkCount < 2 {
		t.Fatalf("expected chunk-driven compact stats, got %#v", stats)
	}
	if gateway.calls != 1 {
		t.Fatalf("expected compact gateway called once, got %d", gateway.calls)
	}
	body := messageText(gateway.lastReq.Messages[1])
	if !strings.Contains(body, "<target-chunks>") {
		t.Fatalf("expected chunk contract block, got %q", body)
	}
}

func TestRunContextCompactLLM_PromotionUsesTargetChunksNotPreviousReplacements(t *testing.T) {
	gateway := &stubCompactGateway{summary: "promoted compact"}
	msgs := buildPromotionCompactMessages("summary one", "summary two", "summary three")

	out, err := runContextCompactLLM(context.Background(), nil, gateway, "stub", msgs, nil, "")
	if err != nil {
		t.Fatalf("runContextCompactLLM: %v", err)
	}
	if out != "promoted compact" {
		t.Fatalf("unexpected compact output: %q", out)
	}
	body := messageText(gateway.lastReq.Messages[1])
	if !strings.Contains(body, "<target-chunks>") {
		t.Fatalf("expected target chunks in request, got %q", body)
	}
	if strings.Contains(body, "<previous-replacements>") {
		t.Fatalf("promotion input should not be treated as previous replacements: %q", body)
	}
	if !strings.Contains(body, "summary one") || !strings.Contains(body, "summary two") || !strings.Contains(body, "summary three") {
		t.Fatalf("expected promotion summaries inside target chunks, got %q", body)
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

	rewritten, stats, err := RewriteOversizeRequest(context.Background(), rc, request, nil, llm.EstimateRequestJSONBytes)
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

	rewritten, stats, err := RewriteOversizeRequest(context.Background(), rc, request, nil, llm.EstimateRequestJSONBytes)
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
	got := compactConsecutiveFailures(t.Context(), noopCompactPersistDB{}, uuid.Nil, uuid.New())
	if got != 0 {
		t.Fatalf("expected 0 for nil accountID, got %d", got)
	}
	got = compactConsecutiveFailures(t.Context(), noopCompactPersistDB{}, uuid.New(), uuid.Nil)
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
