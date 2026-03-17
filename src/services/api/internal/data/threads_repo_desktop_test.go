//go:build desktop

package data_test

import (
	"context"
	"path/filepath"
	"testing"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/jackc/pgx/v5"
)

func TestThreadForkCopiesMessagesInDesktopMode(t *testing.T) {
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

	first, err := messageRepo.Create(ctx, auth.DesktopAccountID, thread.ID, "user", "before", &userID)
	if err != nil {
		t.Fatalf("create first message: %v", err)
	}
	cutoff, err := messageRepo.Create(ctx, auth.DesktopAccountID, thread.ID, "assistant", "cutoff", &userID)
	if err != nil {
		t.Fatalf("create cutoff message: %v", err)
	}
	after, err := messageRepo.Create(ctx, auth.DesktopAccountID, thread.ID, "user", "after", &userID)
	if err != nil {
		t.Fatalf("create trailing message: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`UPDATE messages
		 SET created_at = CASE id
		   WHEN $1 THEN '2026-03-17 04:22:01'
		   WHEN $2 THEN '2026-03-17 04:22:02'
		   WHEN $3 THEN '2026-03-17 04:22:03'
		 END
		 WHERE id IN ($1, $2, $3)`,
		first.ID,
		cutoff.ID,
		after.ID,
	); err != nil {
		t.Fatalf("pin message timestamps: %v", err)
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

	pairs, err := txMessageRepo.CopyUpTo(ctx, auth.DesktopAccountID, thread.ID, forked.ID, cutoff.ID)
	if err != nil {
		t.Fatalf("copy up to cutoff: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("expected 2 copied messages, got %d", len(pairs))
	}
}
