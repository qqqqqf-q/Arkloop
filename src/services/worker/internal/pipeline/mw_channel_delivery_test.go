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
	"fmt"
	"io"
	nethttp "net/http"
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

type threadPersistProviderSpy struct {
	called bool
}

func (p *threadPersistProviderSpy) HookProviderName() string { return "thread_persist_provider_spy" }

func (p *threadPersistProviderSpy) PersistThread(context.Context, *RunContext, ThreadDelta, ThreadPersistHints) ThreadPersistResult {
	p.called = true
	return ThreadPersistResult{Handled: true, Provider: "thread_persist_provider_spy"}
}

type threadPersistOrderSpy struct {
	events *[]string
}

func (p *threadPersistOrderSpy) HookProviderName() string { return "thread_persist_order_spy" }

func (p *threadPersistOrderSpy) PersistThread(context.Context, *RunContext, ThreadDelta, ThreadPersistHints) ThreadPersistResult {
	*p.events = append(*p.events, "thread_persist")
	return ThreadPersistResult{Handled: true, Provider: "thread_persist_order_spy"}
}

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

func TestSplitDiscordMessagePrefersParagraphBoundary(t *testing.T) {
	segments := splitDiscordMessage("alpha paragraph.\n\nbeta gamma delta", 20)
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

func TestShouldShowTelegramProgress_PrivateOnly(t *testing.T) {
	privateRC := &RunContext{
		ChannelContext: &ChannelContext{ConversationType: "private"},
	}
	if !ShouldShowTelegramProgress(privateRC) {
		t.Fatal("expected private telegram conversation to allow progress output")
	}

	groupRC := &RunContext{
		ChannelContext: &ChannelContext{ConversationType: "group"},
	}
	if ShouldShowTelegramProgress(groupRC) {
		t.Fatal("expected group telegram conversation to suppress progress output")
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
		if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
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
			ChannelID:        channelID,
			ChannelType:      "telegram",
			ConversationType: "supergroup",
			Conversation: ChannelConversationRef{
				Target:   "10001",
				ThreadID: &threadRef,
			},
			TriggerMessage: &ChannelMessageRef{MessageID: "55"},
		},
	}

	mw := NewChannelDeliveryMiddleware(pool)
	if err := mw(ctx, rc, func(_ context.Context, rc *RunContext) error {
		if rc.TelegramToolBoundaryFlush != nil {
			t.Fatal("expected silent heartbeat to disable telegram boundary flush")
		}
		return nil
	}); err != nil {
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
	if parentID != nil {
		t.Fatalf("expected no platform_parent_message_id without explicit telegram_reply, got %#v", parentID)
	}
	if messageThread == nil || *messageThread != threadRef {
		t.Fatalf("unexpected platform_thread_id: %#v", messageThread)
	}
	if sent.ReplyToMessageID != "" || sent.MessageThreadID != threadRef {
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

func TestChannelDeliveryFailureDoesNotPreventThreadPersistHooks(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_thread_persist_failure")
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

	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(nethttp.StatusBadGateway)
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

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(ctx, `INSERT INTO secrets (id, encrypted_value, key_version) VALUES ($1, $2, 1)`, secretID, encryptChannelToken(t, keyBytes, "bot-token")); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channels (id, channel_type, credentials_id, is_active) VALUES ($1, 'telegram', $2, TRUE)`, channelID, secretID); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	provider := &threadPersistProviderSpy{}
	registry := NewHookRegistry()
	if err := registry.SetThreadPersistenceProvider(provider); err != nil {
		t.Fatalf("set thread provider: %v", err)
	}

	rc := &RunContext{
		Run:                  data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		HookRuntime:          NewHookRuntime(registry, NewDefaultHookResultApplier()),
		FinalAssistantOutput: "worker delivery text",
		ThreadPersistReady:   true,
		ChannelContext: &ChannelContext{
			ChannelID:        channelID,
			ChannelType:      "telegram",
			ConversationType: "private",
			Conversation: ChannelConversationRef{
				Target: "10001",
			},
		},
	}

	handler := Build([]RunMiddleware{
		NewThreadPersistHookMiddleware(),
		NewChannelDeliveryMiddleware(pool),
	}, func(_ context.Context, _ *RunContext) error { return nil })
	if err := handler(ctx, rc); err == nil {
		t.Fatal("expected delivery error")
	}
	if !provider.called {
		t.Fatal("expected thread persist provider to run before delivery error returned")
	}
}

func TestDeliveryPostRunsBeforeThreadPersistPost(t *testing.T) {
	events := []string{}
	provider := &threadPersistOrderSpy{events: &events}
	registry := NewHookRegistry()
	if err := registry.SetThreadPersistenceProvider(provider); err != nil {
		t.Fatalf("set thread provider: %v", err)
	}

	rc := &RunContext{
		HookRuntime:        NewHookRuntime(registry, NewDefaultHookResultApplier()),
		ThreadPersistReady: true,
	}

	handler := Build([]RunMiddleware{
		NewThreadPersistHookMiddleware(),
		func(ctx context.Context, rc *RunContext, next RunHandler) error {
			events = append(events, "delivery_pre")
			err := next(ctx, rc)
			events = append(events, "delivery_post")
			return err
		},
	}, func(_ context.Context, _ *RunContext) error { return nil })

	if err := handler(context.Background(), rc); err != nil {
		t.Fatalf("handler failed: %v", err)
	}

	want := []string{"delivery_pre", "delivery_post", "thread_persist"}
	if len(events) != len(want) {
		t.Fatalf("events len = %d, want %d: %#v", len(events), len(want), events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events[%d] = %q, want %q; all=%#v", i, events[i], want[i], events)
		}
	}
}

func TestChannelDeliveryMiddlewareSendsTelegramOutputsAsSeparateMessages(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_multi_output")
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

	var sentTexts []string
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		sentTexts = append(sentTexts, payload.Text)
		messageID := 800 + len(sentTexts)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"chat":{"id":10001}}}`, messageID)
	}))
	defer server.Close()
	t.Setenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL", server.URL)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(ctx, `INSERT INTO secrets (id, encrypted_value, key_version) VALUES ($1, $2, 1)`, secretID, encryptChannelToken(t, keyBytes, "bot-token")); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channels (id, channel_type, credentials_id, is_active) VALUES ($1, 'telegram', $2, TRUE)`, channelID, secretID); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	rc := &RunContext{
		Run:                   data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		FinalAssistantOutput:  "第一条第二条",
		FinalAssistantOutputs: []string{"第一条", "第二条"},
		ChannelContext: &ChannelContext{
			ChannelID:   channelID,
			ChannelType: "telegram",
			Conversation: ChannelConversationRef{
				Target: "10001",
			},
		},
	}

	mw := NewChannelDeliveryMiddleware(pool)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
	if len(sentTexts) != 2 {
		t.Fatalf("expected 2 telegram sends, got %d (%#v)", len(sentTexts), sentTexts)
	}
	if !strings.Contains(sentTexts[0], "第一条") || !strings.Contains(sentTexts[1], "第二条") {
		t.Fatalf("unexpected telegram texts: %#v", sentTexts)
	}

	var deliveryCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM channel_message_deliveries`).Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveryCount != 2 {
		t.Fatalf("expected 2 delivery records, got %d", deliveryCount)
	}
}

func TestChannelDeliveryMiddlewareDisablesTelegramProgressTrackerInGroups(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_group_progress_disabled")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	keyBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = byte(i + 1)
	}
	t.Setenv("ARKLOOP_ENCRYPTION_KEY", hex.EncodeToString(keyBytes))

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(ctx, `INSERT INTO secrets (id, encrypted_value, key_version) VALUES ($1, $2, 1)`, secretID, encryptChannelToken(t, keyBytes, "bot-token")); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channels (id, channel_type, credentials_id, is_active) VALUES ($1, 'telegram', $2, TRUE)`, channelID, secretID); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	mw := NewChannelDeliveryMiddleware(pool)
	if err := mw(ctx, &RunContext{
		Run: data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		ChannelContext: &ChannelContext{
			ChannelID:        channelID,
			ChannelType:      "telegram",
			ConversationType: "supergroup",
			Conversation:     ChannelConversationRef{Target: "10001"},
		},
	}, func(_ context.Context, rc *RunContext) error {
		if rc.TelegramToolBoundaryFlush == nil {
			t.Fatal("expected TelegramToolBoundaryFlush to be set for telegram channel")
		}
		if rc.TelegramProgressTracker != nil {
			t.Fatal("expected TelegramProgressTracker to stay disabled in telegram groups")
		}
		return nil
	}); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
}

