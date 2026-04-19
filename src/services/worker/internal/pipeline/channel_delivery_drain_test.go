//go:build !desktop

package pipeline

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"arkloop/services/shared/telegrambot"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestChannelDeliveryDrainerSendsPendingOutbox(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "channel_delivery_drain_success")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	createChannelDeliveryTables(t, pool, `CREATE TABLE channel_message_ledger (
		channel_id UUID NOT NULL,
		channel_type TEXT NOT NULL,
		direction TEXT NOT NULL,
		thread_id UUID NULL,
		run_id UUID NULL,
		platform_conversation_id TEXT NOT NULL,
		platform_message_id TEXT NOT NULL,
		platform_parent_message_id TEXT NULL,
		platform_thread_id TEXT NULL,
		sender_channel_identity_id UUID NULL,
		metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
		UNIQUE (channel_id, direction, platform_conversation_id, platform_message_id)
	)`)

	keyBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = byte(i + 1)
	}
	t.Setenv("ARKLOOP_ENCRYPTION_KEY", hex.EncodeToString(keyBytes))

	var sent bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		sent = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":901,"chat":{"id":10001}}}`))
	}))
	defer server.Close()
	t.Setenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL", server.URL)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()
	outboxID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(ctx, `INSERT INTO secrets (id, encrypted_value, key_version) VALUES ($1, $2, 1)`, secretID, encryptChannelToken(t, keyBytes, "bot-token")); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channels (id, channel_type, credentials_id, is_active) VALUES ($1, 'telegram', $2, TRUE)`, channelID, secretID); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	payload := data.OutboxPayload{
		AccountID:      accountID,
		RunID:          runID,
		ThreadID:       &threadID,
		Outputs:        []string{"drain hello"},
		PlatformChatID: "10001",
	}
	payloadJSON, _ := json.Marshal(payload)
	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_delivery_outbox (
			id, run_id, thread_id, channel_id, channel_type, status, payload_json, segments_sent, attempts, last_error, next_retry_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'telegram', 'pending', $5, 0, 0, NULL, $6, $6, $6)
	`, outboxID, runID, threadID, channelID, payloadJSON, time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatalf("insert outbox: %v", err)
	}

	drainer := NewChannelDeliveryDrainer(pool, ChannelDeliveryDrainOptions{
		Telegram: telegrambot.NewClient(server.URL, server.Client()),
	})
	drainer.Start(ctx)
	time.Sleep(2 * time.Second)
	drainer.Stop()

	if !sent {
		t.Fatal("expected telegram send during drain")
	}

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM channel_delivery_outbox WHERE id = $1`, outboxID).Scan(&status); err != nil {
		t.Fatalf("query outbox status: %v", err)
	}
	if status != "sent" {
		t.Fatalf("expected outbox status sent, got %q", status)
	}

	var deliveryCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM channel_message_deliveries`).Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveryCount != 1 {
		t.Fatalf("expected 1 delivery record, got %d", deliveryCount)
	}
}

func TestChannelDeliveryDrainerRetriesAndMarksDead(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "channel_delivery_drain_dead")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	createChannelDeliveryTables(t, pool, `CREATE TABLE channel_message_ledger (
		channel_id UUID NOT NULL,
		channel_type TEXT NOT NULL,
		direction TEXT NOT NULL,
		thread_id UUID NULL,
		run_id UUID NULL,
		platform_conversation_id TEXT NOT NULL,
		platform_message_id TEXT NOT NULL,
		platform_parent_message_id TEXT NULL,
		platform_thread_id TEXT NULL,
		sender_channel_identity_id UUID NULL,
		metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
		UNIQUE (channel_id, direction, platform_conversation_id, platform_message_id)
	)`)

	keyBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = byte(i + 1)
	}
	t.Setenv("ARKLOOP_ENCRYPTION_KEY", hex.EncodeToString(keyBytes))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"ok":false,"description":"send failed"}`))
	}))
	defer server.Close()
	t.Setenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL", server.URL)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()
	outboxID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(ctx, `INSERT INTO secrets (id, encrypted_value, key_version) VALUES ($1, $2, 1)`, secretID, encryptChannelToken(t, keyBytes, "bot-token")); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channels (id, channel_type, credentials_id, is_active) VALUES ($1, 'telegram', $2, TRUE)`, channelID, secretID); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	payload := data.OutboxPayload{
		AccountID:      accountID,
		RunID:          runID,
		ThreadID:       &threadID,
		Outputs:        []string{"drain fail"},
		PlatformChatID: "10001",
	}
	payloadJSON, _ := json.Marshal(payload)
	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_delivery_outbox (
			id, run_id, thread_id, channel_id, channel_type, status, payload_json, segments_sent, attempts, last_error, next_retry_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'telegram', 'pending', $5, 0, 4, NULL, $6, $6, $6)
	`, outboxID, runID, threadID, channelID, payloadJSON, time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatalf("insert outbox: %v", err)
	}

	drainer := NewChannelDeliveryDrainer(pool, ChannelDeliveryDrainOptions{
		Telegram: telegrambot.NewClient(server.URL, server.Client()),
	})
	drainer.Start(ctx)
	time.Sleep(2 * time.Second)
	drainer.Stop()

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM channel_delivery_outbox WHERE id = $1`, outboxID).Scan(&status); err != nil {
		t.Fatalf("query outbox status: %v", err)
	}
	if status != "dead" {
		t.Fatalf("expected outbox status dead after max attempts, got %q", status)
	}
}


