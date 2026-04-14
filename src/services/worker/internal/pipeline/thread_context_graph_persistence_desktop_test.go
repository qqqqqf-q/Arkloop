//go:build desktop

package pipeline

import (
	"context"
	"path/filepath"
	"testing"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestEnsureCanonicalThreadGraphPersistedDesktop_ReorderShrinkKeepsGraphAligned(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "thread-context.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()

	if _, err := db.Exec(ctx,
		`INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`,
		accountID, "acc-"+accountID.String(), "acc",
	); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, 'p', 'private')`,
		projectID, accountID,
	); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`,
		threadID, accountID, projectID,
	); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden)
		 VALUES ($1, $2, $3, 1, 'user', $4, '{}'::jsonb, false),
		        ($5, $2, $3, 2, 'assistant', $6, '{}'::jsonb, false)`,
		msg1ID, accountID, threadID, "alpha\n\nbeta", msg2ID, "tail",
	); err != nil {
		t.Fatalf("insert messages: %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE threads SET next_message_seq = 3 WHERE id = $1`, threadID); err != nil {
		t.Fatalf("update thread next_message_seq: %v", err)
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	messagesRepo := data.MessagesRepository{}
	firstGraph, err := ensureCanonicalThreadGraphPersisted(ctx, tx, messagesRepo, accountID, threadID)
	if err != nil {
		t.Fatalf("first persist graph: %v", err)
	}
	if len(firstGraph.Chunks) != 3 {
		t.Fatalf("expected first graph with 3 chunks, got %d", len(firstGraph.Chunks))
	}

	if _, err := tx.Exec(ctx,
		`UPDATE messages SET content = $4 WHERE id = $1 AND account_id = $2 AND thread_id = $3`,
		msg1ID, accountID, threadID, "alpha",
	); err != nil {
		t.Fatalf("shrink first message: %v", err)
	}

	secondGraph, err := ensureCanonicalThreadGraphPersisted(ctx, tx, messagesRepo, accountID, threadID)
	if err != nil {
		t.Fatalf("second persist graph after reorder/shrink: %v", err)
	}

	latestMessages, err := messagesRepo.ListRawByThread(ctx, tx, accountID, threadID, canonicalPersistFetchLimit)
	if err != nil {
		t.Fatalf("list latest messages: %v", err)
	}
	expectedAtoms, expectedChunks := buildCanonicalAtomGraph(latestMessages)
	assertCanonicalPersistenceMatchesCurrentGraph(t, ctx, tx, accountID, threadID, secondGraph, expectedAtoms, expectedChunks)
}