func TestChannelDeliveryMiddlewarePreservesFinalOutputsWhenNoStreamFlush(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_steering_multi")
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

	var sentTexts []string
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		sentTexts = append(sentTexts, payload.Text)
		messageID := 900 + len(sentTexts)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"chat":{"id":10001}}}`, messageID)
	}))
	defer server.Close()
	t.Setenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL", server.URL)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(ctx, `INSERT INTO secrets (id, encrypted_value, key_version) VALUES ($1, $2, 1)`, secretID, encryptChannelToken(t, keyBytes, "bot-token")); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channels (id, channel_type, credentials_id, is_active) VALUES ($1, 'telegram', $2, TRUE)`, channelID, secretID); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	mw := NewChannelDeliveryMiddleware(pool)
	if err := mw(ctx, &RunContext{
		Run: data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		ChannelContext: &ChannelContext{
			ChannelID:        channelID,
			ChannelType:      "telegram",
			ConversationType: "supergroup",
			Conversation:     ChannelConversationRef{Target: "10001"},
			TriggerMessage:   &ChannelMessageRef{MessageID: "55"},
		},
	}, func(_ context.Context, rc *RunContext) error {
		if rc.TelegramToolBoundaryFlush == nil {
			t.Fatal("expected TelegramToolBoundaryFlush to be set for telegram channel")
		}
		// Simulate steering: multi-turn outputs without any tool.call boundary flush
		rc.FinalAssistantOutput = "turn1 replyturn2 reply"
		rc.FinalAssistantOutputs = []string{"turn1 reply", "turn2 reply"}
		return nil
	}); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	if len(sentTexts) != 2 {
		t.Fatalf("expected 2 separate telegram sends (one per turn), got %d (%#v)", len(sentTexts), sentTexts)
	}
	if !strings.Contains(sentTexts[0], "turn1") || !strings.Contains(sentTexts[1], "turn2") {
		t.Fatalf("unexpected telegram texts: %#v", sentTexts)
	}

	var deliveryCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM channel_message_deliveries`).Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveryCount != 2 {
		t.Fatalf("expected 2 delivery records, got %d", deliveryCount)
	}
}

func TestChannelDeliveryMiddlewareSkipsReplyReferenceInPrivateTelegram(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_private_no_reply")
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
		keyBytes[i] = byte(i + 41)
	}
	t.Setenv("ARKLOOP_ENCRYPTION_KEY", hex.EncodeToString(keyBytes))

	var sent struct {
		ReplyToMessageID string `json:"reply_to_message_id"`
	}
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path != "/botbot-token/sendMessage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":702,"chat":{"id":10001}}}`))
	}))
	defer server.Close()
	t.Setenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL", server.URL)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()

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
			ChannelID:        channelID,
			ChannelType:      "telegram",
			ConversationType: "private",
			Conversation:     ChannelConversationRef{Target: "10001"},
			InboundMessage:   ChannelMessageRef{MessageID: "77"},
			TriggerMessage:   &ChannelMessageRef{MessageID: "77"},
		},
	}

	mw := NewChannelDeliveryMiddleware(pool)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	if sent.ReplyToMessageID != "" {
		t.Fatalf("expected private telegram send without reply_to_message_id, got %#v", sent)
	}

	var parentID *string
	if err := pool.QueryRow(ctx,
		`SELECT platform_parent_message_id FROM channel_message_ledger LIMIT 1`,
	).Scan(&parentID); err != nil {
		t.Fatalf("load ledger parent: %v", err)
	}
	if parentID != nil {
		t.Fatalf("expected no ledger parent for private telegram, got %#v", parentID)
	}
}

