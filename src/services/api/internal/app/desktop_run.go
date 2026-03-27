//go:build desktop

package app

import (
	"context"
	"fmt"
	"net"
	nethttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	internalcrypto "arkloop/services/api/internal/crypto"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/featureflag"
	apihttp "arkloop/services/api/internal/http"
	"arkloop/services/api/internal/http/accountapi"
	"arkloop/services/api/internal/mcpfilesync"
	repopersonas "arkloop/services/api/internal/personas"
	"arkloop/services/api/internal/personasync"
	"arkloop/services/api/internal/skillseed"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/shared/desktop"
	sharedenvironmentref "arkloop/services/shared/environmentref"
	"arkloop/services/shared/eventbus"
	sharedlog "arkloop/services/shared/log"
	"arkloop/services/shared/objectstore"
)

func desktopJWTSecretValue() string {
	if v := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_JWT_SECRET")); v != "" {
		return v
	}
	return "arkloop-desktop-mode-jwt-secret-not-validated"
}

// RunDesktop starts the desktop-mode API server, blocking until ctx is cancelled or an error occurs.
// The caller handles signals; cancelling ctx triggers graceful shutdown.
func RunDesktop(ctx context.Context) error {
	if _, err := LoadDotenvIfEnabled(false); err != nil {
		return err
	}

	cfg, err := LoadDesktopConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := sharedlog.New(sharedlog.Config{Component: "api"})

	// ---- data directory ----

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	// default workspace root for chat mode (subdirectories created on demand when no workspace exists)
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "workspaces"), 0o755); err != nil {
		return fmt.Errorf("create workspaces dir: %w", err)
	}

	// ---- SQLite ----

	sqlitePath := filepath.Join(cfg.DataDir, "data.db")
	dbPool, err := sqliteadapter.AutoMigrate(ctx, sqlitePath)
	if err != nil {
		return fmt.Errorf("sqlite migrate: %w", err)
	}
	desktop.RegisterSQLiteCloser(func() error {
		return dbPool.Close()
	})
	defer func() {
		desktop.ClearSharedSQLitePool()
		if !desktop.SidecarProcess() {
			if err := desktop.CloseRegisteredSQLite(); err != nil {
				logger.Warn("desktop_sqlite_close", "error", err.Error())
			}
		}
	}()

	sqlitepgx.ConfigureDesktopSQLPool(dbPool.Unwrap())
	pgxPool := sqlitepgx.New(dbPool.Unwrap())
	desktop.SetSharedSQLitePool(pgxPool)

	// ---- seed data ----

	if err := auth.SeedDesktopUser(ctx, pgxPool); err != nil {
		return fmt.Errorf("seed desktop user: %w", err)
	}

	personasRoot, err := repopersonas.BuiltinPersonasRoot()
	if err != nil {
		return fmt.Errorf("personas root: %w", err)
	}
	if err := personasync.SeedDesktopPersonas(ctx, dbPool, personasRoot); err != nil {
		return fmt.Errorf("seed personas: %w", err)
	}

	repoPersonas, err := repopersonas.LoadFromDir(personasRoot)
	if err != nil {
		return fmt.Errorf("load personas: %w", err)
	}

	// ---- encryption key ring ----

	keyRing, err := desktopKeyRing(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("encryption key: %w", err)
	}

	// ---- repositories ----

	userRepo, err := data.NewUserRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init user repo: %w", err)
	}
	accountRepo, err := data.NewAccountRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init account repo: %w", err)
	}
	membershipRepo, err := data.NewAccountMembershipRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init membership repo: %w", err)
	}
	credentialRepo, err := data.NewUserCredentialRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init credential repo: %w", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init refresh token repo: %w", err)
	}
	threadRepo, err := data.NewThreadRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init thread repo: %w", err)
	}
	threadStarRepo, err := data.NewThreadStarRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init thread star repo: %w", err)
	}
	threadShareRepo, err := data.NewThreadShareRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init thread share repo: %w", err)
	}
	threadReportRepo, err := data.NewThreadReportRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init thread report repo: %w", err)
	}
	messageRepo, err := data.NewMessageRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init message repo: %w", err)
	}
	runEventRepo, err := data.NewRunEventRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init run event repo: %w", err)
	}
	shellSessionRepo, err := data.NewShellSessionRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init shell session repo: %w", err)
	}
	jobRepo, err := data.NewJobRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init job repo: %w", err)
	}

	llmCredentialsRepo, err := data.NewLlmCredentialsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init llm credentials repo: %w", err)
	}
	llmRoutesRepo, err := data.NewLlmRoutesRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init llm routes repo: %w", err)
	}
	secretsRepo, err := data.NewSecretsRepository(pgxPool, keyRing)
	if err != nil {
		return fmt.Errorf("init secrets repo: %w", err)
	}
	asrCredentialsRepo, err := data.NewAsrCredentialsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init asr credentials repo: %w", err)
	}
	mcpConfigsRepo, err := data.NewMCPConfigsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init mcp configs repo: %w", err)
	}
	profileMCPInstallsRepo, err := data.NewProfileMCPInstallsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init profile mcp installs repo: %w", err)
	}
	workspaceMCPEnableRepo, err := data.NewWorkspaceMCPEnablementsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init workspace mcp enable repo: %w", err)
	}
	toolProviderConfigsRepo, err := data.NewToolProviderConfigsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init tool provider configs repo: %w", err)
	}
	toolDescOverridesRepo, err := data.NewToolDescriptionOverridesRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init tool desc overrides repo: %w", err)
	}
	personasRepo, err := data.NewPersonasRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init personas repo: %w", err)
	}
	skillPackagesRepo, err := data.NewSkillPackagesRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init skill packages repo: %w", err)
	}
	profileSkillInstallsRepo, err := data.NewProfileSkillInstallsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init profile skill installs repo: %w", err)
	}
	workspaceSkillEnableRepo, err := data.NewWorkspaceSkillEnablementsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init workspace skill enable repo: %w", err)
	}
	platformSkillOverridesRepo, err := data.NewPlatformSkillOverridesRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init platform skill overrides repo: %w", err)
	}
	profileRegistriesRepo, err := data.NewProfileRegistriesRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init profile registries repo: %w", err)
	}
	workspaceRegistriesRepo, err := data.NewWorkspaceRegistriesRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init workspace registries repo: %w", err)
	}
	ipRulesRepo, err := data.NewIPRulesRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init ip rules repo: %w", err)
	}
	apiKeysRepo, err := data.NewAPIKeysRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init api keys repo: %w", err)
	}
	teamRepo, err := data.NewTeamRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init team repo: %w", err)
	}
	projectRepo, err := data.NewProjectRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init project repo: %w", err)
	}
	webhookRepo, err := data.NewWebhookEndpointRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init webhook repo: %w", err)
	}
	channelsRepo, err := data.NewChannelsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init channels repo: %w", err)
	}
	channelIdentitiesRepo, err := data.NewChannelIdentitiesRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init channel identities repo: %w", err)
	}
	channelBindCodesRepo, err := data.NewChannelBindCodesRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init channel bind codes repo: %w", err)
	}
	channelDMThreadsRepo, err := data.NewChannelDMThreadsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init channel dm threads repo: %w", err)
	}
	channelGroupThreadsRepo, err := data.NewChannelGroupThreadsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init channel group threads repo: %w", err)
	}
	channelReceiptsRepo, err := data.NewChannelMessageReceiptsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init channel receipts repo: %w", err)
	}
	planRepo, err := data.NewPlanRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init plan repo: %w", err)
	}
	subscriptionRepo, err := data.NewSubscriptionRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init subscription repo: %w", err)
	}
	entitlementsRepo, err := data.NewEntitlementsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init entitlements repo: %w", err)
	}
	usageRepo, err := data.NewUsageRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init usage repo: %w", err)
	}
	featureFlagsRepo, err := data.NewFeatureFlagRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init feature flags repo: %w", err)
	}
	notificationsRepo, err := data.NewNotificationsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init notifications repo: %w", err)
	}
	auditLogRepo, err := data.NewAuditLogRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init audit log repo: %w", err)
	}
	inviteCodesRepo, err := data.NewInviteCodeRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init invite codes repo: %w", err)
	}
	referralsRepo, err := data.NewReferralRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init referrals repo: %w", err)
	}
	creditsRepo, err := data.NewCreditsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init credits repo: %w", err)
	}
	redemptionCodesRepo, err := data.NewRedemptionCodesRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init redemption codes repo: %w", err)
	}
	platformSettingsRepo, err := data.NewPlatformSettingsRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init platform settings repo: %w", err)
	}
	smtpProviderRepo, err := data.NewSmtpProviderRepository(pgxPool)
	if err != nil {
		return fmt.Errorf("init smtp provider repo: %w", err)
	}

	// ---- services ----

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		return fmt.Errorf("init password hasher: %w", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService(desktopJWTSecretValue(), 3600, 86400)
	if err != nil {
		return fmt.Errorf("init token service: %w", err)
	}
	authService, err := auth.NewService(
		userRepo, credentialRepo, membershipRepo,
		passwordHasher, tokenService, refreshTokenRepo,
		nil, // redis
		projectRepo,
	)
	if err != nil {
		return fmt.Errorf("init auth service: %w", err)
	}

	registry := sharedconfig.DefaultRegistry()
	resolver, err := sharedconfig.NewResolver(registry, nil, nil, 0)
	if err != nil {
		return fmt.Errorf("init config resolver: %w", err)
	}

	entitlementService, err := entitlement.NewService(
		entitlementsRepo, subscriptionRepo, planRepo,
		nil, // redis
		resolver,
	)
	if err != nil {
		return fmt.Errorf("init entitlement service: %w", err)
	}

	featureFlagService, err := featureflag.NewService(featureFlagsRepo, nil)
	if err != nil {
		return fmt.Errorf("init feature flag service: %w", err)
	}

	auditWriter := audit.NewWriter(auditLogRepo, membershipRepo, logger)

	// ---- object stores ----

	storageRoot := desktop.StorageRoot(cfg.DataDir)
	opener := objectstore.NewFilesystemOpener(storageRoot)

	artifactStore, err := opener.Open(ctx, objectstore.ArtifactBucket)
	if err != nil {
		return fmt.Errorf("open artifact store: %w", err)
	}
	messageAttachmentStore, err := opener.Open(ctx, "message-attachments")
	if err != nil {
		return fmt.Errorf("open message store: %w", err)
	}
	environmentStore, err := opener.Open(ctx, objectstore.EnvironmentStateBucket)
	if err != nil {
		return fmt.Errorf("open environment store: %w", err)
	}
	skillStore, err := opener.Open(ctx, objectstore.SkillStoreBucket)
	if err != nil {
		return fmt.Errorf("open skill store: %w", err)
	}

	// ---- platform skill seeder ----
	// Desktop runs as a single process — skip the PG advisory-lock election
	// used in the cloud seeder and sync built-in skills directly.

	skillsRoot, skillsRootErr := skillseed.BuiltinSkillsRoot()
	if skillsRootErr != nil {
		logger.Warn("platform_skills_root_not_found", "error", skillsRootErr.Error())
	} else {
		if seedErr := skillseed.SeedDesktopSkills(ctx, skillsRoot, skillPackagesRepo, skillStore, logger); seedErr != nil {
			logger.Warn("platform_skills_sync_failed", "error", seedErr.Error())
		}
	}

	// ---- HTTP handler ----

	profileRef := sharedenvironmentref.BuildProfileRef(auth.DesktopAccountID, &auth.DesktopUserID)
	mcpDiscoveryService, err := mcpfilesync.NewService(cfg.DataDir, profileMCPInstallsRepo, secretsRepo, pgxPool)
	if err != nil {
		return fmt.Errorf("init desktop mcp discovery service: %w", err)
	}
	if err := mcpDiscoveryService.SyncDesktopMirror(ctx, auth.DesktopAccountID, profileRef); err != nil {
		logger.Warn("desktop_mcp_sync_initial_failed", "error", err.Error())
	}
	if err := mcpDiscoveryService.SyncFromOfficialFile(ctx, auth.DesktopAccountID, profileRef); err != nil {
		logger.Warn("desktop_mcp_import_initial_failed", "error", err.Error())
	}
	mcpDiscoveryService.StartWatcher(ctx, auth.DesktopAccountID, profileRef, 3*time.Second)

	handler := apihttp.NewHandler(apihttp.HandlerConfig{
		Logger:               logger,
		SchemaRepository:     nil,
		TrustIncomingTraceID: false,
		TrustXForwardedFor:   false,
		MaxInFlight:          cfg.MaxInFlight,

		Pool: pgxPool,

		AuthService:           authService,
		RegistrationService:   nil,
		EmailVerifyService:    nil,
		EmailOTPLoginService:  nil,
		AccountService:        nil,
		AppBaseURL:            cfg.AppBaseURL,
		AccountMembershipRepo: membershipRepo,
		ThreadRepo:            threadRepo,
		ThreadStarRepo:        threadStarRepo,
		ThreadShareRepo:       threadShareRepo,
		ThreadReportRepo:      threadReportRepo,
		MessageRepo:           messageRepo,
		RunEventRepo:          runEventRepo,
		ShellSessionRepo:      shellSessionRepo,
		AuditWriter:           auditWriter,

		LlmCredentialsRepo:           llmCredentialsRepo,
		LlmRoutesRepo:                llmRoutesRepo,
		SecretsRepo:                  secretsRepo,
		AsrCredentialsRepo:           asrCredentialsRepo,
		MCPConfigsRepo:               mcpConfigsRepo,
		ProfileMCPInstallsRepo:       profileMCPInstallsRepo,
		WorkspaceMCPEnableRepo:       workspaceMCPEnableRepo,
		ToolProviderConfigsRepo:      toolProviderConfigsRepo,
		ToolDescriptionOverridesRepo: toolDescOverridesRepo,
		PersonasRepo:                 personasRepo,
		SkillPackagesRepo:            skillPackagesRepo,
		ProfileSkillInstallsRepo:     profileSkillInstallsRepo,
		WorkspaceSkillEnableRepo:     workspaceSkillEnableRepo,
		PlatformSkillOverridesRepo:   platformSkillOverridesRepo,
		ProfileRegistriesRepo:        profileRegistriesRepo,
		WorkspaceRegistriesRepo:      workspaceRegistriesRepo,
		IPRulesRepo:                  ipRulesRepo,
		APIKeysRepo:                  apiKeysRepo,
		TeamRepo:                     teamRepo,
		ProjectRepo:                  projectRepo,
		WebhookRepo:                  webhookRepo,
		ChannelsRepo:                 channelsRepo,
		ChannelIdentitiesRepo:        channelIdentitiesRepo,
		ChannelBindCodesRepo:         channelBindCodesRepo,
		ChannelDMThreadsRepo:         channelDMThreadsRepo,
		ChannelGroupThreadsRepo:      channelGroupThreadsRepo,
		ChannelReceiptsRepo:          channelReceiptsRepo,
		PlansRepo:                    planRepo,
		SubscriptionsRepo:            subscriptionRepo,
		EntitlementsRepo:             entitlementsRepo,
		EntitlementService:           entitlementService,
		UsageRepo:                    usageRepo,

		FeatureFlagsRepo:   featureFlagsRepo,
		FeatureFlagService: featureFlagService,

		NotificationsRepo: notificationsRepo,
		AuditLogRepo:      auditLogRepo,

		InviteCodesRepo: inviteCodesRepo,
		ReferralsRepo:   referralsRepo,

		CreditsRepo:         creditsRepo,
		RedemptionCodesRepo: redemptionCodesRepo,

		PlatformSettingsRepo: platformSettingsRepo,
		SmtpProviderRepo:     smtpProviderRepo,

		UsersRepo:   userRepo,
		AccountRepo: accountRepo,

		UserCredentialRepo: credentialRepo,

		JobRepo: jobRepo,

		ArtifactStore:          artifactStore,
		MessageAttachmentStore: messageAttachmentStore,
		EnvironmentStore:       environmentStore,
		SkillStore:             skillStore,
		MCPDiscoveryService:    mcpDiscoveryService,

		RunLimiter: nil,

		SSEConfig: apihttp.SSEConfig{
			HeartbeatSeconds: cfg.SSE.HeartbeatSeconds,
			BatchLimit:       cfg.SSE.BatchLimit,
		},

		ConfigResolver:    resolver,
		ConfigInvalidator: resolver,
		ConfigRegistry:    registry,

		RepoPersonas:       repoPersonas,
		PersonaSyncTrigger: noopSyncTrigger{},
	})

	var desktopBus eventbus.EventBus
	if b, ok := desktop.GetEventBus().(eventbus.EventBus); ok {
		desktopBus = b
	}

	accountapi.StartTelegramDesktopPoller(ctx, accountapi.TelegramDesktopPollerDeps{
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
		Pool:                    pgxPool,
		EntitlementService:      entitlementService,
		MessageAttachmentStore:  messageAttachmentStore,
		TelegramMode:            "polling",
		Bus:                     desktopBus,
	})

	// ---- HTTP server ----

	srv := &nethttp.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		ln, err := net.Listen("tcp", srv.Addr)
		if err != nil {
			errCh <- fmt.Errorf("listen %s: %w", srv.Addr, err)
			return
		}
		logger.Info("desktop api listening",
			"addr", ln.Addr().String(),
			"sqlite", sqlitePath,
			"storage", storageRoot,
		)
		desktop.MarkAPIReady()
		errCh <- srv.Serve(ln)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("shutdown timeout", "error", err.Error())
	}
	return nil
}

// desktopKeyRing loads or generates the encryption key for desktop mode.
func desktopKeyRing(dataDir string) (*internalcrypto.KeyRing, error) {
	return desktop.LoadEncryptionKeyRing(desktop.KeyRingOptions{
		DataDir:           dataDir,
		GenerateIfMissing: true,
	})
}

type noopSyncTrigger struct{}

func (noopSyncTrigger) Trigger() {}
