//go:build desktop

package desktoprun

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"

	"github.com/google/uuid"
)

func TestTryAutoLoopResumeDesktopRunAfterRecoverableOutput(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openLifecycleTestDB(t, ctx)
	defer cleanup()

	accountID, _, threadID, runID := seedLifecycleRun(t, ctx, db)
	tailID := seedLifecycleUserMessage(t, ctx, db, accountID, threadID)
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type: "run.started",
		DataJSON: map[string]any{
			"thread_tail_message_id": tailID.String(),
			"persona_id":             "normal",
			"model":                  "cery^claude-opus-4-6",
			"reasoning_mode":         "enabled",
			"source":                 "retry",
			"trace_id":               "trace-resume",
		},
	})
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type: "message.delta",
		DataJSON: map[string]any{
			"trace_id":      "trace-resume",
			"role":          "assistant",
			"content_delta": "partial output",
		},
	})

	q := &lifecycleQueueStub{}
	handled, err := tryAutoLoopResumeDesktopRun(ctx, db, q, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, "trace-resume", errors.New("llm stream idle timeout"))
	if !handled {
		t.Fatal("expected auto loop resume to handle recoverable output")
	}
	if err != nil {
		t.Fatalf("unexpected auto loop resume error: %v", err)
	}
	if len(q.calls) != 1 {
		t.Fatalf("expected 1 queued resumed run, got %d", len(q.calls))
	}
	if q.calls[0].runID == runID {
		t.Fatal("expected resumed run id to differ from original run id")
	}
	if got, _ := q.calls[0].payload["source"].(string); got != autoLoopResumeSource {
		t.Fatalf("unexpected resumed payload: %#v", q.calls[0].payload)
	}

	var status string
	if err := db.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("query interrupted run status: %v", err)
	}
	if status != "interrupted" {
		t.Fatalf("expected interrupted status, got %q", status)
	}

	var (
		eventType string
		rawJSON   string
	)
	if err := db.QueryRow(ctx,
		`SELECT type, data_json FROM run_events WHERE run_id = $1 ORDER BY seq DESC LIMIT 1`,
		runID,
	).Scan(&eventType, &rawJSON); err != nil {
		t.Fatalf("query interrupted event: %v", err)
	}
	if eventType != "run.interrupted" {
		t.Fatalf("expected latest event run.interrupted, got %q", eventType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
		t.Fatalf("decode interrupted payload: %v", err)
	}
	details, _ := payload["details"].(map[string]any)
	if got, _ := details["recovery_mode"].(string); got != autoLoopResumeMode {
		t.Fatalf("unexpected recovery_mode: %#v", details)
	}
	if got, _ := details["resumed_run_id"].(string); got != q.calls[0].runID.String() {
		t.Fatalf("unexpected resumed_run_id: %#v", details["resumed_run_id"])
	}

	var resumeFromRunID string
	if err := db.QueryRow(ctx, `SELECT resume_from_run_id FROM runs WHERE id = $1`, q.calls[0].runID).Scan(&resumeFromRunID); err != nil {
		t.Fatalf("query resumed run lineage: %v", err)
	}
	if resumeFromRunID != runID.String() {
		t.Fatalf("unexpected resume_from_run_id: %q", resumeFromRunID)
	}

	var startedJSON string
	if err := db.QueryRow(ctx, `SELECT data_json FROM run_events WHERE run_id = $1 AND type = 'run.started' ORDER BY seq ASC LIMIT 1`, q.calls[0].runID).Scan(&startedJSON); err != nil {
		t.Fatalf("query resumed run.started: %v", err)
	}
	var startedData map[string]any
	if err := json.Unmarshal([]byte(startedJSON), &startedData); err != nil {
		t.Fatalf("decode resumed run.started: %v", err)
	}
	if got, _ := startedData["source"].(string); got != autoLoopResumeSource {
		t.Fatalf("unexpected source: %#v", startedData["source"])
	}
	if got, _ := startedData["continuation_source"].(string); got != data.DesktopRuntimeResumeSource {
		t.Fatalf("unexpected continuation_source: %#v", startedData["continuation_source"])
	}
	if got, _ := startedData["thread_tail_message_id"].(string); got != tailID.String() {
		t.Fatalf("unexpected thread_tail_message_id: %#v", startedData["thread_tail_message_id"])
	}
}

func TestTryAutoLoopResumeDesktopRunSkipsDeltaOnlyProgress(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openLifecycleTestDB(t, ctx)
	defer cleanup()

	accountID, _, threadID, runID := seedLifecycleRun(t, ctx, db)
	tailID := seedLifecycleUserMessage(t, ctx, db, accountID, threadID)
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type: "run.started",
		DataJSON: map[string]any{
			"thread_tail_message_id": tailID.String(),
			"trace_id":               "trace-delta-only",
		},
	})
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type: "tool.call.delta",
		DataJSON: map[string]any{
			"tool_name":       "show_widget",
			"arguments_delta": "{\"title\":\"partial",
			"trace_id":        "trace-delta-only",
		},
	})

	q := &lifecycleQueueStub{}
	handled, err := tryAutoLoopResumeDesktopRun(ctx, db, q, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, "trace-delta-only", errors.New("llm stream idle timeout"))
	if handled {
		t.Fatal("did not expect auto loop resume for delta-only progress")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q.calls) != 0 {
		t.Fatalf("expected no resumed run queued, got %d", len(q.calls))
	}
}

