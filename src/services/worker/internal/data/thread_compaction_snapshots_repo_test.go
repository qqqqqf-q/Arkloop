//go:build !desktop

package data

import (
	"context"
	"encoding/json"
	"testing"

	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestThreadCompactionSnapshotsRepositoryReplaceActive(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "thread_compaction_snapshots_repo")
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

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	repo := ThreadCompactionSnapshotsRepository{}
	firstMeta := json.RawMessage(`{"kind":"context_compact"}`)
	first, err := repo.ReplaceActive(ctx, tx, accountID, threadID, "first summary", firstMeta)
	if err != nil {
		t.Fatalf("replace active first: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit first: %v", err)
	}

	if first == nil || !first.IsActive {
		t.Fatalf("expected active first snapshot, got %#v", first)
	}
	if first.SupersedesSnapshotID != nil {
		t.Fatalf("did not expect supersedes on first snapshot: %#v", first.SupersedesSnapshotID)
	}

	tx, err = pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx second: %v", err)
	}
	secondMeta := json.RawMessage(`{"kind":"context_compact","round":2}`)
	second, err := repo.ReplaceActive(ctx, tx, accountID, threadID, "second summary", secondMeta)
	if err != nil {
		t.Fatalf("replace active second: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit second: %v", err)
	}

	if second == nil || !second.IsActive {
		t.Fatalf("expected active second snapshot, got %#v", second)
	}
	if second.SupersedesSnapshotID == nil || *second.SupersedesSnapshotID != first.ID {
		t.Fatalf("expected second to supersede first, got %#v", second.SupersedesSnapshotID)
	}

	readTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer readTx.Rollback(ctx)

	active, err := repo.GetActiveByThread(ctx, readTx, accountID, threadID)
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if active == nil || active.ID != second.ID {
		t.Fatalf("unexpected active snapshot: %#v", active)
	}

	var activeCount int
	if err := readTx.QueryRow(ctx, `SELECT COUNT(*) FROM thread_compaction_snapshots WHERE thread_id = $1 AND is_active = TRUE`, threadID).Scan(&activeCount); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("expected 1 active snapshot, got %d", activeCount)
	}

	var firstActive bool
	if err := readTx.QueryRow(ctx, `SELECT is_active FROM thread_compaction_snapshots WHERE id = $1`, first.ID).Scan(&firstActive); err != nil {
		t.Fatalf("load first active flag: %v", err)
	}
	if firstActive {
		t.Fatal("expected first snapshot to be inactive")
	}
}
