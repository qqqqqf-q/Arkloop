//go:build !desktop

package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/rollout"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLoadRunInputsIncludesRoleFromFirstEvent(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_role")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, runID, accountID, threadID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{"persona_id":"p1","role":"worker"}'::jsonb)`, runID); err != nil {
		t.Fatalf("insert run event: %v", err)
	}

	loaded, err := loadRunInputs(context.Background(), pool, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.Messages) != 0 {
		t.Fatalf("expected no messages, got %d", len(loaded.Messages))
	}
	if got := loaded.InputJSON["role"]; got != "worker" {
		t.Fatalf("unexpected role: %#v", got)
	}
	if got := loaded.InputJSON["persona_id"]; got != "p1" {
		t.Fatalf("unexpected persona_id: %#v", got)
	}
}

func TestLoadRunInputsBoundsFreshChannelHistoryAtThreadTail(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_bounded_channel_history")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	hiddenID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()
	msg3ID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, projectID, accountID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, runID, accountID, threadID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`,
		runID,
		fmt.Sprintf(`{"thread_tail_message_id":"%s","channel_delivery":{"channel_id":"%s"}}`, msg2ID, uuid.New()),
	); err != nil {
		t.Fatalf("insert run event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden, compacted) VALUES ($1, $2, $3, 1, 'assistant', 'hidden', '{}'::jsonb, true, true)`, hiddenID, accountID, threadID); err != nil {
		t.Fatalf("insert hidden message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 2, 'user', 'one', '{}'::jsonb, false)`, msg1ID, accountID, threadID); err != nil {
		t.Fatalf("insert message one: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 3, 'user', 'two', '{}'::jsonb, false)`, msg2ID, accountID, threadID); err != nil {
		t.Fatalf("insert message two: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 4, 'assistant', 'future assistant', '{}'::jsonb, false)`, msg3ID, accountID, threadID); err != nil {
		t.Fatalf("insert future assistant: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO thread_compaction_snapshots (account_id, thread_id, summary_text, metadata_json, is_active) VALUES ($1, $2, 'future summary', '{}'::jsonb, true)`, accountID, threadID); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("expected 3 prompt messages, got %d", len(loaded.Messages))
	}
	if !loaded.HasActiveCompactSnapshot || loaded.ActiveCompactSnapshotText != "future summary" {
		t.Fatalf("expected snapshot prefix, got has=%v text=%q", loaded.HasActiveCompactSnapshot, loaded.ActiveCompactSnapshotText)
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != formatCompactSnapshotText("future summary") {
		t.Fatalf("unexpected snapshot message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "user" || loaded.Messages[1].Content[0].Text != "one" {
		t.Fatalf("unexpected bounded message one: %#v", loaded.Messages[1])
	}
	if loaded.Messages[2].Role != "user" || loaded.Messages[2].Content[0].Text != "two" {
		t.Fatalf("unexpected bounded message two: %#v", loaded.Messages[2])
	}
	if len(loaded.ThreadMessageIDs) != 3 || loaded.ThreadMessageIDs[0] != uuid.Nil || loaded.ThreadMessageIDs[1] != msg1ID || loaded.ThreadMessageIDs[2] != msg2ID {
		t.Fatalf("unexpected bounded thread ids: %#v", loaded.ThreadMessageIDs)
	}
}

func TestLoadRunInputsBoundsChannelHistoryWithReplacementPrefix(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_bounded_replacement")
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

	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, projectID, accountID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, runID, accountID, threadID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`,
		runID,
		fmt.Sprintf(`{"thread_tail_message_id":"%s","channel_delivery":{"channel_id":"%s"}}`, msg2ID, uuid.New()),
	); err != nil {
		t.Fatalf("insert run event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 1, 'user', 'one', '{}'::jsonb, false)`, msg1ID, accountID, threadID); err != nil {
		t.Fatalf("insert message one: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 2, 'user', 'two', '{}'::jsonb, false)`, msg2ID, accountID, threadID); err != nil {
		t.Fatalf("insert message two: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO thread_context_replacements (account_id, thread_id, start_thread_seq, end_thread_seq, summary_text, layer, metadata_json) VALUES ($1, $2, 1, 1, 'rolled summary', 1, '{}'::jsonb)`, accountID, threadID); err != nil {
		t.Fatalf("insert replacement: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if !loaded.HasActiveCompactSnapshot {
		t.Fatal("expected bounded channel run to include replacement prefix")
	}
	if loaded.ActiveCompactSnapshotText != "rolled summary" {
		t.Fatalf("unexpected summary text: %q", loaded.ActiveCompactSnapshotText)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected 2 prompt messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != formatCompactSnapshotText("rolled summary") {
		t.Fatalf("unexpected replacement prompt message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "user" || loaded.Messages[1].Content[0].Text != "two" {
		t.Fatalf("unexpected bounded tail message: %#v", loaded.Messages[1])
	}
}

func TestLoadRunInputsReplaysInterruptedRunBeforeTrailingUserInput(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_resume")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	parentRunID := uuid.New()
	resumedRunID := uuid.New()
	firstMessageID := uuid.New()
	continueMessageID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'interrupted')`, parentRunID, accountID, threadID); err != nil {
		t.Fatalf("insert parent run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status, resume_from_run_id) VALUES ($1, $2, $3, 'running', $4)`, resumedRunID, accountID, threadID, parentRunID); err != nil {
		t.Fatalf("insert resumed run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, parentRunID, `{"thread_tail_message_id":"`+firstMessageID.String()+`"}`); err != nil {
		t.Fatalf("insert parent run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{}'::jsonb)`, resumedRunID); err != nil {
		t.Fatalf("insert resumed run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, firstMessageID, accountID, threadID, "find the file"); err != nil {
		t.Fatalf("insert first user message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, continueMessageID, accountID, threadID, "continue"); err != nil {
		t.Fatalf("insert trailing user message: %v", err)
	}

	storeRoot := t.TempDir()
	store, err := objectstore.NewFilesystemOpener(storeRoot).Open(ctx, objectstore.RolloutBucket)
	if err != nil {
		t.Fatalf("open rollout store: %v", err)
	}
	blobStore, ok := store.(objectstore.BlobStore)
	if !ok {
		t.Fatal("expected blob store")
	}

	toolCallsJSON, err := json.Marshal([]llm.ToolCall{{
		ToolCallID:    "call_1",
		ToolName:      "echo",
		ArgumentsJSON: map[string]any{"text": "hi"},
	}})
	if err != nil {
		t.Fatalf("marshal tool calls: %v", err)
	}
	toolInputJSON, err := json.Marshal(map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("marshal tool input: %v", err)
	}
	rolloutBody := marshalRolloutJSONL(t,
		map[string]any{"type": "turn_start", "payload": map[string]any{"turn_index": 1, "model": "stub"}},
		map[string]any{"type": "assistant_message", "payload": map[string]any{"content": "I am checking", "tool_calls": json.RawMessage(toolCallsJSON)}},
		map[string]any{"type": "tool_call", "payload": map[string]any{"call_id": "call_1", "name": "echo", "input": json.RawMessage(toolInputJSON)}},
		map[string]any{"type": "run_end", "payload": map[string]any{"final_status": "interrupted"}},
	)
	if err := blobStore.Put(ctx, "run/"+parentRunID.String()+".jsonl", rolloutBody); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:              resumedRunID,
		AccountID:       accountID,
		ThreadID:        threadID,
		ResumeFromRunID: &parentRunID,
	}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, blobStore, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if got := loaded.InputJSON["last_user_message"]; got != "continue" {
		t.Fatalf("unexpected last_user_message: %#v", got)
	}
	if len(loaded.Messages) != 4 {
		t.Fatalf("expected 4 prompt messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != "find the file" {
		t.Fatalf("unexpected first message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "assistant" || loaded.Messages[1].Content[0].Text != "I am checking" {
		t.Fatalf("unexpected replayed assistant message: %#v", loaded.Messages[1])
	}
	if len(loaded.Messages[1].ToolCalls) != 1 || loaded.Messages[1].ToolCalls[0].ToolCallID != "call_1" {
		t.Fatalf("unexpected replayed tool calls: %#v", loaded.Messages[1].ToolCalls)
	}
	if loaded.Messages[2].Role != "tool" {
		t.Fatalf("unexpected replayed tool result role: %#v", loaded.Messages[2])
	}
	var toolEnvelope map[string]any
	if err := json.Unmarshal([]byte(loaded.Messages[2].Content[0].Text), &toolEnvelope); err != nil {
		t.Fatalf("decode replayed tool result: %v", err)
	}
	if toolEnvelope["tool_call_id"] != "call_1" {
		t.Fatalf("unexpected replayed tool_call_id: %#v", toolEnvelope["tool_call_id"])
	}
	errorPayload, ok := toolEnvelope["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthetic tool error payload, got %#v", toolEnvelope["error"])
	}
	if errorPayload["error_class"] != interruptedToolErrorClass {
		t.Fatalf("unexpected synthetic tool error class: %#v", errorPayload["error_class"])
	}
	if loaded.Messages[3].Role != "user" || loaded.Messages[3].Content[0].Text != "continue" {
		t.Fatalf("unexpected trailing user message: %#v", loaded.Messages[3])
	}
	if len(loaded.ThreadMessageIDs) != 4 {
		t.Fatalf("expected 4 thread message ids, got %d", len(loaded.ThreadMessageIDs))
	}
	if loaded.ThreadMessageIDs[1] != uuid.Nil || loaded.ThreadMessageIDs[2] != uuid.Nil {
		t.Fatalf("expected replayed entries to use nil ids, got %#v", loaded.ThreadMessageIDs)
	}
	if loaded.ThreadMessageIDs[0] != firstMessageID || loaded.ThreadMessageIDs[3] != continueMessageID {
		t.Fatalf("unexpected preserved thread message ids: %#v", loaded.ThreadMessageIDs)
	}
}

func TestLoadRunInputsReplayCanonicalizesProviderToolNames(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_provider_name_replay")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	parentRunID := uuid.New()
	resumedRunID := uuid.New()
	firstMessageID := uuid.New()
	continueMessageID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'interrupted')`, parentRunID, accountID, threadID); err != nil {
		t.Fatalf("insert parent run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status, resume_from_run_id) VALUES ($1, $2, $3, 'running', $4)`, resumedRunID, accountID, threadID, parentRunID); err != nil {
		t.Fatalf("insert resumed run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, parentRunID, `{"thread_tail_message_id":"`+firstMessageID.String()+`"}`); err != nil {
		t.Fatalf("insert parent run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{}'::jsonb)`, resumedRunID); err != nil {
		t.Fatalf("insert resumed run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, firstMessageID, accountID, threadID, "fetch the page"); err != nil {
		t.Fatalf("insert first user message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, continueMessageID, accountID, threadID, "continue"); err != nil {
		t.Fatalf("insert trailing user message: %v", err)
	}

	storeRoot := t.TempDir()
	store, err := objectstore.NewFilesystemOpener(storeRoot).Open(ctx, objectstore.RolloutBucket)
	if err != nil {
		t.Fatalf("open rollout store: %v", err)
	}
	blobStore, ok := store.(objectstore.BlobStore)
	if !ok {
		t.Fatal("expected blob store")
	}

	toolCallsJSON, err := json.Marshal([]llm.ToolCall{{
		ToolCallID:    "call_1",
		ToolName:      "web_fetch.jina",
		ArgumentsJSON: map[string]any{"url": "https://example.com"},
	}})
	if err != nil {
		t.Fatalf("marshal tool calls: %v", err)
	}
	toolInputJSON, err := json.Marshal(map[string]any{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("marshal tool input: %v", err)
	}
	rolloutBody := marshalRolloutJSONL(t,
		map[string]any{"type": "turn_start", "payload": map[string]any{"turn_index": 1, "model": "stub"}},
		map[string]any{"type": "assistant_message", "payload": map[string]any{"content": "I am fetching", "tool_calls": json.RawMessage(toolCallsJSON)}},
		map[string]any{"type": "tool_call", "payload": map[string]any{"call_id": "call_1", "name": "web_fetch.jina", "input": json.RawMessage(toolInputJSON)}},
		map[string]any{"type": "run_end", "payload": map[string]any{"final_status": "interrupted"}},
	)
	if err := blobStore.Put(ctx, "run/"+parentRunID.String()+".jsonl", rolloutBody); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:              resumedRunID,
		AccountID:       accountID,
		ThreadID:        threadID,
		ResumeFromRunID: &parentRunID,
	}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, blobStore, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.Messages) != 4 {
		t.Fatalf("expected 4 prompt messages, got %d", len(loaded.Messages))
	}
	if len(loaded.Messages[1].ToolCalls) != 1 {
		t.Fatalf("expected replayed assistant tool call, got %#v", loaded.Messages[1].ToolCalls)
	}
	if got := loaded.Messages[1].ToolCalls[0].ToolName; got != "web_fetch" {
		t.Fatalf("expected replayed assistant tool call to use canonical name, got %q", got)
	}
	toolText := loaded.Messages[2].Content[0].Text
	if strings.Contains(toolText, "web_fetch.jina") {
		t.Fatalf("expected replayed tool message to hide provider tool name, got %s", toolText)
	}
	if !strings.Contains(toolText, `"tool_name":"web_fetch"`) {
		t.Fatalf("expected replayed tool message to keep canonical tool name, got %s", toolText)
	}
}

func TestLoadRunInputsFiltersHeartbeatDecisionFromPersistentHistory(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_filter_heartbeat_history")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	userMessageID := uuid.New()
	assistantMessageID := uuid.New()
	heartbeatResultID := uuid.New()
	searchResultID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, runID, accountID, threadID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{}'::jsonb)`, runID); err != nil {
		t.Fatalf("insert run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, userMessageID, accountID, threadID, "hi"); err != nil {
		t.Fatalf("insert user message: %v", err)
	}

	assistantContentJSON, err := json.Marshal(map[string]any{
		"parts": []map[string]any{{"type": "text", "text": "checking"}},
		"tool_calls": []map[string]any{
			{"tool_call_id": "hb_1", "tool_name": "heartbeat_decision", "arguments": map[string]any{"reply": true}},
			{"tool_call_id": "web_1", "tool_name": "web_search", "arguments": map[string]any{"query": "test"}},
		},
	})
	if err != nil {
		t.Fatalf("marshal assistant content json: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, content_json, metadata_json, hidden) VALUES ($1, $2, $3, 'assistant', $4, $5::jsonb, '{}'::jsonb, true)`, assistantMessageID, accountID, threadID, "checking", string(assistantContentJSON)); err != nil {
		t.Fatalf("insert assistant message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'tool', $4, '{}'::jsonb, true)`, heartbeatResultID, accountID, threadID, `{"tool_call_id":"hb_1","tool_name":"heartbeat_decision","result":{"ok":true,"reply":true}}`); err != nil {
		t.Fatalf("insert heartbeat result: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'tool', $4, '{}'::jsonb, true)`, searchResultID, accountID, threadID, `{"tool_call_id":"web_1","tool_name":"web_search","result":{"ok":true}}`); err != nil {
		t.Fatalf("insert search result: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("expected user + assistant + tool, got %d", len(loaded.Messages))
	}
	if len(loaded.Messages[1].ToolCalls) != 1 || loaded.Messages[1].ToolCalls[0].ToolName != "web_search" {
		t.Fatalf("expected heartbeat_decision tool call to be removed, got %#v", loaded.Messages[1].ToolCalls)
	}
	if loaded.Messages[2].Role != "tool" || strings.Contains(loaded.Messages[2].Content[0].Text, "heartbeat_decision") {
		t.Fatalf("expected only non-heartbeat tool result to remain, got %#v", loaded.Messages[2])
	}
}

func TestBuildReplayMessagesFiltersHeartbeatDecisionForNonHeartbeatRun(t *testing.T) {
	toolCallsJSON, err := json.Marshal([]llm.ToolCall{
		{ToolCallID: "hb_1", ToolName: "heartbeat_decision", ArgumentsJSON: map[string]any{"reply": true}},
		{ToolCallID: "web_1", ToolName: "web_search", ArgumentsJSON: map[string]any{"query": "test"}},
	})
	if err != nil {
		t.Fatalf("marshal tool calls: %v", err)
	}
	state := &rollout.ReconstructedState{
		ReplayMessages: []rollout.ReplayMessage{
			{
				Role: "assistant",
				Assistant: &rollout.AssistantMessage{
					Content:   "checking",
					ToolCalls: toolCallsJSON,
				},
			},
			{
				Role: "tool",
				Tool: &rollout.ReplayToolResult{
					CallID: "hb_1",
					Name:   "heartbeat_decision",
					Output: json.RawMessage(`{"ok":true,"reply":true}`),
				},
			},
			{
				Role: "tool",
				Tool: &rollout.ReplayToolResult{
					CallID: "web_1",
					Name:   "web_search",
					Output: json.RawMessage(`{"ok":true}`),
				},
			},
		},
	}

	messages, err := buildReplayMessages(state)
	if err != nil {
		t.Fatalf("buildReplayMessages failed: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected assistant + remaining tool result, got %d", len(messages))
	}
	if len(messages[0].ToolCalls) != 1 || messages[0].ToolCalls[0].ToolName != "web_search" {
		t.Fatalf("expected replayed assistant to keep only web_search, got %#v", messages[0].ToolCalls)
	}
	if messages[1].Role != "tool" || strings.Contains(messages[1].Content[0].Text, "heartbeat_decision") {
		t.Fatalf("expected heartbeat_decision replay tool result to be removed, got %#v", messages[1])
	}
}

// Heartbeat decision artifacts are control-plane and should never affect canonical history.

func TestLoadRunInputsPrependsActiveCompactSnapshotBeforeResumeReplay(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_snapshot_resume")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	parentRunID := uuid.New()
	resumedRunID := uuid.New()
	firstMessageID := uuid.New()
	continueMessageID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, projectID, accountID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'interrupted')`, parentRunID, accountID, threadID); err != nil {
		t.Fatalf("insert parent run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status, resume_from_run_id) VALUES ($1, $2, $3, 'running', $4)`, resumedRunID, accountID, threadID, parentRunID); err != nil {
		t.Fatalf("insert resumed run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, parentRunID, `{"thread_tail_message_id":"`+firstMessageID.String()+`"}`); err != nil {
		t.Fatalf("insert parent run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{}'::jsonb)`, resumedRunID); err != nil {
		t.Fatalf("insert resumed run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, firstMessageID, accountID, threadID, "find the file"); err != nil {
		t.Fatalf("insert first user message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, continueMessageID, accountID, threadID, "continue"); err != nil {
		t.Fatalf("insert trailing user message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO thread_compaction_snapshots (account_id, thread_id, summary_text, metadata_json, is_active) VALUES ($1, $2, $3, '{}'::jsonb, true)`, accountID, threadID, "existing summary"); err != nil {
		t.Fatalf("insert active snapshot: %v", err)
	}

	storeRoot := t.TempDir()
	store, err := objectstore.NewFilesystemOpener(storeRoot).Open(ctx, objectstore.RolloutBucket)
	if err != nil {
		t.Fatalf("open rollout store: %v", err)
	}
	blobStore, ok := store.(objectstore.BlobStore)
	if !ok {
		t.Fatal("expected blob store")
	}

	rolloutBody := marshalRolloutJSONL(t,
		map[string]any{"type": "turn_start", "payload": map[string]any{"turn_index": 1, "model": "stub"}},
		map[string]any{"type": "assistant_message", "payload": map[string]any{"content": "I am checking"}},
		map[string]any{"type": "run_end", "payload": map[string]any{"final_status": "interrupted"}},
	)
	if err := blobStore.Put(ctx, "run/"+parentRunID.String()+".jsonl", rolloutBody); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:              resumedRunID,
		AccountID:       accountID,
		ThreadID:        threadID,
		ResumeFromRunID: &parentRunID,
	}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, blobStore, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if !loaded.HasActiveCompactSnapshot {
		t.Fatal("expected active compact snapshot")
	}
	if loaded.ActiveCompactSnapshotText != "existing summary" {
		t.Fatalf("unexpected active snapshot text: %#v", loaded.ActiveCompactSnapshotText)
	}
	if len(loaded.Messages) != 4 {
		t.Fatalf("expected 4 prompt messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != formatCompactSnapshotText("existing summary") {
		t.Fatalf("unexpected snapshot prompt message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "user" || loaded.Messages[1].Content[0].Text != "find the file" {
		t.Fatalf("unexpected first thread message: %#v", loaded.Messages[1])
	}
	if loaded.Messages[2].Role != "assistant" || loaded.Messages[2].Content[0].Text != "I am checking" {
		t.Fatalf("unexpected replay message: %#v", loaded.Messages[2])
	}
	if loaded.Messages[3].Role != "user" || loaded.Messages[3].Content[0].Text != "continue" {
		t.Fatalf("unexpected trailing user message: %#v", loaded.Messages[3])
	}
	if len(loaded.ThreadMessageIDs) != 4 {
		t.Fatalf("expected 4 thread message ids, got %d", len(loaded.ThreadMessageIDs))
	}
	if loaded.ThreadMessageIDs[0] != uuid.Nil || loaded.ThreadMessageIDs[2] != uuid.Nil {
		t.Fatalf("expected synthetic entries to use nil ids, got %#v", loaded.ThreadMessageIDs)
	}
	if loaded.ThreadMessageIDs[1] != firstMessageID || loaded.ThreadMessageIDs[3] != continueMessageID {
		t.Fatalf("unexpected preserved thread message ids: %#v", loaded.ThreadMessageIDs)
	}
}

func TestLoadRunInputsReplaysAfterSnapshotWhenAnchorMessageWasCompacted(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_snapshot_hidden_anchor")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	parentRunID := uuid.New()
	resumedRunID := uuid.New()
	anchorMessageID := uuid.New()
	continueMessageID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, projectID, accountID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'interrupted')`, parentRunID, accountID, threadID); err != nil {
		t.Fatalf("insert parent run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status, resume_from_run_id) VALUES ($1, $2, $3, 'running', $4)`, resumedRunID, accountID, threadID, parentRunID); err != nil {
		t.Fatalf("insert resumed run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, parentRunID, `{"thread_tail_message_id":"`+anchorMessageID.String()+`"}`); err != nil {
		t.Fatalf("insert parent run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{}'::jsonb)`, resumedRunID); err != nil {
		t.Fatalf("insert resumed run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden, compacted) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, true, true)`, anchorMessageID, accountID, threadID, "find the file"); err != nil {
		t.Fatalf("insert compacted anchor message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, continueMessageID, accountID, threadID, "continue"); err != nil {
		t.Fatalf("insert trailing user message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO thread_compaction_snapshots (account_id, thread_id, summary_text, metadata_json, is_active) VALUES ($1, $2, $3, '{}'::jsonb, true)`, accountID, threadID, "existing summary"); err != nil {
		t.Fatalf("insert active snapshot: %v", err)
	}

	storeRoot := t.TempDir()
	store, err := objectstore.NewFilesystemOpener(storeRoot).Open(ctx, objectstore.RolloutBucket)
	if err != nil {
		t.Fatalf("open rollout store: %v", err)
	}
	blobStore, ok := store.(objectstore.BlobStore)
	if !ok {
		t.Fatal("expected blob store")
	}

	rolloutBody := marshalRolloutJSONL(t,
		map[string]any{"type": "assistant_message", "payload": map[string]any{"content": "I am checking"}},
		map[string]any{"type": "run_end", "payload": map[string]any{"final_status": "interrupted"}},
	)
	if err := blobStore.Put(ctx, "run/"+parentRunID.String()+".jsonl", rolloutBody); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:              resumedRunID,
		AccountID:       accountID,
		ThreadID:        threadID,
		ResumeFromRunID: &parentRunID,
	}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, blobStore, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("expected 3 prompt messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != formatCompactSnapshotText("existing summary") {
		t.Fatalf("unexpected snapshot prompt message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "assistant" || loaded.Messages[1].Content[0].Text != "I am checking" {
		t.Fatalf("unexpected replay message: %#v", loaded.Messages[1])
	}
	if loaded.Messages[2].Role != "user" || loaded.Messages[2].Content[0].Text != "continue" {
		t.Fatalf("unexpected trailing user message: %#v", loaded.Messages[2])
	}
	if len(loaded.ThreadMessageIDs) != 3 {
		t.Fatalf("expected 3 thread message ids, got %d", len(loaded.ThreadMessageIDs))
	}
	if loaded.ThreadMessageIDs[0] != uuid.Nil || loaded.ThreadMessageIDs[1] != uuid.Nil {
		t.Fatalf("expected synthetic ids for snapshot and replay, got %#v", loaded.ThreadMessageIDs)
	}
	if loaded.ThreadMessageIDs[2] != continueMessageID {
		t.Fatalf("unexpected preserved thread message ids: %#v", loaded.ThreadMessageIDs)
	}
}

func TestLoadRunInputsReplaysResumeAfterCompactedAnchorUsingSnapshotPrefix(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_snapshot_hidden_anchor_resume")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	parentRunID := uuid.New()
	resumedRunID := uuid.New()
	anchorMessageID := uuid.New()
	continueMessageID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, projectID, accountID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'interrupted')`, parentRunID, accountID, threadID); err != nil {
		t.Fatalf("insert parent run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status, resume_from_run_id) VALUES ($1, $2, $3, 'running', $4)`, resumedRunID, accountID, threadID, parentRunID); err != nil {
		t.Fatalf("insert resumed run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, parentRunID, `{"thread_tail_message_id":"`+anchorMessageID.String()+`"}`); err != nil {
		t.Fatalf("insert parent run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{}'::jsonb)`, resumedRunID); err != nil {
		t.Fatalf("insert resumed run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden, compacted) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, true, true)`, anchorMessageID, accountID, threadID, "old hidden anchor"); err != nil {
		t.Fatalf("insert hidden anchor: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, continueMessageID, accountID, threadID, "continue after compact"); err != nil {
		t.Fatalf("insert continue message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO thread_compaction_snapshots (account_id, thread_id, summary_text, metadata_json, is_active) VALUES ($1, $2, $3, '{}'::jsonb, true)`, accountID, threadID, "rolled summary"); err != nil {
		t.Fatalf("insert active snapshot: %v", err)
	}

	storeRoot := t.TempDir()
	store, err := objectstore.NewFilesystemOpener(storeRoot).Open(ctx, objectstore.RolloutBucket)
	if err != nil {
		t.Fatalf("open rollout store: %v", err)
	}
	blobStore, ok := store.(objectstore.BlobStore)
	if !ok {
		t.Fatal("expected blob store")
	}
	rolloutBody := marshalRolloutJSONL(t,
		map[string]any{"type": "assistant_message", "payload": map[string]any{"content": "assistant replay after hidden anchor"}},
		map[string]any{"type": "run_end", "payload": map[string]any{"final_status": "interrupted"}},
	)
	if err := blobStore.Put(ctx, "run/"+parentRunID.String()+".jsonl", rolloutBody); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:              resumedRunID,
		AccountID:       accountID,
		ThreadID:        threadID,
		ResumeFromRunID: &parentRunID,
	}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, blobStore, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("expected snapshot + replay + visible tail, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != formatCompactSnapshotText("rolled summary") {
		t.Fatalf("unexpected snapshot message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "assistant" || loaded.Messages[1].Content[0].Text != "assistant replay after hidden anchor" {
		t.Fatalf("unexpected replay message: %#v", loaded.Messages[1])
	}
	if loaded.Messages[2].Role != "user" || loaded.Messages[2].Content[0].Text != "continue after compact" {
		t.Fatalf("unexpected visible tail: %#v", loaded.Messages[2])
	}
	if loaded.ThreadMessageIDs[0] != uuid.Nil || loaded.ThreadMessageIDs[1] != uuid.Nil {
		t.Fatalf("expected snapshot and replay to be synthetic: %#v", loaded.ThreadMessageIDs)
	}
	if loaded.ThreadMessageIDs[2] != continueMessageID {
		t.Fatalf("unexpected visible tail id: %#v", loaded.ThreadMessageIDs)
	}
}

func TestLoadRunInputsRuntimeRecoveryReplaysAfterCompactedAnchorUsingSnapshotPrefix(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_snapshot_hidden_anchor_recovery")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	anchorMessageID := uuid.New()
	continueMessageID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, projectID, accountID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, runID, accountID, threadID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, runID, `{"thread_tail_message_id":"`+anchorMessageID.String()+`"}`); err != nil {
		t.Fatalf("insert run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden, compacted) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, true, true)`, anchorMessageID, accountID, threadID, "old hidden anchor"); err != nil {
		t.Fatalf("insert hidden anchor: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, continueMessageID, accountID, threadID, "continue after recovery"); err != nil {
		t.Fatalf("insert continue message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO thread_compaction_snapshots (account_id, thread_id, summary_text, metadata_json, is_active) VALUES ($1, $2, $3, '{}'::jsonb, true)`, accountID, threadID, "rolled summary"); err != nil {
		t.Fatalf("insert active snapshot: %v", err)
	}

	storeRoot := t.TempDir()
	store, err := objectstore.NewFilesystemOpener(storeRoot).Open(ctx, objectstore.RolloutBucket)
	if err != nil {
		t.Fatalf("open rollout store: %v", err)
	}
	blobStore, ok := store.(objectstore.BlobStore)
	if !ok {
		t.Fatal("expected blob store")
	}
	rolloutBody := marshalRolloutJSONL(t,
		map[string]any{"type": "assistant_message", "payload": map[string]any{"content": "recovered assistant"}},
		map[string]any{"type": "run_end", "payload": map[string]any{"final_status": "interrupted"}},
	)
	if err := blobStore.Put(ctx, "run/"+runID.String()+".jsonl", rolloutBody); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, map[string]any{"source": "desktop_recovery"}, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, blobStore, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("expected snapshot + replay + visible tail, got %d", len(loaded.Messages))
	}
	if loaded.Messages[1].Role != "assistant" || loaded.Messages[1].Content[0].Text != "recovered assistant" {
		t.Fatalf("unexpected recovery replay message: %#v", loaded.Messages[1])
	}
}

func TestLoadRunInputsRuntimeRecoveryReplaysWhenAnchorIsVisibleThreadTail(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_runtime_visible_tail")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	threadTailID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, projectID, accountID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, runID, accountID, threadID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, runID, `{"thread_tail_message_id":"`+threadTailID.String()+`"}`); err != nil {
		t.Fatalf("insert run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 2, 'message.delta', '{"content_delta":"partial"}'::jsonb)`, runID); err != nil {
		t.Fatalf("insert message.delta: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, threadTailID, accountID, threadID, "hi after output"); err != nil {
		t.Fatalf("insert user message: %v", err)
	}

	storeRoot := t.TempDir()
	store, err := objectstore.NewFilesystemOpener(storeRoot).Open(ctx, objectstore.RolloutBucket)
	if err != nil {
		t.Fatalf("open rollout store: %v", err)
	}
	blobStore, ok := store.(objectstore.BlobStore)
	if !ok {
		t.Fatal("expected blob store")
	}
	rolloutBody := marshalRolloutJSONL(t,
		map[string]any{"type": "assistant_message", "payload": map[string]any{"content": "recovered assistant"}},
		map[string]any{"type": "tool_call", "payload": map[string]any{"call_id": "call_1", "name": "memory_write", "input": map[string]any{"key": "qingfeng"}}},
		map[string]any{"type": "tool_result", "payload": map[string]any{"call_id": "call_1", "output": map[string]any{"ok": true}}},
	)
	if err := blobStore.Put(ctx, "run/"+runID.String()+".jsonl", rolloutBody); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, map[string]any{"source": "desktop_recovery"}, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, blobStore, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("expected user + replay assistant + replay tool, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != "hi after output" {
		t.Fatalf("unexpected first message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "assistant" || loaded.Messages[1].Content[0].Text != "recovered assistant" {
		t.Fatalf("unexpected recovery replay message: %#v", loaded.Messages[1])
	}
	if loaded.Messages[2].Role != "tool" {
		t.Fatalf("unexpected replayed tool message: %#v", loaded.Messages[2])
	}
}

func TestLoadRunInputsFallsBackToThreadTranscriptWhenResumeReplayUnavailable(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_resume_fallback")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	parentRunID := uuid.New()
	resumedRunID := uuid.New()
	firstMessageID := uuid.New()
	continueMessageID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'cancelled')`, parentRunID, accountID, threadID); err != nil {
		t.Fatalf("insert parent run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status, resume_from_run_id) VALUES ($1, $2, $3, 'running', $4)`, resumedRunID, accountID, threadID, parentRunID); err != nil {
		t.Fatalf("insert resumed run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, parentRunID, `{"thread_tail_message_id":"`+firstMessageID.String()+`"}`); err != nil {
		t.Fatalf("insert parent run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{"continuation_source":"user_followup","continuation_loop":true,"continuation_response":true}'::jsonb)`, resumedRunID); err != nil {
		t.Fatalf("insert resumed run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, firstMessageID, accountID, threadID, "find the file"); err != nil {
		t.Fatalf("insert first user message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, continueMessageID, accountID, threadID, "刚才你在干什么"); err != nil {
		t.Fatalf("insert trailing user message: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:              resumedRunID,
		AccountID:       accountID,
		ThreadID:        threadID,
		ResumeFromRunID: &parentRunID,
	}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if got := loaded.InputJSON[runStartedContinuationSourceKey]; got != "none" {
		t.Fatalf("unexpected continuation_source: %#v", got)
	}
	if got := loaded.InputJSON[runStartedContinuationLoopKey]; got != false {
		t.Fatalf("unexpected continuation_loop: %#v", got)
	}
	if _, ok := loaded.InputJSON[runStartedContinuationResponseKey]; ok {
		t.Fatalf("unexpected continuation_response: %#v", loaded.InputJSON[runStartedContinuationResponseKey])
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected 2 thread messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != "find the file" {
		t.Fatalf("unexpected first message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "user" || loaded.Messages[1].Content[0].Text != "刚才你在干什么" {
		t.Fatalf("unexpected trailing user message: %#v", loaded.Messages[1])
	}
}

func TestLoadRunInputsReplaysResumeChainInThreadOrder(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_resume_chain")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runAID := uuid.New()
	runBID := uuid.New()
	runCID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()
	msg3ID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, msg1ID, accountID, threadID, "step one"); err != nil {
		t.Fatalf("insert first user message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, msg2ID, accountID, threadID, "step two"); err != nil {
		t.Fatalf("insert second user message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, msg3ID, accountID, threadID, "step three"); err != nil {
		t.Fatalf("insert third user message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'interrupted')`, runAID, accountID, threadID); err != nil {
		t.Fatalf("insert run A: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status, resume_from_run_id) VALUES ($1, $2, $3, 'interrupted', $4)`, runBID, accountID, threadID, runAID); err != nil {
		t.Fatalf("insert run B: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status, resume_from_run_id) VALUES ($1, $2, $3, 'running', $4)`, runCID, accountID, threadID, runBID); err != nil {
		t.Fatalf("insert run C: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, runAID, `{"thread_tail_message_id":"`+msg1ID.String()+`"}`); err != nil {
		t.Fatalf("insert run A started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, runBID, `{"thread_tail_message_id":"`+msg2ID.String()+`"}`); err != nil {
		t.Fatalf("insert run B started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{}'::jsonb)`, runCID); err != nil {
		t.Fatalf("insert run C started event: %v", err)
	}

	storeRoot := t.TempDir()
	store, err := objectstore.NewFilesystemOpener(storeRoot).Open(ctx, objectstore.RolloutBucket)
	if err != nil {
		t.Fatalf("open rollout store: %v", err)
	}
	blobStore, ok := store.(objectstore.BlobStore)
	if !ok {
		t.Fatal("expected blob store")
	}

	rolloutA := marshalRolloutJSONL(t,
		map[string]any{"type": "assistant_message", "payload": map[string]any{"content": "assistant A"}},
	)
	if err := blobStore.Put(ctx, "run/"+runAID.String()+".jsonl", rolloutA); err != nil {
		t.Fatalf("write rollout A: %v", err)
	}

	toolCallsJSON, err := json.Marshal([]llm.ToolCall{{
		ToolCallID:    "call_b",
		ToolName:      "echo",
		ArgumentsJSON: map[string]any{"text": "from b"},
	}})
	if err != nil {
		t.Fatalf("marshal tool calls for B: %v", err)
	}
	toolInputJSON, err := json.Marshal(map[string]any{"text": "from b"})
	if err != nil {
		t.Fatalf("marshal tool input for B: %v", err)
	}
	rolloutB := marshalRolloutJSONL(t,
		map[string]any{"type": "assistant_message", "payload": map[string]any{"content": "assistant B", "tool_calls": json.RawMessage(toolCallsJSON)}},
		map[string]any{"type": "tool_call", "payload": map[string]any{"call_id": "call_b", "name": "echo", "input": json.RawMessage(toolInputJSON)}},
		map[string]any{"type": "run_end", "payload": map[string]any{"final_status": "interrupted"}},
	)
	if err := blobStore.Put(ctx, "run/"+runBID.String()+".jsonl", rolloutB); err != nil {
		t.Fatalf("write rollout B: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:              runCID,
		AccountID:       accountID,
		ThreadID:        threadID,
		ResumeFromRunID: &runBID,
	}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, blobStore, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.Messages) != 6 {
		t.Fatalf("expected 6 prompt messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != "step one" {
		t.Fatalf("unexpected first prompt entry: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "assistant" || loaded.Messages[1].Content[0].Text != "assistant A" {
		t.Fatalf("unexpected replayed A message: %#v", loaded.Messages[1])
	}
	if loaded.Messages[2].Role != "user" || loaded.Messages[2].Content[0].Text != "step two" {
		t.Fatalf("unexpected second user message: %#v", loaded.Messages[2])
	}
	if loaded.Messages[3].Role != "assistant" || loaded.Messages[3].Content[0].Text != "assistant B" {
		t.Fatalf("unexpected replayed B message: %#v", loaded.Messages[3])
	}
	if loaded.Messages[4].Role != "tool" {
		t.Fatalf("unexpected replayed B tool role: %#v", loaded.Messages[4])
	}
	if loaded.Messages[5].Role != "user" || loaded.Messages[5].Content[0].Text != "step three" {
		t.Fatalf("unexpected third user message: %#v", loaded.Messages[5])
	}
	if len(loaded.ThreadMessageIDs) != 6 {
		t.Fatalf("expected 6 thread message ids, got %d", len(loaded.ThreadMessageIDs))
	}
	if loaded.ThreadMessageIDs[1] != uuid.Nil || loaded.ThreadMessageIDs[3] != uuid.Nil || loaded.ThreadMessageIDs[4] != uuid.Nil {
		t.Fatalf("expected replayed entries to use nil ids, got %#v", loaded.ThreadMessageIDs)
	}
	if loaded.ThreadMessageIDs[0] != msg1ID || loaded.ThreadMessageIDs[2] != msg2ID || loaded.ThreadMessageIDs[5] != msg3ID {
		t.Fatalf("unexpected preserved thread ids: %#v", loaded.ThreadMessageIDs)
	}
}

func TestLoadRunInputsReplaysRuntimeRecoveryDraft(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_runtime_draft")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	threadTailID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, runID, accountID, threadID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, runID, fmt.Sprintf(`{"thread_tail_message_id":"%s"}`, threadTailID)); err != nil {
		t.Fatalf("insert run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, threadTailID, accountID, threadID, "hi there"); err != nil {
		t.Fatalf("insert user message: %v", err)
	}

	storeRoot := t.TempDir()
	store, err := objectstore.NewFilesystemOpener(storeRoot).Open(ctx, objectstore.RolloutBucket)
	if err != nil {
		t.Fatalf("open rollout store: %v", err)
	}
	blobStore, ok := store.(objectstore.BlobStore)
	if !ok {
		t.Fatal("expected blob store")
	}
	if err := WriteResponseDraft(ctx, blobStore, runID, threadID, "partial reply", 123); err != nil {
		t.Fatalf("write response draft: %v", err)
	}

	jobPayload := map[string]any{"recovery_source": "runtime_recovery"}
	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, jobPayload, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, blobStore, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}

	if len(loaded.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != "hi there" {
		t.Fatalf("unexpected user message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "assistant" || loaded.Messages[1].Content[0].Text != "partial reply" {
		t.Fatalf("expected runtime draft assistant message, got %#v", loaded.Messages[1])
	}
	if len(loaded.ThreadMessageIDs) != 2 {
		t.Fatalf("expected 2 thread ids, got %d", len(loaded.ThreadMessageIDs))
	}
	if loaded.ThreadMessageIDs[1] != uuid.Nil {
		t.Fatalf("expected inserted draft to use nil thread id, got %s", loaded.ThreadMessageIDs[1])
	}
}

func TestCanonicalThreadHasAssistantMessageForRunIgnoresReplacementCoveredMessages(t *testing.T) {
	runID := uuid.New()
	coveredMessageID := uuid.New()
	visibleMessageID := uuid.New()
	canonicalContext := &canonicalThreadContext{
		ThreadMessageIDs: []uuid.UUID{uuid.Nil, visibleMessageID},
	}
	coveredMetadata, err := json.Marshal(map[string]any{"run_id": runID.String()})
	if err != nil {
		t.Fatalf("marshal covered metadata: %v", err)
	}
	visibleMetadata, err := json.Marshal(map[string]any{"run_id": uuid.New().String()})
	if err != nil {
		t.Fatalf("marshal visible metadata: %v", err)
	}
	threadMessages := []data.ThreadMessage{
		{ID: coveredMessageID, Role: "assistant", MetadataJSON: coveredMetadata},
		{ID: visibleMessageID, Role: "assistant", MetadataJSON: visibleMetadata},
	}

	if canonicalThreadHasAssistantMessageForRun(canonicalContext, threadMessages, runID) {
		t.Fatal("expected replacement-covered assistant message to be ignored")
	}
}

func TestLoadRunInputsAllowsRuntimeRecoveryRestartBeforeFirstRecoverableOutput(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_runtime_pre_output_restart")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	threadTailID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, runID, accountID, threadID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, runID, fmt.Sprintf(`{"thread_tail_message_id":"%s"}`, threadTailID)); err != nil {
		t.Fatalf("insert run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 2, 'llm.request', '{"llm_call_id":"call-pre-output"}'::jsonb)`, runID); err != nil {
		t.Fatalf("insert llm.request: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, threadTailID, accountID, threadID, "hi before output"); err != nil {
		t.Fatalf("insert user message: %v", err)
	}

	storeRoot := t.TempDir()
	store, err := objectstore.NewFilesystemOpener(storeRoot).Open(ctx, objectstore.RolloutBucket)
	if err != nil {
		t.Fatalf("open rollout store: %v", err)
	}
	blobStore, ok := store.(objectstore.BlobStore)
	if !ok {
		t.Fatal("expected blob store")
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, map[string]any{"source": "desktop_recovery"}, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, blobStore, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.Messages) != 1 {
		t.Fatalf("expected only thread transcript message, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != "hi before output" {
		t.Fatalf("unexpected recovered message: %#v", loaded.Messages[0])
	}
	if len(loaded.ThreadMessageIDs) != 1 || loaded.ThreadMessageIDs[0] != threadTailID {
		t.Fatalf("unexpected thread message ids: %#v", loaded.ThreadMessageIDs)
	}
}

func TestLoadRunInputsKeepsRuntimeRecoveryInterruptedAfterRecoverableOutput(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_runtime_missing_recovery_state")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	threadTailID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, runID, accountID, threadID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, runID, fmt.Sprintf(`{"thread_tail_message_id":"%s"}`, threadTailID)); err != nil {
		t.Fatalf("insert run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 2, 'message.delta', '{"content_delta":"partial"}'::jsonb)`, runID); err != nil {
		t.Fatalf("insert message.delta: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, threadTailID, accountID, threadID, "hi after output"); err != nil {
		t.Fatalf("insert user message: %v", err)
	}

	storeRoot := t.TempDir()
	store, err := objectstore.NewFilesystemOpener(storeRoot).Open(ctx, objectstore.RolloutBucket)
	if err != nil {
		t.Fatalf("open rollout store: %v", err)
	}
	blobStore, ok := store.(objectstore.BlobStore)
	if !ok {
		t.Fatal("expected blob store")
	}

	_, err = loadRunInputs(ctx, pool, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, map[string]any{"source": "desktop_recovery"}, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, blobStore, 20)
	if !IsResumeUnavailableError(err) {
		t.Fatalf("expected resume unavailable error, got %v", err)
	}
}

func marshalRolloutJSONL(t *testing.T, items ...map[string]any) []byte {
	t.Helper()
	var out []byte
	for _, item := range items {
		item["timestamp"] = time.Now().UTC()
		encoded, err := json.Marshal(item)
		if err != nil {
			t.Fatalf("marshal rollout item: %v", err)
		}
		out = append(out, encoded...)
		out = append(out, '\n')
	}
	return out
}
