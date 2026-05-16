//go:build !desktop

package data

import (
	"context"
	"encoding/json"
	"testing"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
)

type channelsRepoOwnerTestEnv struct {
	ctx        context.Context
	repo       *ChannelsRepository
	accountID  uuid.UUID
	ownerID    uuid.UUID
	nextID     uuid.UUID
	outsiderID uuid.UUID
}

func setupChannelsRepoOwnerTestEnv(t *testing.T) channelsRepoOwnerTestEnv {
	t.Helper()

	ctx := context.Background()
	db := testutil.SetupPostgresDatabase(t, "api_go_channels_repo_owner")
	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 8, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	repo, err := NewChannelsRepository(pool)
	if err != nil {
		t.Fatalf("new channels repo: %v", err)
	}
	accountRepo, err := NewAccountRepository(pool)
	if err != nil {
		t.Fatalf("new account repo: %v", err)
	}
	userRepo, err := NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	membershipRepo, err := NewAccountMembershipRepository(pool)
	if err != nil {
		t.Fatalf("new membership repo: %v", err)
	}
	account, err := accountRepo.Create(ctx, "channels-owner", "Channels Owner", "personal")
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	owner, err := userRepo.Create(ctx, "channels-owner-1", "channels-owner-1@test.com", "zh")
	if err != nil {
		t.Fatalf("create owner user: %v", err)
	}
	next, err := userRepo.Create(ctx, "channels-owner-2", "channels-owner-2@test.com", "zh")
	if err != nil {
		t.Fatalf("create next user: %v", err)
	}
	outsider, err := userRepo.Create(ctx, "channels-owner-outsider", "channels-owner-outsider@test.com", "zh")
	if err != nil {
		t.Fatalf("create outsider user: %v", err)
	}
	if _, err := membershipRepo.Create(ctx, account.ID, owner.ID, "account_admin"); err != nil {
		t.Fatalf("create owner membership: %v", err)
	}
	if _, err := membershipRepo.Create(ctx, account.ID, next.ID, "account_admin"); err != nil {
		t.Fatalf("create next membership: %v", err)
	}
	return channelsRepoOwnerTestEnv{
		ctx:        ctx,
		repo:       repo,
		accountID:  account.ID,
		ownerID:    owner.ID,
		nextID:     next.ID,
		outsiderID: outsider.ID,
	}
}

func (e channelsRepoOwnerTestEnv) createChannel(t *testing.T, ownerUserID *uuid.UUID) Channel {
	t.Helper()
	channel, err := e.repo.Create(e.ctx, uuid.New(), e.accountID, "discord", nil, nil, ownerUserID, "", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return channel
}

func TestChannelsRepositorySetOwnerIfMissingDoesNotOverwriteExistingOwner(t *testing.T) {
	env := setupChannelsRepoOwnerTestEnv(t)
	channel := env.createChannel(t, nil)

	updated, err := env.repo.SetOwnerIfMissing(env.ctx, channel.ID, env.accountID, env.ownerID)
	if err != nil {
		t.Fatalf("set missing owner: %v", err)
	}
	if updated == nil || updated.OwnerUserID == nil || *updated.OwnerUserID != env.ownerID {
		t.Fatalf("unexpected owner after first set: %#v", updated)
	}

	updated, err = env.repo.SetOwnerIfMissing(env.ctx, channel.ID, env.accountID, env.nextID)
	if err != nil {
		t.Fatalf("set existing owner: %v", err)
	}
	if updated != nil {
		t.Fatalf("expected existing owner update to no-op, got %#v", updated)
	}

	got, err := env.repo.GetByID(env.ctx, channel.ID)
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	if got == nil || got.OwnerUserID == nil || *got.OwnerUserID != env.ownerID {
		t.Fatalf("owner was overwritten: %#v", got)
	}
}

func TestChannelsRepositorySetOwnerIfMissingRejectsNonMember(t *testing.T) {
	env := setupChannelsRepoOwnerTestEnv(t)
	channel := env.createChannel(t, nil)

	updated, err := env.repo.SetOwnerIfMissing(env.ctx, channel.ID, env.accountID, env.outsiderID)
	if err != nil {
		t.Fatalf("set outsider owner: %v", err)
	}
	if updated != nil {
		t.Fatalf("expected outsider owner update to no-op, got %#v", updated)
	}

	got, err := env.repo.GetByID(env.ctx, channel.ID)
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	if got == nil || got.OwnerUserID != nil {
		t.Fatalf("outsider became owner: %#v", got)
	}
}

func TestChannelsRepositoryClearOwnerIfMatchesDoesNotClearTransferredOwner(t *testing.T) {
	env := setupChannelsRepoOwnerTestEnv(t)
	channel := env.createChannel(t, &env.ownerID)

	nextOwner := &env.nextID
	if _, err := env.repo.Update(env.ctx, channel.ID, env.accountID, ChannelUpdate{OwnerUserID: &nextOwner}); err != nil {
		t.Fatalf("transfer owner: %v", err)
	}

	updated, err := env.repo.ClearOwnerIfMatches(env.ctx, channel.ID, env.accountID, env.ownerID)
	if err != nil {
		t.Fatalf("clear stale owner: %v", err)
	}
	if updated != nil {
		t.Fatalf("expected stale owner clear to no-op, got %#v", updated)
	}

	got, err := env.repo.GetByID(env.ctx, channel.ID)
	if err != nil {
		t.Fatalf("get channel after stale clear: %v", err)
	}
	if got == nil || got.OwnerUserID == nil || *got.OwnerUserID != env.nextID {
		t.Fatalf("stale clear changed owner: %#v", got)
	}

	updated, err = env.repo.ClearOwnerIfMatches(env.ctx, channel.ID, env.accountID, env.nextID)
	if err != nil {
		t.Fatalf("clear current owner: %v", err)
	}
	if updated == nil || updated.OwnerUserID != nil {
		t.Fatalf("expected current owner to be cleared, got %#v", updated)
	}
}
