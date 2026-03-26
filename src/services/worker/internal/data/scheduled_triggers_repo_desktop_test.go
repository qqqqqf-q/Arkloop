//go:build desktop

package data

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
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

	row := ScheduledTriggerRow{
		ID:                uuid.New(),
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
	if got.PlatformChatID != "discord-user-1001" {
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

	row := ScheduledTriggerRow{
		ID:                uuid.New(),
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
	if result.PlatformChatID != "discord-user-2001" {
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
	identityID := uuid.New()

	if err := repo.UpsertHeartbeat(ctx, db, accountID, identityID, "persona-a", "model-a", 1); err != nil {
		t.Fatalf("first upsert heartbeat: %v", err)
	}

	firstNextFire := mustReadDesktopNextFireAt(t, ctx, db, identityID)

	time.Sleep(1200 * time.Millisecond)

	if err := repo.UpsertHeartbeat(ctx, db, accountID, identityID, "persona-a", "model-a", 1); err != nil {
		t.Fatalf("second upsert heartbeat: %v", err)
	}

	secondNextFire := mustReadDesktopNextFireAt(t, ctx, db, identityID)
	if !secondNextFire.Equal(firstNextFire) {
		t.Fatalf("expected second next_fire_at to stay unchanged, first=%s second=%s", firstNextFire, secondNextFire)
	}
}

func TestScheduledTriggersRepositoryClaimDueHeartbeatsAdvancesFromOriginalSchedule(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())

	triggerID := uuid.New()
	accountID := uuid.New()
	identityID := uuid.New()
	now := time.Now().UTC()
	originalNextFire := now.Add(-20 * time.Second)

	if _, err := db.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`,
		triggerID.String(),
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

	rows, err := (ScheduledTriggersRepository{}).ClaimDueHeartbeats(ctx, db, 8)
	if err != nil {
		t.Fatalf("claim due heartbeats: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one claimed heartbeat, got %d", len(rows))
	}

	updatedNextFire := mustReadDesktopNextFireAt(t, ctx, db, identityID)
	expected := originalNextFire.Add(time.Minute)
	if !updatedNextFire.Equal(expected) {
		t.Fatalf("expected next_fire_at to advance from original schedule, got=%s want=%s", updatedNextFire, expected)
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
	identityID := uuid.New()
	if err := repo.UpsertHeartbeat(ctx, db, accountID, identityID, "persona-a", "model-a", 5); err != nil {
		t.Fatalf("upsert heartbeat: %v", err)
	}

	firstNextFire := mustReadDesktopNextFireAt(t, ctx, db, identityID)
	time.Sleep(1200 * time.Millisecond)

	resetNextFire, err := repo.ResetHeartbeatNextFire(ctx, db, identityID, 1)
	if err != nil {
		t.Fatalf("reset heartbeat next fire: %v", err)
	}

	secondNextFire := mustReadDesktopNextFireAt(t, ctx, db, identityID)
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
	identityID := uuid.New()
	if err := repo.UpsertHeartbeat(ctx, db, accountID, identityID, "persona-a", "model-a", 2); err != nil {
		t.Fatalf("upsert heartbeat: %v", err)
	}

	row, err := repo.GetHeartbeat(ctx, db, identityID)
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

	got := mustReadDesktopNextFireAt(t, ctx, db, identityID)
	if d := got.Sub(target); d < -time.Millisecond || d > time.Millisecond {
		t.Fatalf("unexpected next_fire_at after reschedule, got=%s want=%s", got, target)
	}
}

func TestScheduledTriggersRepositoryGetEarliestHeartbeatDue(t *testing.T) {
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
		    (id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8),
		       ($9, $10, $3, $4, $5, $6, $11, $8, $8)`,
		uuid.NewString(), uuid.NewString(), "persona-a", uuid.NewString(), "model-a", 1,
		earliest.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		uuid.NewString(), uuid.NewString(), later.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert triggers: %v", err)
	}

	got, err := (ScheduledTriggersRepository{}).GetEarliestHeartbeatDue(ctx, db)
	if err != nil {
		t.Fatalf("get earliest heartbeat due: %v", err)
	}
	if got == nil {
		t.Fatal("expected earliest next_fire_at")
	}
	if !got.Equal(earliest) {
		t.Fatalf("unexpected earliest next_fire_at: got=%s want=%s", *got, earliest)
	}
}

func mustReadDesktopNextFireAt(t *testing.T, ctx context.Context, db *sqlitepgx.Pool, identityID uuid.UUID) time.Time {
	t.Helper()

	var raw string
	if err := db.QueryRow(ctx,
		`SELECT next_fire_at FROM scheduled_triggers WHERE channel_identity_id = $1`,
		identityID.String(),
	).Scan(&raw); err != nil {
		t.Fatalf("query next_fire_at: %v", err)
	}

	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatalf("parse next_fire_at %q: %v", raw, err)
	}
	return ts
}
