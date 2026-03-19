//go:build !desktop

package pipeline

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEscapeTelegramMarkdownV2EscapesReservedCharacters(t *testing.T) {
	input := "_*[]()~`>#+-=|{}.!"
	want := "\\_\\*\\[\\]\\(\\)\\~\\`\\>\\#\\+\\-\\=\\|\\{\\}\\.\\!"

	if got := escapeTelegramMarkdownV2(input); got != want {
		t.Fatalf("unexpected escaped text: got %q want %q", got, want)
	}
}

func TestSplitTelegramMessagePrefersParagraphBoundary(t *testing.T) {
	segments := splitTelegramMessage("alpha paragraph.\n\nbeta gamma delta", 20)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	if segments[0] != "alpha paragraph." {
		t.Fatalf("unexpected first segment: %q", segments[0])
	}
	if segments[1] != "beta gamma delta" {
		t.Fatalf("unexpected second segment: %q", segments[1])
	}
}

func TestSplitTelegramMessageFallsBackToHardLimit(t *testing.T) {
	segments := splitTelegramMessage(strings.Repeat("x", 9), 4)
	if len(segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segments))
	}
	if segments[0] != "xxxx" || segments[1] != "xxxx" || segments[2] != "x" {
		t.Fatalf("unexpected hard split result: %#v", segments)
	}
}

func TestSplitTelegramMessagePreservesUTF8Boundaries(t *testing.T) {
	input := "你好世界今天"
	segments := splitTelegramMessage(input, 3)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	if strings.Join(segments, "") != input {
		t.Fatalf("expected segments to reconstruct original text, got %#v", segments)
	}
}

func TestRecordChannelDeliveryFailureAppendsRunEvent(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)

	recordChannelDeliveryFailure(context.Background(), pool, runID, errors.New("send boom"))

	var errorMessage string
	if err := pool.QueryRow(
		context.Background(),
		`SELECT data_json->>'error'
		   FROM run_events
		  WHERE run_id = $1 AND type = 'run.channel_delivery_failed'
		  ORDER BY seq DESC
		  LIMIT 1`,
		runID,
	).Scan(&errorMessage); err != nil {
		t.Fatalf("load run event: %v", err)
	}
	if errorMessage != "send boom" {
		t.Fatalf("unexpected error payload: %q", errorMessage)
	}
}

func TestChannelDeliveryMiddlewarePersistsDeliveryAndLedger(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_success")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

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

	var sent struct {
		ReplyToMessageID string `json:"reply_to_message_id"`
		MessageThreadID  string `json:"message_thread_id"`
	}
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path != "/botbot-token/sendMessage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":701,"chat":{"id":10001}}}`))
	}))
	defer server.Close()
	t.Setenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL", server.URL)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()
	threadRef := "topic-9"

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(ctx,
		`INSERT INTO secrets (id, encrypted_value, key_version) VALUES ($1, $2, 1)`,
		secretID,
		encryptChannelToken(t, keyBytes, "bot-token"),
	); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO channels (id, channel_type, credentials_id, is_active) VALUES ($1, 'telegram', $2, TRUE)`,
		channelID,
		secretID,
	); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	rc := &RunContext{
		Run:                  data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		FinalAssistantOutput: "worker delivery text",
		ChannelContext: &ChannelContext{
			ChannelID:   channelID,
			ChannelType: "telegram",
			Conversation: ChannelConversationRef{
				Target:   "10001",
				ThreadID: &threadRef,
			},
			TriggerMessage: &ChannelMessageRef{MessageID: "55"},
		},
	}

	mw := NewChannelDeliveryMiddleware(pool)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	var (
		deliveryCount int
		ledgerCount   int
		parentID      *string
		messageThread *string
	)
	if err := pool.QueryRow(ctx,
		`SELECT
			(SELECT COUNT(*) FROM channel_message_deliveries),
			(SELECT COUNT(*) FROM channel_message_ledger),
			(SELECT platform_parent_message_id FROM channel_message_ledger LIMIT 1),
			(SELECT platform_thread_id FROM channel_message_ledger LIMIT 1)`,
	).Scan(&deliveryCount, &ledgerCount, &parentID, &messageThread); err != nil {
		t.Fatalf("load delivery rows: %v", err)
	}
	if deliveryCount != 1 || ledgerCount != 1 {
		t.Fatalf("expected one delivery and one ledger row, got deliveries=%d ledger=%d", deliveryCount, ledgerCount)
	}
	if parentID == nil || *parentID != "55" {
		t.Fatalf("unexpected platform_parent_message_id: %#v", parentID)
	}
	if messageThread == nil || *messageThread != threadRef {
		t.Fatalf("unexpected platform_thread_id: %#v", messageThread)
	}
	if sent.ReplyToMessageID != "55" || sent.MessageThreadID != threadRef {
		t.Fatalf("unexpected telegram send payload: %#v", sent)
	}

	var failureCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM run_events WHERE run_id = $1 AND type = 'run.channel_delivery_failed'`,
		runID,
	).Scan(&failureCount); err != nil {
		t.Fatalf("count failure events: %v", err)
	}
	if failureCount != 0 {
		t.Fatalf("expected no failure events, got %d", failureCount)
	}
}

func TestRecordChannelDeliverySuccessRollsBackOnLedgerFailure(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_atomic")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	createChannelDeliveryTables(t, pool, `CREATE TABLE channel_message_ledger (
		channel_id UUID NOT NULL,
		channel_type TEXT NOT NULL,
		direction TEXT NOT NULL,
		thread_id UUID NULL,
		run_id UUID NULL,
		platform_conversation_id TEXT NOT NULL,
		platform_message_id TEXT NOT NULL,
		platform_parent_message_id TEXT NULL,
		sender_channel_identity_id UUID NULL,
		metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
		UNIQUE (channel_id, direction, platform_conversation_id, platform_message_id)
	)`)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)

	rc := &RunContext{
		Run: data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		ChannelContext: &ChannelContext{
			ChannelID:   channelID,
			ChannelType: "telegram",
			Conversation: ChannelConversationRef{
				Target: "10001",
			},
		},
	}

	err = recordChannelDeliverySuccess(
		ctx,
		pool,
		data.ChannelDeliveryRepository{},
		data.ChannelMessageLedgerRepository{},
		rc,
		[]string{"701"},
	)
	if err == nil {
		t.Fatal("expected ledger write to fail")
	}

	var deliveryCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM channel_message_deliveries`).Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveryCount != 0 {
		t.Fatalf("expected delivery rollback, got %d rows", deliveryCount)
	}
}

func createChannelDeliveryTables(t *testing.T, pool *pgxpool.Pool, ledgerTableSQL string) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TABLE channels (
			id UUID PRIMARY KEY,
			channel_type TEXT NOT NULL,
			credentials_id UUID NULL,
			is_active BOOLEAN NOT NULL DEFAULT FALSE
		)`,
		`CREATE TABLE channel_message_deliveries (
			run_id UUID NULL,
			thread_id UUID NULL,
			channel_id UUID NOT NULL,
			platform_chat_id TEXT NOT NULL,
			platform_message_id TEXT NOT NULL,
			UNIQUE (channel_id, platform_chat_id, platform_message_id)
		)`,
		ledgerTableSQL,
	} {
		if _, err := pool.Exec(context.Background(), stmt); err != nil {
			t.Fatalf("create delivery tables: %v", err)
		}
	}
}

func encryptChannelToken(t *testing.T, key []byte, plaintext string) string {
	t.Helper()

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("new gcm: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("rand nonce: %v", err)
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(append(nonce, ciphertext...))
}
