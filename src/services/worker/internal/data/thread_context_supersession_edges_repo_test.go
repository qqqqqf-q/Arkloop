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

func TestThreadContextSupersessionEdgesRepositoryInsertAndList(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "thread_context_supersession_edges_repo")
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
	replacementsRepo := ThreadContextReplacementsRepository{}
	base, err := replacementsRepo.Insert(ctx, tx, ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadID,
		StartContextSeq: 1,
		EndContextSeq:   5,
		SummaryText:     "base",
	})
	if err != nil {
		t.Fatalf("insert base replacement: %v", err)
	}
	parent, err := replacementsRepo.Insert(ctx, tx, ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadID,
		StartContextSeq: 1,
		EndContextSeq:   10,
		SummaryText:     "parent",
		Layer:           2,
	})
	if err != nil {
		t.Fatalf("insert parent replacement: %v", err)
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
		PayloadText:           "x",
	})
	if err != nil {
		t.Fatalf("insert atom: %v", err)
	}
	chunksRepo := ThreadContextChunksRepository{}
	chunk, err := chunksRepo.Insert(ctx, tx, ContextChunkInsertInput{
		AccountID:   accountID,
		ThreadID:    threadID,
		AtomID:      atom.ID,
		ChunkSeq:    1,
		ContextSeq:  1,
		PayloadText: "chunk",
	})
	if err != nil {
		t.Fatalf("insert chunk: %v", err)
	}

	edgesRepo := ThreadContextSupersessionEdgesRepository{}
	if _, err := edgesRepo.Insert(ctx, tx, ThreadContextSupersessionEdgeInsertInput{
		AccountID:               accountID,
		ThreadID:                threadID,
		ReplacementID:           parent.ID,
		SupersededReplacementID: &base.ID,
	}); err != nil {
		t.Fatalf("insert replacement edge: %v", err)
	}
	if _, err := edgesRepo.Insert(ctx, tx, ThreadContextSupersessionEdgeInsertInput{
		AccountID:         accountID,
		ThreadID:          threadID,
		ReplacementID:     parent.ID,
		SupersededChunkID: &chunk.ID,
	}); err != nil {
		t.Fatalf("insert chunk edge: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	readTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }()

	items, err := edgesRepo.ListByReplacementID(ctx, readTx, accountID, threadID, parent.ID)
	if err != nil {
		t.Fatalf("list edges: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(items))
	}
}

func TestThreadContextSupersessionEdgesRepositoryRejectsCrossThreadTargets(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "thread_context_supersession_edges_cross_thread")
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

	replacementsRepo := ThreadContextReplacementsRepository{}
	replacementA, err := replacementsRepo.Insert(ctx, tx, ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadA,
		StartContextSeq: 1,
		EndContextSeq:   1,
		SummaryText:     "a",
	})
	if err != nil {
		t.Fatalf("insert replacement A: %v", err)
	}
	replacementB, err := replacementsRepo.Insert(ctx, tx, ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadB,
		StartContextSeq: 1,
		EndContextSeq:   1,
		SummaryText:     "b",
	})
	if err != nil {
		t.Fatalf("insert replacement B: %v", err)
	}

	edgesRepo := ThreadContextSupersessionEdgesRepository{}
	if _, err := edgesRepo.Insert(ctx, tx, ThreadContextSupersessionEdgeInsertInput{
		AccountID:               accountID,
		ThreadID:                threadA,
		ReplacementID:           replacementA.ID,
		SupersededReplacementID: &replacementB.ID,
	}); err == nil {
		t.Fatal("expected cross-thread replacement edge insert to fail")
	}
}

func TestThreadContextSupersessionEdgesRepositoryRejectsCrossThreadChunk(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "thread_context_supersession_edges_owner")
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

	replacementsRepo := ThreadContextReplacementsRepository{}
	parent, err := replacementsRepo.Insert(ctx, tx, ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadA,
		StartContextSeq: 1,
		EndContextSeq:   2,
		SummaryText:     "parent",
	})
	if err != nil {
		t.Fatalf("insert parent replacement: %v", err)
	}
	atomsRepo := ThreadContextAtomsRepository{}
	atom, err := atomsRepo.Insert(ctx, tx, ProtocolAtomInsertInput{
		AccountID:             accountID,
		ThreadID:              threadB,
		AtomSeq:               1,
		AtomKind:              "user_text_atom",
		Role:                  "user",
		SourceMessageStartSeq: 1,
		SourceMessageEndSeq:   1,
		PayloadText:           "x",
	})
	if err != nil {
		t.Fatalf("insert atom: %v", err)
	}
	chunksRepo := ThreadContextChunksRepository{}
	chunk, err := chunksRepo.Insert(ctx, tx, ContextChunkInsertInput{
		AccountID:   accountID,
		ThreadID:    threadB,
		AtomID:      atom.ID,
		ChunkSeq:    1,
		ContextSeq:  1,
		PayloadText: "chunk",
	})
	if err != nil {
		t.Fatalf("insert chunk: %v", err)
	}

	edgesRepo := ThreadContextSupersessionEdgesRepository{}
	if _, err := edgesRepo.Insert(ctx, tx, ThreadContextSupersessionEdgeInsertInput{
		AccountID:         accountID,
		ThreadID:          threadA,
		ReplacementID:     parent.ID,
		SupersededChunkID: &chunk.ID,
	}); err == nil {
		t.Fatal("expected cross-thread chunk edge insert to fail")
	}
}