func TestChannelDeliveryMiddlewareUsesReplyOverrideInGroupTelegram(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_reply_override")
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
		keyBytes[i] = byte(i + 51)
	}
	t.Setenv("ARKLOOP_ENCRYPTION_KEY", hex.EncodeToString(keyBytes))

	var sent struct {
		ReplyToMessageID string `json:"reply_to_message_id"`
	}
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if strings.Contains(r.URL.Path, "/sendMessage") {
			if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
				t.Fatalf("decode request: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":703,"chat":{"id":10002}}}`))
	}))
	defer server.Close()
	t.Setenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL", server.URL)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()

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
		FinalAssistantOutput: "override reply text",
		ChannelContext: &ChannelContext{
			ChannelID:        channelID,
			ChannelType:      "telegram",
			ConversationType: "group",
			Conversation:     ChannelConversationRef{Target: "10002"},
			InboundMessage:   ChannelMessageRef{MessageID: "100"},
		},
		ChannelReplyOverride: &ChannelMessageRef{MessageID: "999"},
	}

	mw := NewChannelDeliveryMiddleware(pool)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	if sent.ReplyToMessageID != "999" {
		t.Fatalf("expected reply_to_message_id=999 (from override), got %q", sent.ReplyToMessageID)
	}
}

func TestChannelDeliveryMiddlewareSkipsDefaultReplyAfterFirstOutboundInRun(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_skip_second_reply")
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
		keyBytes[i] = byte(i + 61)
	}
	t.Setenv("ARKLOOP_ENCRYPTION_KEY", hex.EncodeToString(keyBytes))

	var sent struct {
		ReplyToMessageID string `json:"reply_to_message_id"`
	}
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if strings.Contains(r.URL.Path, "/sendMessage") {
			if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
				t.Fatalf("decode request: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":704,"chat":{"id":10003}}}`))
	}))
	defer server.Close()
	t.Setenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL", server.URL)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()

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
	if _, err := pool.Exec(ctx,
		`INSERT INTO channel_message_ledger (
			channel_id, channel_type, direction, thread_id, run_id,
			platform_conversation_id, platform_message_id, platform_parent_message_id, metadata_json
		) VALUES ($1, 'telegram', 'outbound', $2, $3, '10003', '700', '55', '{}'::jsonb)`,
		channelID, threadID, runID,
	); err != nil {
		t.Fatalf("seed prior outbound ledger: %v", err)
	}

	rc := &RunContext{
		Run:                  data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		FinalAssistantOutput: "second message in same run",
		ChannelContext: &ChannelContext{
			ChannelID:        channelID,
			ChannelType:      "telegram",
			ConversationType: "group",
			Conversation:     ChannelConversationRef{Target: "10003"},
			TriggerMessage:   &ChannelMessageRef{MessageID: "55"},
		},
	}

	mw := NewChannelDeliveryMiddleware(pool)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	if sent.ReplyToMessageID != "" {
		t.Fatalf("expected no default reply_to_message_id after first outbound in run, got %q", sent.ReplyToMessageID)
	}
}

