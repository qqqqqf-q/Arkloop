package accountapi

import (
	"context"
	"path/filepath"
	"testing"

	"arkloop/services/api/internal/auth"
	internalcrypto "arkloop/services/api/internal/crypto"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
)

func TestPollTelegramDesktopOnceNoActiveChannels(t *testing.T) {
	ctx := context.Background()
	channelsRepo, _ := openTelegramDesktopPollTestRepos(t, ctx)

	didPoll, err := pollTelegramDesktopOnce(ctx, nil, telegramConnector{}, channelsRepo, nil, map[uuid.UUID]int64{}, 20, telegramLongPollSeconds)
	if err != nil {
		t.Fatalf("poll once: %v", err)
	}
	if didPoll {
		t.Fatal("expected no telegram poll")
	}
}

func TestPollTelegramDesktopOnceSkipsActiveChannelWithBlankToken(t *testing.T) {
	ctx := context.Background()
	channelsRepo, secretsRepo := openTelegramDesktopPollTestRepos(t, ctx)

	secret, err := secretsRepo.Create(ctx, auth.DesktopUserID, "telegram-blank", "   ")
	if err != nil {
		t.Fatalf("create secret: %v", err)
	}
	channel, err := channelsRepo.Create(ctx, uuid.Nil, auth.DesktopAccountID, "telegram", nil, &secret.ID, &auth.DesktopUserID, "", "", nil)
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	active := true
	if _, err := channelsRepo.Update(ctx, channel.ID, auth.DesktopAccountID, data.ChannelUpdate{IsActive: &active}); err != nil {
		t.Fatalf("activate channel: %v", err)
	}

	didPoll, err := pollTelegramDesktopOnce(ctx, nil, telegramConnector{}, channelsRepo, secretsRepo, map[uuid.UUID]int64{}, 20, telegramLongPollSeconds)
	if err != nil {
		t.Fatalf("poll once: %v", err)
	}
	if didPoll {
		t.Fatal("expected no telegram poll")
	}
}

func openTelegramDesktopPollTestRepos(t *testing.T, ctx context.Context) (*data.ChannelsRepository, *data.SecretsRepository) {
	t.Helper()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	t.Cleanup(func() { sqlitePool.Close() })
	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	channelsRepo, err := data.NewChannelsRepository(pool)
	if err != nil {
		t.Fatalf("new channels repo: %v", err)
	}
	keyRing, err := internalcrypto.NewKeyRing(map[int][]byte{1: make([]byte, 32)})
	if err != nil {
		t.Fatalf("new key ring: %v", err)
	}
	secretsRepo, err := data.NewSecretsRepository(pool, keyRing)
	if err != nil {
		t.Fatalf("new secrets repo: %v", err)
	}
	return channelsRepo, secretsRepo
}
