package data

import (
	"context"
	"testing"
	"time"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
)

func TestChannelMessageLedgerDeleteOlderThan(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_channel_message_ledger_ttl")
	ctx := context.Background()
	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 4, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	repo, err := NewChannelMessageLedgerRepository(pool)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}

	channelID := uuid.New()
	accountID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, slug, name, type) VALUES ($1, 'ledger-account', 'Ledger Account', 'personal');
		INSERT INTO channels (id, account_id, channel_type, persona_id, owner_user_id, webhook_secret, webhook_url, is_active, config_json)
		VALUES ($2, $1, 'discord', NULL, NULL, 'whsec', 'https://example.com', true, '{}'::jsonb);
		INSERT INTO channel_message_ledger (
			channel_id, channel_type, direction, platform_conversation_id, platform_message_id, metadata_json, created_at
		) VALUES (
			$2, 'discord', 'inbound', 'conv', 'msg', '{}'::jsonb, $3
		)`,
		accountID,
		channelID,
		time.Now().UTC().Add(-48*time.Hour),
	); err != nil {
		t.Fatalf("seed ledger row: %v", err)
	}

	count, err := repo.DeleteOlderThan(ctx, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 deleted row, got %d", count)
	}
}
