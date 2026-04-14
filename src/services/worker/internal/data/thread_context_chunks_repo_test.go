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

func TestThreadContextChunksRepositoryInsertListAndRange(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "thread_context_chunks_repo")
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
	atomsRepo := ThreadContextAtomsRepository{}
	atom, err := atomsRepo.Insert(ctx, tx, ProtocolAtomInsertInput{
		AccountID:             accountID,
		ThreadID:              threadID,
		AtomSeq:               1,
		AtomKind:              "user_text_atom",
		Role:                  "user",
		SourceMessageStartSeq: 1,
		SourceMessageEndSeq:   1,
		PayloadText:           "payload",
	})
	if err != nil {
		t.Fatalf("insert atom: %v", err)
	}

	repo := ThreadContextChunksRepository{}
	first, err := repo.Insert(ctx, tx, ContextChunkInsertInput{
		AccountID:   accountID,
		ThreadID:    threadID,
		AtomID:      atom.ID,
		ChunkSeq:    1,
		ContextSeq:  10,
		PayloadText: "a",
	})
	if err != nil {
		t.Fatalf("insert chunk 1: %v", err)
	}
	second, err := repo.Insert(ctx, tx, ContextChunkInsertInput{
		AccountID:   accountID,
		ThreadID:    threadID,
		AtomID:      atom.ID,
		ChunkSeq:    2,
		ContextSeq:  11,
		PayloadText: "b",
	})
	if err != nil {
		t.Fatalf("insert chunk 2: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	readTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }()

	upper := int64(10)
	items, err := repo.ListByThreadUpToContextSeq(ctx, readTx, accountID, threadID, &upper)
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	if len(items) != 1 || items[0].ID != first.ID {
		t.Fatalf("unexpected chunk list: %#v", items)
	}

	start, end, err := repo.GetContextSeqRangeForChunkIDs(ctx, readTx, accountID, threadID, []uuid.UUID{first.ID, second.ID})
	if err != nil {
		t.Fatalf("get context seq range: %v", err)
	}
	if start != 10 || end != 11 {
		t.Fatalf("unexpected range: %d-%d", start, end)
	}
}

func TestThreadContextChunksRepositoryRejectsCrossThreadAtom(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "thread_context_chunks_cross_thread")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadA := uuid.New()
	threadB := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, type) VALUES ($1, 'personal')`, accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`, projectID, accountID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	for _, threadID := range []uuid.UUID{threadA, threadB} {
		if _, err := pool.Exec(ctx, `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
			t.Fatalf("insert thread: %v", err)
		}
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	atomsRepo := ThreadContextAtomsRepository{}
	atom, err := atomsRepo.Insert(ctx, tx, ProtocolAtomInsertInput{
		AccountID:             accountID,
		ThreadID:              threadA,
		AtomSeq:               1,
		AtomKind:              "user_text_atom",
		Role:                  "user",
		SourceMessageStartSeq: 1,
		SourceMessageEndSeq:   1,
		PayloadText:           "payload",
	})
	if err != nil {
		t.Fatalf("insert atom: %v", err)
	}

	repo := ThreadContextChunksRepository{}
	if _, err := repo.Insert(ctx, tx, ContextChunkInsertInput{
		AccountID:   accountID,
		ThreadID:    threadB,
		AtomID:      atom.ID,
		ChunkSeq:    1,
		ContextSeq:  1,
		PayloadText: "bad",
	}); err == nil {
		t.Fatal("expected cross-thread atom insert to fail")
	}
}
