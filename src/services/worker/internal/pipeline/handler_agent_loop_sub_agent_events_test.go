//go:build !desktop

package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	creditpolicy "arkloop/services/shared/creditpolicy"
	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/testutil"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type stubJobQueue struct {
	enqueuedRunIDs []uuid.UUID
}

func (s *stubJobQueue) EnqueueRun(ctx context.Context, accountID uuid.UUID, runID uuid.UUID, traceID string, queueJobType string, payload map[string]any, availableAt *time.Time) (uuid.UUID, error) {
	s.enqueuedRunIDs = append(s.enqueuedRunIDs, runID)
	return uuid.New(), nil
}

func (s *stubJobQueue) Lease(context.Context, int, []string) (*queue.JobLease, error) {
	return nil, nil
}
func (s *stubJobQueue) Heartbeat(context.Context, queue.JobLease, int) error { return nil }
func (s *stubJobQueue) Ack(context.Context, queue.JobLease) error            { return nil }
func (s *stubJobQueue) Nack(context.Context, queue.JobLease, *int) error     { return nil }
func (s *stubJobQueue) QueueDepth(context.Context, []string) (int, error)    { return 0, nil }

type captureExecutor struct {
	called bool
}

func (e *captureExecutor) Execute(ctx context.Context, rc *RunContext, emitter events.Emitter, yield func(events.RunEvent) error) error {
	e.called = true
	return yield(emitter.Emit("run.completed", map[string]any{}, nil, nil))
}

type assistantMessageExecutor struct {
	message llm.Message
}

func (e assistantMessageExecutor) Execute(ctx context.Context, rc *RunContext, emitter events.Emitter, yield func(events.RunEvent) error) error {
	return yield(emitter.Emit("run.completed", llm.StreamRunCompleted{AssistantMessage: &e.message}.ToDataJSON(), nil, nil))
}

type staticExecutorBuilder struct {
	executor AgentExecutor
}

func (b staticExecutorBuilder) Build(string, map[string]any) (AgentExecutor, error) {
	return b.executor, nil
}

func TestEventWriterAppend_AppendsSubAgentCompletedEvent(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_sub_agent_completed_event")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	subAgentID := uuid.New()
	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	seedPipelineSubAgent(t, pool, subAgentID, accountID, threadID, runID)

	writer := newEventWriter(pool, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, "trace-1", nil, nil, nil, "", "", data.UsageRecordsRepository{}, data.CreditsRepository{}, 1000, 1, nil, nil, nil, nil, creditpolicy.DefaultPolicy, false, nil, nil, nil, "", nil, nil, false)
	ev := events.NewEmitter("trace-1").Emit("run.completed", map[string]any{}, nil, nil)
	if err := writer.Append(context.Background(), data.RunsRepository{}, data.RunEventsRepository{}, runID, ev); err != nil {
		t.Fatalf("append terminal event: %v", err)
	}
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatalf("flush events: %v", err)
	}

	eventsList, err := (data.SubAgentEventsRepository{}).ListBySubAgent(context.Background(), pool, subAgentID, 0, 10)
	if err != nil {
		t.Fatalf("list sub_agent_events: %v", err)
	}
	if len(eventsList) != 1 || eventsList[0].Type != data.SubAgentEventTypeCompleted {
		t.Fatalf("unexpected sub_agent_events: %#v", eventsList)
	}
}

