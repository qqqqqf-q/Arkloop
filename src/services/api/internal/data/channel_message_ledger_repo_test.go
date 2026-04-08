package data

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type channelLedgerTestEnv struct {
	ctx       context.Context
	pool      *pgxpool.Pool
	repo      *ChannelMessageLedgerRepository
	accountID uuid.UUID
	channelID uuid.UUID
	threadID  uuid.UUID
}

const (
	testInboundStatePendingDispatch = "pending_dispatch"
	testInboundStateEnqueuedNewRun  = "new_run_enqueued"
)

func TestChannelMessageLedgerDeleteOlderThan(t *testing.T) {
	env := setupChannelLedgerTestEnv(t, "api_go_channel_message_ledger_ttl")
	if _, err := env.pool.Exec(env.ctx, `
		INSERT INTO accounts (id, slug, name, type) VALUES ($1, 'ledger-account', 'Ledger Account', 'personal');
		INSERT INTO channels (id, account_id, channel_type, persona_id, owner_user_id, webhook_secret, webhook_url, is_active, config_json)
		VALUES ($2, $1, 'discord', NULL, NULL, 'whsec', 'https://example.com', true, '{}'::jsonb);
		INSERT INTO channel_message_ledger (
			channel_id, channel_type, direction, platform_conversation_id, platform_message_id, metadata_json, created_at
		) VALUES (
			$2, 'discord', 'inbound', 'conv', 'msg', '{}'::jsonb, $3
		)`,
		uuid.New(),
		uuid.New(),
		time.Now().UTC().Add(-48*time.Hour),
	); err != nil {
		t.Fatalf("seed ledger row: %v", err)
	}

	count, err := env.repo.DeleteOlderThan(env.ctx, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 deleted row, got %d", count)
	}
}

func TestChannelMessageLedgerGetOpenInboundBatchByThread(t *testing.T) {
	env := setupChannelLedgerTestEnv(t, "api_go_channel_message_ledger_open_batch")

	now := time.Now().UTC().Truncate(time.Second)
	if _, err := env.pool.Exec(env.ctx, `
		INSERT INTO channel_message_ledger (
			channel_id, channel_type, direction, thread_id, platform_conversation_id, platform_message_id, metadata_json, created_at
		) VALUES
			($1, 'telegram', 'inbound', $2, 'c-1', 'm-1', $3::jsonb, $4),
			($1, 'telegram', 'inbound', $2, 'c-1', 'm-2', $3::jsonb, $5),
			($1, 'telegram', 'inbound', $2, 'c-1', 'm-3', $6::jsonb, $7)`,
		env.channelID,
		env.threadID,
		mustInboundMetadataJSON(t, testInboundStatePendingDispatch, "batch-a", now.Add(2*time.Minute)),
		now.Add(-3*time.Second),
		now.Add(-2*time.Second),
		mustInboundMetadataJSON(t, testInboundStatePendingDispatch, "batch-b", now.Add(1*time.Minute)),
		now.Add(-1*time.Second),
	); err != nil {
		t.Fatalf("seed ledger rows: %v", err)
	}

	batch, err := env.repo.GetOpenInboundBatchByThread(env.ctx, env.channelID, env.threadID)
	if err != nil {
		t.Fatalf("GetOpenInboundBatchByThread: %v", err)
	}
	if batch == nil {
		t.Fatal("expected batch, got nil")
	}
	if batch.BatchID != "batch-b" {
		t.Fatalf("batch id = %q, want batch-b", batch.BatchID)
	}
	if got := batch.MessageCount(); got != 1 {
		t.Fatalf("message count = %d, want 1", got)
	}
	if !batch.DueAt.Equal(now.Add(1 * time.Minute)) {
		t.Fatalf("due_at = %s, want %s", batch.DueAt, now.Add(1*time.Minute))
	}
}

