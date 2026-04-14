//go:build !desktop

package data

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestThreadContextAtomsRepositoryInsertAndList(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "thread_context_atoms_repo")
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
	repo := ThreadContextAtomsRepository{}
	if _, err := repo.Insert(ctx, tx, ProtocolAtomInsertInput{
		AccountID:             accountID,
		ThreadID:              threadID,
		AtomSeq:               1,
		AtomKind:              "user_text_atom",
		Role:                  "user",
		SourceMessageStartSeq: 1,
		SourceMessageEndSeq:   1,
		PayloadText:           "hello",
	}); err != nil {
		t.Fatalf("insert atom 1: %v", err)
	}
	if _, err := repo.Insert(ctx, tx, ProtocolAtomInsertInput{
		AccountID:             accountID,
		ThreadID:              threadID,
		AtomSeq:               2,
		AtomKind:              "assistant_text_atom",
		Role:                  "assistant",
		SourceMessageStartSeq: 2,
		SourceMessageEndSeq:   2,
		PayloadText:           "world",
	}); err != nil {
		t.Fatalf("insert atom 2: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	readTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }()

	upper := int64(1)
	items, err := repo.ListByThreadUpToAtomSeq(ctx, readTx, accountID, threadID, &upper)
	if err != nil {
		t.Fatalf("list atoms: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one atom, got %d", len(items))
	}
	if items[0].AtomSeq != 1 || items[0].PayloadText != "hello" {
		t.Fatalf("unexpected atom item: %#v", items[0])
	}
}
