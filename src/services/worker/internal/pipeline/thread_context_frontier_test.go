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

func TestBuildThreadContextFrontierUsesSupersessionEdges(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "thread_context_frontier")
	conn, err := pgx.Connect(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	accountID := uuid.New()
	threadID := uuid.New()
	if _, err := conn.Exec(context.Background(),
		`INSERT INTO threads (id, account_id, next_message_seq) VALUES ($1, $2, 10)`,
		threadID, accountID,
	); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	msg1 := uuid.New()
	msg2 := uuid.New()
	if _, err := conn.Exec(context.Background(),
		`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
		 VALUES ($1, $2, $3, 1, 'user', $4, '{}'::jsonb, false),
		        ($5, $2, $3, 2, 'user', $6, '{}'::jsonb, false)`,
		msg1, accountID, threadID, "alpha\n\nbeta\n\ngamma", msg2, "tail",
	); err != nil {
		t.Fatalf("insert messages: %v", err)
	}

	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	graph, err := ensureCanonicalThreadGraphPersisted(context.Background(), tx, data.MessagesRepository{}, accountID, threadID)
	if err != nil {
		t.Fatalf("persist graph: %v", err)
	}
	if len(graph.Chunks) < 4 {
		t.Fatalf("expected at least 4 chunks, got %d", len(graph.Chunks))
	}

	replacementsRepo := data.ThreadContextReplacementsRepository{}
	edgesRepo := data.ThreadContextSupersessionEdgesRepository{}

	r1, err := replacementsRepo.Insert(context.Background(), tx, data.ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadID,
		StartThreadSeq:  graph.Chunks[0].StartThreadSeq,
		EndThreadSeq:    graph.Chunks[1].EndThreadSeq,
		StartContextSeq: graph.Chunks[0].ContextSeq,
		EndContextSeq:   graph.Chunks[1].ContextSeq,
		SummaryText:     "summary-1",
		Layer:           1,
	})
	if err != nil {
		t.Fatalf("insert replacement 1: %v", err)
	}
	for _, seq := range []int64{graph.Chunks[0].ContextSeq, graph.Chunks[1].ContextSeq} {
		chunkID := graph.ChunkRecordsByContextSeq[seq].ID
		if _, err := edgesRepo.Insert(context.Background(), tx, data.ThreadContextSupersessionEdgeInsertInput{
			AccountID:         accountID,
			ThreadID:          threadID,
			ReplacementID:     r1.ID,
			SupersededChunkID: &chunkID,
		}); err != nil {
			t.Fatalf("insert replacement 1 edge: %v", err)
		}
	}

	r2, err := replacementsRepo.Insert(context.Background(), tx, data.ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadID,
		StartThreadSeq:  graph.Chunks[0].StartThreadSeq,
		EndThreadSeq:    graph.Chunks[2].EndThreadSeq,
		StartContextSeq: graph.Chunks[0].ContextSeq,
		EndContextSeq:   graph.Chunks[2].ContextSeq,
		SummaryText:     "summary-2",
		Layer:           2,
	})
	if err != nil {
		t.Fatalf("insert replacement 2: %v", err)
	}
	if _, err := edgesRepo.Insert(context.Background(), tx, data.ThreadContextSupersessionEdgeInsertInput{
		AccountID:               accountID,
		ThreadID:                threadID,
		ReplacementID:           r2.ID,
		SupersededReplacementID: &r1.ID,
	}); err != nil {
		t.Fatalf("insert nested replacement edge: %v", err)
	}
	chunkID := graph.ChunkRecordsByContextSeq[graph.Chunks[2].ContextSeq].ID
	if _, err := edgesRepo.Insert(context.Background(), tx, data.ThreadContextSupersessionEdgeInsertInput{
		AccountID:         accountID,
		ThreadID:          threadID,
		ReplacementID:     r2.ID,
		SupersededChunkID: &chunkID,
	}); err != nil {
		t.Fatalf("insert replacement 2 chunk edge: %v", err)
	}
	if err := replacementsRepo.SupersedeActiveOverlapsByContextSeq(context.Background(), tx, accountID, threadID, r2.StartContextSeq, r2.EndContextSeq, r2.ID); err != nil {
		t.Fatalf("supersede overlaps: %v", err)
	}

	items, err := replacementsRepo.ListActiveByThreadUpToContextSeq(context.Background(), tx, accountID, threadID, nil)
	if err != nil {
		t.Fatalf("list active replacements: %v", err)
	}
	frontier, err := buildThreadContextFrontier(context.Background(), tx, graph, accountID, threadID, items, nil)
	if err != nil {
		t.Fatalf("build frontier: %v", err)
	}
	if len(frontier) != 2 {
		t.Fatalf("expected replacement + tail chunk frontier, got %#v", frontier)
	}
	if frontier[0].Kind != FrontierNodeReplacement || frontier[0].SourceText != "summary-2" {
		t.Fatalf("unexpected first frontier node: %#v", frontier[0])
	}
	if frontier[1].Kind != FrontierNodeChunk || frontier[1].SourceText != "tail" {
		t.Fatalf("unexpected second frontier node: %#v", frontier[1])
	}
}

