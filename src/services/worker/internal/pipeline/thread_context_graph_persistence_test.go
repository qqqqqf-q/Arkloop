package pipeline

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestEnsureCanonicalThreadGraphPersisted_ReorderShrinkKeepsGraphAligned(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "thread_context_graph_reorder_shrink")
	conn, err := pgx.Connect(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	defer conn.Close(context.Background())

	accountID := uuid.New()
	threadID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()

	if _, err := conn.Exec(context.Background(),
		`INSERT INTO threads (id, account_id, next_message_seq) VALUES ($1, $2, 10)`,
		threadID, accountID,
	); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := conn.Exec(context.Background(),
		`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
		 VALUES ($1, $2, $3, 1, 'user', $4, '{}'::jsonb, false),
		        ($5, $2, $3, 2, 'assistant', $6, '{}'::jsonb, false)`,
		msg1ID, accountID, threadID, "alpha\n\nbeta", msg2ID, "tail",
	); err != nil {
		t.Fatalf("insert messages: %v", err)
	}

	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }() //nolint:errcheck

	messagesRepo := data.MessagesRepository{}
	firstGraph, err := ensureCanonicalThreadGraphPersisted(context.Background(), tx, messagesRepo, accountID, threadID)
	if err != nil {
		t.Fatalf("first persist graph: %v", err)
	}
	if len(firstGraph.Chunks) != 3 {
		t.Fatalf("expected first graph with 3 chunks, got %d", len(firstGraph.Chunks))
	}

	if _, err := tx.Exec(context.Background(),
		`UPDATE messages SET content = $4 WHERE id = $1 AND account_id = $2 AND thread_id = $3`,
		msg1ID, accountID, threadID, "alpha",
	); err != nil {
		t.Fatalf("shrink first message: %v", err)
	}

	secondGraph, err := ensureCanonicalThreadGraphPersisted(context.Background(), tx, messagesRepo, accountID, threadID)
	if err != nil {
		t.Fatalf("second persist graph after reorder/shrink: %v", err)
	}

	latestMessages, err := messagesRepo.ListRawByThread(context.Background(), tx, accountID, threadID, canonicalPersistFetchLimit)
	if err != nil {
		t.Fatalf("list latest messages: %v", err)
	}
	expectedAtoms, expectedChunks := buildCanonicalAtomGraph(latestMessages)
	assertCanonicalPersistenceMatchesCurrentGraph(t, context.Background(), tx, accountID, threadID, secondGraph, expectedAtoms, expectedChunks)
}

func TestEnsureCanonicalThreadGraphPersisted_RemovesSupersessionEdgesBeforeDeletingStaleChunks(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "thread_context_graph_stale_chunk_edge_cleanup")
	conn, err := pgx.Connect(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	defer conn.Close(context.Background())

	accountID := uuid.New()
	threadID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()

	if _, err := conn.Exec(
		context.Background(),
		`INSERT INTO threads (id, account_id, next_message_seq) VALUES ($1, $2, 10)`,
		threadID, accountID,
	); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := conn.Exec(
		context.Background(),
		`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
		 VALUES ($1, $2, $3, 1, 'user', $4, '{}'::jsonb, false),
		        ($5, $2, $3, 2, 'assistant', $6, '{}'::jsonb, false)`,
		msg1ID, accountID, threadID, "alpha\n\nbeta", msg2ID, "tail",
	); err != nil {
		t.Fatalf("insert messages: %v", err)
	}

	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }() //nolint:errcheck

	messagesRepo := data.MessagesRepository{}
	firstGraph, err := ensureCanonicalThreadGraphPersisted(context.Background(), tx, messagesRepo, accountID, threadID)
	if err != nil {
		t.Fatalf("first persist graph: %v", err)
	}
	staleChunk := firstGraph.ChunkRecordsByContextSeq[2]
	if staleChunk == nil {
		t.Fatal("expected stale chunk at context_seq=2")
	}

	replacementsRepo := data.ThreadContextReplacementsRepository{}
	replacement, err := replacementsRepo.Insert(context.Background(), tx, data.ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadID,
		StartContextSeq: 1,
		EndContextSeq:   2,
		SummaryText:     "summary",
	})
	if err != nil {
		t.Fatalf("insert replacement: %v", err)
	}
	edgesRepo := data.ThreadContextSupersessionEdgesRepository{}
	if _, err := edgesRepo.Insert(context.Background(), tx, data.ThreadContextSupersessionEdgeInsertInput{
		AccountID:         accountID,
		ThreadID:          threadID,
		ReplacementID:     replacement.ID,
		SupersededChunkID: &staleChunk.ID,
	}); err != nil {
		t.Fatalf("insert supersession edge: %v", err)
	}

	if _, err := tx.Exec(
		context.Background(),
		`UPDATE messages SET content = $4 WHERE id = $1 AND account_id = $2 AND thread_id = $3`,
		msg1ID, accountID, threadID, "alpha",
	); err != nil {
		t.Fatalf("shrink first message: %v", err)
	}

	secondGraph, err := ensureCanonicalThreadGraphPersisted(context.Background(), tx, messagesRepo, accountID, threadID)
	if err != nil {
		t.Fatalf("second persist graph should succeed after stale edge cleanup: %v", err)
	}
	if len(secondGraph.Chunks) != 2 {
		t.Fatalf("expected second graph with 2 chunks, got %d", len(secondGraph.Chunks))
	}

	items, err := edgesRepo.ListByReplacementID(context.Background(), tx, accountID, threadID, replacement.ID)
	if err != nil {
		t.Fatalf("list edges: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected stale chunk supersession edges to be deleted, got %d", len(items))
	}
}