func TestEventWriterAppend_AppendsSubAgentCancelledEventOnCancelRequest(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_sub_agent_cancelled_event")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	subAgentID := uuid.New()
	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	seedPipelineSubAgent(t, pool, subAgentID, accountID, threadID, runID)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := (data.RunEventsRepository{}).AppendEvent(context.Background(), tx, runID, "run.cancel_requested", map[string]any{}, nil, nil); err != nil {
		t.Fatalf("append cancel_requested: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit cancel_requested: %v", err)
	}

	writer := newEventWriter(pool, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, "trace-2", nil, nil, nil, "", "", data.UsageRecordsRepository{}, data.CreditsRepository{}, 1000, 1, nil, nil, nil, nil, creditpolicy.DefaultPolicy, false, nil, nil, nil, "", nil, nil, false)
	ev := events.NewEmitter("trace-2").Emit("message.delta", map[string]any{"content_delta": "ignored"}, nil, nil)
	if err := writer.Append(context.Background(), data.RunsRepository{}, data.RunEventsRepository{}, runID, ev); err != nil {
		t.Fatalf("append after cancel request: %v", err)
	}

	eventsList, err := (data.SubAgentEventsRepository{}).ListBySubAgent(context.Background(), pool, subAgentID, 0, 10)
	if err != nil {
		t.Fatalf("list sub_agent_events: %v", err)
	}
	if len(eventsList) != 1 || eventsList[0].Type != data.SubAgentEventTypeCancelled {
		t.Fatalf("unexpected sub_agent_events: %#v", eventsList)
	}
}

func TestEventWriterNotifiesVisibleTextDelta(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_visible_text_delta_notify")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)

	rc := &RunContext{}
	writer := newEventWriter(pool, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, "trace-visible-delta", nil, nil, nil, "", "", data.UsageRecordsRepository{}, data.CreditsRepository{}, 1000, 1, nil, nil, nil, nil, creditpolicy.DefaultPolicy, false, nil, nil, nil, "", nil, nil, false)
	defer writer.Close(context.Background())

	var visibleCalls int
	writer.telegramVisibleTextDelta = func(_ context.Context, role, channel, delta string) {
		if IsVisibleAssistantTextDelta(rc, role, channel, delta) {
			visibleCalls++
		}
	}
	emitter := events.NewEmitter("trace-visible-delta")

	if err := writer.Append(context.Background(), data.RunsRepository{}, data.RunEventsRepository{}, runID, emitter.Emit("message.delta", map[string]any{
		"role":          "assistant",
		"content_delta": "hello",
	}, nil, nil)); err != nil {
		t.Fatalf("append visible delta: %v", err)
	}
	if visibleCalls != 1 {
		t.Fatalf("expected visible callback once, got %d", visibleCalls)
	}

	if err := writer.Append(context.Background(), data.RunsRepository{}, data.RunEventsRepository{}, runID, emitter.Emit("message.delta", map[string]any{
		"role":          "assistant",
		"channel":       "thinking",
		"content_delta": "hidden",
	}, nil, nil)); err != nil {
		t.Fatalf("append thinking delta: %v", err)
	}
	if visibleCalls != 1 {
		t.Fatalf("thinking delta should not notify visible callback, got %d", visibleCalls)
	}

	rc.DiscussRun = true
	if err := writer.Append(context.Background(), data.RunsRepository{}, data.RunEventsRepository{}, runID, emitter.Emit("message.delta", map[string]any{
		"role":          "assistant",
		"content_delta": "private",
	}, nil, nil)); err != nil {
		t.Fatalf("append private discuss delta: %v", err)
	}
	if visibleCalls != 1 {
		t.Fatalf("private discuss delta should not notify visible callback, got %d", visibleCalls)
	}

	rc.DiscussTextVisible = true
	if err := writer.Append(context.Background(), data.RunsRepository{}, data.RunEventsRepository{}, runID, emitter.Emit("message.delta", map[string]any{
		"role":          "assistant",
		"content_delta": "public",
	}, nil, nil)); err != nil {
		t.Fatalf("append visible discuss delta: %v", err)
	}
	if visibleCalls != 2 {
		t.Fatalf("expected visible discuss delta to notify callback, got %d", visibleCalls)
	}
}

