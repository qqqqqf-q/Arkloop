package data

import (
	"context"
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
	orgID := uuid.New()
	userID := uuid.New()
	otherUserID := uuid.New()
	visibleThreadID := uuid.New()
	privateThreadID := uuid.New()
	deletedThreadID := uuid.New()
	otherUserThreadID := uuid.New()

	_, err = pool.Exec(ctx, `
		INSERT INTO threads (id, org_id, created_by_user_id, is_private, deleted_at, created_at)
		VALUES
			($1, $2, $3, FALSE, NULL, now() - interval '2 hour'),
			($4, $2, $3, TRUE, NULL, now() - interval '90 minute'),
			($5, $2, $3, FALSE, now(), now() - interval '80 minute'),
			($6, $2, $7, FALSE, NULL, now() - interval '70 minute')`,
		visibleThreadID, orgID, userID,
		privateThreadID,
		deletedThreadID,
		otherUserThreadID, otherUserID,
	)
	if err != nil {
		t.Fatalf("insert threads: %v", err)
	}

	now := time.Now().UTC()
	_, err = pool.Exec(ctx, `
		INSERT INTO messages (id, org_id, thread_id, role, content, hidden, deleted_at, created_at)
		VALUES
			($1, $2, $3, 'assistant', 'alpha memory', FALSE, NULL, $8),
			($4, $2, $3, 'user', '100!% sure', FALSE, NULL, $9),
			($5, $2, $6, 'assistant', 'alpha hidden', FALSE, NULL, $10),
			($7, $2, $3, 'assistant', 'alpha deleted', FALSE, now(), $11)`,
		uuid.New(), orgID, visibleThreadID,
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

	hits, err := repo.SearchVisibleByOwner(ctx, pool, orgID, userID, "alpha", 10)
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

	escapedHits, err := repo.SearchVisibleByOwner(ctx, pool, orgID, userID, "100!% sure", 10)
	if err != nil {
		t.Fatalf("escaped search failed: %v", err)
	}
	if len(escapedHits) != 1 || escapedHits[0].Content != "100!% sure" {
		t.Fatalf("unexpected escaped search hits: %+v", escapedHits)
	}
}
