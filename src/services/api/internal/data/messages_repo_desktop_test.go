//go:build desktop

package data_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/jackc/pgx/v5"
)

func TestListForkedThreadMessagesInDesktopMode(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	messageRepo, err := data.NewMessageRepository(pool)
	if err != nil {
		t.Fatalf("new message repo: %v", err)
	}

	project, err := projectRepo.GetOrCreateDefaultByOwner(ctx, auth.DesktopAccountID, auth.DesktopUserID)
	if err != nil {
		t.Fatalf("get or create default project: %v", err)
	}

	userID := auth.DesktopUserID
	thread, err := threadRepo.Create(ctx, auth.DesktopAccountID, &userID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := messageRepo.Create(ctx, auth.DesktopAccountID, thread.ID, "user", "before", &userID); err != nil {
		t.Fatalf("create first message: %v", err)
	}
	cutoff, err := messageRepo.Create(ctx, auth.DesktopAccountID, thread.ID, "assistant", "cutoff", &userID)
	if err != nil {
		t.Fatalf("create cutoff message: %v", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	txThreadRepo, err := data.NewThreadRepository(tx)
	if err != nil {
		t.Fatalf("new tx thread repo: %v", err)
	}
	txMessageRepo, err := data.NewMessageRepository(tx)
	if err != nil {
		t.Fatalf("new tx message repo: %v", err)
	}

	forked, err := txThreadRepo.Fork(ctx, auth.DesktopAccountID, &userID, thread.ID, cutoff.ID, false)
	if err != nil {
		t.Fatalf("fork thread: %v", err)
	}
	if _, err := txMessageRepo.CopyUpTo(ctx, auth.DesktopAccountID, thread.ID, forked.ID, cutoff.ID); err != nil {
		t.Fatalf("copy up to cutoff: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit fork tx: %v", err)
	}

	messages, err := messageRepo.ListByThread(ctx, auth.DesktopAccountID, forked.ID, 100)
	if err != nil {
		t.Fatalf("list forked messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 forked messages, got %d", len(messages))
	}
}

func TestCreateMessageDesktopWritesHighPrecisionCreatedAt(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	messageRepo, err := data.NewMessageRepository(pool)
	if err != nil {
		t.Fatalf("new message repo: %v", err)
	}

	project, err := projectRepo.GetOrCreateDefaultByOwner(ctx, auth.DesktopAccountID, auth.DesktopUserID)
	if err != nil {
		t.Fatalf("get or create default project: %v", err)
	}

	userID := auth.DesktopUserID
	thread, err := threadRepo.Create(ctx, auth.DesktopAccountID, &userID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	msg, err := messageRepo.Create(ctx, auth.DesktopAccountID, thread.ID, "user", "hello", &userID)
	if err != nil {
		t.Fatalf("create message: %v", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var createdAt string
	if err := tx.QueryRow(ctx, `SELECT created_at FROM messages WHERE id = $1`, msg.ID).Scan(&createdAt); err != nil {
		t.Fatalf("query created_at: %v", err)
	}
	if !strings.Contains(createdAt, ".") || !strings.Contains(createdAt, "+0000") {
		t.Fatalf("expected fixed-width high precision created_at, got %q", createdAt)
	}
}
