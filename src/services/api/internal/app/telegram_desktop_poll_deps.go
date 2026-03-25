//go:build desktop

package app

import (
	"context"
	"fmt"

	internalcrypto "arkloop/services/api/internal/crypto"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/http/accountapi"
	shareddesktop "arkloop/services/shared/desktop"
	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/objectstore"
)

// TelegramDesktopPollerDepsForPool 为共享 SQLite 构造 getUpdates 依赖（供 Worker 独占进程 fallback）。
func TelegramDesktopPollerDepsForPool(pgxPool data.DB, keyRing *internalcrypto.KeyRing) (accountapi.TelegramDesktopPollerDeps, error) {
	var zero accountapi.TelegramDesktopPollerDeps
	if pgxPool == nil {
		return zero, fmt.Errorf("pool must not be nil")
	}
	if keyRing == nil {
		return zero, fmt.Errorf("key ring must not be nil")
	}

	userRepo, err := data.NewUserRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init user repo: %w", err)
	}
	accountRepo, err := data.NewAccountRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init account repo: %w", err)
	}
	membershipRepo, err := data.NewAccountMembershipRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init membership repo: %w", err)
	}
	threadRepo, err := data.NewThreadRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init thread repo: %w", err)
	}
	messageRepo, err := data.NewMessageRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init message repo: %w", err)
	}
	runEventRepo, err := data.NewRunEventRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init run event repo: %w", err)
	}
	jobRepo, err := data.NewJobRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init job repo: %w", err)
	}
	secretsRepo, err := data.NewSecretsRepository(pgxPool, keyRing)
	if err != nil {
		return zero, fmt.Errorf("init secrets repo: %w", err)
	}
	personasRepo, err := data.NewPersonasRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init personas repo: %w", err)
	}
	projectRepo, err := data.NewProjectRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init project repo: %w", err)
	}
	channelsRepo, err := data.NewChannelsRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init channels repo: %w", err)
	}
	channelIdentitiesRepo, err := data.NewChannelIdentitiesRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init channel identities repo: %w", err)
	}
	channelBindCodesRepo, err := data.NewChannelBindCodesRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init channel bind codes repo: %w", err)
	}
	channelDMThreadsRepo, err := data.NewChannelDMThreadsRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init channel dm threads repo: %w", err)
	}
	channelGroupThreadsRepo, err := data.NewChannelGroupThreadsRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init channel group threads repo: %w", err)
	}
	channelReceiptsRepo, err := data.NewChannelMessageReceiptsRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init channel receipts repo: %w", err)
	}
	planRepo, err := data.NewPlanRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init plan repo: %w", err)
	}
	subscriptionRepo, err := data.NewSubscriptionRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init subscription repo: %w", err)
	}
	entitlementsRepo, err := data.NewEntitlementsRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init entitlements repo: %w", err)
	}
	creditsRepo, err := data.NewCreditsRepository(pgxPool)
	if err != nil {
		return zero, fmt.Errorf("init credits repo: %w", err)
	}

	registry := sharedconfig.DefaultRegistry()
	resolver, err := sharedconfig.NewResolver(registry, nil, nil, 0)
	if err != nil {
		return zero, fmt.Errorf("init config resolver: %w", err)
	}
	entitlementService, err := entitlement.NewService(
		entitlementsRepo, subscriptionRepo, planRepo,
		nil,
		resolver,
	)
	if err != nil {
		return zero, fmt.Errorf("init entitlement service: %w", err)
	}

	cfg, err := LoadDesktopConfig()
	if err != nil {
		return zero, fmt.Errorf("load desktop config: %w", err)
	}
	storageRoot := shareddesktop.StorageRoot(cfg.DataDir)
	opener := objectstore.NewFilesystemOpener(storageRoot)
	msgAttach, err := opener.Open(context.Background(), "message-attachments")
	if err != nil {
		return zero, fmt.Errorf("open message-attachments: %w", err)
	}

	var bus eventbus.EventBus
	if b, ok := shareddesktop.GetEventBus().(eventbus.EventBus); ok {
		bus = b
	}

	return accountapi.TelegramDesktopPollerDeps{
		ChannelsRepo:            channelsRepo,
		ChannelIdentitiesRepo:   channelIdentitiesRepo,
		ChannelBindCodesRepo:    channelBindCodesRepo,
		ChannelDMThreadsRepo:    channelDMThreadsRepo,
		ChannelGroupThreadsRepo: channelGroupThreadsRepo,
		ChannelReceiptsRepo:     channelReceiptsRepo,
		SecretsRepo:             secretsRepo,
		PersonasRepo:            personasRepo,
		UsersRepo:               userRepo,
		AccountRepo:             accountRepo,
		AccountMembershipRepo:   membershipRepo,
		ProjectRepo:             projectRepo,
		ThreadRepo:              threadRepo,
		MessageRepo:             messageRepo,
		RunEventRepo:            runEventRepo,
		JobRepo:                 jobRepo,
		CreditsRepo:             creditsRepo,
		Pool:                   pgxPool,
		EntitlementService:     entitlementService,
		MessageAttachmentStore: msgAttach,
		TelegramMode:            "polling",
		Bus:                     bus,
	}, nil
}

// StartTelegramDesktopPollWorker Worker 进程启动时尝试承担 Telegram getUpdates；pool 传 nil 时仅用共享池。
func StartTelegramDesktopPollWorker(ctx context.Context, pool any) error {
	var dbPool data.DB
	if pool != nil {
		if d, ok := pool.(data.DB); ok {
			dbPool = d
		}
	}
	if dbPool == nil {
		dbPool = shareddesktop.GetSharedSQLitePool()
	}
	if dbPool == nil {
		return nil
	}
	cfg, err := LoadDesktopConfig()
	if err != nil {
		return err
	}
	keyRing, err := desktopKeyRing(cfg.DataDir)
	if err != nil {
		return err
	}
	deps, err := TelegramDesktopPollerDepsForPool(dbPool, keyRing)
	if err != nil {
		return err
	}
	accountapi.StartTelegramDesktopPoller(ctx, deps)
	return nil
}