func TestEventWriterAppend_AutoQueuesNextRunFromPendingInputs(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_sub_agent_pending_autorun")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	parentThreadID := uuid.New()
	childThreadID := uuid.New()
	parentRunID := uuid.New()
	childRunID := uuid.New()
	subAgentID := uuid.New()
	seedPipelineThread(t, pool, accountID, parentThreadID, projectID)
	seedPipelineThread(t, pool, accountID, childThreadID, projectID)
	seedPipelineRun(t, pool, accountID, parentThreadID, parentRunID, nil)
	seedPipelineRun(t, pool, accountID, childThreadID, childRunID, &parentRunID)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO sub_agents (
			id, account_id, owner_thread_id, agent_thread_id, origin_run_id,
			depth, source_type, context_mode, status, current_run_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, subAgentID, accountID, parentThreadID, childThreadID, parentRunID, 1, data.SubAgentSourceTypeThreadSpawn, data.SubAgentContextModeIsolated, data.SubAgentStatusRunning, childRunID)
	if err != nil {
		t.Fatalf("insert sub_agent: %v", err)
	}
	_, err = pool.Exec(context.Background(), `INSERT INTO sub_agent_pending_inputs (sub_agent_id, input, priority) VALUES ($1, $2, $3), ($1, $4, $5)`, subAgentID, "phase two", false, "urgent", true)
	if err != nil {
		t.Fatalf("insert pending inputs: %v", err)
	}

	jobQueue := &stubJobQueue{}
	writer := newEventWriter(pool, data.Run{ID: childRunID, AccountID: accountID, ThreadID: childThreadID, ParentRunID: &parentRunID}, "trace-3", nil, nil, jobQueue, "", "", data.UsageRecordsRepository{}, data.CreditsRepository{}, 1000, 1, nil, nil, nil, nil, creditpolicy.DefaultPolicy, false, nil, nil, nil, "", nil, nil, false)
	ev := events.NewEmitter("trace-3").Emit("run.completed", map[string]any{}, nil, nil)
	if err := writer.Append(context.Background(), data.RunsRepository{}, data.RunEventsRepository{}, childRunID, ev); err != nil {
		t.Fatalf("append terminal event: %v", err)
	}
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatalf("flush events: %v", err)
	}
	if len(jobQueue.enqueuedRunIDs) != 1 {
		t.Fatalf("expected 1 auto-enqueued run, got %d", len(jobQueue.enqueuedRunIDs))
	}

	agent, err := (data.SubAgentRepository{}).ListByOwnerThread(context.Background(), pool, parentThreadID)
	if err != nil {
		t.Fatalf("list sub_agents: %v", err)
	}
	if len(agent) != 1 || agent[0].CurrentRunID == nil || *agent[0].CurrentRunID != jobQueue.enqueuedRunIDs[0] {
		t.Fatalf("unexpected projected sub_agent: %#v", agent)
	}
	var merged string
	if err := pool.QueryRow(context.Background(), `
		SELECT content FROM messages
		 WHERE thread_id = $1 AND role = 'user'
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`, childThreadID).Scan(&merged); err != nil {
		t.Fatalf("load merged input: %v", err)
	}
	if merged != "urgent\n\nphase two" {
		t.Fatalf("unexpected merged input: %q", merged)
	}
}

func TestEventWriterAppend_TouchesRunActivityOnNonTerminalCommit(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_run_activity_touch")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)

	oldActivity := time.Date(2000, time.January, 2, 3, 4, 5, 0, time.UTC)
	if _, err := pool.Exec(context.Background(), `UPDATE runs SET status_updated_at = $2 WHERE id = $1`, runID, oldActivity); err != nil {
		t.Fatalf("set old activity: %v", err)
	}

	writer := newEventWriter(pool, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, "trace-activity", nil, nil, nil, "", "", data.UsageRecordsRepository{}, data.CreditsRepository{}, 1000, 1, nil, nil, nil, nil, creditpolicy.DefaultPolicy, false, nil, nil, nil, "", nil, nil, false)
	ev := events.NewEmitter("trace-activity").Emit("llm.turn.completed", map[string]any{
		"usage": map[string]any{
			"input_tokens":  3,
			"output_tokens": 2,
		},
	}, nil, nil)
	if err := writer.Append(context.Background(), data.RunsRepository{}, data.RunEventsRepository{}, runID, ev); err != nil {
		t.Fatalf("append non-terminal event: %v", err)
	}
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatalf("flush writer: %v", err)
	}

	var (
		status  string
		touched bool
	)
	if err := pool.QueryRow(
		context.Background(),
		`SELECT status, status_updated_at > $2
		   FROM runs
		  WHERE id = $1`,
		runID,
		oldActivity,
	).Scan(&status, &touched); err != nil {
		t.Fatalf("query run activity: %v", err)
	}
	if status != "running" {
		t.Fatalf("expected run to stay running, got %q", status)
	}
	if !touched {
		t.Fatal("expected status_updated_at to refresh on non-terminal commit")
	}
}