func TestChannelDeliveryDrainerOffset(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "channel_delivery_drain_offset")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	createChannelDeliveryTables(t, pool, `CREATE TABLE channel_message_ledger (
		channel_id UUID NOT NULL,
		channel_type TEXT NOT NULL,
		direction TEXT NOT NULL,
		thread_id UUID NULL,
		run_id UUID NULL,
		platform_conversation_id TEXT NOT NULL,
		platform_message_id TEXT NOT NULL,
		platform_parent_message_id TEXT NULL,
		platform_thread_id TEXT NULL,
		sender_channel_identity_id UUID NULL,
		metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
		UNIQUE (channel_id, direction, platform_conversation_id, platform_message_id)
	)`)

	keyBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = byte(i + 1)
	}
	t.Setenv("ARKLOOP_ENCRYPTION_KEY", hex.EncodeToString(keyBytes))

	var sendCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		sendCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":901,"chat":{"id":10001}}}`))
	}))
	defer server.Close()
	t.Setenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL", server.URL)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()
	outboxID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(ctx, `INSERT INTO secrets (id, encrypted_value, key_version) VALUES ($1, $2, 1)`, secretID, encryptChannelToken(t, keyBytes, "bot-token")); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channels (id, channel_type, credentials_id, is_active) VALUES ($1, 'telegram', $2, TRUE)`, channelID, secretID); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	payload := data.OutboxPayload{
		AccountID:      accountID,
		RunID:          runID,
		ThreadID:       &threadID,
		Outputs:        []string{"seg0", "seg1", "seg2"},
		PlatformChatID: "10001",
	}
	payloadJSON, _ := json.Marshal(payload)
	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_delivery_outbox (
			id, run_id, thread_id, channel_id, channel_type, status, payload_json, segments_sent, attempts, last_error, next_retry_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'telegram', 'pending', $5, 1, 0, NULL, $6, $6, $6)
	`, outboxID, runID, threadID, channelID, payloadJSON, time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatalf("insert outbox: %v", err)
	}

	drainer := NewChannelDeliveryDrainer(pool, ChannelDeliveryDrainOptions{
		Telegram: telegrambot.NewClient(server.URL, server.Client()),
	})
	drainer.Start(ctx)
	time.Sleep(2 * time.Second)
	drainer.Stop()

	if sendCount != 2 {
		t.Fatalf("expected 2 sends for offset=1, got %d", sendCount)
	}

	var segmentsSent int
	if err := pool.QueryRow(ctx, `SELECT segments_sent FROM channel_delivery_outbox WHERE id = $1`, outboxID).Scan(&segmentsSent); err != nil {
		t.Fatalf("query segments_sent: %v", err)
	}
	if segmentsSent != 3 {
		t.Fatalf("expected segments_sent=3, got %d", segmentsSent)
	}

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM channel_delivery_outbox WHERE id = $1`, outboxID).Scan(&status); err != nil {
		t.Fatalf("query outbox status: %v", err)
	}
	if status != "sent" {
		t.Fatalf("expected outbox status sent, got %q", status)
	}
}