func TestBuildCanonicalThreadContextReindexesFrontierForTrimmedMessages(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "thread_context_frontier_trimmed")
	conn, err := pgx.Connect(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	if _, err := conn.Exec(context.Background(),
		`INSERT INTO accounts (id, type) VALUES ($1, 'personal')`,
		accountID,
	); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := conn.Exec(context.Background(),
		`INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`,
		projectID, accountID,
	); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := conn.Exec(context.Background(),
		`INSERT INTO threads (id, account_id, project_id, next_message_seq) VALUES ($1, $2, $3, 10)`,
		threadID, accountID, projectID,
	); err != nil {
		t.Fatalf("insert thread: %v", err)
	}

	huge := strings.Repeat("alpha beta gamma delta\n\n", 180)
	msgIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	payloads := []struct {
		id        uuid.UUID
		threadSeq int
		role      string
		content   string
	}{
		{id: msgIDs[0], threadSeq: 1, role: "user", content: huge},
		{id: msgIDs[1], threadSeq: 2, role: "assistant", content: "done"},
		{id: msgIDs[2], threadSeq: 3, role: "user", content: "tail"},
	}
	for _, msg := range payloads {
		if _, err := conn.Exec(context.Background(),
			`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
			 VALUES ($1, $2, $3, $4, $5, $6, '{}'::jsonb, false)`,
			msg.id, accountID, threadID, msg.threadSeq, msg.role, msg.content,
		); err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}

	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	ctx, err := buildCanonicalThreadContext(
		context.Background(),
		tx,
		data.Run{AccountID: accountID, ThreadID: threadID},
		data.MessagesRepository{},
		nil,
		nil,
		2,
	)
	if err != nil {
		t.Fatalf("build canonical context: %v", err)
	}
	if len(ctx.Messages) != 2 {
		t.Fatalf("expected trimmed rendered messages, got %d", len(ctx.Messages))
	}
	if len(ctx.Frontier) == 0 {
		t.Fatal("expected frontier nodes")
	}
	for i, node := range ctx.Frontier {
		if node.AtomSeq <= 0 {
			t.Fatalf("frontier[%d] missing atom seq: %#v", i, node)
		}
		if node.MsgStart < 0 || node.MsgEnd < node.MsgStart {
			t.Fatalf("frontier[%d] missing message bounds: %#v", i, node)
		}
		if node.MsgEnd >= len(ctx.Messages) {
			t.Fatalf("frontier[%d] message bounds drifted past trimmed messages: %#v", i, node)
		}
	}
}

func TestBuildCanonicalThreadContextKeepsVisibleTailWhenRawGraphHasHiddenGap(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "thread_context_frontier_hidden_gap")
	conn, err := pgx.Connect(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()
	msg3ID := uuid.New()
	msg4ID := uuid.New()

	if _, err := conn.Exec(context.Background(),
		`INSERT INTO accounts (id, type) VALUES ($1, 'personal')`,
		accountID,
	); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := conn.Exec(context.Background(),
		`INSERT INTO projects (id, account_id, name) VALUES ($1, $2, 'p')`,
		projectID, accountID,
	); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := conn.Exec(context.Background(),
		`INSERT INTO threads (id, account_id, project_id, next_message_seq) VALUES ($1, $2, $3, 10)`,
		threadID, accountID, projectID,
	); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := conn.Exec(context.Background(),
		`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
		 VALUES
		 ($1, $2, $3, 1, 'user', 'head', '{}'::jsonb, false),
		 ($4, $2, $3, 2, 'assistant', 'thinking', $8::jsonb, true),
		 ($5, $2, $3, 3, 'tool', $9, $10::jsonb, true),
		 ($6, $2, $3, 4, 'user', 'tail', '{}'::jsonb, false)`,
		msg1ID, accountID, threadID,
		msg2ID,
		msg3ID,
		msg4ID,
		runID,
		`{"intermediate":true,"run_id":"`+runID.String()+`"}`,
		`{"tool_call_id":"call-1","tool_name":"demo","result":{"ok":true}}`,
		`{"intermediate":true,"run_id":"`+runID.String()+`","tool_call_id":"call-1"}`,
	); err != nil {
		t.Fatalf("insert messages: %v", err)
	}

	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	ctx, err := buildCanonicalThreadContext(
		context.Background(),
		tx,
		data.Run{AccountID: accountID, ThreadID: threadID},
		data.MessagesRepository{},
		nil,
		nil,
		0,
	)
	if err != nil {
		t.Fatalf("build canonical context: %v", err)
	}
	if len(ctx.Messages) != 2 {
		t.Fatalf("expected visible head and tail messages, got %d", len(ctx.Messages))
	}
	if got := messageText(ctx.Messages[0]); got != "head" {
		t.Fatalf("unexpected first message: %q", got)
	}
	if got := messageText(ctx.Messages[1]); got != "tail" {
		t.Fatalf("unexpected tail message: %q", got)
	}
}

