//go:build !desktop

package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

type failingCompactEventAppender struct{}

func (failingCompactEventAppender) AppendRunEvent(_ context.Context, _ pgx.Tx, _ uuid.UUID, _ events.RunEvent) (int64, error) {
	return 0, errors.New("append failed")
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
	if out[0].Role != "user" {
		t.Fatalf("expected summary snapshot user message, got %q", out[0].Role)
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

func TestContextCompactPersistFailureDoesNotMarkTrimmedMessages(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "context_compact_persist_failure_trim")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()
	msg3ID := uuid.New()
	msg4ID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, []any{accountID}},
		{`INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, []any{projectID, accountID}},
		{`INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, []any{threadID, accountID, projectID}},
		{`INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, []any{runID, accountID, threadID}},
	} {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed data: %v", err)
		}
	}
	for _, msg := range []struct {
		id      uuid.UUID
		role    string
		content string
	}{
		{msg1ID, "user", "m1"},
		{msg2ID, "assistant", "m2"},
		{msg3ID, "user", "m3"},
		{msg4ID, "user", "m4"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden, compacted) VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, false, false)`,
			msg.id, accountID, threadID, msg.role, msg.content,
		); err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}

	rc := &RunContext{
		Run:     data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		Emitter: events.NewEmitter("trace"),
		ContextCompact: ContextCompactSettings{
			Enabled:                     true,
			MaxMessages:                 1,
			PersistEnabled:              true,
			PersistTriggerApproxTokens:  1,
			PersistTriggerContextPct:    0,
			FallbackContextWindowTokens: 1_000_000,
			PersistKeepLastMessages:     2,
		},
		Gateway: &compactSummaryGateway{summary: "persisted summary"},
		SelectedRoute: &routing.SelectedProviderRoute{
			Route:      routing.ProviderRouteRule{Model: "gpt-4o", ID: "route-1"},
			Credential: routing.ProviderCredential{ProviderKind: routing.ProviderKindOpenAI},
		},
		Messages: []llm.Message{
			{Role: "user", Content: []llm.TextPart{{Text: "m1"}}},
			{Role: "assistant", Content: []llm.TextPart{{Text: "m2"}}},
			{Role: "user", Content: []llm.TextPart{{Text: "m3"}}},
			{Role: "user", Content: []llm.TextPart{{Text: "m4"}}},
		},
		ThreadMessageIDs: []uuid.UUID{msg1ID, msg2ID, msg3ID, msg4ID},
	}

	mw := NewContextCompactMiddleware(pool, data.MessagesRepository{}, failingCompactEventAppender{}, rc.Gateway, false)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}

	var compacted, hidden bool
	if err := pool.QueryRow(ctx,
		`SELECT compacted, hidden FROM messages WHERE id = $1`,
		msg3ID,
	).Scan(&compacted, &hidden); err != nil {
		t.Fatalf("query message 3: %v", err)
	}
	if compacted || hidden {
		t.Fatalf("expected message 3 to stay visible after persist failure, compacted=%v hidden=%v", compacted, hidden)
	}

	var eventType, phase, op, errText string
	if err := pool.QueryRow(ctx,
		`SELECT type, data_json->>'phase', data_json->>'op', data_json->>'error'
		   FROM run_events
		  WHERE run_id = $1 AND type = 'run.context_compact' AND data_json->>'phase' = 'mark_compacted'
		  ORDER BY seq DESC
		  LIMIT 1`,
		runID,
	).Scan(&eventType, &phase, &op, &errText); err != nil {
		t.Fatalf("query failure event: %v", err)
	}
	if eventType != "run.context_compact" || phase != "mark_compacted" || op != "persist" {
		t.Fatalf("unexpected failure event: type=%s phase=%s op=%s", eventType, phase, op)
	}
	if strings.TrimSpace(errText) == "" {
		t.Fatal("expected failure event to include error text")
	}
}

func TestContextCompactMiddlewareAfterCompactReceivesPersistOutput(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "context_compact_after_output")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()
	msg3ID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, []any{accountID}},
		{`INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, []any{projectID, accountID}},
		{`INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, []any{threadID, accountID, projectID}},
		{`INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, []any{runID, accountID, threadID}},
	} {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed data: %v", err)
		}
	}
	for _, msg := range []struct {
		id      uuid.UUID
		role    string
		content string
	}{
		{msg1ID, "user", "m1"},
		{msg2ID, "assistant", "m2"},
		{msg3ID, "user", "m3"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden, compacted) VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, false, false)`,
			msg.id, accountID, threadID, msg.role, msg.content,
		); err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}

	advisor := &captureCompactionAdvisor{}
	registry := NewHookRegistry()
	registry.RegisterCompactionAdvisor(advisor)

	rc := &RunContext{
		Run:     data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		Emitter: events.NewEmitter("trace"),
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			PersistTriggerApproxTokens:  1,
			PersistTriggerContextPct:    0,
			FallbackContextWindowTokens: 1_000_000,
			PersistKeepLastMessages:     1,
		},
		Gateway: &compactSummaryGateway{summary: "persisted summary"},
		SelectedRoute: &routing.SelectedProviderRoute{
			Route:      routing.ProviderRouteRule{Model: "gpt-4o", ID: "route-1"},
			Credential: routing.ProviderCredential{ProviderKind: routing.ProviderKindOpenAI},
		},
		Messages:                  []llm.Message{{Role: "user", Content: []llm.TextPart{{Text: "m1"}}}, {Role: "assistant", Content: []llm.TextPart{{Text: "m2"}}}, {Role: "user", Content: []llm.TextPart{{Text: "m3"}}}},
		ThreadMessageIDs:          []uuid.UUID{msg1ID, msg2ID, msg3ID},
		HookRuntime:               NewHookRuntime(registry, NewDefaultHookResultApplier()),
		ActiveCompactSnapshotText: "",
	}

	mw := NewContextCompactMiddleware(pool, data.MessagesRepository{}, nil, rc.Gateway, false)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}

	if len(advisor.outputs) != 1 {
		t.Fatalf("expected one after compact callback, got %d", len(advisor.outputs))
	}
	got := advisor.outputs[0]
	if !got.Changed {
		t.Fatal("expected Changed=true")
	}
	if strings.TrimSpace(got.Summary) != "persisted summary" {
		t.Fatalf("unexpected summary: %q", got.Summary)
	}
	if len(got.Messages) != len(rc.Messages) {
		t.Fatalf("expected %d messages, got %d", len(rc.Messages), len(got.Messages))
	}
}