func assertCanonicalPersistenceMatchesCurrentGraph(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	graph *persistedCanonicalThreadGraph,
	expectedAtoms []canonicalAtom,
	expectedChunks []canonicalChunk,
) {
	t.Helper()

	if graph == nil {
		t.Fatal("expected graph, got nil")
	}
	if len(graph.Atoms) != len(expectedAtoms) {
		t.Fatalf("graph atoms mismatch: got=%d want=%d", len(graph.Atoms), len(expectedAtoms))
	}
	if len(graph.Chunks) != len(expectedChunks) {
		t.Fatalf("graph chunks mismatch: got=%d want=%d", len(graph.Chunks), len(expectedChunks))
	}

	atomsRepo := data.ThreadContextAtomsRepository{}
	chunksRepo := data.ThreadContextChunksRepository{}

	persistedAtoms, err := atomsRepo.ListByThreadUpToAtomSeq(ctx, tx, accountID, threadID, nil)
	if err != nil {
		t.Fatalf("list persisted atoms: %v", err)
	}
	if len(persistedAtoms) != len(expectedAtoms) {
		t.Fatalf("persisted atoms mismatch: got=%d want=%d", len(persistedAtoms), len(expectedAtoms))
	}
	for i, atom := range expectedAtoms {
		record := persistedAtoms[i]
		if record.AtomSeq != int64(i+1) {
			t.Fatalf("atom seq mismatch: got=%d want=%d", record.AtomSeq, i+1)
		}
		if record.SourceMessageStartSeq != atom.StartThreadSeq || record.SourceMessageEndSeq != atom.EndThreadSeq {
			t.Fatalf(
				"atom source range mismatch at atom_seq=%d: got=%d-%d want=%d-%d",
				record.AtomSeq,
				record.SourceMessageStartSeq,
				record.SourceMessageEndSeq,
				atom.StartThreadSeq,
				atom.EndThreadSeq,
			)
		}
		mapped := graph.AtomRecordsByKey[atom.Key]
		if mapped == nil || mapped.ID != record.ID {
			t.Fatalf("graph atom record mismatch for key=%s", atom.Key)
		}
	}

	persistedChunks, err := chunksRepo.ListByThreadUpToContextSeq(ctx, tx, accountID, threadID, nil)
	if err != nil {
		t.Fatalf("list persisted chunks: %v", err)
	}
	if len(persistedChunks) != len(expectedChunks) {
		t.Fatalf("persisted chunks mismatch: got=%d want=%d", len(persistedChunks), len(expectedChunks))
	}

	expectedByContextSeq := make(map[int64]canonicalChunk, len(expectedChunks))
	for _, chunk := range expectedChunks {
		expectedByContextSeq[chunk.ContextSeq] = chunk
	}
	for _, row := range persistedChunks {
		expected, ok := expectedByContextSeq[row.ContextSeq]
		if !ok {
			t.Fatalf("found stale persisted chunk at context_seq=%d", row.ContextSeq)
		}
		if strings.TrimSpace(row.PayloadText) != strings.TrimSpace(expected.Content) {
			t.Fatalf("chunk content mismatch at context_seq=%d: got=%q want=%q", row.ContextSeq, row.PayloadText, expected.Content)
		}
		atomRecord := graph.AtomRecordsByKey[expected.AtomKey]
		if atomRecord == nil {
			t.Fatalf("missing atom record for chunk atom_key=%s", expected.AtomKey)
		}
		if row.AtomID != atomRecord.ID {
			t.Fatalf("chunk atom mismatch at context_seq=%d: got=%s want=%s", row.ContextSeq, row.AtomID, atomRecord.ID)
		}
		graphChunk := graph.ChunkRecordsByContextSeq[row.ContextSeq]
		if graphChunk == nil || graphChunk.ID != row.ID {
			t.Fatalf("graph chunk record mismatch at context_seq=%d", row.ContextSeq)
		}
	}
}
