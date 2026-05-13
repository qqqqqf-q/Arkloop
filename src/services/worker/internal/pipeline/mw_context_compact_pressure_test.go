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

func TestContextCompactMiddlewareEmitsCheckTiming(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "context_compact_check_timing")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, []any{accountID}},
		{`INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, []any{projectID, accountID}},
		{`INSERT INTO threads (id, account_id, project_id, next_message_seq) VALUES ($1, $2, $3, 1)`, []any{threadID, accountID, projectID}},
		{`INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, []any{runID, accountID, threadID}},
	} {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed db: %v", err)
		}
	}

	tracer := &spyTracer{}
	rc := &RunContext{
		Run:     data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		Emitter: events.NewEmitter("trace"),
		Tracer:  tracer,
		ContextCompact: ContextCompactSettings{
			Enabled:        true,
			PersistEnabled: true,
		},
		SelectedRoute: &routing.SelectedProviderRoute{
			Route:      routing.ProviderRouteRule{Model: "gpt-4o", ID: "route-1"},
			Credential: routing.ProviderCredential{ProviderKind: routing.ProviderKindOpenAI},
		},
		Messages: []llm.Message{
			{Role: "user", Content: []llm.TextPart{{Text: "hello"}}},
		},
	}

	mw := NewContextCompactMiddleware(pool, data.MessagesRepository{}, data.RunEventsRepository{}, nil, false)
	if err := mw(ctx, rc, func(context.Context, *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}

	var phase, status string
	var durationMs, thresholdMs, anchorQueryMs, tokenEstimateMs, microcompactMs int64
	if err := pool.QueryRow(ctx,
		`SELECT data_json->>'phase',
		        data_json->>'status',
		        COALESCE((data_json->>'duration_ms')::bigint, -1),
		        COALESCE((data_json->>'threshold_ms')::bigint, -1),
		        COALESCE((data_json->>'anchor_query_ms')::bigint, -1),
		        COALESCE((data_json->>'token_estimate_ms')::bigint, -1),
		        COALESCE((data_json->>'microcompact_ms')::bigint, -1)
		   FROM run_events
		  WHERE run_id = $1 AND type = 'run.context_compact'
		  ORDER BY seq DESC
		  LIMIT 1`,
		runID,
	).Scan(&phase, &status, &durationMs, &thresholdMs, &anchorQueryMs, &tokenEstimateMs, &microcompactMs); err != nil {
		t.Fatalf("query context compact event: %v", err)
	}
	if phase != "middleware_completed" {
		t.Fatalf("phase = %q, want middleware_completed", phase)
	}
	if status != "completed" && status != "slow" {
		t.Fatalf("unexpected status: %q", status)
	}
	for name, value := range map[string]int64{
		"duration_ms":       durationMs,
		"threshold_ms":      thresholdMs,
		"anchor_query_ms":   anchorQueryMs,
		"token_estimate_ms": tokenEstimateMs,
		"microcompact_ms":   microcompactMs,
	} {
		if value < 0 {
			t.Fatalf("%s missing from context compact event", name)
		}
	}

	record := findTraceEvent(tracer.records, "context_compact.check_completed")
	if record == nil {
		t.Fatal("expected context compact check trace")
	}
	if _, ok := record.fields["event_write_ms"]; !ok {
		t.Fatal("expected context compact trace to include event_write_ms")
	}
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
	rc.ContextCompact.TargetContextPct = 1
	anchor := &ContextCompactPressureAnchor{
		LastRealPromptTokens:             estimate + 20,
		LastRequestContextEstimateTokens: estimate,
	}

	out, stats, changed, err := MaybeInlineCompactMessages(context.Background(), rc, msgs, anchor, false)
	if err != nil {
		t.Fatalf("MaybeInlineCompactMessages: %v", err)
	}
	if changed {
		t.Fatal("expected normal inline compaction retired")
	}
	if stats.ContextPressureTokens <= stats.ContextEstimateTokens {
		t.Fatalf("expected anchored pressure to exceed estimate, got pressure=%d estimate=%d", stats.ContextPressureTokens, stats.ContextEstimateTokens)
	}
	if len(gateway.requests) != 0 {
		t.Fatalf("expected no summary request, got %d", len(gateway.requests))
	}
	if len(out) != len(msgs) {
		t.Fatalf("expected messages unchanged, got %d", len(out))
	}
}