func TestChannelMessageLedgerListDueInboundBatchesByChannel(t *testing.T) {
	env := setupChannelLedgerTestEnv(t, "api_go_channel_message_ledger_due_batches")
	threadRepo, err := NewThreadRepository(env.pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	projectRepo, err := NewProjectRepository(env.pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	userRepo, err := NewUserRepository(env.pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	user, err := userRepo.Create(env.ctx, "ledger-due-user", "ledger-due-user@test.com", "zh")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	project, err := projectRepo.CreateDefaultForOwner(env.ctx, env.accountID, user.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	thread2, err := threadRepo.Create(env.ctx, env.accountID, &user.ID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create thread2: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if _, err := env.pool.Exec(env.ctx, `
		INSERT INTO channel_message_ledger (
			channel_id, channel_type, direction, thread_id, platform_conversation_id, platform_message_id, metadata_json, created_at
		) VALUES
			($1, 'discord', 'inbound', $2, 'd-1', 'm-due', $3::jsonb, $4),
			($1, 'discord', 'inbound', $5, 'd-2', 'm-future', $6::jsonb, $7)`,
		env.channelID,
		env.threadID,
		mustInboundMetadataJSON(t, testInboundStatePendingDispatch, "batch-due", now.Add(-1*time.Second)),
		now.Add(-5*time.Second),
		thread2.ID,
		mustInboundMetadataJSON(t, testInboundStatePendingDispatch, "batch-future", now.Add(5*time.Minute)),
		now.Add(-4*time.Second),
	); err != nil {
		t.Fatalf("seed due rows: %v", err)
	}

	items, err := env.repo.ListDueInboundBatchesByChannel(env.ctx, env.channelID, now, 8)
	if err != nil {
		t.Fatalf("ListDueInboundBatchesByChannel: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("due batches = %d, want 1", len(items))
	}
	if items[0].BatchID != "batch-due" {
		t.Fatalf("batch id = %q, want batch-due", items[0].BatchID)
	}
}

func TestChannelMessageLedgerUpdateInboundMetadata(t *testing.T) {
	env := setupChannelLedgerTestEnv(t, "api_go_channel_message_ledger_update_metadata")
	if _, err := env.pool.Exec(env.ctx, `
		INSERT INTO channel_message_ledger (
			channel_id, channel_type, direction, thread_id, platform_conversation_id, platform_message_id, metadata_json
		) VALUES (
			$1, 'telegram', 'inbound', $2, 'c-update', 'm-update', $3::jsonb
		)`,
		env.channelID,
		env.threadID,
		mustInboundMetadataJSON(t, testInboundStatePendingDispatch, "batch-update", time.Now().UTC()),
	); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	nextRaw := mustInboundMetadataJSON(t, testInboundStateEnqueuedNewRun, "batch-update", time.Now().UTC().Add(time.Minute))
	updated, err := env.repo.UpdateInboundMetadata(env.ctx, env.channelID, "c-update", "m-update", nextRaw)
	if err != nil {
		t.Fatalf("UpdateInboundMetadata: %v", err)
	}
	if !updated {
		t.Fatal("expected metadata update to affect row")
	}
	entry, err := env.repo.GetInboundEntry(env.ctx, env.channelID, "c-update", "m-update")
	if err != nil {
		t.Fatalf("GetInboundEntry: %v", err)
	}
	if entry == nil {
		t.Fatal("expected entry, got nil")
	}
	if got := inboundLedgerState(entry.MetadataJSON); got != testInboundStateEnqueuedNewRun {
		t.Fatalf("ingress state = %q, want %q", got, testInboundStateEnqueuedNewRun)
	}
}

func setupChannelLedgerTestEnv(t *testing.T, dbName string) channelLedgerTestEnv {
	t.Helper()
	db := testutil.SetupPostgresDatabase(t, dbName)
	ctx := context.Background()
	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 4, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	repo, err := NewChannelMessageLedgerRepository(pool)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	accountRepo, err := NewAccountRepository(pool)
	if err != nil {
		t.Fatalf("new account repo: %v", err)
	}
	userRepo, err := NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	projectRepo, err := NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	threadRepo, err := NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	channelsRepo, err := NewChannelsRepository(pool)
	if err != nil {
		t.Fatalf("new channels repo: %v", err)
	}

	account, err := accountRepo.Create(ctx, "ledger-test-"+uuid.NewString()[:8], "Ledger Test", "personal")
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	user, err := userRepo.Create(ctx, "ledger-owner-"+uuid.NewString()[:8], "ledger-owner@test.com", "zh")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	project, err := projectRepo.CreateDefaultForOwner(ctx, account.ID, user.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	thread, err := threadRepo.Create(ctx, account.ID, &user.ID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	channelID := uuid.New()
	if _, err := channelsRepo.Create(
		ctx,
		channelID,
		account.ID,
		"telegram",
		nil,
		nil,
		&user.ID,
		"whsec",
		"https://example.com",
		json.RawMessage(`{}`),
	); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	return channelLedgerTestEnv{
		ctx:       ctx,
		pool:      pool,
		repo:      repo,
		accountID: account.ID,
		channelID: channelID,
		threadID:  thread.ID,
	}
}

func mustInboundMetadataJSON(t *testing.T, state string, batchID string, dueAt time.Time) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"inbound_state":             state,
		"ingress_state":             state,
		"pending_dispatch_batch_id": batchID,
		"pending_dispatch_due_at":   dueAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	return raw
}

func inboundLedgerState(raw json.RawMessage) string {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	value, _ := payload["ingress_state"].(string)
	return value
}