func TestThreadContextSupersessionEdgesRepositoryDeleteBySupersededChunkIDs(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "thread_context_supersession_edges_delete_chunks")
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
	defer func() { _ = tx.Rollback(ctx) }()

	replacementsRepo := ThreadContextReplacementsRepository{}
	base, err := replacementsRepo.Insert(ctx, tx, ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadID,
		StartContextSeq: 1,
		EndContextSeq:   1,
		SummaryText:     "base",
	})
	if err != nil {
		t.Fatalf("insert base replacement: %v", err)
	}
	parent, err := replacementsRepo.Insert(ctx, tx, ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadID,
		StartContextSeq: 1,
		EndContextSeq:   2,
		SummaryText:     "parent",
		Layer:           1,
	})
	if err != nil {
		t.Fatalf("insert parent replacement: %v", err)
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
	chunksRepo := ThreadContextChunksRepository{}
	chunkA, err := chunksRepo.Insert(ctx, tx, ContextChunkInsertInput{
		AccountID:   accountID,
		ThreadID:    threadID,
		AtomID:      atom.ID,
		ChunkSeq:    1,
		ContextSeq:  1,
		PayloadText: "a",
	})
	if err != nil {
		t.Fatalf("insert chunk A: %v", err)
	}
	chunkB, err := chunksRepo.Insert(ctx, tx, ContextChunkInsertInput{
		AccountID:   accountID,
		ThreadID:    threadID,
		AtomID:      atom.ID,
		ChunkSeq:    2,
		ContextSeq:  2,
		PayloadText: "b",
	})
	if err != nil {
		t.Fatalf("insert chunk B: %v", err)
	}

	edgesRepo := ThreadContextSupersessionEdgesRepository{}
	if _, err := edgesRepo.Insert(ctx, tx, ThreadContextSupersessionEdgeInsertInput{
		AccountID:         accountID,
		ThreadID:          threadID,
		ReplacementID:     parent.ID,
		SupersededChunkID: &chunkA.ID,
	}); err != nil {
		t.Fatalf("insert chunk edge A: %v", err)
	}
	if _, err := edgesRepo.Insert(ctx, tx, ThreadContextSupersessionEdgeInsertInput{
		AccountID:         accountID,
		ThreadID:          threadID,
		ReplacementID:     parent.ID,
		SupersededChunkID: &chunkB.ID,
	}); err != nil {
		t.Fatalf("insert chunk edge B: %v", err)
	}
	if _, err := edgesRepo.Insert(ctx, tx, ThreadContextSupersessionEdgeInsertInput{
		AccountID:               accountID,
		ThreadID:                threadID,
		ReplacementID:           parent.ID,
		SupersededReplacementID: &base.ID,
	}); err != nil {
		t.Fatalf("insert replacement edge: %v", err)
	}

	if err := edgesRepo.DeleteBySupersededChunkIDs(ctx, tx, accountID, threadID, []uuid.UUID{chunkA.ID}); err != nil {
		t.Fatalf("delete by superseded chunk ids: %v", err)
	}

	items, err := edgesRepo.ListByReplacementID(ctx, tx, accountID, threadID, parent.ID)
	if err != nil {
		t.Fatalf("list edges after delete: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 remaining edges, got %d", len(items))
	}
	var hasChunkB bool
	var hasReplacement bool
	for _, item := range items {
		if item.SupersededChunkID != nil && *item.SupersededChunkID == chunkB.ID {
			hasChunkB = true
		}
		if item.SupersededReplacementID != nil && *item.SupersededReplacementID == base.ID {
			hasReplacement = true
		}
		if item.SupersededChunkID != nil && *item.SupersededChunkID == chunkA.ID {
			t.Fatalf("stale chunk edge should be deleted for chunk %s", chunkA.ID)
		}
	}
	if !hasChunkB {
		t.Fatalf("expected chunk B edge to remain")
	}
	if !hasReplacement {
		t.Fatalf("expected replacement edge to remain")
	}
}
