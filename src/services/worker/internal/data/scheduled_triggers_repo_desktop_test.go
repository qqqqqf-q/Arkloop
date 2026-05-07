//go:build desktop

package data

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestScheduledTriggersRepositoryResolveHeartbeatThreadUsesPersonaKeyColumn(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	personaID := uuid.New()
	personaKey := "heartbeat-agent"

	seedDesktopAccount(t, db, accountID)
	seedDesktopProject(t, db, accountID, projectID)
	seedDesktopThread(t, db, accountID, projectID, threadID)

	if _, err := db.Exec(ctx,
		`INSERT INTO personas (id, account_id, persona_key, version, display_name, prompt_md, tool_allowlist, tool_denylist, budgets_json, is_active)
		 VALUES ($1, $2, $3, '1', 'Heartbeat Agent', 'prompt', '[]', '[]', '{}', 1)`,
		personaID,
		accountID,
		personaKey,
	); err != nil {
		t.Fatalf("insert persona: %v", err)
	}

	if _, err := db.Exec(ctx,
		`INSERT INTO channels (id, account_id, channel_type, persona_id, is_active, config_json)
		 VALUES ($1, $2, 'telegram', $3, 1, '{}')`,
		channelID,
		accountID,
		personaID,
	); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	if _, err := db.Exec(ctx,
		`INSERT INTO channel_identities (id, channel_type, platform_subject_id, metadata)
		 VALUES ($1, 'telegram', 'chat-1001', '{}')`,
		identityID,
	); err != nil {
		t.Fatalf("insert channel identity: %v", err)
	}

	if _, err := db.Exec(ctx,
		`INSERT INTO channel_group_threads (channel_id, platform_chat_id, persona_id, thread_id)
		 VALUES ($1, 'chat-1001', $2, $3)`,
		channelID,
		personaID,
		threadID,
	); err != nil {
		t.Fatalf("insert channel group thread: %v", err)
	}

	row := ScheduledTriggerRow{
		ID:                uuid.New(),
		ChannelID:         channelID,
		ChannelIdentityID: identityID,
		PersonaKey:        personaKey,
		AccountID:         accountID,
	}

	got, err := (ScheduledTriggersRepository{}).ResolveHeartbeatThread(ctx, db, row)
	if err != nil {
		t.Fatalf("resolve heartbeat thread: %v", err)
	}
	if got == nil {
		t.Fatal("expected heartbeat context")
	}
	if got.ThreadID != threadID {
		t.Fatalf("unexpected thread id: got %s want %s", got.ThreadID, threadID)
	}
	if got.ChannelID != channelID.String() {
		t.Fatalf("unexpected channel id: got %s want %s", got.ChannelID, channelID)
	}
	if got.PlatformChatID != "chat-1001" {
		t.Fatalf("unexpected platform chat id: %q", got.PlatformChatID)
	}
}

