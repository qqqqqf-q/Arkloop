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

func TestScheduledTriggersRepositoryUpsertHeartbeatResetsNextFireAtFromCurrentTime(t *testing.T) {
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
	if !secondNextFire.After(firstNextFire) {
		t.Fatalf("expected second next_fire_at to move forward, first=%s second=%s", firstNextFire, secondNextFire)
	}

	drift := secondNextFire.Sub(firstNextFire)
	if drift < time.Second {
		t.Fatalf("expected observable drift after re-upsert, got %s", drift)
	}
}

func TestScheduledTriggersRepositoryClaimDueHeartbeatsAdvancesFromClaimTime(t *testing.T) {
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
	expectedLowerBound := time.Now().UTC().Add(59 * time.Second)
	if updatedNextFire.Before(expectedLowerBound) {
		t.Fatalf("expected next_fire_at to be recalculated from claim time, got %s", updatedNextFire)
	}

	originalSchedule := originalNextFire.Add(time.Minute)
	if !updatedNextFire.After(originalSchedule.Add(15 * time.Second)) {
		t.Fatalf("expected claimed heartbeat to drift beyond original schedule, original=%s updated=%s", originalSchedule, updatedNextFire)
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
