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

func TestThreadContextReplacementsRepositoryContextSeqCompatibility(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "thread_context_replacements_context_seq")
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
	repo := ThreadContextReplacementsRepository{}
	inserted, err := repo.Insert(ctx, tx, ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadID,
		StartContextSeq: 11,
		EndContextSeq:   20,
		SummaryText:     "summary",
		Layer:           2,
	})
	if err != nil {
		t.Fatalf("insert replacement: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if inserted.StartThreadSeq != 11 || inserted.EndThreadSeq != 20 {
		t.Fatalf("expected legacy thread seq fallback from context seq, got %#v", inserted)
	}

	readTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }()

	upperBound := int64(15)
	items, err := repo.ListActiveByThreadUpToContextSeq(ctx, readTx, accountID, threadID, &upperBound)
	if err != nil {
		t.Fatalf("list by upper bound: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected replacement excluded when end_context_seq exceeds upper bound, got %#v", items)
	}

	upperBound = 20
	items, err = repo.ListActiveByThreadUpToContextSeq(ctx, readTx, accountID, threadID, &upperBound)
	if err != nil {
		t.Fatalf("list by upper bound full: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one replacement, got %d", len(items))
	}
	if items[0].StartContextSeq != 11 || items[0].EndContextSeq != 20 {
		t.Fatalf("unexpected context seq range: %#v", items[0])
	}
}

func TestThreadContextReplacementsRepositoryListByThreadSeqUsesThreadRange(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "thread_context_replacements_thread_seq_bound")
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
	repo := ThreadContextReplacementsRepository{}
	if _, err := repo.Insert(ctx, tx, ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadID,
		StartThreadSeq:  1,
		EndThreadSeq:    1,
		StartContextSeq: 1,
		EndContextSeq:   10,
		SummaryText:     "summary",
	}); err != nil {
		t.Fatalf("insert replacement: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	readTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }()

	upperBoundThreadSeq := int64(1)
	items, err := repo.ListActiveByThreadUpToSeq(ctx, readTx, accountID, threadID, &upperBoundThreadSeq)
	if err != nil {
		t.Fatalf("list by thread seq: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected replacement visible under thread seq bound, got %#v", items)
	}
}

func TestThreadContextReplacementsRepositorySupersedeByContextSeq(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "thread_context_replacements_supersede")
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
	repo := ThreadContextReplacementsRepository{}
	a, err := repo.Insert(ctx, tx, ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadID,
		StartContextSeq: 1,
		EndContextSeq:   10,
		SummaryText:     "a",
	})
	if err != nil {
		t.Fatalf("insert replacement a: %v", err)
	}
	b, err := repo.Insert(ctx, tx, ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadID,
		StartContextSeq: 8,
		EndContextSeq:   20,
		SummaryText:     "b",
	})
	if err != nil {
		t.Fatalf("insert replacement b: %v", err)
	}
	if err := repo.SupersedeActiveOverlapsByContextSeq(ctx, tx, accountID, threadID, b.StartContextSeq, b.EndContextSeq, b.ID); err != nil {
		t.Fatalf("supersede overlaps: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	readTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }()

	var supersededAt *string
	if err := readTx.QueryRow(ctx, `SELECT superseded_at::text FROM thread_context_replacements WHERE id = $1`, a.ID).Scan(&supersededAt); err != nil {
		t.Fatalf("load superseded_at: %v", err)
	}
	if supersededAt == nil || *supersededAt == "" {
		t.Fatal("expected overlapping replacement to be superseded")
	}
}

func TestThreadContextReplacementsRepositoryListByThreadSeqUsesThreadBounds(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "thread_context_replacements_thread_bound")
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
	repo := ThreadContextReplacementsRepository{}
	if _, err := repo.Insert(ctx, tx, ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadID,
		StartThreadSeq:  1,
		EndThreadSeq:    1,
		StartContextSeq: 1,
		EndContextSeq:   4,
		SummaryText:     "single message split into chunks",
		Layer:           1,
	}); err != nil {
		t.Fatalf("insert replacement: %v", err)
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
	items, err := repo.ListActiveByThreadUpToSeq(ctx, readTx, accountID, threadID, &upper)
	if err != nil {
		t.Fatalf("list by thread upper bound: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected replacement visible by thread upper bound, got %#v", items)
	}
}
