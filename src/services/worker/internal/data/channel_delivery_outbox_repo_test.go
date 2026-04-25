//go:build !desktop

package data

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestChannelDeliveryOutboxInsertRejectsEmptyPayload(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "channel_delivery_outbox_empty_payload")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	repo := ChannelDeliveryOutboxRepository{}
	_, err = repo.InsertPending(ctx, pool, uuid.New(), uuid.New(), nil, "telegram", OutboxKindMessage, OutboxPayload{PlatformChatID: "10001"})
	if err == nil || !strings.Contains(err.Error(), "no deliverable content") {
		t.Fatalf("expected empty payload error, got %v", err)
	}
}
