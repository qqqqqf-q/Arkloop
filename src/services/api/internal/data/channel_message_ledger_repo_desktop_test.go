//go:build desktop

package data_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
)

func TestChannelMessageLedgerRecordDesktopWritesHighPrecisionCreatedAt(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	channelID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO channels (id, account_id, channel_type, is_active, config_json)
		VALUES ($1, $2, 'telegram', 1, '{}'::jsonb)
	`, channelID, auth.DesktopAccountID); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	repo, err := data.NewChannelMessageLedgerRepository(pool)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}

	metadata := json.RawMessage(`{"ingress_state":"pending_dispatch"}`)
	if _, err := repo.Record(ctx, data.ChannelMessageLedgerRecordInput{
		ChannelID:              channelID,
		ChannelType:            "telegram",
		Direction:              data.ChannelMessageDirectionInbound,
		PlatformConversationID: "chat-1",
		PlatformMessageID:      "msg-1",
		MetadataJSON:           metadata,
	}); err != nil {
		t.Fatalf("record first entry: %v", err)
	}
	if _, err := repo.Record(ctx, data.ChannelMessageLedgerRecordInput{
		ChannelID:              channelID,
		ChannelType:            "telegram",
		Direction:              data.ChannelMessageDirectionInbound,
		PlatformConversationID: "chat-1",
		PlatformMessageID:      "msg-2",
		MetadataJSON:           metadata,
	}); err != nil {
		t.Fatalf("record second entry: %v", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT platform_message_id, created_at
		  FROM channel_message_ledger
		 WHERE channel_id = $1
		 ORDER BY created_at ASC, platform_message_id ASC
	`, channelID)
	if err != nil {
		t.Fatalf("query created_at: %v", err)
	}
	defer rows.Close()

	var order []string
	for rows.Next() {
		var messageID string
		var createdAt string
		if err := rows.Scan(&messageID, &createdAt); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		if !strings.Contains(createdAt, ".") || !strings.Contains(createdAt, "+0000") {
			t.Fatalf("expected fixed-width high precision created_at for %s, got %q", messageID, createdAt)
		}
		order = append(order, messageID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}

	if len(order) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(order))
	}
	if order[0] != "msg-1" || order[1] != "msg-2" {
		t.Fatalf("unexpected ledger order: %v", order)
	}
}

func TestChannelMessageLedgerRecordDesktopSortsMixedOldAndNewTimestampFormats(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "ledger-mixed.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	channelID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO channels (id, account_id, channel_type, is_active, config_json)
		VALUES ($1, $2, 'telegram', 1, '{}'::jsonb)
	`, channelID, auth.DesktopAccountID); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_message_ledger (
			channel_id, channel_type, direction, platform_conversation_id, platform_message_id, metadata_json, created_at
		) VALUES (
			$1, 'telegram', 'inbound', 'chat-1', 'legacy', '{}'::jsonb, '2026-04-08 14:39:19'
		)
	`, channelID); err != nil {
		t.Fatalf("insert legacy ledger row: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	repo, err := data.NewChannelMessageLedgerRepository(pool)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	if _, err := repo.Record(ctx, data.ChannelMessageLedgerRecordInput{
		ChannelID:              channelID,
		ChannelType:            "telegram",
		Direction:              data.ChannelMessageDirectionInbound,
		PlatformConversationID: "chat-1",
		PlatformMessageID:      "new",
		MetadataJSON:           json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("record new ledger row: %v", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT platform_message_id
		  FROM channel_message_ledger
		 WHERE channel_id = $1
		 ORDER BY created_at ASC, platform_message_id ASC
	`, channelID)
	if err != nil {
		t.Fatalf("query ordered ids: %v", err)
	}
	defer rows.Close()

	var order []string
	for rows.Next() {
		var messageID string
		if err := rows.Scan(&messageID); err != nil {
			t.Fatalf("scan ordered id: %v", err)
		}
		order = append(order, messageID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}

	if len(order) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(order))
	}
	if order[0] != "legacy" || order[1] != "new" {
		t.Fatalf("unexpected mixed-format ledger order: %v", order)
	}
}