func TestMaybeInlineCompactMessagesUsesTokenWindowWhenProviderBytesEstimatorExists(t *testing.T) {
	gateway := &compactSummaryGateway{summary: "summary"}
	rc := &RunContext{
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			PersistTriggerContextPct:    85,
			TargetContextPct:            50,
			FallbackContextWindowTokens: 32,
			PersistKeepLastMessages:     1,
		},
		ContextWindowTokens: 32,
		Gateway:             gateway,
		SelectedRoute: &routing.SelectedProviderRoute{
			Route:      routing.ProviderRouteRule{Model: "gpt-4o", ID: "route-1"},
			Credential: routing.ProviderCredential{ProviderKind: routing.ProviderKindOpenAI},
		},
		SystemPrompt: "system prompt",
	}
	rc.EstimateProviderRequestBytes = func(llm.Request) (int, error) {
		return 40, nil
	}
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda"}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "ack"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "mu nu xi omicron pi rho sigma tau upsilon phi chi psi omega"}}},
	}

	estimate, stats := inlineCompactEstimatePressure(rc, msgs, nil)
	if estimate <= rc.ContextWindowTokens || stats.ContextPressureTokens <= rc.ContextWindowTokens {
		t.Fatalf("expected token estimate to exceed window, got estimate=%d pressure=%d window=%d", estimate, stats.ContextPressureTokens, rc.ContextWindowTokens)
	}

	out, _, changed, err := MaybeInlineCompactMessages(context.Background(), rc, msgs, nil, false)
	if err != nil {
		t.Fatalf("MaybeInlineCompactMessages: %v", err)
	}
	if changed {
		t.Fatal("expected normal inline compaction retired")
	}
	if len(gateway.requests) != 0 {
		t.Fatalf("expected no summary request, got %d", len(gateway.requests))
	}
	if len(out) != len(msgs) {
		t.Fatalf("expected messages unchanged, got %d", len(out))
	}
}

