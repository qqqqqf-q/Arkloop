package data

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMessagesRepository_SearchVisibleByOwner(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_messages_search")
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	repo := MessagesRepository{}
	accountID := uuid.New()
	userID := uuid.New()
	otherUserID := uuid.New()
	visibleThreadID := uuid.New()
	privateThreadID := uuid.New()
	deletedThreadID := uuid.New()
	otherUserThreadID := uuid.New()

	_, err = pool.Exec(ctx, `
		INSERT INTO threads (id, account_id, created_by_user_id, is_private, deleted_at, created_at)
		VALUES
			($1, $2, $3, FALSE, NULL, now() - interval '2 hour'),
			($4, $2, $3, TRUE, NULL, now() - interval '90 minute'),
			($5, $2, $3, FALSE, now(), now() - interval '80 minute'),
			($6, $2, $7, FALSE, NULL, now() - interval '70 minute')`,
		visibleThreadID, accountID, userID,
		privateThreadID,
		deletedThreadID,
		otherUserThreadID, otherUserID,
	)
	if err != nil {
		t.Fatalf("insert threads: %v", err)
	}

	now := time.Now().UTC()
	_, err = pool.Exec(ctx, `
		INSERT INTO messages (id, account_id, thread_id, role, content, hidden, deleted_at, created_at)
		VALUES
			($1, $2, $3, 'assistant', 'alpha memory', FALSE, NULL, $8),
			($4, $2, $3, 'user', '100!% sure', FALSE, NULL, $9),
			($5, $2, $6, 'assistant', 'alpha hidden', FALSE, NULL, $10),
			($7, $2, $3, 'assistant', 'alpha deleted', FALSE, now(), $11)`,
		uuid.New(), accountID, visibleThreadID,
		uuid.New(),
		uuid.New(), privateThreadID,
		uuid.New(),
		now.Add(-2*time.Minute), now.Add(-1*time.Minute), now.Add(-30*time.Second), now,
	)
	if err != nil {
		t.Fatalf("insert messages: %v", err)
	}
	_, err = pool.Exec(ctx, `UPDATE messages SET hidden = TRUE WHERE content = 'alpha hidden'`)
	if err != nil {
		t.Fatalf("update hidden message: %v", err)
	}

	hits, err := repo.SearchVisibleByOwner(ctx, pool, accountID, userID, "alpha", 10)
	if err != nil {
		t.Fatalf("search visible by owner: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 visible hit, got %d", len(hits))
	}
	if hits[0].ThreadID != visibleThreadID {
		t.Fatalf("unexpected thread id: %s", hits[0].ThreadID)
	}
	if hits[0].Content != "alpha memory" {
		t.Fatalf("unexpected content: %q", hits[0].Content)
	}

	escapedHits, err := repo.SearchVisibleByOwner(ctx, pool, accountID, userID, "100!% sure", 10)
	if err != nil {
		t.Fatalf("escaped search failed: %v", err)
	}
	if len(escapedHits) != 1 || escapedHits[0].Content != "100!% sure" {
		t.Fatalf("unexpected escaped search hits: %+v", escapedHits)
	}
}

func TestMessagesRepository_ListRawByThreadIncludesHiddenAndCompacted(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_messages_list_raw")
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	repo := MessagesRepository{}
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

	for _, row := range []struct {
		seq       int
		role      string
		content   string
		hidden    bool
		compacted bool
	}{
		{1, "user", "visible", false, false},
		{2, "assistant", "hidden", true, false},
		{3, "assistant", "compacted", true, true},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden, compacted, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, '{}'::jsonb, $7, $8, now())`,
			uuid.New(), accountID, threadID, row.seq, row.role, row.content, row.hidden, row.compacted,
		); err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	msgs, err := repo.ListRawByThread(ctx, tx, accountID, threadID, 50)
	if err != nil {
		t.Fatalf("list raw by thread: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 raw messages, got %#v", msgs)
	}
	if msgs[1].Content != "hidden" || msgs[2].Content != "compacted" {
		t.Fatalf("unexpected raw ordering: %#v", msgs)
	}
}

func TestMessagesRepository_ListRawByThreadUpToIDIncludesHiddenAndCompacted(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_messages_list_raw_upto")
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	repo := MessagesRepository{}
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
	var upToID uuid.UUID
	for _, row := range []struct {
		seq       int
		role      string
		content   string
		hidden    bool
		compacted bool
	}{
		{1, "user", "visible", false, false},
		{2, "assistant", "hidden", true, false},
		{3, "assistant", "compacted", true, true},
	} {
		id := uuid.New()
		if row.seq == 2 {
			upToID = id
		}
		contentJSON, _ := json.Marshal(map[string]any{})
		if _, err := pool.Exec(ctx, `
			INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, content_json, metadata_json, hidden, compacted, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, '{}'::jsonb, $8, $9, now())`,
			id, accountID, threadID, row.seq, row.role, row.content, string(contentJSON), row.hidden, row.compacted,
		); err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	msgs, err := repo.ListRawByThreadUpToID(ctx, tx, accountID, threadID, upToID, 50)
	if err != nil {
		t.Fatalf("list raw by thread up to id: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 raw messages, got %#v", msgs)
	}
	if msgs[1].Content != "hidden" {
		t.Fatalf("expected hidden message included, got %#v", msgs)
	}
}

func TestMessagesRepository_ListByThreadWithoutLimitLoadsFullVisibleHistory(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_messages_unbounded_history")
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	repo := MessagesRepository{}
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

	for seq := 1; seq <= 205; seq++ {
		if _, err := pool.Exec(ctx, `
			INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden, compacted, created_at)
			VALUES ($1, $2, $3, $4, 'user', $5, '{}'::jsonb, FALSE, FALSE, now())`,
			uuid.New(), accountID, threadID, seq, fmt.Sprintf("msg-%03d", seq),
		); err != nil {
			t.Fatalf("insert message %d: %v", seq, err)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	msgs, err := repo.ListByThread(ctx, tx, accountID, threadID, 0)
	if err != nil {
		t.Fatalf("list by thread: %v", err)
	}
	if len(msgs) != 205 {
		t.Fatalf("expected full visible history, got %d", len(msgs))
	}
	if msgs[0].Content != "msg-001" || msgs[len(msgs)-1].Content != "msg-205" {
		t.Fatalf("unexpected ordering: first=%q last=%q", msgs[0].Content, msgs[len(msgs)-1].Content)
	}
}