func TestEventWriterAppend_ConsumesPendingCallbacksOnCompletedRun(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagent_pending_callback_consume")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	callbackID := uuid.New()
	subAgentID := uuid.New()
	sourceRunID := uuid.New()
	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO thread_subagent_callbacks (
			id, account_id, thread_id, sub_agent_id, source_run_id, status, payload_json
		) VALUES ($1, $2, $3, $4, $5, $6, '{"status":"completed"}'::jsonb)
	`, callbackID, accountID, threadID, subAgentID, sourceRunID, data.SubAgentStatusCompleted); err != nil {
		t.Fatalf("insert callback: %v", err)
	}

	writer := newEventWriter(pool, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, "trace-consume", nil, nil, nil, "", "", data.UsageRecordsRepository{}, data.CreditsRepository{}, 1000, 1, nil, nil, nil, nil, creditpolicy.DefaultPolicy, false, nil, nil, nil, "", nil, []uuid.UUID{callbackID}, false)
	ev := events.NewEmitter("trace-consume").Emit("run.completed", map[string]any{}, nil, nil)
	if err := writer.Append(context.Background(), data.RunsRepository{}, data.RunEventsRepository{}, runID, ev); err != nil {
		t.Fatalf("append terminal event: %v", err)
	}
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatalf("flush writer: %v", err)
	}

	var consumedByRunID *uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT consumed_by_run_id
		   FROM thread_subagent_callbacks
		  WHERE id = $1`,
		callbackID,
	).Scan(&consumedByRunID); err != nil {
		t.Fatalf("load callback consumption: %v", err)
	}
	if consumedByRunID == nil || *consumedByRunID != runID {
		t.Fatalf("unexpected consumed_by_run_id: %#v", consumedByRunID)
	}
}

func TestEventWriterAppend_KeepsPendingCallbacksOnFailedRun(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagent_pending_callback_retry")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	callbackID := uuid.New()
	subAgentID := uuid.New()
	sourceRunID := uuid.New()
	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO thread_subagent_callbacks (
			id, account_id, thread_id, sub_agent_id, source_run_id, status, payload_json
		) VALUES ($1, $2, $3, $4, $5, $6, '{"status":"completed"}'::jsonb)
	`, callbackID, accountID, threadID, subAgentID, sourceRunID, data.SubAgentStatusCompleted); err != nil {
		t.Fatalf("insert callback: %v", err)
	}

	writer := newEventWriter(pool, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, "trace-failed", nil, nil, nil, "", "", data.UsageRecordsRepository{}, data.CreditsRepository{}, 1000, 1, nil, nil, nil, nil, creditpolicy.DefaultPolicy, false, nil, nil, nil, "", nil, []uuid.UUID{callbackID}, false)
	ev := events.NewEmitter("trace-failed").Emit("run.failed", map[string]any{"message": "boom"}, nil, nil)
	if err := writer.Append(context.Background(), data.RunsRepository{}, data.RunEventsRepository{}, runID, ev); err != nil {
		t.Fatalf("append terminal event: %v", err)
	}
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatalf("flush writer: %v", err)
	}

	var consumedByRunID *uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT consumed_by_run_id
		   FROM thread_subagent_callbacks
		  WHERE id = $1`,
		callbackID,
	).Scan(&consumedByRunID); err != nil {
		t.Fatalf("load callback consumption: %v", err)
	}
	if consumedByRunID != nil {
		t.Fatalf("expected callback to stay pending, got consumed_by_run_id=%s", consumedByRunID.String())
	}
}