func TestRewriteOversizeRequestPersistsReplacementAndRebuildsRealView(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "context_compact_emergency_persist")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	msgIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	if _, err := pool.Exec(context.Background(), `INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, projectID, accountID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO threads (id, account_id, project_id, next_message_seq) VALUES ($1, $2, $3, 10)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	payloads := []struct {
		id        uuid.UUID
		threadSeq int
		role      string
		content   string
	}{
		{id: msgIDs[0], threadSeq: 1, role: "user", content: "first persisted message"},
		{id: msgIDs[1], threadSeq: 2, role: "assistant", content: "second persisted message"},
		{id: msgIDs[2], threadSeq: 3, role: "user", content: "tail message"},
	}
	for _, msg := range payloads {
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
			 VALUES ($1, $2, $3, $4, $5, $6, '{}'::jsonb, false)`,
			msg.id, accountID, threadID, msg.threadSeq, msg.role, msg.content,
		); err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}

	tx, err := pool.BeginTx(context.Background(), pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	canonical, err := buildCanonicalThreadContext(
		context.Background(),
		tx,
		data.Run{AccountID: accountID, ThreadID: threadID},
		data.MessagesRepository{},
		nil,
		nil,
		0,
	)
	_ = tx.Rollback(context.Background())
	if err != nil {
		t.Fatalf("build canonical context: %v", err)
	}

	gateway := &compactSummaryGateway{summary: "rolled summary"}
	rc := &RunContext{
		Run:                   data.Run{AccountID: accountID, ThreadID: threadID},
		DB:                    pool,
		Messages:              canonical.Messages,
		ThreadMessageIDs:      canonical.ThreadMessageIDs,
		ThreadContextFrontier: canonical.Frontier,
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			TargetContextPct:            65,
			FallbackContextWindowTokens: 4096,
		},
		ContextWindowTokens: 4096,
		Gateway:             gateway,
		SelectedRoute: &routing.SelectedProviderRoute{
			Route:      routing.ProviderRouteRule{Model: "gpt-4o", ID: "route-1"},
			Credential: routing.ProviderCredential{ProviderKind: routing.ProviderKindOpenAI},
		},
	}
	request := llm.Request{Model: "stub", Messages: canonical.Messages}
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
	if !stats.CompactApplied {
		t.Fatalf("expected emergency persist rewrite, got %#v", stats)
	}
	if len(gateway.requests) != 1 {
		t.Fatalf("expected one compact summary request, got %d", len(gateway.requests))
	}
	if len(rewritten.Messages) != 2 {
		t.Fatalf("expected rebuilt request to shrink to replacement + tail, got %d", len(rewritten.Messages))
	}
	if rewritten.Messages[0].Role != "system" {
		t.Fatalf("expected replacement rendered as system, got %q", rewritten.Messages[0].Role)
	}
	if rewritten.Messages[0].Phase == nil || *rewritten.Messages[0].Phase != compactSyntheticPhase {
		t.Fatalf("expected replacement phase %q, got %#v", compactSyntheticPhase, rewritten.Messages[0].Phase)
	}
	if messageText(rewritten.Messages[0]) != "rolled summary" {
		t.Fatalf("unexpected replacement text: %q", messageText(rewritten.Messages[0]))
	}
	if len(rc.ThreadContextFrontier) < 2 || rc.ThreadContextFrontier[0].Kind != FrontierNodeReplacement || rc.ThreadContextFrontier[1].Kind != FrontierNodeChunk {
		t.Fatalf("expected prefix-only replacement frontier, got %#v", rc.ThreadContextFrontier)
	}
	if requestEstimateCalls < 3 {
		t.Fatalf("expected request estimator used before and after rebuild, got %d calls", requestEstimateCalls)
	}

	var replacements int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM thread_context_replacements WHERE account_id = $1 AND thread_id = $2 AND superseded_at IS NULL`,
		accountID, threadID,
	).Scan(&replacements); err != nil {
		t.Fatalf("count replacements: %v", err)
	}
	if replacements != 1 {
		t.Fatalf("expected one active replacement, got %d", replacements)
	}
}

func TestRewriteOversizeRequestForceCompactsAfterProviderContextError(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "context_compact_provider_forced")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, projectID, accountID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO threads (id, account_id, project_id, next_message_seq) VALUES ($1, $2, $3, 10)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	for i, content := range []string{"first persisted message", "second persisted message", "tail message"} {
		role := "user"
		if i == 1 {
			role = "assistant"
		}
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
			 VALUES ($1, $2, $3, $4, $5, $6, '{}'::jsonb, false)`,
			uuid.New(), accountID, threadID, i+1, role, content,
		); err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}

	tx, err := pool.BeginTx(context.Background(), pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	canonical, err := buildCanonicalThreadContext(
		context.Background(),
		tx,
		data.Run{AccountID: accountID, ThreadID: threadID},
		data.MessagesRepository{},
		nil,
		nil,
		0,
	)
	_ = tx.Rollback(context.Background())
	if err != nil {
		t.Fatalf("build canonical context: %v", err)
	}

	gateway := &compactSummaryGateway{summary: "provider forced summary"}
	rc := &RunContext{
		Run:                   data.Run{AccountID: accountID, ThreadID: threadID},
		DB:                    pool,
		Messages:              canonical.Messages,
		ThreadMessageIDs:      canonical.ThreadMessageIDs,
		ThreadContextFrontier: canonical.Frontier,
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			TargetContextPct:            65,
			FallbackContextWindowTokens: 4096,
		},
		ContextWindowTokens: 4096,
		Gateway:             gateway,
		SelectedRoute: &routing.SelectedProviderRoute{
			Route:      routing.ProviderRouteRule{Model: "gpt-4o", ID: "route-1"},
			Credential: routing.ProviderCredential{ProviderKind: routing.ProviderKindOpenAI},
		},
	}
	request := llm.Request{Model: "stub", Messages: canonical.Messages}
	requestEstimate := func(req llm.Request) (int, error) {
		return len(req.Messages) * 100, nil
	}

	rewritten, stats, err := RewriteOversizeRequestWithOptions(context.Background(), rc, request, nil, requestEstimate, true)
	if err != nil {
		t.Fatalf("RewriteOversizeRequestWithOptions failed: %v", err)
	}
	if !stats.CompactApplied {
		t.Fatalf("expected forced provider rewrite to compact despite fitting local estimate, got %#v", stats)
	}
	if len(gateway.requests) != 1 {
		t.Fatalf("expected one compact summary request, got %d", len(gateway.requests))
	}
	if messageText(rewritten.Messages[0]) != "provider forced summary" {
		t.Fatalf("unexpected replacement text: %q", messageText(rewritten.Messages[0]))
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

func TestCompactConsecutiveFailuresIgnoresAttemptProgressEvents(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "context_compact_ignore_attempt_progress")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO threads (id, account_id) VALUES ($1, $2)`,
		threadID, accountID,
	); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'completed')`,
		runID, accountID, threadID,
	); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO run_events (run_id, seq, ts, type, data_json)
		 VALUES
		 ($1, 1, now() - interval '3 second', 'run.context_compact', '{"phase":"llm_failed","llm_error":"boom"}'::jsonb),
		 ($1, 2, now() - interval '2 second', 'run.context_compact', '{"phase":"llm_request_retrying","llm_error":"retry"}'::jsonb),
		 ($1, 3, now() - interval '1 second', 'run.context_compact', '{"phase":"llm_request_completed"}'::jsonb)`,
		runID,
	); err != nil {
		t.Fatalf("insert run events: %v", err)
	}

	got := compactConsecutiveFailures(context.Background(), pool, accountID, threadID)
	if got != 1 {
		t.Fatalf("expected attempt progress events to be ignored, got %d", got)
	}
}

func TestResolveCompactionGatewayDefaultsToCurrentThreadRoute(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "context_compact_current_route_default")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO accounts (id, type) VALUES ($1, 'personal')`,
		accountID,
	); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO account_entitlement_overrides (account_id, key, value, value_type)
		 VALUES ($1, 'spawn.profile.tool', 'tool-route', 'string')`,
		accountID,
	); err != nil {
		t.Fatalf("insert tool override: %v", err)
	}

	threadGateway := &compactSummaryGateway{summary: "thread"}
	toolGateway := &compactSummaryGateway{summary: "tool"}
	rc := &RunContext{
		Run:     data.Run{AccountID: accountID},
		Gateway: threadGateway,
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{Model: "thread-model", ID: "thread-route"},
		},
	}
	configLoader := routing.NewConfigLoader(nil, routing.ProviderRoutingConfig{
		DefaultRouteID: "tool-route",
		Credentials: []routing.ProviderCredential{{
			ID:           "stub-tool",
			Name:         "tool",
			OwnerKind:    routing.CredentialScopePlatform,
			ProviderKind: routing.ProviderKindStub,
		}},
		Routes: []routing.ProviderRouteRule{{
			ID:           "tool-route",
			Model:        "tool-model",
			CredentialID: "stub-tool",
		}},
	})

	gotGateway, gotModel := resolveCompactionGateway(ctx, pool, rc, toolGateway, false, configLoader)
	if gotGateway != threadGateway {
		t.Fatalf("expected compaction gateway to follow current thread route by default")
	}
	if gotModel != "thread-model" {
		t.Fatalf("expected compaction model %q, got %q", "thread-model", gotModel)
	}
}

func TestResolveCompactionGatewayHonorsExplicitSelector(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "context_compact_explicit_selector")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO accounts (id, type) VALUES ($1, 'personal')`,
		accountID,
	); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO platform_settings (key, value) VALUES ($1, $2)`,
		settingContextCompactionModel, "tool-route",
	); err != nil {
		t.Fatalf("insert platform setting: %v", err)
	}

	threadGateway := &compactSummaryGateway{summary: "thread"}
	toolGateway := &compactSummaryGateway{summary: "tool"}
	rc := &RunContext{
		Run:                 data.Run{AccountID: accountID},
		Gateway:             threadGateway,
		LlmMaxResponseBytes: 8192,
		RoutingByokEnabled:  false,
		SelectedRoute:       &routing.SelectedProviderRoute{Route: routing.ProviderRouteRule{Model: "thread-model", ID: "thread-route"}},
	}
	configLoader := routing.NewConfigLoader(nil, routing.ProviderRoutingConfig{
		DefaultRouteID: "tool-route",
		Credentials: []routing.ProviderCredential{{
			ID:           "stub-tool",
			Name:         "tool",
			OwnerKind:    routing.CredentialScopePlatform,
			ProviderKind: routing.ProviderKindStub,
		}},
		Routes: []routing.ProviderRouteRule{{
			ID:           "tool-route",
			Model:        "tool-model",
			CredentialID: "stub-tool",
		}},
	})

	gotGateway, gotModel := resolveCompactionGateway(ctx, pool, rc, toolGateway, false, configLoader)
	if gotGateway != toolGateway {
		t.Fatalf("expected explicit compaction selector to use configured gateway")
	}
	if gotModel != "tool-model" {
		t.Fatalf("expected compaction model %q, got %q", "tool-model", gotModel)
	}
}

func TestEstimateContextCompactRequestBytesUsesInjectedEstimator(t *testing.T) {
	var seen llm.Request
	rc := &RunContext{
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{Model: "gpt-4o", ID: "route-1"},
		},
		ToolSpecs: []llm.ToolSpec{{
			Name:       "search",
			JSONSchema: map[string]any{"type": "object"},
		}},
		ReasoningMode: "medium",
	}
	rc.EstimateProviderRequestBytes = func(req llm.Request) (int, error) {
		seen = req
		return 4321, nil
	}

	got := estimateContextCompactRequestBytes(rc, "system prompt", []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "hello"}}},
	})
	if got != 4321 {
		t.Fatalf("expected injected estimator result, got %d", got)
	}
	if seen.Model != "gpt-4o" {
		t.Fatalf("expected selected route model, got %q", seen.Model)
	}
	if len(seen.Messages) != 2 || seen.Messages[0].Role != "system" {
		t.Fatalf("expected system prompt to be materialized into request messages, got %#v", seen.Messages)
	}
	if len(seen.Tools) != 1 || seen.Tools[0].Name != "search" {
		t.Fatalf("expected tool specs to be included, got %#v", seen.Tools)
	}
	if seen.ReasoningMode != "medium" {
		t.Fatalf("expected reasoning mode to be preserved, got %q", seen.ReasoningMode)
	}
}

func TestRoutingMiddlewareInjectsProviderRequestEstimator(t *testing.T) {
	router := routing.NewProviderRouter(routing.ProviderRoutingConfig{
		DefaultRouteID: "route-openai",
		Credentials: []routing.ProviderCredential{{
			ID:           "cred-openai",
			Name:         "openai",
			OwnerKind:    routing.CredentialScopePlatform,
			ProviderKind: routing.ProviderKindOpenAI,
			APIKeyValue:  compactPressureStringPtr("sk-test"),
		}},
		Routes: []routing.ProviderRouteRule{{
			ID:           "route-openai",
			Model:        "gpt-4o",
			CredentialID: "cred-openai",
			AdvancedJSON: map[string]any{
				"available_catalog": map[string]any{
					"context_length": float64(200000),
				},
			},
		}},
	})

	mw := NewRoutingMiddleware(
		router, nil, nil, false,
		data.RunsRepository{}, data.RunEventsRepository{},
		nil, nil,
	)

	rc := &RunContext{
		Emitter:             events.NewEmitter("test"),
		InputJSON:           map[string]any{},
		LlmMaxResponseBytes: 8192,
	}
	handler := Build([]RunMiddleware{mw}, func(_ context.Context, rc *RunContext) error {
		if rc.EstimateProviderRequestBytes == nil {
			t.Fatal("expected provider request estimator to be injected")
		}
		if rc.ContextWindowTokens != 200000 {
			t.Fatalf("expected context window tokens to be injected, got %d", rc.ContextWindowTokens)
		}
		request := llm.Request{
			Messages: []llm.Message{
				{Role: "system", Content: []llm.TextPart{{Text: "guardrails"}}},
				{Role: "user", Content: []llm.TextPart{{Text: "hello"}}},
			},
			Tools: []llm.ToolSpec{{
				Name:       "search",
				JSONSchema: map[string]any{"type": "object"},
			}},
		}
		resolved, err := ResolveGatewayConfigFromSelectedRoute(*rc.SelectedRoute, false, rc.LlmMaxResponseBytes)
		if err != nil {
			t.Fatalf("ResolveGatewayConfigFromSelectedRoute: %v", err)
		}
		want, err := llm.EstimateProviderPayloadBytes(resolved, request)
		if err != nil {
			t.Fatalf("EstimateProviderPayloadBytes: %v", err)
		}
		got, err := rc.EstimateProviderRequestBytes(request)
		if err != nil {
			t.Fatalf("RunContext estimator: %v", err)
		}
		if got != want {
			t.Fatalf("expected injected estimator bytes=%d, got %d", want, got)
		}
		return nil
	})

	if err := handler(context.Background(), rc); err != nil {
		t.Fatalf("routing middleware failed: %v", err)
	}
}

func TestContextCompactMiddlewareIgnoresPreviousRunAnchorAfterSyntheticPrefix(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "context_compact_previous_run_anchor_ignored")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	previousRunID := uuid.New()
	runID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, []any{accountID}},
		{`INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, []any{projectID, accountID}},
		{`INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, []any{threadID, accountID, projectID}},
		{`INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'completed'), ($4, $2, $3, 'running')`, []any{previousRunID, accountID, threadID, runID}},
		{`INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'llm.turn.completed', '{"last_real_prompt_tokens":114039,"last_request_context_estimate_tokens":17371}'::jsonb)`, []any{previousRunID}},
		{`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 1, 'user', 'short one', '{}'::jsonb, false)`, []any{msg1ID, accountID, threadID}},
		{`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 2, 'user', 'short two', '{}'::jsonb, false)`, []any{msg2ID, accountID, threadID}},
	} {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed data: %v", err)
		}
	}

	gateway := &compactSummaryGateway{summary: "summary"}
	rc := &RunContext{
		Run:     data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		Emitter: events.NewEmitter("trace"),
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			PersistTriggerContextPct:    85,
			TargetContextPct:            75,
			FallbackContextWindowTokens: 128000,
			PersistKeepLastMessages:     40,
		},
		Gateway: gateway,
		SelectedRoute: &routing.SelectedProviderRoute{
			Route:      routing.ProviderRouteRule{Model: "gpt-4o", ID: "route-1"},
			Credential: routing.ProviderCredential{ProviderKind: routing.ProviderKindOpenAI},
		},
		Messages: []llm.Message{
			makeThreadContextReplacementMessage("rolled summary"),
			{Role: "user", Content: []llm.TextPart{{Text: "short two"}}},
		},
		ThreadMessageIDs: []uuid.UUID{uuid.Nil, msg2ID},
	}

	mw := NewContextCompactMiddleware(pool, data.MessagesRepository{}, nil, gateway, false)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}
	if gateway.requests != nil {
		t.Fatalf("expected compact gateway to remain unused, got %d requests", len(gateway.requests))
	}
	if len(rc.Messages) != 2 || rc.Messages[0].Content[0].Text != "short one" || rc.Messages[1].Content[0].Text != "short two" {
		t.Fatalf("unexpected message rewrite: %#v", rc.Messages)
	}
}

func TestContextCompactPersistFailureDoesNotHideMessages(t *testing.T) {
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
			`INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, false)`,
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

	var hidden bool
	if err := pool.QueryRow(ctx,
		`SELECT hidden FROM messages WHERE id = $1`,
		msg3ID,
	).Scan(&hidden); err != nil {
		t.Fatalf("query message 3: %v", err)
	}
	if hidden {
		t.Fatalf("expected message 3 to stay visible after persist failure, hidden=%v", hidden)
	}

	var eventType, phase, op, errText string
	if err := pool.QueryRow(ctx,
		`SELECT type, data_json->>'phase', data_json->>'op', data_json->>'error'
		   FROM run_events
		  WHERE run_id = $1 AND type = 'run.context_compact' AND data_json->>'op' = 'persist'
		  ORDER BY seq DESC
		  LIMIT 1`,
		runID,
	).Scan(&eventType, &phase, &op, &errText); err != nil {
		t.Fatalf("query failure event: %v", err)
	}
	if eventType != "run.context_compact" || strings.TrimSpace(phase) == "" || op != "persist" {
		t.Fatalf("unexpected failure event: type=%s phase=%s op=%s", eventType, phase, op)
	}
	if strings.TrimSpace(errText) == "" {
		t.Fatal("expected failure event to include error text")
	}
}

func compactPressureStringPtr(v string) *string {
	return &v
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
			`INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, false)`,
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
		Messages:         []llm.Message{{Role: "user", Content: []llm.TextPart{{Text: "m1"}}}, {Role: "assistant", Content: []llm.TextPart{{Text: "m2"}}}, {Role: "user", Content: []llm.TextPart{{Text: "m3"}}}},
		ThreadMessageIDs: []uuid.UUID{msg1ID, msg2ID, msg3ID},
		HookRuntime:      NewHookRuntime(registry, NewDefaultHookResultApplier()),
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

func TestContextCompactMiddlewarePersistsReplacementFromThreadFrontier(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "context_compact_persist_from_frontier")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO accounts (id, type) VALUES ($1, 'personal')`,
		accountID,
	); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`,
		projectID, accountID,
	); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO threads (id, account_id, project_id, next_message_seq) VALUES ($1, $2, $3, 10)`,
		threadID, accountID, projectID,
	); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`,
		runID, accountID, threadID,
	); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	huge := strings.Repeat("alpha beta gamma delta\n\n", 180)
	msgs := []struct {
		id        uuid.UUID
		threadSeq int
		role      string
		content   string
	}{
		{id: uuid.New(), threadSeq: 1, role: "user", content: huge},
		{id: uuid.New(), threadSeq: 2, role: "assistant", content: "done"},
		{id: uuid.New(), threadSeq: 3, role: "user", content: "tail"},
	}
	for _, msg := range msgs {
		if _, err := pool.Exec(ctx,
			`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
			 VALUES ($1, $2, $3, $4, $5, $6, '{}'::jsonb, false)`,
			msg.id, accountID, threadID, msg.threadSeq, msg.role, msg.content,
		); err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	canonical, err := buildCanonicalThreadContext(
		ctx,
		tx,
		data.Run{AccountID: accountID, ThreadID: threadID},
		data.MessagesRepository{},
		nil,
		nil,
		0,
	)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("build canonical context: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Fatalf("rollback tx: %v", err)
	}
	if len(canonical.Frontier) == 0 {
		t.Fatal("expected canonical frontier nodes")
	}

	rc := &RunContext{
		Run:     data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		Emitter: events.NewEmitter("trace"),
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			PersistTriggerApproxTokens:  1,
			PersistTriggerContextPct:    0,
			TargetContextPct:            1,
			FallbackContextWindowTokens: 1_000_000,
			PersistKeepLastMessages:     1,
		},
		Gateway:               &compactSummaryGateway{summary: "persisted summary"},
		SelectedRoute:         &routing.SelectedProviderRoute{Route: routing.ProviderRouteRule{Model: "gpt-4o", ID: "route-1"}, Credential: routing.ProviderCredential{ProviderKind: routing.ProviderKindOpenAI}},
		Messages:              canonical.Messages,
		ThreadMessageIDs:      canonical.ThreadMessageIDs,
		ThreadContextFrontier: canonical.Frontier,
	}

	mw := NewContextCompactMiddleware(pool, data.MessagesRepository{}, nil, rc.Gateway, false)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}

	var replacementID uuid.UUID
	var summary string
	var startContextSeq int64
	var endContextSeq int64
	if err := pool.QueryRow(ctx,
		`SELECT id, summary_text, start_context_seq, end_context_seq
		   FROM thread_context_replacements
		  WHERE account_id = $1 AND thread_id = $2
		  ORDER BY created_at DESC
		  LIMIT 1`,
		accountID, threadID,
	).Scan(&replacementID, &summary, &startContextSeq, &endContextSeq); err != nil {
		t.Fatalf("query persisted replacement: %v", err)
	}
	if strings.TrimSpace(summary) != "persisted summary" {
		t.Fatalf("unexpected persisted summary: %q", summary)
	}
	if startContextSeq <= 0 || endContextSeq < startContextSeq {
		t.Fatalf("invalid persisted context range: start=%d end=%d", startContextSeq, endContextSeq)
	}

	var edgeCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM replacement_supersession_edges WHERE account_id = $1 AND thread_id = $2 AND replacement_id = $3`,
		accountID, threadID, replacementID,
	).Scan(&edgeCount); err != nil {
		t.Fatalf("query supersession edges: %v", err)
	}
	if edgeCount == 0 {
		t.Fatal("expected persisted replacement to supersede at least one frontier node")
	}
}

