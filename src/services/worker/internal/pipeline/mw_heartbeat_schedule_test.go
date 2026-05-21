//go:build !desktop

package pipeline

import (
	"context"
	"testing"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHeartbeatScheduleMiddlewareCreatesTriggerForDiscordPrivateIdentity(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "worker_heartbeat_schedule_discord_dm")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	senderIdentityID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)

	if _, err := pool.Exec(ctx,
		`INSERT INTO channel_identities (id, channel_type, platform_subject_id, metadata)
		 VALUES ($1, 'discord', 'user-42', '{}'::jsonb)`,
		senderIdentityID,
	); err != nil {
		t.Fatalf("insert channel identity: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE threads
		    SET config_json = '{"heartbeat_enabled":true,"heartbeat_interval_minutes":17,"heartbeat_model":"discord-model"}'::jsonb
		  WHERE id = $1`,
		threadID,
	); err != nil {
		t.Fatalf("update thread config: %v", err)
	}

	rc := &RunContext{
		Run:               data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		PersonaDefinition: &personas.Definition{ID: "discord-persona", HeartbeatEnabled: true},
		ChannelContext: &ChannelContext{
			ChannelID:               channelID,
			ChannelType:             "discord",
			ConversationType:        "private",
			SenderChannelIdentityID: senderIdentityID,
			Conversation:            ChannelConversationRef{Target: "dm-channel-1"},
		},
	}

	mw := NewHeartbeatScheduleMiddleware(pool)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	repo := data.ScheduledTriggersRepository{}
	row, err := repo.GetHeartbeatForThread(ctx, pool, threadID)
	if err != nil {
		t.Fatalf("get heartbeat: %v", err)
	}
	if row == nil {
		t.Fatal("expected heartbeat trigger")
	}
	if row.PersonaKey != "discord-persona" {
		t.Fatalf("unexpected persona key: %q", row.PersonaKey)
	}
	if row.Model != "discord-model" {
		t.Fatalf("unexpected model: %q", row.Model)
	}
	if row.IntervalMin != 17 {
		t.Fatalf("unexpected interval: %d", row.IntervalMin)
	}
}

func TestHeartbeatScheduleMiddlewareKeepsTelegramGroupIdentityBehavior(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "worker_heartbeat_schedule_tg_group")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	groupIdentityID := uuid.New()
	senderIdentityID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)

	if _, err := pool.Exec(ctx,
		`INSERT INTO channel_identities (id, channel_type, platform_subject_id, metadata)
		 VALUES
		 ($1, 'telegram', 'chat-1001', '{}'::jsonb),
		 ($2, 'telegram', 'user-1002', '{}'::jsonb)`,
		groupIdentityID,
		senderIdentityID,
	); err != nil {
		t.Fatalf("insert channel identities: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE threads
		    SET config_json = '{"heartbeat_enabled":true,"heartbeat_interval_minutes":9,"heartbeat_model":"group-model"}'::jsonb
		  WHERE id = $1`,
		threadID,
	); err != nil {
		t.Fatalf("update thread config: %v", err)
	}

	rc := &RunContext{
		Run:               data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		PersonaDefinition: &personas.Definition{ID: "telegram-persona", HeartbeatEnabled: true},
		ChannelContext: &ChannelContext{
			ChannelID:               channelID,
			ChannelType:             "telegram",
			ConversationType:        "supergroup",
			SenderChannelIdentityID: senderIdentityID,
			Conversation:            ChannelConversationRef{Target: "chat-1001"},
		},
	}

	mw := NewHeartbeatScheduleMiddleware(pool)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	repo := data.ScheduledTriggersRepository{}
	groupRow, err := repo.GetHeartbeatForThread(ctx, pool, threadID)
	if err != nil {
		t.Fatalf("get group heartbeat: %v", err)
	}
	if groupRow == nil {
		t.Fatal("expected group heartbeat trigger")
	}
	if groupRow.Model != "group-model" {
		t.Fatalf("unexpected group model: %q", groupRow.Model)
	}
	if senderRow, err := repo.GetHeartbeat(ctx, pool, channelID, senderIdentityID); err != nil {
		t.Fatalf("get sender heartbeat: %v", err)
	} else if senderRow != nil {
		t.Fatalf("expected no sender trigger, got %#v", senderRow)
	}
}

