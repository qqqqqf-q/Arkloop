//go:build !desktop

package pipeline

import (
	"context"
	"testing"

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
	senderIdentityID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)

	if _, err := pool.Exec(ctx,
		`INSERT INTO channel_identities (id, channel_type, platform_subject_id, heartbeat_enabled, heartbeat_interval_minutes, heartbeat_model, metadata)
		 VALUES ($1, 'discord', 'user-42', 1, 17, 'discord-model', '{}'::jsonb)`,
		senderIdentityID,
	); err != nil {
		t.Fatalf("insert channel identity: %v", err)
	}

	rc := &RunContext{
		Run:               data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		PersonaDefinition: &personas.Definition{ID: "discord-persona", HeartbeatEnabled: true},
		ChannelContext: &ChannelContext{
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
	row, err := repo.GetHeartbeat(ctx, pool, senderIdentityID)
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
	groupIdentityID := uuid.New()
	senderIdentityID := uuid.New()

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)

	if _, err := pool.Exec(ctx,
		`INSERT INTO channel_identities (id, channel_type, platform_subject_id, heartbeat_enabled, heartbeat_interval_minutes, heartbeat_model, metadata)
		 VALUES
		 ($1, 'telegram', 'chat-1001', 1, 9, 'group-model', '{}'::jsonb),
		 ($2, 'telegram', 'user-1002', 1, 21, 'sender-model', '{}'::jsonb)`,
		groupIdentityID,
		senderIdentityID,
	); err != nil {
		t.Fatalf("insert channel identities: %v", err)
	}

	rc := &RunContext{
		Run:               data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		PersonaDefinition: &personas.Definition{ID: "telegram-persona", HeartbeatEnabled: true},
		ChannelContext: &ChannelContext{
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
	groupRow, err := repo.GetHeartbeat(ctx, pool, groupIdentityID)
	if err != nil {
		t.Fatalf("get group heartbeat: %v", err)
	}
	if groupRow == nil {
		t.Fatal("expected group heartbeat trigger")
	}
	if groupRow.Model != "group-model" {
		t.Fatalf("unexpected group model: %q", groupRow.Model)
	}
	if senderRow, err := repo.GetHeartbeat(ctx, pool, senderIdentityID); err != nil {
		t.Fatalf("get sender heartbeat: %v", err)
	} else if senderRow != nil {
		t.Fatalf("expected no sender trigger, got %#v", senderRow)
	}
}