func TestChannelDeliveryMiddlewarePersistsDiscordDeliveryAndReplyReference(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_discord_delivery_success")
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
		keyBytes[i] = byte(i + 11)
	}
	t.Setenv("ARKLOOP_ENCRYPTION_KEY", hex.EncodeToString(keyBytes))

	var sent struct {
		Content          string `json:"content"`
		MessageReference *struct {
			MessageID string `json:"message_id"`
		} `json:"message_reference"`
	}
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path != "/channels/9001/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bot discord-token" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"701"}`))
	}))
	defer server.Close()
	t.Setenv("ARKLOOP_DISCORD_API_BASE_URL", server.URL)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(ctx,
		`INSERT INTO secrets (id, encrypted_value, key_version) VALUES ($1, $2, 1)`,
		secretID,
		encryptChannelToken(t, keyBytes, "discord-token"),
	); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO channels (id, channel_type, credentials_id, is_active) VALUES ($1, 'discord', $2, TRUE)`,
		channelID,
		secretID,
	); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	rc := &RunContext{
		Run:                  data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		FinalAssistantOutput: "discord delivery text",
		ChannelContext: &ChannelContext{
			ChannelID:   channelID,
			ChannelType: "discord",
			Conversation: ChannelConversationRef{
				Target: "9001",
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
		ledgerType    string
	)
	if err := pool.QueryRow(ctx,
		`SELECT
			(SELECT COUNT(*) FROM channel_message_deliveries),
			(SELECT COUNT(*) FROM channel_message_ledger),
			(SELECT platform_parent_message_id FROM channel_message_ledger LIMIT 1),
			(SELECT channel_type FROM channel_message_ledger LIMIT 1)`,
	).Scan(&deliveryCount, &ledgerCount, &parentID, &ledgerType); err != nil {
		t.Fatalf("load delivery rows: %v", err)
	}
	if deliveryCount != 1 || ledgerCount != 1 {
		t.Fatalf("expected one delivery and one ledger row, got deliveries=%d ledger=%d", deliveryCount, ledgerCount)
	}
	if parentID == nil || *parentID != "55" {
		t.Fatalf("unexpected platform_parent_message_id: %#v", parentID)
	}
	if ledgerType != "discord" {
		t.Fatalf("unexpected channel_type: %q", ledgerType)
	}
	if sent.Content != "discord delivery text" {
		t.Fatalf("unexpected discord content: %q", sent.Content)
	}
	if sent.MessageReference == nil || sent.MessageReference.MessageID != "55" {
		t.Fatalf("unexpected discord message reference: %#v", sent.MessageReference)
	}
}

func TestChannelDeliveryMiddlewareSetsInboundReaction(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_reaction")
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

	var reactionJSON string
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/setMessageReaction") {
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read reaction body: %v", err)
			}
			reactionJSON = string(raw)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		if r.URL.Path != "/botbot-token/sendMessage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":702,"chat":{"id":10001}}}`))
	}))
	defer server.Close()

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()

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
		`INSERT INTO channels (id, channel_type, credentials_id, is_active, config_json)
		 VALUES ($1, 'telegram', $2, TRUE, '{"telegram_reaction_emoji":"👍"}'::jsonb)`,
		channelID,
		secretID,
	); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	telegramClient := telegrambot.NewClient(server.URL, server.Client())

	rc := &RunContext{
		Run:                  data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		FinalAssistantOutput: "reaction probe",
		ChannelContext: &ChannelContext{
			ChannelID:   channelID,
			ChannelType: "telegram",
			Conversation: ChannelConversationRef{
				Target: "10001",
			},
			InboundMessage: ChannelMessageRef{MessageID: "601"},
			TriggerMessage: &ChannelMessageRef{MessageID: "601"},
		},
	}

	mw := NewChannelDeliveryMiddlewareWithOptions(pool, ChannelDeliveryMiddlewareOptions{Telegram: telegramClient})
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
	if !strings.Contains(reactionJSON, `"message_id":601`) || !strings.Contains(reactionJSON, "👍") {
		t.Fatalf("unexpected setMessageReaction body: %s", reactionJSON)
	}
}

func TestChannelDeliveryMiddlewareSuppressesSilentHeartbeat(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_heartbeat_silent")
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

	sendCount := 0
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		sendCount++
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
		HeartbeatRun:         true,
		FinalAssistantOutput: "（静默心跳，没有需要跟进的事项）",
		HeartbeatToolOutcome: &HeartbeatDecisionOutcome{Reply: false},
		ChannelContext: &ChannelContext{
			ChannelID:   channelID,
			ChannelType: "telegram",
			Conversation: ChannelConversationRef{
				Target: "10001",
			},
		},
	}

	mw := NewChannelDeliveryMiddleware(pool)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	if sendCount != 0 {
		t.Fatalf("expected silent heartbeat to skip telegram send, got %d requests", sendCount)
	}

	var deliveryCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM channel_message_deliveries`).Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveryCount != 0 {
		t.Fatalf("expected no delivery rows, got %d", deliveryCount)
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
		nil,
		[]string{"701"},
		nil,
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

func TestTerminalStatusMessagePrefersProviderMessage(t *testing.T) {
	got := TerminalStatusMessage(map[string]any{
		"message": "OpenAI stream returned error",
		"details": map[string]any{
			"provider_message": "usage limit exceeded (2056)",
			"type":             "rate_limit_error",
		},
	})
	want := "usage limit exceeded (2056) (rate_limit_error)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTerminalStatusMessageOmitsRedundantType(t *testing.T) {
	got := TerminalStatusMessage(map[string]any{
		"message": "x",
		"details": map[string]any{
			"provider_message": "rate_limit_error: slow down",
			"type":             "rate_limit_error",
		},
	})
	want := "rate_limit_error: slow down"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTerminalStatusMessageFallbackMessage(t *testing.T) {
	got := TerminalStatusMessage(map[string]any{
		"message": "build executor failed",
	})
	if got != "build executor failed" {
		t.Fatalf("got %q", got)
	}
}

func TestChannelDeliveryMiddlewareSendsChannelTerminalNotice(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_notice")
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
		Text             string `json:"text"`
	}
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		if r.URL.Path != "/botbot-token/sendMessage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":801,"chat":{"id":10001}}}`))
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

	notice := "usage limit exceeded (2056) (rate_limit_error)"
	rc := &RunContext{
		Run:                   data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		FinalAssistantOutput:  "",
		ChannelTerminalNotice: notice,
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
	if !strings.Contains(sent.Text, "usage limit exceeded") {
		t.Fatalf("expected notice in telegram text, got %q", sent.Text)
	}
	if sent.ReplyToMessageID != "55" || sent.MessageThreadID != threadRef {
		t.Fatalf("unexpected telegram send payload: %#v", sent)
	}

	var (
		storedContent string
		metadataRaw   []byte
	)
	if err := pool.QueryRow(ctx, `SELECT content, metadata_json FROM messages WHERE thread_id = $1 ORDER BY thread_seq DESC LIMIT 1`, threadID).Scan(&storedContent, &metadataRaw); err != nil {
		t.Fatalf("query terminal notice message: %v", err)
	}
	if storedContent != notice {
		t.Fatalf("unexpected stored terminal notice: %q", storedContent)
	}
	var metadata map[string]any
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata["delivery_notice"] != true || metadata["terminal_notice"] != true || metadata["exclude_from_prompt"] != true {
		t.Fatalf("unexpected terminal notice metadata: %#v", metadata)
	}
}

func TestChannelDeliveryMiddlewareDoesNotPersistNoticeWhenSendFails(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_notice_send_fail")
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

	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		if r.URL.Path == "/botbot-token/sendMessage" {
			nethttp.Error(w, "fail", nethttp.StatusInternalServerError)
			return
		}
		t.Fatalf("unexpected path: %s", r.URL.Path)
	}))
	defer server.Close()
	t.Setenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL", server.URL)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()

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
		Run:                   data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		FinalAssistantOutput:  "",
		ChannelTerminalNotice: "notice",
		ChannelContext: &ChannelContext{
			ChannelID:   channelID,
			ChannelType: "telegram",
			Conversation: ChannelConversationRef{
				Target: "10001",
			},
		},
	}

	mw := NewChannelDeliveryMiddleware(pool)
	_ = mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil })

	var msgCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM messages WHERE thread_id = $1`, threadID).Scan(&msgCount); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if msgCount != 0 {
		t.Fatalf("expected no persisted notice message, got %d", msgCount)
	}
}

