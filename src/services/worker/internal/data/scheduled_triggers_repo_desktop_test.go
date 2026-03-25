//go:build desktop

package data

import (
	"context"
	"path/filepath"
	"testing"

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
