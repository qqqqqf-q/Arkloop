//go:build desktop

package data

import (
	"context"
	"path/filepath"
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
