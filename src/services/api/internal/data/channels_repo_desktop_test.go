//go:build desktop

package data_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
)

func TestChannelRepositoriesWorkInDesktopMode(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	channelsRepo, err := data.NewChannelsRepository(pool)
	if err != nil {
		t.Fatalf("new channels repo: %v", err)
	}
	identitiesRepo, err := data.NewChannelIdentitiesRepository(pool)
	if err != nil {
		t.Fatalf("new channel identities repo: %v", err)
	}

	channels, err := channelsRepo.ListByAccount(ctx, auth.DesktopAccountID)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(channels) != 0 {
		t.Fatalf("expected empty channels list, got %d", len(channels))
	}

	identities, err := identitiesRepo.ListByUserID(ctx, auth.DesktopUserID)
	if err != nil {
		t.Fatalf("list identities: %v", err)
	}
	if len(identities) != 0 {
		t.Fatalf("expected empty identities list, got %d", len(identities))
	}

	channel, err := channelsRepo.Create(
		ctx,
		uuid.New(),
		auth.DesktopAccountID,
		"telegram",
		nil,
		nil,
		&auth.DesktopUserID,
		"secret",
		"http://127.0.0.1/webhook",
		json.RawMessage(`{"allowed_user_ids":["12345"]}`),
	)
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if channel.ChannelType != "telegram" {
		t.Fatalf("unexpected channel type: %q", channel.ChannelType)
	}

	channels, err = channelsRepo.ListByAccount(ctx, auth.DesktopAccountID)
	if err != nil {
		t.Fatalf("list channels after create: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}

	identity, err := identitiesRepo.Upsert(
		ctx,
		"telegram",
		"12345",
		ptr("Telegram User"),
		nil,
		json.RawMessage(`{"username":"tester"}`),
	)
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	if err := identitiesRepo.UpdateUserID(ctx, identity.ID, &auth.DesktopUserID); err != nil {
		t.Fatalf("attach identity user: %v", err)
	}

	identities, err = identitiesRepo.ListByUserID(ctx, auth.DesktopUserID)
	if err != nil {
		t.Fatalf("list identities after upsert: %v", err)
	}
	if len(identities) != 1 {
		t.Fatalf("expected 1 identity, got %d", len(identities))
	}
	if identities[0].PlatformSubjectID != "12345" {
		t.Fatalf("unexpected identity subject: %q", identities[0].PlatformSubjectID)
	}
}

func ptr[T any](value T) *T {
	return &value
}
