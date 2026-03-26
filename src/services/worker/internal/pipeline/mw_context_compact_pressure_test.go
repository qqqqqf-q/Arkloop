//go:build !desktop

package pipeline

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type compactSummaryGateway struct {
	requests []llm.Request
	summary  string
}

func (g *compactSummaryGateway) Stream(_ context.Context, request llm.Request, yield func(llm.StreamEvent) error) error {
	g.requests = append(g.requests, request)
	if err := yield(llm.StreamMessageDelta{ContentDelta: g.summary, Role: "assistant"}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{})
}

func TestMaybeInlineCompactMessagesUsesAnchorPressure(t *testing.T) {
	gateway := &compactSummaryGateway{summary: "summary"}
	rc := &RunContext{
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			PersistTriggerApproxTokens:  0,
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
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "first"}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "second"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "tail"}}},
	}
	estimate := HistoryThreadPromptTokensForRoute(rc.SelectedRoute, msgs)
	rc.ContextCompact.PersistTriggerApproxTokens = estimate + 1
	anchor := &ContextCompactPressureAnchor{
		LastRealPromptTokens:             estimate + 20,
		LastRequestContextEstimateTokens: estimate,
	}

	out, stats, changed, err := MaybeInlineCompactMessages(context.Background(), rc, msgs, anchor)
	if err != nil {
		t.Fatalf("MaybeInlineCompactMessages: %v", err)
	}
	if !changed {
		t.Fatal("expected inline compaction to trigger from anchored pressure")
	}
	if stats.ContextPressureTokens <= stats.ContextEstimateTokens {
		t.Fatalf("expected anchored pressure to exceed estimate, got pressure=%d estimate=%d", stats.ContextPressureTokens, stats.ContextEstimateTokens)
	}
	if len(gateway.requests) != 1 {
		t.Fatalf("expected exactly one summary request, got %d", len(gateway.requests))
	}
	if len(out) != 2 {
		t.Fatalf("expected summary plus tail, got %d messages", len(out))
	}
	if out[0].Role != "system" {
		t.Fatalf("expected summary system message, got %q", out[0].Role)
	}
}

func TestResolveContextCompactPressureAnchorReadsNewestTurnAnchor(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "context_compact_pressure_anchor")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	threadID := uuid.New()
	runOld := uuid.New()
	runNew := uuid.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO threads (id, account_id) VALUES ($1, $2)`,
		threadID, accountID,
	); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'completed'), ($4, $2, $3, 'completed')`,
		runOld, accountID, threadID, runNew,
	); err != nil {
		t.Fatalf("insert runs: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO run_events (run_id, seq, ts, type, data_json)
		 VALUES
		 ($1, 1, now() - interval '1 minute', 'llm.turn.completed', '{"last_real_prompt_tokens":100,"last_request_context_estimate_tokens":90}'::jsonb),
		 ($2, 1, now(), 'llm.turn.completed', '{"last_real_prompt_tokens":130,"last_request_context_estimate_tokens":120}'::jsonb)`,
		runOld, runNew,
	); err != nil {
		t.Fatalf("insert run events: %v", err)
	}

	rc := &RunContext{}
	rc.Run.AccountID = accountID
	rc.Run.ThreadID = threadID
	anchor, ok := resolveContextCompactPressureAnchor(context.Background(), pool, rc)
	if !ok {
		t.Fatal("expected anchor")
	}
	if anchor.LastRealPromptTokens != 130 {
		t.Fatalf("unexpected last real prompt tokens: %d", anchor.LastRealPromptTokens)
	}
	if anchor.LastRequestContextEstimateTokens != 120 {
		t.Fatalf("unexpected last request estimate: %d", anchor.LastRequestContextEstimateTokens)
	}
}
