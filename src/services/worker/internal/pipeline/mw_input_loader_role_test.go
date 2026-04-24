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
	"github.com/jackc/pgx/v5"
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
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 1, 'assistant', 'hidden', '{}'::jsonb, true)`, hiddenID, accountID, threadID); err != nil {
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
	if _, err := pool.Exec(ctx, `INSERT INTO thread_context_replacements (
		account_id, thread_id,
		start_thread_seq, end_thread_seq,
		start_context_seq, end_context_seq,
		summary_text, layer, metadata_json
	) VALUES ($1, $2, 2, 2, 1, 1, 'future summary', 1, '{}'::jsonb)`, accountID, threadID); err != nil {
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
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected 2 prompt messages, got %d", len(loaded.Messages))
	}
	if len(loaded.ThreadContextFrontier) == 0 || loaded.ThreadContextFrontier[0].SourceText != "future summary" {
		t.Fatalf("expected replacement prefix, got frontier=%#v", loaded.ThreadContextFrontier)
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != "future summary" {
		t.Fatalf("unexpected replacement message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[0].Phase == nil || *loaded.Messages[0].Phase != compactSyntheticPhase {
		t.Fatalf("unexpected replacement phase: %#v", loaded.Messages[0].Phase)
	}
	if loaded.Messages[1].Role != "user" || loaded.Messages[1].Content[0].Text != "two" {
		t.Fatalf("unexpected bounded tail message: %#v", loaded.Messages[1])
	}
	if len(loaded.ThreadMessageIDs) != 2 || loaded.ThreadMessageIDs[0] != uuid.Nil || loaded.ThreadMessageIDs[1] != msg2ID {
		t.Fatalf("unexpected bounded thread ids: %#v", loaded.ThreadMessageIDs)
	}
}

func TestLoadRunInputsPersistsRenderableGraphAndKeepsLatestBoundedTail(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_renderable_graph_persist")
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
	channelID := uuid.New()

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
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`,
		runID,
		fmt.Sprintf(`{"thread_tail_message_id":"%s","channel_delivery":{"channel_id":"%s"}}`, msg3ID, channelID),
	); err != nil {
		t.Fatalf("insert run event: %v", err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 1, 'user', 'older', '{}'::jsonb, false)`, msg1ID, accountID, threadID); err != nil {
		t.Fatalf("insert message 1: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 2, 'assistant', 'excluded-middle', '{"exclude_from_prompt":true}'::jsonb, false)`, msg2ID, accountID, threadID); err != nil {
		t.Fatalf("insert excluded message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 3, 'user', 'latest-tail', '{}'::jsonb, false)`, msg3ID, accountID, threadID); err != nil {
		t.Fatalf("insert message 3: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected 2 renderable prompt messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != "older" {
		t.Fatalf("unexpected first prompt message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "user" || loaded.Messages[1].Content[0].Text != "latest-tail" {
		t.Fatalf("unexpected bounded tail prompt message: %#v", loaded.Messages[1])
	}
	if len(loaded.ThreadMessageIDs) != 2 || loaded.ThreadMessageIDs[0] != msg1ID || loaded.ThreadMessageIDs[1] != msg3ID {
		t.Fatalf("unexpected thread message ids: %#v", loaded.ThreadMessageIDs)
	}

	rows, err := pool.Query(ctx, `SELECT atom_seq, source_message_start_seq, source_message_end_seq FROM thread_context_atoms WHERE account_id = $1 AND thread_id = $2 ORDER BY atom_seq ASC`, accountID, threadID)
	if err != nil {
		t.Fatalf("query persisted atoms: %v", err)
	}
	defer rows.Close()

	type atomRange struct {
		atomSeq  int64
		startSeq int64
		endSeq   int64
	}
	var ranges []atomRange
	for rows.Next() {
		var item atomRange
		if err := rows.Scan(&item.atomSeq, &item.startSeq, &item.endSeq); err != nil {
			t.Fatalf("scan atom row: %v", err)
		}
		ranges = append(ranges, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate atom rows: %v", err)
	}
	if len(ranges) != 2 {
		t.Fatalf("expected 2 persisted atoms after commit, got %d", len(ranges))
	}
	if ranges[0].startSeq != 1 || ranges[0].endSeq != 1 {
		t.Fatalf("unexpected first persisted atom range: %#v", ranges[0])
	}
	if ranges[1].startSeq != 3 || ranges[1].endSeq != 3 {
		t.Fatalf("expected excluded seq=2 to be absent from persisted graph, got %#v", ranges[1])
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
	if len(loaded.ThreadContextFrontier) == 0 || loaded.ThreadContextFrontier[0].SourceText != "rolled summary" {
		t.Fatalf("unexpected frontier prefix: %#v", loaded.ThreadContextFrontier)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected 2 prompt messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != "rolled summary" {
		t.Fatalf("unexpected replacement prompt message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "user" || loaded.Messages[1].Content[0].Text != "two" {
		t.Fatalf("unexpected bounded tail message: %#v", loaded.Messages[1])
	}
}

func TestLoadRunInputsRejectsReplacementCrossingBoundedUpperSeq(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_bounded_replacement_crossing_upper")
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
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 3, 'assistant', 'future assistant', '{}'::jsonb, false)`, msg3ID, accountID, threadID); err != nil {
		t.Fatalf("insert message three: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO thread_context_replacements (account_id, thread_id, start_thread_seq, end_thread_seq, summary_text, layer, metadata_json) VALUES ($1, $2, 1, 3, 'crossing summary', 3, '{}'::jsonb)`, accountID, threadID); err != nil {
		t.Fatalf("insert crossing replacement: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.ThreadContextFrontier) > 0 && loaded.ThreadContextFrontier[0].Kind == FrontierNodeReplacement {
		t.Fatalf("expected crossing replacement rejected, got frontier=%#v", loaded.ThreadContextFrontier)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected bounded raw messages only, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != "one" {
		t.Fatalf("unexpected bounded raw message one: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "user" || loaded.Messages[1].Content[0].Text != "two" {
		t.Fatalf("unexpected bounded raw message two: %#v", loaded.Messages[1])
	}
}

func TestLoadRunInputsKeepsBoundedLatestVisibleUserTailAcrossBuilderAndLoader(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_bounded_latest_visible_tail")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	hiddenPrefixID := uuid.New()
	oldTailID := uuid.New()
	oldAssistantID := uuid.New()
	latestTailID := uuid.New()
	futureAssistantID := uuid.New()
	channelID := uuid.New()

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
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`,
		runID,
		fmt.Sprintf(`{"thread_tail_message_id":"%s","channel_delivery":{"channel_id":"%s"}}`, latestTailID, channelID),
	); err != nil {
		t.Fatalf("insert run started event: %v", err)
	}

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
		 VALUES ($1, $2, $3, 1, 'assistant', $4, '{}'::jsonb, true)`,
		hiddenPrefixID, accountID, threadID, "[hidden-prefix]",
	); err != nil {
		t.Fatalf("insert hidden prefix message: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
		 VALUES ($1, $2, $3, 2, 'user', $4, '{}'::jsonb, false)`,
		oldTailID, accountID, threadID, "Telegram #10152 old visible user tail",
	); err != nil {
		t.Fatalf("insert old visible user tail: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
		 VALUES ($1, $2, $3, 3, 'assistant', $4, '{}'::jsonb, false)`,
		oldAssistantID, accountID, threadID, "assistant ack for #10152",
	); err != nil {
		t.Fatalf("insert old assistant message: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
		 VALUES ($1, $2, $3, 4, 'user', $4, '{}'::jsonb, false)`,
		latestTailID, accountID, threadID, "Telegram #10785 latest visible user tail",
	); err != nil {
		t.Fatalf("insert latest visible user tail: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
		 VALUES ($1, $2, $3, 5, 'assistant', $4, '{}'::jsonb, false)`,
		futureAssistantID, accountID, threadID, "future assistant outside bound",
	); err != nil {
		t.Fatalf("insert future assistant message: %v", err)
	}

	readPromptText := func(msg llm.Message) string {
		parts := make([]string, 0, len(msg.Content))
		for _, part := range msg.Content {
			text := strings.TrimSpace(llm.PartPromptText(part))
			if text == "" {
				continue
			}
			parts = append(parts, text)
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	lastVisibleUserText := func(messages []llm.Message) string {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role != "user" {
				continue
			}
			text := readPromptText(messages[i])
			if text != "" {
				return text
			}
		}
		return ""
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin builder tx: %v", err)
	}
	canonical, err := buildCanonicalThreadContext(
		ctx,
		tx,
		data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		data.MessagesRepository{},
		nil,
		&latestTailID,
		20,
	)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("buildCanonicalThreadContext failed: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback builder tx: %v", err)
	}

	if len(canonical.VisibleMessages) == 0 {
		t.Fatal("expected visible messages in canonical context")
	}
	lastVisibleRecord := canonical.VisibleMessages[len(canonical.VisibleMessages)-1]
	if lastVisibleRecord.ID != latestTailID {
		t.Fatalf("canonical visible tail id mismatch: got=%s want=%s", lastVisibleRecord.ID, latestTailID)
	}
	if !strings.Contains(lastVisibleRecord.Content, "#10785") {
		t.Fatalf("canonical visible tail content mismatch: %#v", lastVisibleRecord)
	}
	if len(canonical.ThreadMessageIDs) == 0 || canonical.ThreadMessageIDs[len(canonical.ThreadMessageIDs)-1] != latestTailID {
		t.Fatalf("canonical rendered tail id mismatch: %#v", canonical.ThreadMessageIDs)
	}
	if got := lastVisibleUserText(canonical.Messages); !strings.Contains(got, "#10785") {
		t.Fatalf("canonical rendered latest visible user tail missing, got %q", got)
	}

	loaded, err := loadRunInputs(
		ctx,
		pool,
		data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		nil,
		data.RunsRepository{},
		data.RunEventsRepository{},
		data.MessagesRepository{},
		nil,
		nil,
		20,
	)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if got, _ := loaded.InputJSON[runStartedThreadTailMessageIDKey].(string); got != latestTailID.String() {
		t.Fatalf("unexpected bounded upper id in input: got=%q want=%q", got, latestTailID)
	}
	if got := lastVisibleUserText(loaded.Messages); !strings.Contains(got, "#10785") {
		t.Fatalf("loadRunInputs latest visible user tail missing, got %q", got)
	}
	if len(loaded.ThreadMessageIDs) == 0 || loaded.ThreadMessageIDs[len(loaded.ThreadMessageIDs)-1] != latestTailID {
		t.Fatalf("loadRunInputs rendered tail id mismatch: %#v", loaded.ThreadMessageIDs)
	}
	if got, _ := loaded.InputJSON["last_user_message"].(string); !strings.Contains(got, "#10785") {
		t.Fatalf("last_user_message mismatch: %q", got)
	}
	for i, msg := range loaded.Messages {
		if strings.Contains(readPromptText(msg), "future assistant outside bound") {
			t.Fatalf("bounded history leaked future message at index=%d: %#v", i, msg)
		}
	}
}

func TestLoadRunInputsMergesLeadingReplacementSummaryBlocks(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_leading_summary_merge")
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
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{}'::jsonb)`, runID); err != nil {
		t.Fatalf("insert run event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 1, 'user', 'one', '{}'::jsonb, false)`, msg1ID, accountID, threadID); err != nil {
		t.Fatalf("insert message one: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 2, 'assistant', 'two', '{}'::jsonb, false)`, msg2ID, accountID, threadID); err != nil {
		t.Fatalf("insert message two: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 3, 'user', 'tail', '{}'::jsonb, false)`, msg3ID, accountID, threadID); err != nil {
		t.Fatalf("insert message three: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO thread_context_replacements (account_id, thread_id, start_thread_seq, end_thread_seq, summary_text, layer, metadata_json) VALUES ($1, $2, 1, 1, 'summary one', 2, '{}'::jsonb)`, accountID, threadID); err != nil {
		t.Fatalf("insert replacement one: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO thread_context_replacements (account_id, thread_id, start_thread_seq, end_thread_seq, summary_text, layer, metadata_json) VALUES ($1, $2, 2, 2, 'summary two', 2, '{}'::jsonb)`, accountID, threadID); err != nil {
		t.Fatalf("insert replacement two: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}

	if len(loaded.ThreadContextFrontier) < 2 || loaded.ThreadContextFrontier[0].SourceText != "summary one" || loaded.ThreadContextFrontier[1].SourceText != "summary two" {
		t.Fatalf("unexpected merged leading frontier: %#v", loaded.ThreadContextFrontier)
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("expected two leading replacements plus tail, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Content[0].Text != "summary one" {
		t.Fatalf("unexpected first replacement message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Content[0].Text != "summary two" {
		t.Fatalf("unexpected second replacement message: %#v", loaded.Messages[1])
	}
	if loaded.Messages[2].Role != "user" || loaded.Messages[2].Content[0].Text != "tail" {
		t.Fatalf("unexpected tail message: %#v", loaded.Messages[2])
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

func TestLoadRunInputsReplaysFailedRunBeforeTrailingUserInput(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_resume_failed")
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
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'failed')`, parentRunID, accountID, threadID); err != nil {
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

	contentPartsJSON, err := json.Marshal([]map[string]any{
		{"type": "thinking", "thinking": "pondering", "signature": "sig_failed"},
		{"type": "text", "text": "I am checking"},
	})
	if err != nil {
		t.Fatalf("marshal content parts: %v", err)
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
		map[string]any{"type": "assistant_message", "payload": map[string]any{"content": "I am checking", "content_parts": json.RawMessage(contentPartsJSON), "tool_calls": json.RawMessage(toolCallsJSON)}},
		map[string]any{"type": "tool_call", "payload": map[string]any{"call_id": "call_1", "name": "echo", "input": json.RawMessage(toolInputJSON)}},
		map[string]any{"type": "run_end", "payload": map[string]any{"final_status": "failed"}},
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
	if loaded.Messages[1].Role != "assistant" {
		t.Fatalf("unexpected replayed assistant role: %#v", loaded.Messages[1])
	}
	if len(loaded.Messages[1].Content) != 2 || loaded.Messages[1].Content[0].Kind() != "thinking" {
		t.Fatalf("expected failed replay to preserve thinking content, got %#v", loaded.Messages[1].Content)
	}
	if loaded.Messages[1].Content[0].Signature != "sig_failed" {
		t.Fatalf("expected failed replay to preserve thinking signature, got %#v", loaded.Messages[1].Content[0])
	}
	if len(loaded.Messages[1].ToolCalls) != 1 || loaded.Messages[1].ToolCalls[0].ToolCallID != "call_1" {
		t.Fatalf("unexpected replayed tool calls: %#v", loaded.Messages[1].ToolCalls)
	}
	if loaded.Messages[3].Role != "user" || loaded.Messages[3].Content[0].Text != "continue" {
		t.Fatalf("unexpected trailing user message: %#v", loaded.Messages[3])
	}
}

func TestLoadRunInputsSkipsVisibleAssistantWhenReplayingSameRun(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_resume_visible_parent")
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
	parentAssistantID := uuid.New()
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
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'assistant', $4, $5::jsonb, false)`, parentAssistantID, accountID, threadID, "I am checking", fmt.Sprintf(`{"run_id":"%s"}`, parentRunID)); err != nil {
		t.Fatalf("insert visible assistant message: %v", err)
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
	}, map[string]any{"source": "continue"}, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, blobStore, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.Messages) != 4 {
		t.Fatalf("expected 4 prompt messages without duplicate canonical assistant, got %d", len(loaded.Messages))
	}
	if loaded.Messages[1].Role != "assistant" || len(loaded.Messages[1].ToolCalls) != 1 {
		t.Fatalf("expected replayed assistant with tool calls, got %#v", loaded.Messages[1])
	}
	if loaded.Messages[2].Role != "tool" {
		t.Fatalf("expected replayed tool result, got %#v", loaded.Messages[2])
	}
	if loaded.Messages[3].Role != "user" || loaded.Messages[3].Content[0].Text != "continue" {
		t.Fatalf("unexpected trailing user message: %#v", loaded.Messages[3])
	}
}

func TestLoadRunInputsExplicitContinueRequiresRolloutEvenWithVisibleAssistant(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_continue_requires_rollout")
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
	parentAssistantID := uuid.New()
	continueMessageID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'failed')`, parentRunID, accountID, threadID); err != nil {
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
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'assistant', $4, $5::jsonb, false)`, parentAssistantID, accountID, threadID, "I am checking", fmt.Sprintf(`{"run_id":"%s"}`, parentRunID)); err != nil {
		t.Fatalf("insert visible assistant message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, continueMessageID, accountID, threadID, "continue"); err != nil {
		t.Fatalf("insert trailing user message: %v", err)
	}

	_, err = loadRunInputs(ctx, pool, data.Run{
		ID:              resumedRunID,
		AccountID:       accountID,
		ThreadID:        threadID,
		ResumeFromRunID: &parentRunID,
	}, map[string]any{"source": "continue"}, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if !IsResumeUnavailableError(err) {
		t.Fatalf("expected explicit continue to fail without rollout, got %v", err)
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

func TestBuildReplayMessagesPreservesThinkingParts(t *testing.T) {
	toolCallsJSON, err := json.Marshal([]llm.ToolCall{
		{ToolCallID: "call_1", ToolName: "echo", ArgumentsJSON: map[string]any{"text": "hi"}},
	})
	if err != nil {
		t.Fatalf("marshal tool calls: %v", err)
	}
	contentPartsJSON, err := json.Marshal([]map[string]any{
		{"type": "thinking", "thinking": "pondering", "signature": "sig_1"},
		{"type": "text", "text": "working"},
	})
	if err != nil {
		t.Fatalf("marshal content parts: %v", err)
	}
	state := &rollout.ReconstructedState{
		ReplayMessages: []rollout.ReplayMessage{
			{
				Role: "assistant",
				Assistant: &rollout.AssistantMessage{
					Content:      "working",
					ContentParts: contentPartsJSON,
					ToolCalls:    toolCallsJSON,
				},
			},
		},
	}

	messages, err := buildReplayMessages(state)
	if err != nil {
		t.Fatalf("buildReplayMessages failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 replayed assistant message, got %d", len(messages))
	}
	if len(messages[0].Content) != 2 || messages[0].Content[0].Kind() != "thinking" {
		t.Fatalf("expected replayed thinking part, got %#v", messages[0].Content)
	}
	if messages[0].Content[0].Signature != "sig_1" {
		t.Fatalf("expected thinking signature preserved, got %#v", messages[0].Content[0])
	}
	if len(messages[0].ToolCalls) != 1 || messages[0].ToolCalls[0].ToolCallID != "call_1" {
		t.Fatalf("expected replayed tool call preserved, got %#v", messages[0].ToolCalls)
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
	if _, err := pool.Exec(ctx, `INSERT INTO thread_context_replacements (
		account_id, thread_id,
		start_thread_seq, end_thread_seq,
		start_context_seq, end_context_seq,
		summary_text, layer, metadata_json
	) VALUES ($1, $2, 1, 1, 1, 1, $3, 1, '{}'::jsonb)`, accountID, threadID, "existing summary"); err != nil {
		t.Fatalf("insert active replacement: %v", err)
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
	if len(loaded.ThreadContextFrontier) == 0 || loaded.ThreadContextFrontier[0].SourceText != "existing summary" {
		t.Fatalf("unexpected frontier prefix: %#v", loaded.ThreadContextFrontier)
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("expected 3 prompt messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != "existing summary" {
		t.Fatalf("unexpected replacement prompt message: %#v", loaded.Messages[0])
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
		t.Fatalf("expected synthetic entries to use nil ids, got %#v", loaded.ThreadMessageIDs)
	}
	if loaded.ThreadMessageIDs[2] != continueMessageID {
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
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, anchorMessageID, accountID, threadID, "find the file"); err != nil {
		t.Fatalf("insert anchor message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, continueMessageID, accountID, threadID, "continue"); err != nil {
		t.Fatalf("insert trailing user message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO thread_context_replacements (
		account_id, thread_id,
		start_thread_seq, end_thread_seq,
		start_context_seq, end_context_seq,
		summary_text, layer, metadata_json
	) VALUES ($1, $2, 1, 1, 1, 1, $3, 1, '{}'::jsonb)`, accountID, threadID, "existing summary"); err != nil {
		t.Fatalf("insert active replacement: %v", err)
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
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != "existing summary" {
		t.Fatalf("unexpected replacement prompt message: %#v", loaded.Messages[0])
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
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, anchorMessageID, accountID, threadID, "old hidden anchor"); err != nil {
		t.Fatalf("insert anchor: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, continueMessageID, accountID, threadID, "continue after compact"); err != nil {
		t.Fatalf("insert continue message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO thread_context_replacements (
		account_id, thread_id,
		start_thread_seq, end_thread_seq,
		start_context_seq, end_context_seq,
		summary_text, layer, metadata_json
	) VALUES ($1, $2, 1, 1, 1, 1, $3, 1, '{}'::jsonb)`, accountID, threadID, "rolled summary"); err != nil {
		t.Fatalf("insert active replacement: %v", err)
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
	if loaded.Messages[0].Role != "user" || loaded.Messages[0].Content[0].Text != "rolled summary" {
		t.Fatalf("unexpected replacement message: %#v", loaded.Messages[0])
	}
	if loaded.Messages[1].Role != "assistant" || loaded.Messages[1].Content[0].Text != "assistant replay after hidden anchor" {
		t.Fatalf("unexpected replay message: %#v", loaded.Messages[1])
	}
	if loaded.Messages[2].Role != "user" || loaded.Messages[2].Content[0].Text != "continue after compact" {
		t.Fatalf("unexpected visible tail: %#v", loaded.Messages[2])
	}
	if loaded.ThreadMessageIDs[0] != uuid.Nil || loaded.ThreadMessageIDs[1] != uuid.Nil {
		t.Fatalf("expected replacement and replay to be synthetic: %#v", loaded.ThreadMessageIDs)
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
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, anchorMessageID, accountID, threadID, "old hidden anchor"); err != nil {
		t.Fatalf("insert anchor: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, continueMessageID, accountID, threadID, "continue after recovery"); err != nil {
		t.Fatalf("insert continue message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO thread_context_replacements (
		account_id, thread_id,
		start_thread_seq, end_thread_seq,
		start_context_seq, end_context_seq,
		summary_text, layer, metadata_json
	) VALUES ($1, $2, 1, 1, 1, 1, $3, 1, '{}'::jsonb)`, accountID, threadID, "rolled summary"); err != nil {
		t.Fatalf("insert active replacement: %v", err)
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

func TestLoadRunInputsExplicitContinueFailsWhenResumeContextUnavailable(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_continue_missing_rollout")
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
	threadTailID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'failed')`, parentRunID, accountID, threadID); err != nil {
		t.Fatalf("insert parent run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status, resume_from_run_id) VALUES ($1, $2, $3, 'running', $4)`, resumedRunID, accountID, threadID, parentRunID); err != nil {
		t.Fatalf("insert resumed run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, parentRunID, fmt.Sprintf(`{"thread_tail_message_id":"%s"}`, threadTailID)); err != nil {
		t.Fatalf("insert parent run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{}'::jsonb)`, resumedRunID); err != nil {
		t.Fatalf("insert resumed run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, threadTailID, accountID, threadID, "continue"); err != nil {
		t.Fatalf("insert user message: %v", err)
	}

	_, err = loadRunInputs(ctx, pool, data.Run{
		ID:              resumedRunID,
		AccountID:       accountID,
		ThreadID:        threadID,
		ResumeFromRunID: &parentRunID,
	}, map[string]any{"source": "continue"}, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if !IsResumeUnavailableError(err) {
		t.Fatalf("expected resume unavailable error, got %v", err)
	}
}

func TestLoadRunInputsExplicitContinueFallsBackToCanonicalVisibleAssistantWhenRolloutMissing(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_continue_visible_without_rollout")
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
	threadTailID := uuid.New()
	parentAssistantID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'failed')`, parentRunID, accountID, threadID); err != nil {
		t.Fatalf("insert parent run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status, resume_from_run_id) VALUES ($1, $2, $3, 'running', $4)`, resumedRunID, accountID, threadID, parentRunID); err != nil {
		t.Fatalf("insert resumed run: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, parentRunID, fmt.Sprintf(`{"thread_tail_message_id":"%s"}`, threadTailID)); err != nil {
		t.Fatalf("insert parent started: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{}'::jsonb)`, resumedRunID); err != nil {
		t.Fatalf("insert resumed started: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, threadTailID, accountID, threadID, "hello"); err != nil {
		t.Fatalf("insert user message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'assistant', $4, $5::jsonb, false)`, parentAssistantID, accountID, threadID, "partial", fmt.Sprintf(`{"run_id":"%s"}`, parentRunID)); err != nil {
		t.Fatalf("insert assistant message: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:              resumedRunID,
		AccountID:       accountID,
		ThreadID:        threadID,
		ResumeFromRunID: &parentRunID,
	}, map[string]any{"source": "continue"}, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected canonical visible history only, got %d messages", len(loaded.Messages))
	}
	if loaded.Messages[1].Role != "assistant" || loaded.Messages[1].Content[0].Text != "partial" {
		t.Fatalf("unexpected canonical assistant fallback: %#v", loaded.Messages[1])
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

func TestLoadRunInputsCopiesOutputModelKeyFromStartedEvent(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_output_model_key")
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
	if _, err := pool.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, runID, fmt.Sprintf(`{"thread_tail_message_id":"%s","output_model_key":"gpt5"}`, threadTailID)); err != nil {
		t.Fatalf("insert run started event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (id, account_id, thread_id, role, content, metadata_json, hidden) VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb, false)`, threadTailID, accountID, threadID, "hello"); err != nil {
		t.Fatalf("insert user message: %v", err)
	}

	loaded, err := loadRunInputs(ctx, pool, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, nil, data.RunsRepository{}, data.RunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if got, _ := loaded.InputJSON["output_model_key"].(string); got != "gpt5" {
		t.Fatalf("unexpected output_model_key: %#v", loaded.InputJSON["output_model_key"])
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
