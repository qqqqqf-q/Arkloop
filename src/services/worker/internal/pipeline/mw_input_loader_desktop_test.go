//go:build desktop

package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestLoadRunInputsDesktopBoundsFreshChannelHistoryAtThreadTail(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "bounded-input.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	hiddenID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()
	msg3ID := uuid.New()

	if _, err := db.Exec(ctx, `INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`, accountID, "acc-"+accountID.String(), "acc"); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, 'p', 'private')`, projectID, accountID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, runID, accountID, threadID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`,
		runID,
		fmt.Sprintf(`{"thread_tail_message_id":"%s","channel_delivery":{"channel_id":"%s"}}`, msg2ID, uuid.New()),
	); err != nil {
		t.Fatalf("insert run started: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, hiddenID, 1, "assistant", "hidden", "2026-04-09 05:18:28.100000000 +0000"); err != nil {
		t.Fatalf("insert hidden message: %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE messages SET hidden = TRUE WHERE id = $1`, hiddenID); err != nil {
		t.Fatalf("mark hidden message hidden: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, msg1ID, 2, "user", "one", "2026-04-09 05:18:30.100000000 +0000"); err != nil {
		t.Fatalf("insert message one: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, msg2ID, 3, "user", "two", "2026-04-09 05:18:31.100000000 +0000"); err != nil {
		t.Fatalf("insert message two: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, msg3ID, 4, "assistant", "future assistant", "2026-04-09 05:18:29.100000000 +0000"); err != nil {
		t.Fatalf("insert future assistant: %v", err)
	}
	if err := insertDesktopReplacementPrefix(ctx, db, accountID, threadID, "future summary"); err != nil {
		t.Fatalf("insert replacement: %v", err)
	}

	loaded, err := loadRunInputs(ctx, db, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, nil, data.DesktopRunsRepository{}, data.DesktopRunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
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

func TestLoadRunInputsDesktopResolvesChannelHistoryUpperBoundFromLedger(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "bounded-ledger.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	hiddenID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()
	msg3ID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`, []any{accountID, "acc-" + accountID.String(), "acc"}},
		{`INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, 'p', 'private')`, []any{projectID, accountID}},
		{`INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`, []any{threadID, accountID, projectID}},
		{`INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, []any{runID, accountID, threadID}},
		{`INSERT INTO channels (id, account_id, channel_type, is_active, config_json) VALUES ($1, $2, 'telegram', 1, '{}')`, []any{channelID, accountID}},
		{`INSERT INTO channel_identities (id, channel_type, platform_subject_id, metadata) VALUES ($1, 'telegram', $2, '{}')`, []any{identityID, "chat-1-user"}},
	} {
		if _, err := db.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}
	runStarted := fmt.Sprintf(`{
		"channel_delivery":{
			"channel_id":"%s",
			"channel_type":"telegram",
			"sender_channel_identity_id":"%s",
			"conversation_type":"supergroup",
			"conversation_ref":{"target":"chat-1"},
			"trigger_message_ref":{"message_id":"m-2"},
			"inbound_message_ref":{"message_id":"m-2"}
		}
	}`, channelID, identityID)
	if _, err := db.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, runID, runStarted); err != nil {
		t.Fatalf("insert run started: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, hiddenID, 1, "assistant", "hidden", "2026-04-09 05:18:28.100000000 +0000"); err != nil {
		t.Fatalf("insert hidden message: %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE messages SET hidden = TRUE WHERE id = $1`, hiddenID); err != nil {
		t.Fatalf("mark hidden message hidden: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, msg1ID, 2, "user", "one", "2026-04-09 05:18:30.100000000 +0000"); err != nil {
		t.Fatalf("insert message one: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, msg2ID, 3, "user", "two", "2026-04-09 05:18:31.100000000 +0000"); err != nil {
		t.Fatalf("insert message two: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, msg3ID, 4, "assistant", "future assistant", "2026-04-09 05:18:29.100000000 +0000"); err != nil {
		t.Fatalf("insert future assistant: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO channel_message_ledger (channel_id, channel_type, direction, thread_id, platform_conversation_id, platform_message_id, sender_channel_identity_id, message_id, metadata_json, created_at) VALUES ($1, 'telegram', 'inbound', $2, 'chat-1', 'm-2', $3, $4, '{}', $5)`, channelID, threadID, identityID, msg2ID, "2026-04-09 05:18:31.200000000 +0000"); err != nil {
		t.Fatalf("insert ledger row: %v", err)
	}
	if err := insertDesktopReplacementPrefix(ctx, db, accountID, threadID, "future summary"); err != nil {
		t.Fatalf("insert replacement: %v", err)
	}

	loaded, err := loadRunInputs(ctx, db, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, nil, data.DesktopRunsRepository{}, data.DesktopRunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if got := loaded.InputJSON[runStartedThreadTailMessageIDKey]; got != msg2ID.String() {
		t.Fatalf("unexpected resolved thread tail: %#v", got)
	}
	if len(loaded.ThreadContextFrontier) == 0 || loaded.ThreadContextFrontier[0].SourceText != "future summary" {
		t.Fatalf("expected replacement prefix, got frontier=%#v", loaded.ThreadContextFrontier)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected 2 bounded prompt messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Content[0].Text != "future summary" ||
		loaded.Messages[1].Content[0].Text != "two" {
		t.Fatalf("unexpected bounded contents: %#v", loaded.Messages)
	}
	if len(loaded.ThreadMessageIDs) != 2 || loaded.ThreadMessageIDs[0] != uuid.Nil || loaded.ThreadMessageIDs[1] != msg2ID {
		t.Fatalf("unexpected bounded thread ids: %#v", loaded.ThreadMessageIDs)
	}
}

func TestLoadRunInputsDesktopSkipsSnapshotWhenChannelUpperBoundMissing(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "channel-no-upper-bound.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	hiddenID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()
	msg3ID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`, []any{accountID, "acc-" + accountID.String(), "acc"}},
		{`INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, 'p', 'private')`, []any{projectID, accountID}},
		{`INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`, []any{threadID, accountID, projectID}},
		{`INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, []any{runID, accountID, threadID}},
		{`INSERT INTO channels (id, account_id, channel_type, is_active, config_json) VALUES ($1, $2, 'telegram', 1, '{}')`, []any{channelID, accountID}},
		{`INSERT INTO channel_identities (id, channel_type, platform_subject_id, metadata) VALUES ($1, 'telegram', $2, '{}')`, []any{identityID, "chat-1-user"}},
	} {
		if _, err := db.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}
	runStarted := fmt.Sprintf(`{
		"channel_delivery":{
			"channel_id":"%s",
			"channel_type":"telegram",
			"sender_channel_identity_id":"%s",
			"conversation_type":"supergroup",
			"conversation_ref":{"target":"chat-1"},
			"trigger_message_ref":{"message_id":"missing"},
			"inbound_message_ref":{"message_id":"missing"}
		}
	}`, channelID, identityID)
	if _, err := db.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2::jsonb)`, runID, runStarted); err != nil {
		t.Fatalf("insert run started: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, hiddenID, 1, "assistant", "hidden", "2026-04-09 05:18:28.100000000 +0000"); err != nil {
		t.Fatalf("insert hidden message: %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE messages SET hidden = TRUE WHERE id = $1`, hiddenID); err != nil {
		t.Fatalf("mark hidden message hidden: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, msg1ID, 2, "user", "one", "2026-04-09 05:18:30.100000000 +0000"); err != nil {
		t.Fatalf("insert message one: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, msg2ID, 3, "user", "two", "2026-04-09 05:18:31.100000000 +0000"); err != nil {
		t.Fatalf("insert message two: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, msg3ID, 4, "assistant", "future assistant", "2026-04-09 05:18:29.100000000 +0000"); err != nil {
		t.Fatalf("insert future assistant: %v", err)
	}
	if err := insertDesktopReplacementPrefix(ctx, db, accountID, threadID, "future summary"); err != nil {
		t.Fatalf("insert replacement: %v", err)
	}

	loaded, err := loadRunInputs(ctx, db, data.Run{
		ID:        runID,
		AccountID: accountID,
		ThreadID:  threadID,
	}, nil, data.DesktopRunsRepository{}, data.DesktopRunEventsRepository{}, data.MessagesRepository{}, nil, nil, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(loaded.ThreadContextFrontier) == 0 || loaded.ThreadContextFrontier[0].SourceText != "future summary" {
		t.Fatalf("expected channel downgrade path to keep replacement prefix, got frontier=%#v", loaded.ThreadContextFrontier)
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("expected replacement plus visible history tail, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Content[0].Text != "future summary" ||
		loaded.Messages[1].Content[0].Text != "two" ||
		loaded.Messages[2].Content[0].Text != "future assistant" {
		t.Fatalf("unexpected downgrade contents: %#v", loaded.Messages)
	}
	if len(loaded.ThreadMessageIDs) != 3 || loaded.ThreadMessageIDs[0] != uuid.Nil {
		t.Fatalf("unexpected downgrade thread ids: %#v", loaded.ThreadMessageIDs)
	}
}

func insertDesktopThreadMessage(
	ctx context.Context,
	db *sqlitepgx.Pool,
	accountID uuid.UUID,
	threadID uuid.UUID,
	messageID uuid.UUID,
	threadSeq int64,
	role string,
	content string,
	createdAt string,
) error {
	_, err := db.Exec(
		ctx,
		`INSERT INTO messages (
			id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, '{}', FALSE, $7
		)`,
		messageID,
		accountID,
		threadID,
		threadSeq,
		role,
		content,
		createdAt,
	)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		ctx,
		`UPDATE threads
		    SET next_message_seq = CASE
		        WHEN next_message_seq <= $2 THEN $2 + 1
		        ELSE next_message_seq
		    END
		  WHERE id = $1`,
		threadID,
		threadSeq,
	)
	return err
}

func insertDesktopReplacementPrefix(
	ctx context.Context,
	db *sqlitepgx.Pool,
	accountID uuid.UUID,
	threadID uuid.UUID,
	summary string,
) error {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	graph, err := ensureCanonicalThreadGraphPersisted(ctx, tx, data.MessagesRepository{}, accountID, threadID)
	if err != nil {
		return err
	}
	if len(graph.Chunks) == 0 {
		return fmt.Errorf("no chunks available for replacement seed")
	}
	firstChunk := graph.Chunks[0]
	replacementsRepo := data.ThreadContextReplacementsRepository{}
	edgesRepo := data.ThreadContextSupersessionEdgesRepository{}
	replacement, err := replacementsRepo.Insert(ctx, tx, data.ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadID,
		StartThreadSeq:  firstChunk.StartThreadSeq,
		EndThreadSeq:    firstChunk.EndThreadSeq,
		StartContextSeq: firstChunk.ContextSeq,
		EndContextSeq:   firstChunk.ContextSeq,
		SummaryText:     summary,
		Layer:           1,
	})
	if err != nil {
		return err
	}
	chunkID := graph.ChunkRecordsByContextSeq[firstChunk.ContextSeq].ID
	if _, err := edgesRepo.Insert(ctx, tx, data.ThreadContextSupersessionEdgeInsertInput{
		AccountID:         accountID,
		ThreadID:          threadID,
		ReplacementID:     replacement.ID,
		SupersededChunkID: &chunkID,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