func TestTryDeliverTelegramInjectionBlockNoticePersistsThreadNotice(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_injection_notice")
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

	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path != "/botbot-token/sendMessage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
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
		Run: data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		ChannelContext: &ChannelContext{
			ChannelID:   channelID,
			ChannelType: "telegram",
			Conversation: ChannelConversationRef{
				Target: "10001",
			},
		},
	}

	notice := "blocked by injection guard"
	TryDeliverTelegramInjectionBlockNotice(ctx, pool, rc, notice)

	var (
		content     string
		metadataRaw []byte
		ledgerCount int
	)
	if err := pool.QueryRow(ctx, `SELECT content, metadata_json FROM messages WHERE thread_id = $1 ORDER BY thread_seq DESC LIMIT 1`, threadID).Scan(&content, &metadataRaw); err != nil {
		t.Fatalf("query notice message: %v", err)
	}
	if content != notice {
		t.Fatalf("unexpected notice content: %q", content)
	}
	var metadata map[string]any
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata["delivery_notice"] != true || metadata["terminal_notice"] != true || metadata["exclude_from_prompt"] != true {
		t.Fatalf("unexpected notice metadata: %#v", metadata)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM channel_message_ledger WHERE run_id = $1`, runID).Scan(&ledgerCount); err != nil {
		t.Fatalf("query ledger count: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("expected one outbound ledger row, got %d", ledgerCount)
	}
}

func TestTryDeliverChannelInjectionBlockNoticePersistsDiscordThreadNotice(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_discord_injection_notice")
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
		keyBytes[i] = byte(i + 21)
	}
	t.Setenv("ARKLOOP_ENCRYPTION_KEY", hex.EncodeToString(keyBytes))

	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path != "/channels/9001/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"901"}`))
	}))
	defer server.Close()
	t.Setenv("ARKLOOP_DISCORD_API_BASE_URL", server.URL)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(ctx,
		`INSERT INTO secrets (id, encrypted_value, key_version) VALUES ($1, $2, 1)`,
		secretID,
		encryptChannelToken(t, keyBytes, "discord-token"),
	); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO channels (id, channel_type, credentials_id, is_active) VALUES ($1, 'discord', $2, TRUE)`,
		channelID,
		secretID,
	); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	rc := &RunContext{
		Run: data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		ChannelContext: &ChannelContext{
			ChannelID:   channelID,
			ChannelType: "discord",
			Conversation: ChannelConversationRef{
				Target: "9001",
			},
		},
	}

	notice := "blocked by injection guard"
	TryDeliverChannelInjectionBlockNotice(ctx, pool, rc, notice)

	var (
		content     string
		metadataRaw []byte
		ledgerCount int
	)
	if err := pool.QueryRow(ctx, `SELECT content, metadata_json FROM messages WHERE thread_id = $1 ORDER BY thread_seq DESC LIMIT 1`, threadID).Scan(&content, &metadataRaw); err != nil {
		t.Fatalf("query notice message: %v", err)
	}
	if content != notice {
		t.Fatalf("unexpected notice content: %q", content)
	}
	var metadata map[string]any
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata["delivery_notice"] != true || metadata["terminal_notice"] != true || metadata["exclude_from_prompt"] != true {
		t.Fatalf("unexpected notice metadata: %#v", metadata)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM channel_message_ledger WHERE run_id = $1`, runID).Scan(&ledgerCount); err != nil {
		t.Fatalf("query ledger count: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("expected one outbound ledger row, got %d", ledgerCount)
	}
}