func TestHeartbeatScheduleMiddlewarePreservesThreadCooldown(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "worker_heartbeat_schedule_thread_cooldown")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	groupIdentityID := uuid.New()
	senderIdentityID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)

	if _, err := pool.Exec(ctx,
		`INSERT INTO channel_identities (id, channel_type, platform_subject_id, metadata)
		 VALUES
		 ($1, 'telegram', 'chat-2001', '{}'::jsonb),
		 ($2, 'telegram', 'user-2002', '{}'::jsonb)`,
		groupIdentityID,
		senderIdentityID,
	); err != nil {
		t.Fatalf("insert channel identities: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE threads
		    SET config_json = '{"heartbeat_enabled":true,"heartbeat_interval_minutes":9,"heartbeat_model":"group-model"}'::jsonb
		  WHERE id = $1`,
		threadID,
	); err != nil {
		t.Fatalf("update thread config: %v", err)
	}

	repo := data.ScheduledTriggersRepository{}
	if err := repo.UpsertHeartbeatForThread(ctx, pool, accountID, channelID, groupIdentityID, threadID, "telegram-persona", "group-model", false, 9); err != nil {
		t.Fatalf("upsert thread heartbeat: %v", err)
	}
	suspendedUntil := time.Now().UTC().Truncate(time.Microsecond).AddDate(1, 0, 0)
	if err := repo.UpdateCooldownAfterHeartbeatForThread(ctx, pool, threadID, 2, suspendedUntil, nil); err != nil {
		t.Fatalf("seed thread cooldown: %v", err)
	}

	rc := &RunContext{
		Run:               data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		PersonaDefinition: &personas.Definition{ID: "telegram-persona", HeartbeatEnabled: true},
		ChannelContext: &ChannelContext{
			ChannelID:               channelID,
			ChannelType:             "telegram",
			ConversationType:        "supergroup",
			SenderChannelIdentityID: senderIdentityID,
			Conversation:            ChannelConversationRef{Target: "chat-2001"},
		},
	}

	mw := NewHeartbeatScheduleMiddleware(pool)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	row, err := repo.GetHeartbeatForThread(ctx, pool, threadID)
	if err != nil {
		t.Fatalf("get thread heartbeat: %v", err)
	}
	if row == nil {
		t.Fatal("expected thread heartbeat")
	}
	if row.CooldownLevel != 2 {
		t.Fatalf("cooldown_level = %d, want 2", row.CooldownLevel)
	}
	if !row.NextFireAt.Equal(suspendedUntil) {
		t.Fatalf("next_fire_at = %s, want %s", row.NextFireAt, suspendedUntil)
	}
}

func TestHeartbeatScheduleMiddlewareSuspendsAfterFirstDiscussRun(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "worker_heartbeat_schedule_first_discuss_cooldown")
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	channelID := uuid.New()
	groupIdentityID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)

	if _, err := pool.Exec(ctx,
		`INSERT INTO channel_identities (id, channel_type, platform_subject_id, metadata)
		 VALUES ($1, 'telegram', 'chat-3001', '{}'::jsonb)`,
		groupIdentityID,
	); err != nil {
		t.Fatalf("insert channel identity: %v", err)
	}

	repo := data.ScheduledTriggersRepository{}
	if err := repo.UpsertHeartbeatForThread(ctx, pool, accountID, channelID, groupIdentityID, threadID, "telegram-persona", "group-model", false, 9); err != nil {
		t.Fatalf("upsert thread heartbeat: %v", err)
	}
	beforeRun := time.Now().UTC()

	rc := &RunContext{
		Run:               data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		PersonaDefinition: &personas.Definition{ID: "telegram-persona", HeartbeatEnabled: true},
		ChannelContext: &ChannelContext{
			ChannelID:        channelID,
			ChannelType:      "telegram",
			ConversationType: "supergroup",
			Conversation:     ChannelConversationRef{Target: "chat-3001"},
		},
		InputJSON: map[string]any{"run_kind": "discuss"},
	}

	mw := NewHeartbeatScheduleMiddleware(pool)
	if err := mw(ctx, rc, func(_ context.Context, _ *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	row, err := repo.GetHeartbeatForThread(ctx, pool, threadID)
	if err != nil {
		t.Fatalf("get thread heartbeat: %v", err)
	}
	if row == nil {
		t.Fatal("expected thread heartbeat")
	}
	if row.CooldownLevel != 1 {
		t.Fatalf("cooldown_level = %d, want 1", row.CooldownLevel)
	}
	if row.NextFireAt.Before(beforeRun.AddDate(1, 0, 0).Add(-time.Second)) {
		t.Fatalf("next_fire_at = %s, expected suspended cooldown instead of one-minute followup", row.NextFireAt)
	}
}