func TestScheduledTriggersRepositoryResolveHeartbeatThreadUsesGroupThreadWithoutPersonaKey(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	personaID := uuid.New()

	seedDesktopAccount(t, db, accountID)
	seedDesktopProject(t, db, accountID, projectID)
	seedDesktopThread(t, db, accountID, projectID, threadID)

	if _, err := db.Exec(ctx,
		`INSERT INTO personas (id, account_id, persona_key, version, display_name, prompt_md, tool_allowlist, tool_denylist, budgets_json, is_active)
		 VALUES ($1, $2, 'persona-a', '1', 'Heartbeat Agent', 'prompt', '[]', '[]', '{}', 1)`,
		personaID,
		accountID,
	); err != nil {
		t.Fatalf("insert persona: %v", err)
	}

	if _, err := db.Exec(ctx,
		`INSERT INTO channels (id, account_id, channel_type, persona_id, is_active, config_json)
		 VALUES ($1, $2, 'telegram', $3, 1, '{}')`,
		channelID,
		accountID,
		personaID,
	); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	if _, err := db.Exec(ctx,
		`INSERT INTO channel_identities (id, channel_type, platform_subject_id, metadata)
		 VALUES ($1, 'telegram', 'chat-2002', '{}')`,
		identityID,
	); err != nil {
		t.Fatalf("insert channel identity: %v", err)
	}

	if _, err := db.Exec(ctx,
		`INSERT INTO channel_group_threads (channel_id, platform_chat_id, persona_id, thread_id)
		 VALUES ($1, 'chat-2002', $2, $3)`,
		channelID,
		personaID,
		threadID,
	); err != nil {
		t.Fatalf("insert channel group thread: %v", err)
	}

	row := ScheduledTriggerRow{
		ID:                uuid.New(),
		ChannelID:         channelID,
		ChannelIdentityID: identityID,
		AccountID:         accountID,
	}

	got, err := (ScheduledTriggersRepository{}).ResolveHeartbeatThread(ctx, db, row)
	if err != nil {
		t.Fatalf("resolve heartbeat thread: %v", err)
	}
	if got == nil {
		t.Fatal("expected heartbeat context")
	}
	if got.ThreadID != threadID {
		t.Fatalf("unexpected thread id: got %s want %s", got.ThreadID, threadID)
	}
	if got.ChannelID != channelID.String() {
		t.Fatalf("unexpected channel id: got %s want %s", got.ChannelID, channelID)
	}
	if got.ConversationType != "supergroup" {
		t.Fatalf("unexpected conversation type: %q", got.ConversationType)
	}
}