func TestEnsureCanonicalThreadGraphPersisted_ClearsStaleTailEdgesWithoutBreakingFrontier(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "thread_context_frontier_stale_tail_edges")
	conn, err := pgx.Connect(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

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

	graphBeforeTrim, err := ensureCanonicalThreadGraphPersisted(context.Background(), tx, data.MessagesRepository{}, accountID, threadID)
	if err != nil {
		t.Fatalf("persist initial graph: %v", err)
	}
	if len(graphBeforeTrim.Chunks) != 3 {
		t.Fatalf("expected 3 chunks before trim, got %d", len(graphBeforeTrim.Chunks))
	}

	replacementsRepo := data.ThreadContextReplacementsRepository{}
	edgesRepo := data.ThreadContextSupersessionEdgesRepository{}
	prefixReplacement, err := replacementsRepo.Insert(context.Background(), tx, data.ThreadContextReplacementInsertInput{
		AccountID:       accountID,
		ThreadID:        threadID,
		StartThreadSeq:  graphBeforeTrim.Chunks[0].StartThreadSeq,
		EndThreadSeq:    graphBeforeTrim.Chunks[1].EndThreadSeq,
		StartContextSeq: graphBeforeTrim.Chunks[0].ContextSeq,
		EndContextSeq:   graphBeforeTrim.Chunks[1].ContextSeq,
		SummaryText:     "prefix-summary",
		Layer:           1,
	})
	if err != nil {
		t.Fatalf("insert replacement: %v", err)
	}
	for _, seq := range []int64{graphBeforeTrim.Chunks[0].ContextSeq, graphBeforeTrim.Chunks[1].ContextSeq} {
		chunkID := graphBeforeTrim.ChunkRecordsByContextSeq[seq].ID
		if _, err := edgesRepo.Insert(context.Background(), tx, data.ThreadContextSupersessionEdgeInsertInput{
			AccountID:         accountID,
			ThreadID:          threadID,
			ReplacementID:     prefixReplacement.ID,
			SupersededChunkID: &chunkID,
		}); err != nil {
			t.Fatalf("insert supersession edge for context_seq=%d: %v", seq, err)
		}
	}

	if _, err := tx.Exec(context.Background(),
		`UPDATE messages SET content = $4 WHERE id = $1 AND account_id = $2 AND thread_id = $3`,
		msg1ID, accountID, threadID, "alpha",
	); err != nil {
		t.Fatalf("trim first message: %v", err)
	}

	graphAfterTrim, err := ensureCanonicalThreadGraphPersisted(context.Background(), tx, data.MessagesRepository{}, accountID, threadID)
	if err != nil {
		t.Fatalf("persist trimmed graph should not fail on stale chunk cleanup: %v", err)
	}
	if len(graphAfterTrim.Chunks) != 2 {
		t.Fatalf("expected 2 chunks after trim, got %d", len(graphAfterTrim.Chunks))
	}

	replacementItems, err := replacementsRepo.ListActiveByThreadUpToContextSeq(context.Background(), tx, accountID, threadID, nil)
	if err != nil {
		t.Fatalf("list active replacements: %v", err)
	}
	frontier, err := buildThreadContextFrontier(context.Background(), tx, graphAfterTrim, accountID, threadID, replacementItems, nil)
	if err != nil {
		t.Fatalf("build frontier after trim: %v", err)
	}
	if len(frontier) != 2 {
		t.Fatalf("expected prefix replacement + tail chunk frontier, got %#v", frontier)
	}
	if frontier[0].Kind != FrontierNodeReplacement || frontier[0].SourceText != "prefix-summary" {
		t.Fatalf("unexpected replacement frontier node after trim: %#v", frontier[0])
	}
	if frontier[1].Kind != FrontierNodeChunk || frontier[1].SourceText != "tail" {
		t.Fatalf("unexpected tail frontier node after trim: %#v", frontier[1])
	}
}