func TestContextCompactMiddlewarePersistsIteratively(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "context_compact_persist_iterative")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, projectID, accountID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id, next_message_seq) VALUES ($1, $2, $3, 20)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, runID, accountID, threadID); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	huge := strings.Repeat("x", 50_000)
	for i := 0; i < 9; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
			 VALUES ($1, $2, $3, $4, $5, $6, '{}'::jsonb, false)`,
			uuid.New(), accountID, threadID, i+1, role, huge,
		); err != nil {
			t.Fatalf("insert message %d: %v", i+1, err)
		}
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	canonical, err := buildCanonicalThreadContext(
		ctx,
		tx,
		data.Run{AccountID: accountID, ThreadID: threadID},
		data.MessagesRepository{},
		nil,
		nil,
		0,
	)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("build canonical context: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Fatalf("rollback tx: %v", err)
	}
	if len(canonical.Frontier) == 0 {
		t.Fatal("expected canonical frontier nodes")
	}

	gateway := &compactSummaryGateway{summary: "persisted summary"}
	rc := &RunContext{
		Run:     data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		Emitter: events.NewEmitter("trace"),
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			PersistTriggerContextPct:    80,
			TargetContextPct:            50,
			FallbackContextWindowTokens: 40_000,
			PersistKeepLastMessages:     1,
		},
		Gateway:               gateway,
		SelectedRoute:         &routing.SelectedProviderRoute{Route: routing.ProviderRouteRule{Model: "gpt-4o", ID: "route-1"}, Credential: routing.ProviderCredential{ProviderKind: routing.ProviderKindOpenAI}},
		Messages:              canonical.Messages,
		ThreadMessageIDs:      canonical.ThreadMessageIDs,
		ThreadContextFrontier: canonical.Frontier,
	}

	mw := NewContextCompactMiddleware(pool, data.MessagesRepository{}, nil, gateway, false)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}

	if len(gateway.requests) < 2 {
		t.Fatalf("expected iterative persist compaction, got %d requests", len(gateway.requests))
	}

	var replacementCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM thread_context_replacements WHERE account_id = $1 AND thread_id = $2`,
		accountID, threadID,
	).Scan(&replacementCount); err != nil {
		t.Fatalf("count replacements: %v", err)
	}
	if replacementCount < 2 {
		t.Fatalf("expected at least 2 persisted replacements, got %d", replacementCount)
	}
}