func TestScheduledTriggersRepositoryResolveHeartbeatThreadUsesDMBindingForDiscordIdentity(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	personaID := uuid.New()
	personaKey := "discord-heartbeat-agent"

	seedDesktopAccount(t, db, accountID)
	seedDesktopProject(t, db, accountID, projectID)
	seedDesktopThread(t, db, accountID, projectID, threadID)

	if _, err := db.Exec(ctx,
		`INSERT INTO personas (id, account_id, persona_key, version, display_name, prompt_md, tool_allowlist, tool_denylist, budgets_json, is_active)
		 VALUES ($1, $2, $3, '1', 'Discord Heartbeat Agent', 'prompt', '[]', '[]', '{}', 1)`,
		personaID,
		accountID,
		personaKey,
	); err != nil {
		t.Fatalf("insert persona: %v", err)
	}

	if _, err := db.Exec(ctx,
		`INSERT INTO channels (id, account_id, channel_type, persona_id, is_active, config_json)
		 VALUES ($1, $2, 'discord', $3, 1, '{}')`,
		channelID,
		accountID,
		personaID,
	); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	if _, err := db.Exec(ctx,
		`INSERT INTO channel_identities (id, channel_type, platform_subject_id, metadata)
		 VALUES ($1, 'discord', 'discord-user-1001', '{}')`,
		identityID,
	); err != nil {
		t.Fatalf("insert channel identity: %v", err)
	}

	if _, err := db.Exec(ctx,
		`INSERT INTO channel_dm_threads (channel_id, channel_identity_id, persona_id, thread_id)
		 VALUES ($1, $2, $3, $4)`,
		channelID,
		identityID,
		personaID,
		threadID,
	); err != nil {
		t.Fatalf("insert channel dm thread: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO channel_message_ledger (
			channel_id, channel_type, direction, thread_id, run_id,
			platform_conversation_id, platform_message_id, platform_parent_message_id, platform_thread_id,
			sender_channel_identity_id, metadata_json
		) VALUES ($1, 'discord', 'inbound', $2, NULL, 'dm-channel-1001', 'msg-1001', NULL, NULL, $3, '{}')`,
		channelID.String(),
		threadID.String(),
		identityID.String(),
	); err != nil {
		t.Fatalf("insert channel message ledger: %v", err)
	}

	row := ScheduledTriggerRow{
		ID:                uuid.New(),
		ChannelID:         channelID,
		ChannelIdentityID: identityID,
		PersonaKey:        personaKey,
		AccountID:         accountID,
	}

	got, err := (ScheduledTriggersRepository{}).ResolveHeartbeatThread(ctx, db, row)
	if err != nil {
		t.Fatalf("resolve heartbeat thread: %v", err)
	}
	if got == nil {
		t.Fatal("expected heartbeat context")
	}
	if got.ThreadID != threadID {
		t.Fatalf("unexpected thread id: got %s want %s", got.ThreadID, threadID)
	}
	if got.ChannelID != channelID.String() {
		t.Fatalf("unexpected channel id: got %s want %s", got.ChannelID, channelID)
	}
	if got.ChannelType != "discord" {
		t.Fatalf("unexpected channel type: %q", got.ChannelType)
	}
	if got.PlatformChatID != "dm-channel-1001" {
		t.Fatalf("unexpected platform chat id: %q", got.PlatformChatID)
	}
	if got.ConversationType != "private" {
		t.Fatalf("unexpected conversation type: %q", got.ConversationType)
	}
}

func TestDesktopCreateHeartbeatRunUsesDiscordDMThreadContext(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	personaID := uuid.New()
	personaKey := "discord-heartbeat-run"

	seedDesktopAccount(t, db, accountID)
	seedDesktopProject(t, db, accountID, projectID)
	seedDesktopThread(t, db, accountID, projectID, threadID)

	if _, err := db.Exec(ctx,
		`INSERT INTO personas (id, account_id, persona_key, version, display_name, prompt_md, tool_allowlist, tool_denylist, budgets_json, is_active)
		 VALUES ($1, $2, $3, '1', 'Discord Heartbeat Run', 'prompt', '[]', '[]', '{}', 1)`,
		personaID,
		accountID,
		personaKey,
	); err != nil {
		t.Fatalf("insert persona: %v", err)
	}

	if _, err := db.Exec(ctx,
		`INSERT INTO channels (id, account_id, channel_type, persona_id, is_active, config_json)
		 VALUES ($1, $2, 'discord', $3, 1, '{}')`,
		channelID,
		accountID,
		personaID,
	); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	if _, err := db.Exec(ctx,
		`INSERT INTO channel_identities (id, channel_type, platform_subject_id, metadata)
		 VALUES ($1, 'discord', 'discord-user-2001', '{}')`,
		identityID,
	); err != nil {
		t.Fatalf("insert channel identity: %v", err)
	}

	if _, err := db.Exec(ctx,
		`INSERT INTO channel_dm_threads (channel_id, channel_identity_id, persona_id, thread_id)
		 VALUES ($1, $2, $3, $4)`,
		channelID,
		identityID,
		personaID,
		threadID,
	); err != nil {
		t.Fatalf("insert channel dm thread: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO channel_message_ledger (
			channel_id, channel_type, direction, thread_id, run_id,
			platform_conversation_id, platform_message_id, platform_parent_message_id, platform_thread_id,
			sender_channel_identity_id, metadata_json
		) VALUES ($1, 'discord', 'inbound', $2, NULL, 'dm-channel-2001', 'msg-2001', NULL, NULL, $3, '{}')`,
		channelID.String(),
		threadID.String(),
		identityID.String(),
	); err != nil {
		t.Fatalf("insert channel message ledger: %v", err)
	}

	row := ScheduledTriggerRow{
		ID:                uuid.New(),
		ChannelID:         channelID,
		ChannelIdentityID: identityID,
		PersonaKey:        personaKey,
		AccountID:         accountID,
		Model:             "discord-model",
		IntervalMin:       15,
		NextFireAt:        time.Now().UTC(),
	}

	result, err := DesktopCreateHeartbeatRun(ctx, db, row, "discord-model")
	if err != nil {
		t.Fatalf("desktop create heartbeat run: %v", err)
	}
	if result.ChannelType != "discord" {
		t.Fatalf("unexpected channel type: %q", result.ChannelType)
	}
	if result.ChannelID != channelID.String() {
		t.Fatalf("unexpected channel id: %q", result.ChannelID)
	}
	if result.PlatformChatID != "dm-channel-2001" {
		t.Fatalf("unexpected platform chat id: %q", result.PlatformChatID)
	}
	if result.IdentityID != identityID.String() {
		t.Fatalf("unexpected identity id: %q", result.IdentityID)
	}
	if result.ConversationType != "private" {
		t.Fatalf("unexpected conversation type: %q", result.ConversationType)
	}

	var runThreadID string
	if err := db.QueryRow(ctx, `SELECT thread_id FROM runs WHERE id = $1`, result.RunID.String()).Scan(&runThreadID); err != nil {
		t.Fatalf("load created run: %v", err)
	}
	if runThreadID != threadID.String() {
		t.Fatalf("unexpected run thread id: %q", runThreadID)
	}
}