func TestSubAgentCallbackMiddlewareFiltersToCurrentCallback(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagent_callback_filter_current")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	callbackAID := uuid.New()
	callbackBID := uuid.New()
	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO thread_subagent_callbacks (
			id, account_id, thread_id, sub_agent_id, source_run_id, status, payload_json
		) VALUES
			($1, $2, $3, $4, $5, $6, '{"message":"first"}'::jsonb),
			($7, $2, $3, $8, $5, $6, '{"message":"second"}'::jsonb)
	`, callbackAID, accountID, threadID, uuid.New(), runID, data.SubAgentStatusCompleted, callbackBID, uuid.New()); err != nil {
		t.Fatalf("insert callbacks: %v", err)
	}

	rc := &RunContext{
		Run:       data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		Pool:      pool,
		InputJSON: map[string]any{"run_kind": runkind.SubagentCallback, "callback_id": callbackBID.String()},
	}
	handler := Build([]RunMiddleware{NewSubAgentCallbackMiddleware()}, func(_ context.Context, rc *RunContext) error {
		if len(rc.PendingSubAgentCallbacks) != 1 || rc.PendingSubAgentCallbacks[0].ID != callbackBID {
			t.Fatalf("unexpected visible callbacks: %#v", rc.PendingSubAgentCallbacks)
		}
		if rc.InputJSON[staleSubAgentCallbackRunKey] != nil {
			t.Fatalf("unexpected stale flag: %#v", rc.InputJSON[staleSubAgentCallbackRunKey])
		}
		return nil
	})
	if err := handler(context.Background(), rc); err != nil {
		t.Fatalf("run middleware: %v", err)
	}
}

func TestAgentLoopHandlerShortCircuitsStaleCallbackRun(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_stale_callback_run_short_circuit")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	callbackID := uuid.New()
	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)

	executor := &captureExecutor{}
	handler := NewAgentLoopHandler(data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, &stubJobQueue{}, data.UsageRecordsRepository{}, data.CreditsRepository{}, nil)
	rc := &RunContext{
		Run:       data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		Pool:      pool,
		InputJSON: map[string]any{"run_kind": runkind.SubagentCallback, "callback_id": callbackID.String(), staleSubAgentCallbackRunKey: true},
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{ID: "route-main", Model: "gpt-test", Multiplier: 1},
		},
		Emitter:         events.NewEmitter("trace-stale"),
		ToolExecutors:   map[string]tools.Executor{},
		CreditPerUSD:    1000,
		ExecutorBuilder: staticExecutorBuilder{executor: executor},
	}

	if err := handler(context.Background(), rc); err != nil {
		t.Fatalf("run handler: %v", err)
	}
	if executor.called {
		t.Fatal("expected stale callback run to skip executor")
	}
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM runs WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("query run status: %v", err)
	}
	if status != "completed" {
		t.Fatalf("unexpected run status: %s", status)
	}
}

func TestAgentLoopHandlerPersistsStickerPlaceholderForHistory(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "arkloop_sticker_placeholder_history")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)

	handler := NewAgentLoopHandler(data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, &stubJobQueue{}, data.UsageRecordsRepository{}, data.CreditsRepository{}, nil)
	rc := &RunContext{
		Run:       data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		Pool:      pool,
		InputJSON: map[string]any{},
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{ID: "route-main", Model: "gpt-test", Multiplier: 1},
		},
		Emitter:       events.NewEmitter("trace-sticker"),
		ToolExecutors: map[string]tools.Executor{},
		CreditPerUSD:  1000,
		ExecutorBuilder: staticExecutorBuilder{executor: assistantMessageExecutor{message: llm.Message{
			Role:    "assistant",
			Content: []llm.ContentPart{{Type: "text", Text: "[sticker:hash]"}},
		}}},
	}

	if err := handler(ctx, rc); err != nil {
		t.Fatalf("run handler: %v", err)
	}
	if len(rc.ChannelDeliverySegments) != 1 || rc.ChannelDeliverySegments[0].Kind != "sticker" || rc.ChannelDeliverySegments[0].StickerID != "hash" {
		t.Fatalf("unexpected sticker delivery segments: %#v", rc.ChannelDeliverySegments)
	}

	var stored data.ThreadMessage
	if err := pool.QueryRow(ctx,
		`SELECT id, role, content, content_json
		   FROM messages
		  WHERE thread_id = $1 AND role = 'assistant' AND hidden = FALSE
		  ORDER BY thread_seq DESC
		  LIMIT 1`,
		threadID,
	).Scan(&stored.ID, &stored.Role, &stored.Content, &stored.ContentJSON); err != nil {
		t.Fatalf("select persisted assistant: %v", err)
	}
	if stored.Content != "[sticker:hash]" {
		t.Fatalf("expected persisted sticker placeholder, got %q", stored.Content)
	}
	if !strings.Contains(string(stored.ContentJSON), "[sticker:hash]") {
		t.Fatalf("expected content_json to preserve sticker placeholder, got %s", string(stored.ContentJSON))
	}
	parts, err := BuildMessageParts(ctx, nil, stored)
	if err != nil {
		t.Fatalf("BuildMessageParts: %v", err)
	}
	if len(parts) != 1 || parts[0].Text != "[sticker:hash]" {
		t.Fatalf("expected sticker placeholder in reconstructed history, got %#v", parts)
	}
}

func TestEventWriterFlushRewakesPendingCallbackAfterCallbackRunCompletes(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_callback_run_rewake_pending")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	callbackID := uuid.New()
	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO run_events (run_id, seq, type, data_json)
		 VALUES ($1, 1, 'run.started', '{"persona_id":"callback-persona@1"}'::jsonb)`,
		runID,
	); err != nil {
		t.Fatalf("insert callback run.started: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO thread_subagent_callbacks (
			id, account_id, thread_id, sub_agent_id, source_run_id, status, payload_json
		) VALUES ($1, $2, $3, $4, $5, $6, '{"message":"late callback"}'::jsonb)
	`, callbackID, accountID, threadID, uuid.New(), uuid.New(), data.SubAgentStatusCompleted); err != nil {
		t.Fatalf("insert pending callback: %v", err)
	}

	jobQueue := &stubJobQueue{}
	writer := newEventWriter(pool, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, "trace-rewake", nil, nil, jobQueue, "", "", data.UsageRecordsRepository{}, data.CreditsRepository{}, 1000, 1, nil, nil, nil, nil, creditpolicy.DefaultPolicy, false, nil, nil, nil, runkind.SubagentCallback, uuidPtr(callbackID), nil, false)
	ev := events.NewEmitter("trace-rewake").Emit("run.completed", map[string]any{}, nil, nil)
	if err := writer.Append(context.Background(), data.RunsRepository{}, data.RunEventsRepository{}, runID, ev); err != nil {
		t.Fatalf("append completed: %v", err)
	}
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatalf("flush writer: %v", err)
	}
	if len(jobQueue.enqueuedRunIDs) != 1 {
		t.Fatalf("expected one rewoken callback run, got %d", len(jobQueue.enqueuedRunIDs))
	}
}

func seedPipelineThread(t *testing.T, pool *pgxpool.Pool, accountID, threadID, projectID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID)
	if err != nil {
		t.Fatalf("insert thread: %v", err)
	}
}

func seedPipelineRun(t *testing.T, pool *pgxpool.Pool, accountID, threadID, runID uuid.UUID, parentRunID *uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `INSERT INTO runs (id, account_id, thread_id, parent_run_id, status) VALUES ($1, $2, $3, $4, 'running')`, runID, accountID, threadID, parentRunID)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
}

func seedPipelineSubAgent(t *testing.T, pool *pgxpool.Pool, subAgentID, accountID, threadID, runID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO sub_agents (
			id, account_id, owner_thread_id, agent_thread_id, origin_run_id,
			depth, source_type, context_mode, status, current_run_id
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10
		)
	`, subAgentID, accountID, threadID, threadID, runID, 1, data.SubAgentSourceTypeThreadSpawn, data.SubAgentContextModeIsolated, data.SubAgentStatusRunning, runID)
	if err != nil {
		t.Fatalf("insert sub_agent: %v", err)
	}
}
