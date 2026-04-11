//go:build !desktop

package data

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExternalThreadLinksRepositoryUpsertAndGet(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "external_thread_links_repo")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, projectID, accountID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}

	repo := ExternalThreadLinksRepository{}
	if err := repo.Upsert(ctx, pool, accountID, threadID, "nowledge", "remote-thread-1"); err != nil {
		t.Fatalf("upsert first: %v", err)
	}

	got, found, err := repo.Get(ctx, pool, accountID, threadID, "nowledge")
	if err != nil {
		t.Fatalf("get first: %v", err)
	}
	if !found || got != "remote-thread-1" {
		t.Fatalf("unexpected first link: found=%v value=%q", found, got)
	}

	if err := repo.Upsert(ctx, pool, accountID, threadID, "nowledge", "remote-thread-2"); err != nil {
		t.Fatalf("upsert second: %v", err)
	}

	got, found, err = repo.Get(ctx, pool, accountID, threadID, "nowledge")
	if err != nil {
		t.Fatalf("get second: %v", err)
	}
	if !found || got != "remote-thread-2" {
		t.Fatalf("unexpected updated link: found=%v value=%q", found, got)
	}
}