func TestDesktopCreateHeartbeatRunUsesChannelOwnerWhenThreadOwnerMissing(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	personaID := uuid.New()
	ownerUserID := uuid.New()
	personaKey := "telegram-heartbeat-owner"

	seedDesktopAccount(t, db, accountID)
	seedDesktopProject(t, db, accountID, projectID)
	seedDesktopThread(t, db, accountID, projectID, threadID)

	if _, err := db.Exec(ctx,
		`INSERT INTO users (id, username, email, status) VALUES ($1, $2, $3, 'active')`,
		ownerUserID,
		"heartbeat-owner-"+ownerUserID.String(),
		"heartbeat-owner-"+ownerUserID.String()+"@test.local",
	); err != nil {
		t.Fatalf("insert owner user: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO personas (id, account_id, persona_key, version, display_name, prompt_md, tool_allowlist, tool_denylist, budgets_json, is_active)
		 VALUES ($1, $2, $3, '1', 'Telegram Heartbeat Owner', 'prompt', '[]', '[]', '{}', 1)`,
		personaID,
		accountID,
		personaKey,
	); err != nil {
		t.Fatalf("insert persona: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO channels (id, account_id, channel_type, persona_id, owner_user_id, is_active, config_json)
		 VALUES ($1, $2, 'telegram', $3, $4, 1, '{}')`,
		channelID,
		accountID,
		personaID,
		ownerUserID,
	); err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO channel_identities (id, channel_type, platform_subject_id, metadata)
		 VALUES ($1, 'telegram', 'chat-owner', '{}')`,
		identityID,
	); err != nil {
		t.Fatalf("insert channel identity: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO channel_group_threads (channel_id, platform_chat_id, persona_id, thread_id)
		 VALUES ($1, 'chat-owner', $2, $3)`,
		channelID,
		personaID,
		threadID,
	); err != nil {
		t.Fatalf("insert channel group thread: %v", err)
	}

	row := ScheduledTriggerRow{
		ID:                uuid.New(),
		ChannelID:         channelID,
		ChannelIdentityID: identityID,
		PersonaKey:        personaKey,
		AccountID:         accountID,
		Model:             "owner-model",
		IntervalMin:       15,
		NextFireAt:        time.Now().UTC(),
	}

	resolved, err := (ScheduledTriggersRepository{}).ResolveHeartbeatThread(ctx, db, row)
	if err != nil {
		t.Fatalf("resolve heartbeat thread: %v", err)
	}
	if resolved == nil || resolved.CreatedByUserID == nil || *resolved.CreatedByUserID != ownerUserID {
		t.Fatalf("expected heartbeat context owner %s, got %#v", ownerUserID, resolved)
	}

	result, err := DesktopCreateHeartbeatRun(ctx, db, row, "owner-model")
	if err != nil {
		t.Fatalf("desktop create heartbeat run: %v", err)
	}

	var runOwner string
	if err := db.QueryRow(ctx, `SELECT created_by_user_id FROM runs WHERE id = $1`, result.RunID.String()).Scan(&runOwner); err != nil {
		t.Fatalf("load created run owner: %v", err)
	}
	if runOwner != ownerUserID.String() {
		t.Fatalf("unexpected run owner: got %s want %s", runOwner, ownerUserID)
	}
}

