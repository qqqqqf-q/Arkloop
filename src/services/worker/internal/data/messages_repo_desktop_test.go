//go:build desktop

package data

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestMessagesRepository_ListByThreadDesktop_joinsOutputTokens(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "msg.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()

	seedDesktopAccount(t, pool, accountID)
	seedDesktopProject(t, pool, accountID, projectID)
	seedDesktopThread(t, pool, accountID, projectID, threadID)
	seedDesktopRun(t, pool, accountID, threadID, runID, nil)

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	repo := MessagesRepository{}
	if err := (UsageRecordsRepository{}).Insert(ctx, tx, accountID, runID, "test-model", 10, 4242, 0, 0, 0, 0.01); err != nil {
		t.Fatalf("usage insert: %v", err)
	}
	if _, err = repo.InsertAssistantMessage(ctx, tx, accountID, threadID, runID, "assistant body", nil, false); err != nil {
		t.Fatalf("insert assistant: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	readTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }() //nolint:errcheck

	msgs, err := repo.ListByThread(ctx, readTx, accountID, threadID, 50)
	if err != nil {
		t.Fatalf("list by thread: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Fatalf("expected assistant, got %q", msgs[0].Role)
	}
	if msgs[0].OutputTokens == nil || *msgs[0].OutputTokens != 4242 {
		t.Fatalf("expected output_tokens 4242, got %#v", msgs[0].OutputTokens)
	}
}

func TestMessagesRepository_DesktopWritesHighPrecisionCreatedAt(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "msg.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()

	seedDesktopAccount(t, pool, accountID)
	seedDesktopProject(t, pool, accountID, projectID)
	seedDesktopThread(t, pool, accountID, projectID, threadID)
	seedDesktopRun(t, pool, accountID, threadID, runID, nil)

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	repo := MessagesRepository{}
	firstID, err := repo.InsertThreadMessage(ctx, tx, accountID, threadID, "user", "first", nil, nil)
	if err != nil {
		t.Fatalf("insert first user: %v", err)
	}
	secondID, err := repo.InsertAssistantMessage(ctx, tx, accountID, threadID, runID, "assistant", nil, false)
	if err != nil {
		t.Fatalf("insert assistant: %v", err)
	}
	thirdID, err := repo.InsertThreadMessage(ctx, tx, accountID, threadID, "user", "second", nil, nil)
	if err != nil {
		t.Fatalf("insert second user: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	readTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }() //nolint:errcheck

	rows, err := readTx.Query(ctx, `
		SELECT id, created_at
		  FROM messages
		 WHERE thread_id = $1
		 ORDER BY thread_seq ASC
	`, threadID)
	if err != nil {
		t.Fatalf("query raw created_at: %v", err)
	}
	defer rows.Close()

	rawCreatedAt := make(map[uuid.UUID]string, 3)
	for rows.Next() {
		var id uuid.UUID
		var createdAt string
		if err := rows.Scan(&id, &createdAt); err != nil {
			t.Fatalf("scan raw created_at: %v", err)
		}
		rawCreatedAt[id] = createdAt
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}

	for _, id := range []uuid.UUID{firstID, secondID, thirdID} {
		createdAt := rawCreatedAt[id]
		if !strings.Contains(createdAt, ".") || !strings.Contains(createdAt, "+0000") {
			t.Fatalf("expected fixed-width high precision created_at for %s, got %q", id, createdAt)
		}
	}

	msgs, err := repo.ListByThread(ctx, readTx, accountID, threadID, 50)
	if err != nil {
		t.Fatalf("list by thread: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].ID != firstID || msgs[1].ID != secondID || msgs[2].ID != thirdID {
		t.Fatalf("unexpected message order: got %s %s %s", msgs[0].ID, msgs[1].ID, msgs[2].ID)
	}
}

func TestMessagesRepository_DesktopSortsMixedOldAndNewTimestampFormats(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "mixed.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()

	seedDesktopAccount(t, pool, accountID)
	seedDesktopProject(t, pool, accountID, projectID)
	seedDesktopThread(t, pool, accountID, projectID, threadID)
	seedDesktopRun(t, pool, accountID, threadID, runID, nil)

	legacyID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, hidden, deleted_at, created_at)
		VALUES ($1, $2, $3, 1, 'user', 'legacy', false, NULL, $4)
	`, legacyID, accountID, threadID, "2026-04-08 14:39:19"); err != nil {
		t.Fatalf("insert legacy message: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE threads SET next_message_seq = 2 WHERE id = $1`, threadID); err != nil {
		t.Fatalf("bump next_message_seq: %v", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	repo := MessagesRepository{}
	newID, err := repo.InsertAssistantMessage(ctx, tx, accountID, threadID, runID, "new", nil, false)
	if err != nil {
		t.Fatalf("insert new assistant: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	readTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }() //nolint:errcheck

	msgs, err := repo.ListByThread(ctx, readTx, accountID, threadID, 50)
	if err != nil {
		t.Fatalf("list by thread: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].ID != legacyID || msgs[1].ID != newID {
		t.Fatalf("unexpected mixed-format order: got %s then %s", msgs[0].ID, msgs[1].ID)
	}
}

func TestMessagesRepository_ListByThreadDesktopIncludesBooleanIntermediateHistory(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "intermediate-bool.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.NewString()

	seedDesktopAccount(t, pool, accountID)
	seedDesktopProject(t, pool, accountID, projectID)
	seedDesktopThread(t, pool, accountID, projectID, threadID)

	if err := insertDesktopRepoMessage(ctx, pool, accountID, threadID, uuid.New(), 1, "user", "before", `{}`, false, ""); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := insertDesktopRepoMessage(ctx, pool, accountID, threadID, uuid.New(), 2, "assistant", "", `{"run_id":"`+runID+`","intermediate":true}`, true, ""); err != nil {
		t.Fatalf("insert assistant intermediate: %v", err)
	}
	if err := insertDesktopRepoMessage(ctx, pool, accountID, threadID, uuid.New(), 3, "tool", `{"tool_call_id":"call_1","tool_name":"exec_command","result":{"ok":true}}`, `{"run_id":"`+runID+`","intermediate":true,"tool_call_id":"call_1"}`, true, ""); err != nil {
		t.Fatalf("insert tool intermediate: %v", err)
	}
	if err := insertDesktopRepoMessage(ctx, pool, accountID, threadID, uuid.New(), 4, "assistant", "after", `{"run_id":"`+runID+`"}`, false, ""); err != nil {
		t.Fatalf("insert final assistant: %v", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	msgs, err := (MessagesRepository{}).ListByThread(ctx, tx, accountID, threadID, 50)
	if err != nil {
		t.Fatalf("list by thread: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected visible history plus boolean intermediate pair, got %#v", msgs)
	}
	if msgs[1].Role != "assistant" || msgs[2].Role != "tool" || msgs[3].Role != "assistant" {
		t.Fatalf("unexpected message order: %#v", msgs)
	}
}

func TestMessagesRepository_ListByThreadDesktopSkipsRolledBackIntermediateHistory(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "rolled-back.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.NewString()

	seedDesktopAccount(t, pool, accountID)
	seedDesktopProject(t, pool, accountID, projectID)
	seedDesktopThread(t, pool, accountID, projectID, threadID)

	if err := insertDesktopRepoMessage(ctx, pool, accountID, threadID, uuid.New(), 1, "user", "before", `{}`, false, ""); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := insertDesktopRepoMessage(ctx, pool, accountID, threadID, uuid.New(), 2, "assistant", "tool call", `{"run_id":"`+runID+`","intermediate":"true"}`, true, ""); err != nil {
		t.Fatalf("insert intermediate: %v", err)
	}
	if err := insertDesktopRepoMessage(ctx, pool, accountID, threadID, uuid.New(), 3, "assistant", "rolled back final", `{"run_id":"`+runID+`"}`, true, "2026-04-10 05:35:00.000000000 +0000"); err != nil {
		t.Fatalf("insert rolled back final: %v", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	msgs, err := (MessagesRepository{}).ListByThread(ctx, tx, accountID, threadID, 50)
	if err != nil {
		t.Fatalf("list by thread: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "before" {
		t.Fatalf("expected only stable visible history, got %#v", msgs)
	}
}

func TestMessagesRepository_ListRawByThreadDesktopIncludesHiddenAndCompacted(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "raw.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()

	seedDesktopAccount(t, pool, accountID)
	seedDesktopProject(t, pool, accountID, projectID)
	seedDesktopThread(t, pool, accountID, projectID, threadID)

	if err := insertDesktopRepoMessage(ctx, pool, accountID, threadID, uuid.New(), 1, "user", "visible", `{}`, false, ""); err != nil {
		t.Fatalf("insert visible: %v", err)
	}
	if err := insertDesktopRepoMessage(ctx, pool, accountID, threadID, uuid.New(), 2, "assistant", "hidden", `{}`, true, ""); err != nil {
		t.Fatalf("insert hidden: %v", err)
	}
	if err := insertDesktopRepoMessage(ctx, pool, accountID, threadID, uuid.New(), 3, "assistant", "compacted", `{}`, true, ""); err != nil {
		t.Fatalf("insert compacted: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE messages SET compacted = TRUE WHERE content = 'compacted'`); err != nil {
		t.Fatalf("mark compacted: %v", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	msgs, err := (MessagesRepository{}).ListRawByThread(ctx, tx, accountID, threadID, 50)
	if err != nil {
		t.Fatalf("list raw by thread: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 raw messages, got %#v", msgs)
	}
	if msgs[1].Content != "hidden" || msgs[2].Content != "compacted" {
		t.Fatalf("unexpected raw ordering: %#v", msgs)
	}
}

func TestMessagesRepository_ListByThreadDesktopWithoutLimitLoadsFullVisibleHistory(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "unbounded.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()

	seedDesktopAccount(t, pool, accountID)
	seedDesktopProject(t, pool, accountID, projectID)
	seedDesktopThread(t, pool, accountID, projectID, threadID)

	for seq := int64(1); seq <= 205; seq++ {
		if err := insertDesktopRepoMessage(ctx, pool, accountID, threadID, uuid.New(), seq, "user", fmt.Sprintf("msg-%03d", seq), `{}`, false, ""); err != nil {
			t.Fatalf("insert message %d: %v", seq, err)
		}
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	msgs, err := (MessagesRepository{}).ListByThread(ctx, tx, accountID, threadID, 0)
	if err != nil {
		t.Fatalf("list by thread: %v", err)
	}
	if len(msgs) != 205 {
		t.Fatalf("expected full visible history, got %d", len(msgs))
	}
	if msgs[0].Content != "msg-001" || msgs[len(msgs)-1].Content != "msg-205" {
		t.Fatalf("unexpected ordering: first=%q last=%q", msgs[0].Content, msgs[len(msgs)-1].Content)
	}
}

func insertDesktopRepoMessage(
	ctx context.Context,
	pool *sqlitepgx.Pool,
	accountID uuid.UUID,
	threadID uuid.UUID,
	messageID uuid.UUID,
	threadSeq int64,
	role string,
	content string,
	metadataJSON string,
	hidden bool,
	deletedAt string,
) error {
	var deletedAtArg any
	if strings.TrimSpace(deletedAt) != "" {
		deletedAtArg = deletedAt
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO messages (
			id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden, deleted_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)`,
		messageID,
		accountID,
		threadID,
		threadSeq,
		role,
		content,
		metadataJSON,
		hidden,
		deletedAtArg,
	); err != nil {
		return err
	}
	_, err := pool.Exec(
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
