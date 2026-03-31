package data

import (
	"context"
	"testing"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
)

func TestMessageRepositoryListAllAttachmentKeysByThreadIncludesHiddenDeletedAndCompacted(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_messages_attachments")
	ctx := context.Background()
	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 4, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	repo, err := NewMessageRepository(pool)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}

	accountID := uuid.New()
	threadID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, slug, name, type) VALUES ($1, 'message-account', 'Message Account', 'personal');
		INSERT INTO threads (id, account_id, created_by_user_id, title, is_private, created_at) VALUES ($2, $1, NULL, 'demo', false, now());
		INSERT INTO messages (id, account_id, thread_id, role, content, content_json, metadata_json, hidden, deleted_at, compacted)
		VALUES
		(gen_random_uuid(), $1, $2, 'user', 'a', '{"parts":[{"type":"file","attachment":{"key":"placeholder_attachment_key_a","filename":"placeholder-a.txt","mime_type":"text/plain","size":1}}]}'::jsonb, '{}'::jsonb, true, NULL, false),
		(gen_random_uuid(), $1, $2, 'user', 'b', '{"parts":[{"type":"file","attachment":{"key":"placeholder_attachment_key_b","filename":"placeholder-b.txt","mime_type":"text/plain","size":1}}]}'::jsonb, '{}'::jsonb, false, now(), false),
		(gen_random_uuid(), $1, $2, 'user', 'c', '{"parts":[{"type":"file","attachment":{"key":"placeholder_attachment_key_c","filename":"placeholder-c.txt","mime_type":"text/plain","size":1}}]}'::jsonb, '{}'::jsonb, false, NULL, true)`,
		accountID,
		threadID,
	); err != nil {
		t.Fatalf("seed messages: %v", err)
	}

	keys, err := repo.ListAllAttachmentKeysByThread(ctx, accountID, threadID)
	if err != nil {
		t.Fatalf("ListAllAttachmentKeysByThread: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %#v", keys)
	}
}