func TestScheduledTriggersRepositoryUpsertHeartbeatPreservesNextFireAtOnConflict(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	repo := ScheduledTriggersRepository{}

	accountID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()

	if err := repo.UpsertHeartbeat(ctx, db, accountID, channelID, identityID, "persona-a", "model-a", 1); err != nil {
		t.Fatalf("first upsert heartbeat: %v", err)
	}

	firstNextFire := mustReadDesktopNextFireAt(t, ctx, db, channelID, identityID)

	time.Sleep(1200 * time.Millisecond)

	if err := repo.UpsertHeartbeat(ctx, db, accountID, channelID, identityID, "persona-a", "model-a", 1); err != nil {
		t.Fatalf("second upsert heartbeat: %v", err)
	}

	secondNextFire := mustReadDesktopNextFireAt(t, ctx, db, channelID, identityID)
	if !secondNextFire.Equal(firstNextFire) {
		t.Fatalf("expected second next_fire_at to stay unchanged, first=%s second=%s", firstNextFire, secondNextFire)
	}
}

func TestScheduledTriggersRepositoryClaimDueTriggersAdvancesFromOriginalSchedule(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())

	triggerID := uuid.New()
	accountID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	now := time.Now().UTC()
	originalNextFire := now.Add(-20 * time.Second)

	if _, err := db.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)`,
		triggerID.String(),
		channelID.String(),
		identityID.String(),
		"persona-a",
		accountID.String(),
		"model-a",
		1,
		originalNextFire.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert scheduled trigger: %v", err)
	}

	rows, err := (ScheduledTriggersRepository{}).ClaimDueTriggers(ctx, db, 8)
	if err != nil {
		t.Fatalf("claim due triggers: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one claimed trigger, got %d", len(rows))
	}

	updatedNextFire := mustReadDesktopNextFireAt(t, ctx, db, channelID, identityID)
	expected := originalNextFire.Add(time.Minute)
	if !updatedNextFire.Equal(expected) {
		t.Fatalf("expected next_fire_at to advance from original schedule, got=%s want=%s", updatedNextFire, expected)
	}
}

func TestScheduledTriggersRepositoryClaimDueTriggersKeepsHeartbeatFollowupAtOneMinute(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())

	triggerID := uuid.New()
	accountID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	now := time.Now().UTC()
	originalNextFire := now.Add(-20 * time.Second)

	if _, err := db.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, cooldown_level, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9, $9)`,
		triggerID.String(),
		channelID.String(),
		identityID.String(),
		"persona-a",
		accountID.String(),
		"model-a",
		1,
		originalNextFire.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert scheduled trigger: %v", err)
	}

	rows, err := (ScheduledTriggersRepository{}).ClaimDueTriggers(ctx, db, 8)
	if err != nil {
		t.Fatalf("claim due triggers: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one claimed trigger, got %d", len(rows))
	}

	updatedNextFire := mustReadDesktopNextFireAt(t, ctx, db, channelID, identityID)
	expected := originalNextFire.Add(time.Minute)
	if !updatedNextFire.Equal(expected) {
		t.Fatalf("expected level 1 heartbeat to advance by one minute, got=%s want=%s", updatedNextFire, expected)
	}
}

