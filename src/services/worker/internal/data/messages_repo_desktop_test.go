//go:build desktop

package data

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	defer readTx.Rollback(ctx) //nolint:errcheck

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
	defer readTx.Rollback(ctx) //nolint:errcheck

	rows, err := readTx.Query(ctx, `
		SELECT id, created_at
		  FROM messages
		 WHERE thread_id = $1
		 ORDER BY created_at ASC, id ASC
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
		INSERT INTO messages (id, account_id, thread_id, role, content, hidden, deleted_at, created_at)
		VALUES ($1, $2, $3, 'user', 'legacy', false, NULL, $4)
	`, legacyID, accountID, threadID, "2026-04-08 14:39:19"); err != nil {
		t.Fatalf("insert legacy message: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

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
	defer readTx.Rollback(ctx) //nolint:errcheck

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