func createChannelDeliveryTables(t *testing.T, pool *pgxpool.Pool, ledgerTableSQL string) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TABLE channels (
			id UUID PRIMARY KEY,
			channel_type TEXT NOT NULL,
			credentials_id UUID NULL,
			is_active BOOLEAN NOT NULL DEFAULT FALSE,
			config_json JSONB NOT NULL DEFAULT '{}'::jsonb
		)`,
		`CREATE TABLE channel_message_deliveries (
			run_id UUID NULL,
			thread_id UUID NULL,
			channel_id UUID NOT NULL,
			platform_chat_id TEXT NOT NULL,
			platform_message_id TEXT NOT NULL,
			UNIQUE (channel_id, platform_chat_id, platform_message_id)
		)`,
		`CREATE TABLE channel_delivery_outbox (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			run_id UUID NOT NULL,
			thread_id UUID NULL,
			channel_id UUID NOT NULL,
			channel_type TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			payload_json JSONB NOT NULL,
			segments_sent INTEGER NOT NULL DEFAULT 0,
			attempts INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NULL,
			next_retry_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX idx_outbox_drain ON channel_delivery_outbox (status, next_retry_at)
			WHERE status = 'pending'`,
		`CREATE UNIQUE INDEX idx_outbox_run ON channel_delivery_outbox (run_id)
			WHERE status != 'dead'`,
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

func TestChannelDeliveryMiddlewareWritesOutboxAndInlineTrySucceeds(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_outbox_success")
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

	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":801,"chat":{"id":10001}}}`))
	}))
	defer server.Close()
	t.Setenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL", server.URL)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	secretID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(ctx, `INSERT INTO secrets (id, encrypted_value, key_version) VALUES ($1, $2, 1)`, secretID, encryptChannelToken(t, keyBytes, "bot-token")); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channels (id, channel_type, credentials_id, is_active) VALUES ($1, 'telegram', $2, TRUE)`, channelID, secretID); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	rc := &RunContext{
		Run:                  data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		FinalAssistantOutput: "outbox inline try text",
		ChannelContext: &ChannelContext{
			ChannelID:   channelID,
			ChannelType: "telegram",
			Conversation: ChannelConversationRef{
				Target: "10001",
			},
		},
	}

	mw := NewChannelDeliveryMiddleware(pool)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	var outboxStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM channel_delivery_outbox WHERE run_id = $1`, runID).Scan(&outboxStatus); err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if outboxStatus != "sent" {
		t.Fatalf("expected outbox status sent, got %q", outboxStatus)
	}

	var createdAt, nextRetryAt time.Time
	if err := pool.QueryRow(ctx, `SELECT created_at, next_retry_at FROM channel_delivery_outbox WHERE run_id = $1`, runID).Scan(&createdAt, &nextRetryAt); err != nil {
		t.Fatalf("query outbox lease: %v", err)
	}
	if !nextRetryAt.After(createdAt) {
		t.Fatalf("expected fresh outbox lease to push next_retry_at after created_at, got created_at=%s next_retry_at=%s", createdAt, nextRetryAt)
	}
}

func TestChannelDeliveryMiddlewareWritesOutboxAndInlineTryFails(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery_outbox_fail")
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

	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		w.WriteHeader(nethttp.StatusBadGateway)
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

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)
	if _, err := pool.Exec(ctx, `INSERT INTO secrets (id, encrypted_value, key_version) VALUES ($1, $2, 1)`, secretID, encryptChannelToken(t, keyBytes, "bot-token")); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channels (id, channel_type, credentials_id, is_active) VALUES ($1, 'telegram', $2, TRUE)`, channelID, secretID); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	rc := &RunContext{
		Run:                  data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		FinalAssistantOutput: "outbox inline try fail text",
		ChannelContext: &ChannelContext{
			ChannelID:   channelID,
			ChannelType: "telegram",
			Conversation: ChannelConversationRef{
				Target: "10001",
			},
		},
	}

	mw := NewChannelDeliveryMiddleware(pool)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	var outboxStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM channel_delivery_outbox WHERE run_id = $1`, runID).Scan(&outboxStatus); err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if outboxStatus != "pending" {
		t.Fatalf("expected outbox status pending after inline try failure, got %q", outboxStatus)
	}

	var failureCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM run_events WHERE run_id = $1 AND type = 'run.channel_delivery_failed'`, runID).Scan(&failureCount); err != nil {
		t.Fatalf("count failure events: %v", err)
	}
	if failureCount != 1 {
		t.Fatalf("expected 1 failure event, got %d", failureCount)
	}
}