func TestScheduledTriggersRepositoryUpdateCooldownAfterHeartbeatCanSuspendHeartbeat(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	repo := ScheduledTriggersRepository{}
	accountID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	now := time.Now().UTC()
	originalNextFire := now.Add(-20 * time.Second)

	if _, err := db.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, cooldown_level, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9, $9)`,
		uuid.New().String(),
		channelID.String(),
		identityID.String(),
		"persona-a",
		accountID.String(),
		"model-a",
		1,
		originalNextFire.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert scheduled trigger: %v", err)
	}

	suspendedUntil := now.AddDate(1, 0, 0)
	if err := repo.UpdateCooldownAfterHeartbeat(ctx, db, channelID, identityID, 2, suspendedUntil, nil); err != nil {
		t.Fatalf("suspend heartbeat: %v", err)
	}

	rows, err := repo.ClaimDueTriggers(ctx, db, 8)
	if err != nil {
		t.Fatalf("claim due triggers: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected suspended heartbeat not to be claimed, got %d", len(rows))
	}
}

func TestScheduledTriggersRepositoryResetHeartbeatNextFire(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	repo := ScheduledTriggersRepository{}

	accountID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	if err := repo.UpsertHeartbeat(ctx, db, accountID, channelID, identityID, "persona-a", "model-a", 5); err != nil {
		t.Fatalf("upsert heartbeat: %v", err)
	}

	firstNextFire := mustReadDesktopNextFireAt(t, ctx, db, channelID, identityID)
	time.Sleep(1200 * time.Millisecond)

	resetNextFire, err := repo.ResetHeartbeatNextFire(ctx, db, channelID, identityID, 1)
	if err != nil {
		t.Fatalf("reset heartbeat next fire: %v", err)
	}

	secondNextFire := mustReadDesktopNextFireAt(t, ctx, db, channelID, identityID)
	if !secondNextFire.Equal(resetNextFire) {
		t.Fatalf("expected stored next_fire_at to match reset result, got=%s want=%s", secondNextFire, resetNextFire)
	}
	if !secondNextFire.Before(firstNextFire) {
		t.Fatalf("expected reset with shorter interval to bring next_fire_at earlier, first=%s second=%s", firstNextFire, secondNextFire)
	}
}

func TestScheduledTriggersRepositoryRescheduleHeartbeatNextFireAt(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	repo := ScheduledTriggersRepository{}

	accountID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	if err := repo.UpsertHeartbeat(ctx, db, accountID, channelID, identityID, "persona-a", "model-a", 2); err != nil {
		t.Fatalf("upsert heartbeat: %v", err)
	}

	row, err := repo.GetHeartbeat(ctx, db, channelID, identityID)
	if err != nil {
		t.Fatalf("get heartbeat: %v", err)
	}
	if row == nil {
		t.Fatal("expected heartbeat row")
	}

	target := time.Now().UTC().Add(5 * time.Minute)
	if err := repo.RescheduleHeartbeatNextFireAt(ctx, db, row.ID, target); err != nil {
		t.Fatalf("reschedule heartbeat next fire: %v", err)
	}

	got := mustReadDesktopNextFireAt(t, ctx, db, channelID, identityID)
	if d := got.Sub(target); d < -time.Millisecond || d > time.Millisecond {
		t.Fatalf("unexpected next_fire_at after reschedule, got=%s want=%s", got, target)
	}
}

func TestScheduledTriggersRepositoryGetEarliestDue(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	now := time.Now().UTC()
	earliest := now.Add(30 * time.Second)
	later := now.Add(2 * time.Minute)

	if _, err := db.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9),
		       ($10, $11, $12, $4, $5, $6, $7, $13, $9, $9)`,
		uuid.NewString(), uuid.NewString(), uuid.NewString(), "persona-a", uuid.NewString(), "model-a", 1,
		earliest.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		uuid.NewString(), uuid.NewString(), uuid.NewString(), later.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert triggers: %v", err)
	}

	got, err := (ScheduledTriggersRepository{}).GetEarliestDue(ctx, db)
	if err != nil {
		t.Fatalf("get earliest due: %v", err)
	}
	if got == nil {
		t.Fatal("expected earliest next_fire_at")
	}
	if !got.Equal(earliest) {
		t.Fatalf("unexpected earliest next_fire_at: got=%s want=%s", *got, earliest)
	}
}