func TestLifecycleRecoverRunsAutoResumesRecoverableRun(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openLifecycleTestDB(t, ctx)
	defer cleanup()

	accountID, _, threadID, runID := seedLifecycleRun(t, ctx, db)
	tailID := seedLifecycleUserMessage(t, ctx, db, accountID, threadID)
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type:       "run.started",
		OccurredAt: time.Now().UTC().Add(-20 * time.Second),
		DataJSON: map[string]any{
			"thread_tail_message_id": tailID.String(),
			"trace_id":               "trace-recovery-auto",
		},
	})
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type:       "message.delta",
		OccurredAt: time.Now().UTC().Add(-10 * time.Second),
		DataJSON: map[string]any{
			"trace_id":      "trace-recovery-auto",
			"role":          "assistant",
			"content_delta": "visible output",
		},
	})

	q := &lifecycleQueueStub{}
	manager := newLifecycleManager(db, q, nil, nil)
	if err := manager.recoverRuns(ctx); err != nil {
		t.Fatalf("recover runs failed: %v", err)
	}

	if len(q.calls) != 1 {
		t.Fatalf("expected 1 resumed run queued, got %d", len(q.calls))
	}
	if q.calls[0].runID == runID {
		t.Fatal("expected lifecycle recovery to queue a new resumed run")
	}
	if got, _ := q.calls[0].payload["source"].(string); got != autoLoopResumeSource {
		t.Fatalf("unexpected recovery payload: %#v", q.calls[0].payload)
	}
}

func TestTryAutoLoopResumeDesktopRunMarksResumedRunFailedWhenEnqueueFails(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openLifecycleTestDB(t, ctx)
	defer cleanup()

	accountID, _, threadID, runID := seedLifecycleRun(t, ctx, db)
	tailID := seedLifecycleUserMessage(t, ctx, db, accountID, threadID)
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type: "run.started",
		DataJSON: map[string]any{
			"thread_tail_message_id": tailID.String(),
			"trace_id":               "trace-enqueue-fail",
		},
	})
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type: "tool.call",
		DataJSON: map[string]any{
			"tool_name":    "show_widget",
			"tool_call_id": "call-1",
			"arguments":    map[string]any{"title": "Widget"},
			"trace_id":     "trace-enqueue-fail",
		},
	})

	q := &lifecycleQueueStub{err: errors.New("enqueue failed")}
	handled, err := tryAutoLoopResumeDesktopRun(ctx, db, q, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, "trace-enqueue-fail", errors.New("llm stream idle timeout"))
	if !handled {
		t.Fatal("expected enqueue failure path to still handle the original run")
	}
	if err == nil {
		t.Fatal("expected enqueue failure to be returned for logging")
	}
	if len(q.calls) != 1 {
		t.Fatalf("expected resumed run enqueue attempt, got %d", len(q.calls))
	}

	var status string
	if err := db.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1`, q.calls[0].runID).Scan(&status); err != nil {
		t.Fatalf("query resumed run status: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected resumed run failed after enqueue error, got %q", status)
	}
}

func TestTryAutoLoopResumeDesktopRunSkipsCancelledError(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openLifecycleTestDB(t, ctx)
	defer cleanup()

	accountID, _, threadID, runID := seedLifecycleRun(t, ctx, db)
	tailID := seedLifecycleUserMessage(t, ctx, db, accountID, threadID)
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type: "run.started",
		DataJSON: map[string]any{
			"thread_tail_message_id": tailID.String(),
			"trace_id":               "trace-cancelled",
		},
	})
	appendLifecycleEvent(t, ctx, db, runID, events.RunEvent{
		Type: "message.delta",
		DataJSON: map[string]any{
			"trace_id":      "trace-cancelled",
			"role":          "assistant",
			"content_delta": "partial output",
		},
	})

	q := &lifecycleQueueStub{}
	handled, err := tryAutoLoopResumeDesktopRun(ctx, db, q, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, "trace-cancelled", context.Canceled)
	if handled {
		t.Fatal("did not expect context.Canceled to trigger auto loop resume")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func seedLifecycleUserMessage(t *testing.T, ctx context.Context, db data.DesktopDB, accountID, threadID uuid.UUID) uuid.UUID {
	t.Helper()

	messageID := uuid.New()
	if _, err := db.Exec(ctx,
		`INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden)
		 VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`,
		messageID,
		accountID,
		threadID,
		"hello",
	); err != nil {
		t.Fatalf("insert lifecycle user message: %v", err)
	}
	return messageID
}
