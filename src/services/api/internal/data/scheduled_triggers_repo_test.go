package data

import (
	"context"
	"path/filepath"
	"testing"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
)

func TestScheduledTriggersRepositoryUpsertHeartbeatWorksInDesktopSQLite(t *testing.T) {
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

	if err := repo.UpsertHeartbeat(ctx, db, accountID, channelID, identityID, "persona", "model", 5); err != nil {
		t.Fatalf("upsert heartbeat: %v", err)
	}

	row, err := repo.GetHeartbeat(ctx, db, channelID, identityID)
	if err != nil {
		t.Fatalf("get heartbeat: %v", err)
	}
	if row == nil {
		t.Fatal("expected heartbeat row")
	}
	if row.ID == uuid.Nil {
		t.Fatal("expected generated heartbeat id")
	}
	if row.ChannelID != channelID {
		t.Fatalf("unexpected channel id: got %s want %s", row.ChannelID, channelID)
	}
	if row.ChannelIdentityID != identityID {
		t.Fatalf("unexpected channel identity id: got %s want %s", row.ChannelIdentityID, identityID)
	}
}