func TestScheduledTriggersRepositoryReadsSQLiteDriverTimeString(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	repo := ScheduledTriggersRepository{}

	triggerID := uuid.New()
	accountID := uuid.New()
	channelID := uuid.New()
	identityID := uuid.New()
	nextFireRaw := "2026-03-30 05:01:39.662272 +0000 UTC"
	nextFireAt, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", nextFireRaw)
	if err != nil {
		t.Fatalf("parse test time: %v", err)
	}
	now := nextFireAt.Add(2 * time.Minute).Format(time.RFC3339Nano)

	if _, err := db.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)`,
		triggerID.String(),
		channelID.String(),
		identityID.String(),
		"persona-a",
		accountID.String(),
		"model-a",
		1,
		nextFireRaw,
		now,
	); err != nil {
		t.Fatalf("insert trigger: %v", err)
	}

	row, err := repo.GetHeartbeat(ctx, db, channelID, identityID)
	if err != nil {
		t.Fatalf("get heartbeat: %v", err)
	}
	if row == nil {
		t.Fatal("expected heartbeat row")
	}
	if !row.NextFireAt.Equal(nextFireAt) {
		t.Fatalf("unexpected heartbeat next_fire_at: got=%s want=%s", row.NextFireAt, nextFireAt)
	}

	earliest, err := repo.GetEarliestDue(ctx, db)
	if err != nil {
		t.Fatalf("get earliest due: %v", err)
	}
	if earliest == nil {
		t.Fatal("expected earliest due")
	}
	if !earliest.Equal(nextFireAt) {
		t.Fatalf("unexpected earliest next_fire_at: got=%s want=%s", *earliest, nextFireAt)
	}

	rows, err := repo.ClaimDueTriggers(ctx, db, 8)
	if err != nil {
		t.Fatalf("claim due triggers: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one due trigger, got %d", len(rows))
	}
	if rows[0].ID != triggerID {
		t.Fatalf("unexpected claimed trigger id: got %s want %s", rows[0].ID, triggerID)
	}
}

func TestScheduledTriggersRepositoryInsertHeartbeatRunInTxWritesRecoveryMetadata(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	tailMessageID := uuid.New()
	triggerID := uuid.New()
	identityID := uuid.New()

	seedDesktopAccount(t, db, accountID)
	seedDesktopProject(t, db, accountID, projectID)
	seedDesktopThread(t, db, accountID, projectID, threadID)

	if _, err := db.Exec(ctx,
		`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, hidden, deleted_at, created_at)
		 VALUES ($1, $2, $3, 1, 'user', $4, FALSE, NULL, datetime('now'))`,
		tailMessageID,
		accountID,
		threadID,
		"heartbeat tail",
	); err != nil {
		t.Fatalf("insert message: %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE threads SET next_message_seq = 2 WHERE id = $1`, threadID); err != nil {
		t.Fatalf("bump next_message_seq: %v", err)
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	result, err := (ScheduledTriggersRepository{}).InsertHeartbeatRunInTx(ctx, tx, ScheduledTriggerRow{
		ID:                triggerID,
		ChannelIdentityID: identityID,
		PersonaKey:        "persona-heartbeat",
		AccountID:         accountID,
		Model:             "model-heartbeat",
		IntervalMin:       5,
	}, &HeartbeatThreadContext{ThreadID: threadID}, "model-heartbeat")
	if err != nil {
		t.Fatalf("insert heartbeat run in tx: %v", err)
	}

	var rawJSON string
	if err := tx.QueryRow(ctx,
		`SELECT data_json FROM run_events WHERE run_id = $1 AND type = 'run.started' LIMIT 1`,
		result.RunID,
	).Scan(&rawJSON); err != nil {
		t.Fatalf("query run.started: %v", err)
	}

	var started map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &started); err != nil {
		t.Fatalf("decode run.started: %v", err)
	}
	if got, _ := started["thread_tail_message_id"].(string); got != tailMessageID.String() {
		t.Fatalf("unexpected thread_tail_message_id: got %q want %q", got, tailMessageID)
	}
	if got, _ := started["continuation_source"].(string); got != "none" {
		t.Fatalf("unexpected continuation_source: %q", got)
	}
	if got, _ := started["continuation_loop"].(bool); got {
		t.Fatalf("unexpected continuation_loop: %v", got)
	}
}

func mustReadDesktopNextFireAt(t *testing.T, ctx context.Context, db *sqlitepgx.Pool, channelID uuid.UUID, identityID uuid.UUID) time.Time {
	t.Helper()

	var ts time.Time
	if err := db.QueryRow(ctx,
		`SELECT next_fire_at FROM scheduled_triggers WHERE channel_id = $1 AND channel_identity_id = $2`,
		channelID.String(),
		identityID.String(),
	).Scan(&ts); err != nil {
		t.Fatalf("query next_fire_at: %v", err)
	}
	return ts
}
