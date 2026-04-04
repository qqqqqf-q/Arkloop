//go:build !desktop

package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/crypto"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/featureflag"
	apihttp "arkloop/services/api/internal/http"
	"arkloop/services/api/internal/http/accountapi"
	"arkloop/services/api/internal/jobs"
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/personas"
	"arkloop/services/api/internal/personasync"
	"arkloop/services/api/internal/scheduler"
	"arkloop/services/api/internal/skillseed"
	"arkloop/services/shared/acptoken"
	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/discordbot"
	"arkloop/services/shared/objectstore"
	sharedredis "arkloop/services/shared/redis"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Application struct {
	config Config
	logger *slog.Logger
}

func NewApplication(config Config, logger *slog.Logger) (*Application, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}
	return &Application{
		config: config,
		logger: logger,
	}, nil
}

func (a *Application) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var (
		pool       *pgxpool.Pool
		directPool *pgxpool.Pool
		schemaRepo *data.SchemaRepository
	)
	var poolCloser func()

	dsn := strings.TrimSpace(a.config.DatabaseDSN)
	if dsn != "" {
		createdPool, err := data.NewPool(ctx, dsn, data.PoolLimits{
			MaxConns: int32(a.config.DBPoolMaxConns),
			MinConns: int32(a.config.DBPoolMinConns),
		})
		if err != nil {
			return err
		}
		pool = createdPool
		poolCloser = createdPool.Close

		repo, err := data.NewSchemaRepository(createdPool)
		if err != nil {
			createdPool.Close()
			return err
		}
		schemaRepo = repo

		schemaVersion, vErr := repo.CurrentSchemaVersion(ctx)
		if vErr != nil {
			a.logger.Error("schema version check skipped", "reason", vErr.Error())
		} else if schemaVersion != migrate.ExpectedVersion {
			a.logger.Error("schema version mismatch",
				"current", schemaVersion,
				"expected", migrate.ExpectedVersion,
			)
		}
	}
	if poolCloser != nil {
		defer poolCloser()
	}

	if directDSN := strings.TrimSpace(a.config.DirectDatabaseDSN); directDSN != "" {
		dpCfg, err := pgxpool.ParseConfig(data.NormalizePostgresDSN(directDSN))
		if err != nil {
			return fmt.Errorf("direct pool config: %w", err)
		}
		dpCfg.MaxConns = int32(a.config.DBDirectPoolMaxConns)
		dpCfg.MinConns = int32(a.config.DBDirectPoolMinConns)
		dp, err := pgxpool.NewWithConfig(ctx, dpCfg)
		if err != nil {
			return fmt.Errorf("direct pool: %w", err)
		}
		directPool = dp
		defer directPool.Close()
	} else if pool != nil {
		a.logger.Warn("ARKLOOP_DATABASE_DIRECT_URL not set: LISTEN/NOTIFY uses main pool, breaks with PgBouncer")
		directPool = pool
	}

	statsIntervalSeconds := a.config.DBPoolStatsIntervalSeconds
	if statsIntervalSeconds > 0 && pool != nil {
		interval := time.Duration(statsIntervalSeconds) * time.Second
		startDBPoolStatsLogger(ctx, a.logger, pool, "db_primary", interval)
		if directPool != nil && directPool != pool {
			startDBPoolStatsLogger(ctx, a.logger, directPool, "db_direct", interval)
		}
	}

	var redisClient *redis.Client
	if strings.TrimSpace(a.config.RedisURL) != "" {
		rc, err := sharedredis.NewClient(ctx, a.config.RedisURL)
		if err != nil {
			return fmt.Errorf("redis: %w", err)
		}
		defer rc.Close()
		redisClient = rc
		a.logger.Info("redis connected")
	}

	gatewayRedisClient := redisClient
	gatewayRedisURL := strings.TrimSpace(a.config.GatewayRedisURL)
	redisURL := strings.TrimSpace(a.config.RedisURL)
	if gatewayRedisURL != "" && gatewayRedisURL != redisURL {
		rc, err := sharedredis.NewClient(ctx, gatewayRedisURL)
		if err != nil {
			return fmt.Errorf("gateway redis: %w", err)
		}
		defer rc.Close()
		gatewayRedisClient = rc
		a.logger.Info("gateway redis connected")
	}

	var runLimiter *data.RunLimiter
	if redisClient != nil && a.config.MaxConcurrentRunsPerAccount > 0 {
		rl, err := data.NewRunLimiter(redisClient, a.config.MaxConcurrentRunsPerAccount)
		if err != nil {
			return fmt.Errorf("run limiter: %w", err)
		}
		runLimiter = rl
		a.logger.Info("run limiter enabled", "max_per_account", a.config.MaxConcurrentRunsPerAccount)
	}

	configRegistry := sharedconfig.DefaultRegistry()
	var configCache sharedconfig.Cache
	cacheTTL := sharedconfig.CacheTTLFromEnv()
	if redisClient != nil && cacheTTL > 0 {
		configCache = sharedconfig.NewRedisCache(redisClient)
	}
	configResolver, _ := sharedconfig.NewResolver(configRegistry, sharedconfig.NewPGXStore(pool), configCache, cacheTTL)

	var artifactStore objectstore.Store
	var messageAttachmentStore objectstore.Store
	var environmentStore objectstore.Store
	var skillStore objectstore.Store
	bucketOpener, err := buildStorageBucketOpener(a.config)
	if err != nil {
		return err
	}
	if bucketOpener != nil {
		mainStore, err := bucketOpener.Open(ctx, a.config.S3Bucket)
		if err != nil {
			return fmt.Errorf("objectstore: %w", err)
		}
		messageAttachmentStore = mainStore
		a.logger.Info("objectstore connected", "bucket", a.config.S3Bucket)

		as, err := bucketOpener.Open(ctx, objectstore.ArtifactBucket)
		if err != nil {
			return fmt.Errorf("artifact store: %w", err)
		}
		artifactStore = as
		a.logger.Info("artifact store connected")

		es, err := bucketOpener.Open(ctx, objectstore.EnvironmentStateBucket)
		if err != nil {
			return fmt.Errorf("environment store: %w", err)
		}
		environmentStore = es
		a.logger.Info("environment store connected")

		ss, err := bucketOpener.Open(ctx, objectstore.SkillStoreBucket)
		if err != nil {
			return fmt.Errorf("skill store: %w", err)
		}
		skillStore = ss
		a.logger.Info("skill store connected")
	}

	var (
		userRepo         *data.UserRepository
		credentialRepo   *data.UserCredentialRepository
		membershipRepo   *data.AccountMembershipRepository
		accountRepo      *data.AccountRepository
		threadRepo       *data.ThreadRepository
		threadStarRepo   *data.ThreadStarRepository
		threadShareRepo  *data.ThreadShareRepository
		threadReportRepo *data.ThreadReportRepository
		messageRepo      *data.MessageRepository
		runEventRepo     *data.RunEventRepository
		shellSessionRepo *data.ShellSessionRepository
		auditRepo        *data.AuditLogRepository

		secretsRepo                  *data.SecretsRepository
		llmCredRepo                  *data.LlmCredentialsRepository
		llmRoutesRepo                *data.LlmRoutesRepository
		mcpConfigsRepo               *data.MCPConfigsRepository
		profileMCPInstallsRepo       *data.ProfileMCPInstallsRepository
		workspaceMCPEnableRepo       *data.WorkspaceMCPEnablementsRepository
		toolProviderConfigsRepo      *data.ToolProviderConfigsRepository
		toolDescriptionOverridesRepo *data.ToolDescriptionOverridesRepository
		personasRepo                 *data.PersonasRepository
		skillPackagesRepo            *data.SkillPackagesRepository
		profileSkillInstallsRepo     *data.ProfileSkillInstallsRepository
		platformSkillOverridesRepo   *data.PlatformSkillOverridesRepository
		workspaceSkillEnableRepo     *data.WorkspaceSkillEnablementsRepository
		profileRegistriesRepo        *data.ProfileRegistriesRepository
		workspaceRegistriesRepo      *data.WorkspaceRegistriesRepository
		ipRulesRepo                  *data.IPRulesRepository
		apiKeysRepo                  *data.APIKeysRepository
		teamRepo                     *data.TeamRepository
		projectRepo                  *data.ProjectRepository
		webhookRepo                  *data.WebhookEndpointRepository
		channelsRepo                 *data.ChannelsRepository
		channelIdentitiesRepo        *data.ChannelIdentitiesRepository
		channelIdentityLinksRepo     *data.ChannelIdentityLinksRepository
		channelBindCodesRepo         *data.ChannelBindCodesRepository
		channelDMThreadsRepo         *data.ChannelDMThreadsRepository
		channelGroupThreadsRepo      *data.ChannelGroupThreadsRepository
		channelReceiptsRepo          *data.ChannelMessageReceiptsRepository
		channelLedgerRepo            *data.ChannelMessageLedgerRepository
		plansRepo                    *data.PlanRepository
		subscriptionsRepo            *data.SubscriptionRepository
		entitlementsRepo             *data.EntitlementsRepository
		entitlementSvc               *entitlement.Service
		usageRepo                    *data.UsageRepository

		featureFlagsRepo *data.FeatureFlagRepository
		featureFlagSvc   *featureflag.Service

		notificationsRepo *data.NotificationsRepository

		inviteCodesRepo     *data.InviteCodeRepository
		referralsRepo       *data.ReferralRepository
		creditsRepo         *data.CreditsRepository
		redemptionCodesRepo *data.RedemptionCodesRepository

		platformSettingsRepo *data.PlatformSettingsRepository
		smtpProviderRepo     *data.SmtpProviderRepository

		asrCredRepo *data.AsrCredentialsRepository

		refreshTokenRepo *data.RefreshTokenRepository
		jobRepo          *data.JobRepository

		emailVerifyTokenRepo *data.EmailVerificationTokenRepository

		acpTokenValidator    *acptoken.Validator
		authService          *auth.Service
		registrationService  *auth.RegistrationService
		emailVerifyService   *auth.EmailVerifyService
		emailOTPLoginService *auth.EmailOTPLoginService
		accountService       *auth.AccountService
		auditWriter          *audit.Writer

		emailOTPTokenRepo *data.EmailOTPTokenRepository
	)

	if pool != nil {
		var err error
		userRepo, err = data.NewUserRepository(pool)
		if err != nil {
			return err
		}
		credentialRepo, err = data.NewUserCredentialRepository(pool)
		if err != nil {
			return err
		}
		membershipRepo, err = data.NewAccountMembershipRepository(pool)
		if err != nil {
			return err
		}
		accountRepo, err = data.NewAccountRepository(pool)
		if err != nil {
			return err
		}
		threadRepo, err = data.NewThreadRepository(pool)
		if err != nil {
			return err
		}
		threadStarRepo, err = data.NewThreadStarRepository(pool)
		if err != nil {
			return err
		}
		threadShareRepo, err = data.NewThreadShareRepository(pool)
		if err != nil {
			return err
		}
		threadReportRepo, err = data.NewThreadReportRepository(pool)
		if err != nil {
			return err
		}
		messageRepo, err = data.NewMessageRepository(pool)
		if err != nil {
			return err
		}
		runEventRepo, err = data.NewRunEventRepository(pool)
		if err != nil {
			return err
		}
		shellSessionRepo, err = data.NewShellSessionRepository(pool)
		if err != nil {
			return err
		}
		auditRepo, err = data.NewAuditLogRepository(pool)
		if err != nil {
			return err
		}

		llmCredRepo, err = data.NewLlmCredentialsRepository(pool)
		if err != nil {
			return err
		}
		llmRoutesRepo, err = data.NewLlmRoutesRepository(pool)
		if err != nil {
			return err
		}
		mcpConfigsRepo, err = data.NewMCPConfigsRepository(pool)
		if err != nil {
			return err
		}
		profileMCPInstallsRepo, err = data.NewProfileMCPInstallsRepository(pool)
		if err != nil {
			return err
		}
		workspaceMCPEnableRepo, err = data.NewWorkspaceMCPEnablementsRepository(pool)
		if err != nil {
			return err
		}
		toolProviderConfigsRepo, err = data.NewToolProviderConfigsRepository(pool)
		if err != nil {
			return err
		}
		toolDescriptionOverridesRepo, err = data.NewToolDescriptionOverridesRepository(pool)
		if err != nil {
			return err
		}
		personasRepo, err = data.NewPersonasRepository(pool)
		if err != nil {
			return err
		}
		skillPackagesRepo, err = data.NewSkillPackagesRepository(pool)
		if err != nil {
			return err
		}
		profileSkillInstallsRepo, err = data.NewProfileSkillInstallsRepository(pool)
		if err != nil {
			return err
		}
		platformSkillOverridesRepo, err = data.NewPlatformSkillOverridesRepository(pool)
		if err != nil {
			return err
		}
		workspaceSkillEnableRepo, err = data.NewWorkspaceSkillEnablementsRepository(pool)
		if err != nil {
			return err
		}
		profileRegistriesRepo, err = data.NewProfileRegistriesRepository(pool)
		if err != nil {
			return err
		}
		workspaceRegistriesRepo, err = data.NewWorkspaceRegistriesRepository(pool)
		if err != nil {
			return err
		}
		ipRulesRepo, err = data.NewIPRulesRepository(pool)
		if err != nil {
			return err
		}
		apiKeysRepo, err = data.NewAPIKeysRepository(pool)
		if err != nil {
			return err
		}
		teamRepo, err = data.NewTeamRepository(pool)
		if err != nil {
			return err
		}
		projectRepo, err = data.NewProjectRepository(pool)
		if err != nil {
			return err
		}
		webhookRepo, err = data.NewWebhookEndpointRepository(pool)
		if err != nil {
			return err
		}
		channelsRepo, err = data.NewChannelsRepository(pool)
		if err != nil {
			return err
		}
		channelIdentitiesRepo, err = data.NewChannelIdentitiesRepository(pool)
		if err != nil {
			return err
		}
		channelIdentityLinksRepo, err = data.NewChannelIdentityLinksRepository(pool)
		if err != nil {
			return err
		}
		channelBindCodesRepo, err = data.NewChannelBindCodesRepository(pool)
		if err != nil {
			return err
		}
		channelDMThreadsRepo, err = data.NewChannelDMThreadsRepository(pool)
		if err != nil {
			return err
		}
		channelGroupThreadsRepo, err = data.NewChannelGroupThreadsRepository(pool)
		if err != nil {
			return err
		}
		channelReceiptsRepo, err = data.NewChannelMessageReceiptsRepository(pool)
		if err != nil {
			return err
		}
		channelLedgerRepo, err = data.NewChannelMessageLedgerRepository(pool)
		if err != nil {
			return err
		}
		plansRepo, err = data.NewPlanRepository(pool)
		if err != nil {
			return err
		}
		subscriptionsRepo, err = data.NewSubscriptionRepository(pool)
		if err != nil {
			return err
		}
		entitlementsRepo, err = data.NewEntitlementsRepository(pool)
		if err != nil {
			return err
		}
		entitlementSvc, err = entitlement.NewService(entitlementsRepo, subscriptionsRepo, plansRepo, redisClient, configResolver)
		if err != nil {
			return err
		}
		usageRepo, err = data.NewUsageRepository(pool)
		if err != nil {
			return err
		}
		featureFlagsRepo, err = data.NewFeatureFlagRepository(pool)
		if err != nil {
			return err
		}
		featureFlagSvc, err = featureflag.NewService(featureFlagsRepo, redisClient)
		if err != nil {
			return err
		}
		notificationsRepo, err = data.NewNotificationsRepository(pool)
		if err != nil {
			return err
		}
		inviteCodesRepo, err = data.NewInviteCodeRepository(pool)
		if err != nil {
			return err
		}
		referralsRepo, err = data.NewReferralRepository(pool)
		if err != nil {
			return err
		}
		creditsRepo, err = data.NewCreditsRepository(pool)
		if err != nil {
			return err
		}
		redemptionCodesRepo, err = data.NewRedemptionCodesRepository(pool)
		if err != nil {
			return err
		}
		platformSettingsRepo, err = data.NewPlatformSettingsRepository(pool)
		if err != nil {
			return err
		}
		smtpProviderRepo, err = data.NewSmtpProviderRepository(pool)
		if err != nil {
			return err
		}

		asrCredRepo, err = data.NewAsrCredentialsRepository(pool)
		if err != nil {
			return err
		}

		refreshTokenRepo, err = data.NewRefreshTokenRepository(pool)
		if err != nil {
			return err
		}

		jobRepo, err = data.NewJobRepository(pool)
		if err != nil {
			return err
		}

		hbSched := scheduler.NewLLMHeartbeat(pool, directPool, jobRepo, runEventRepo, threadRepo, runLimiter)
		go hbSched.Run(ctx)

		emailVerifyTokenRepo, err = data.NewEmailVerificationTokenRepository(pool)
		if err != nil {
			return err
		}

		emailOTPTokenRepo, err = data.NewEmailOTPTokenRepository(pool)
		if err != nil {
			return err
		}
		// when encryption key is not configured, secrets/llm-credentials endpoints are unavailable but other features still start
		keyRing, keyRingErr := crypto.NewKeyRingFromEnv()
		if keyRingErr == nil {
			secretsRepo, err = data.NewSecretsRepository(pool, keyRing)
			if err != nil {
				return err
			}
		} else {
			a.logger.Error("encryption key not configured, secrets disabled", "reason", keyRingErr.Error())
		}

		warnUnsafeOutboundBaseURLs(ctx, pool, a.logger)
	}

	if pool != nil && a.config.Auth != nil {
		passwordHasher, err := auth.NewBcryptPasswordHasher(0)
		if err != nil {
			return err
		}
		tokenService, err := auth.NewJwtAccessTokenService(a.config.Auth.JWTSecret, a.config.Auth.AccessTokenTTLSeconds, a.config.Auth.RefreshTokenTTLSeconds)
		if err != nil {
			return err
		}
		acpTokenValidator, err = acptoken.NewValidator(a.config.Auth.JWTSecret)
		if err != nil {
			return err
		}
		authService, err = auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, redisClient, projectRepo)
		if err != nil {
			return err
		}
		registrationService, err = auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
		if err != nil {
			return err
		}
		accountService, err = auth.NewAccountService(pool, accountRepo, membershipRepo)
		if err != nil {
			return err
		}
		if entitlementSvc != nil {
			registrationService.SetEntitlementResolver(&entitlementAdapter{svc: entitlementSvc})
		}

		if emailVerifyTokenRepo != nil && userRepo != nil && jobRepo != nil {
			emailVerifyService, err = auth.NewEmailVerifyService(emailVerifyTokenRepo, userRepo, jobRepo)
			if err != nil {
				return err
			}
			emailVerifyService.SetAppBaseURL(a.config.AppBaseURL, platformSettingsRepo)
		}

		registrationService.SetEmailVerifyService(emailVerifyService)

		if userRepo != nil && emailOTPTokenRepo != nil && jobRepo != nil && tokenService != nil && refreshTokenRepo != nil && membershipRepo != nil {
			var emailOTPRiskControl auth.EmailOTPRiskControl
			if redisClient != nil {
				emailOTPRiskControl = auth.NewRedisEmailOTPRiskControl(redisClient)
			}
			emailOTPLoginService, err = auth.NewEmailOTPLoginService(userRepo, emailOTPTokenRepo, jobRepo, tokenService, refreshTokenRepo, membershipRepo, emailOTPRiskControl, projectRepo)
			if err != nil {
				return err
			}
			emailOTPLoginService.SetAppBaseURL(a.config.AppBaseURL, platformSettingsRepo)
		}

		if featureFlagSvc != nil {
			authService.SetFlagService(featureFlagSvc)
		}

		if auditRepo != nil {
			auditWriter = audit.NewWriter(auditRepo, membershipRepo, a.logger)
		}

		if raw := strings.TrimSpace(a.config.BootstrapPlatformAdminUserID); raw != "" {
			userID, err := uuid.Parse(raw)
			if err != nil {
				a.logger.Error(
					"bootstrap platform admin invalid user_id",
					"value", raw,
					"error", err.Error(),
				)
			} else {
				if err := bootstrapPlatformAdminOnce(ctx, credentialRepo, membershipRepo, platformSettingsRepo, userID, a.logger); err != nil {
					a.logger.Error(
						"bootstrap platform admin failed",
						"user_id", userID.String(),
						"error", err.Error(),
					)
				}
			}
		}
	}

	// start partition manager (auto-create/cleanup run_events monthly partitions)
	if pool != nil {
		partitionMgr := data.NewPartitionManagerWithRetention(pool, a.logger, a.config.RunEventsRetentionMonths)
		go partitionMgr.Run(ctx)
	}

	// start stale run reaper (R73: fix Redis concurrent counter leak)
	if pool != nil && runLimiter != nil {
		reaper := jobs.NewStaleRunReaper(runEventRepo, runLimiter, auditRepo, pool, a.logger, a.config.RunTimeoutMinutes)
		go reaper.Run(ctx)
	}

	// start private thread reaper (hard-delete expired private threads hourly)
	if threadRepo != nil {
		privateReaper := jobs.NewPrivateThreadReaper(threadRepo, a.logger)
		go privateReaper.Run(ctx)
	}

	if pool != nil && channelLedgerRepo != nil {
		storageGovernance := jobs.NewStorageGovernance(pool, channelLedgerRepo, a.logger)
		go storageGovernance.Run(ctx)
	}

	if messageAttachmentStore != nil {
		stagingReaper := jobs.NewStagingAttachmentReaper(messageAttachmentStore, a.logger)
		go stagingReaper.Run(ctx)
	}

	listener, err := net.Listen("tcp", a.config.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

	personasRoot, err := personas.BuiltinPersonasRoot()
	if err != nil {
		return err
	}
	repoPersonas, err := personas.LoadFromDir(personasRoot)
	if err != nil {
		return err
	}

	var personaSyncManager *personasync.Manager
	if pool != nil && personasRepo != nil {
		if deleted, err := personasRepo.DeleteInvalidLuaRuntimeRows(ctx); err != nil {
			return err
		} else if deleted > 0 {
			a.logger.Warn("persona_runtime_rows_deleted", "rows", deleted)
		}
		personaSyncManager = personasync.NewManager(personasRoot, pool, personasRepo, a.logger)
		if err := personaSyncManager.SyncNow(ctx); err != nil {
			return err
		}
		if refreshed, refreshErr := personas.LoadFromDir(personasRoot); refreshErr == nil {
			repoPersonas = refreshed
		}
		go personaSyncManager.Run(ctx)
	}

	// Platform skill seeder
	var skillSeeder *skillseed.Seeder
	if pool != nil && skillPackagesRepo != nil && skillStore != nil {
		skillsRoot, skillsRootErr := skillseed.BuiltinSkillsRoot()
		if skillsRootErr != nil {
			a.logger.Warn("platform_skills_root_not_found", "error", skillsRootErr.Error())
		} else {
			skillSeeder = skillseed.NewSeeder(skillsRoot, pool, skillPackagesRepo, skillStore, a.logger)
			if err := skillSeeder.SyncNow(ctx); err != nil {
				a.logger.Warn("platform_skills_sync_failed", "error", err.Error())
			}
			go skillSeeder.Run(ctx)
		}
	}
	discordClient := discordbot.NewClient("", nil)
	if channelsRepo != nil && channelIdentitiesRepo != nil && channelIdentityLinksRepo != nil && channelBindCodesRepo != nil &&
		channelDMThreadsRepo != nil && channelReceiptsRepo != nil && secretsRepo != nil &&
		personasRepo != nil && threadRepo != nil && messageRepo != nil &&
		runEventRepo != nil && jobRepo != nil && creditsRepo != nil && pool != nil {
		accountapi.StartDiscordIngressRunner(ctx, accountapi.DiscordIngressRunnerDeps{
			ChannelsRepo:             channelsRepo,
			ChannelIdentitiesRepo:    channelIdentitiesRepo,
			ChannelIdentityLinksRepo: channelIdentityLinksRepo,
			ChannelBindCodesRepo:     channelBindCodesRepo,
			ChannelDMThreadsRepo:     channelDMThreadsRepo,
			ChannelReceiptsRepo:      channelReceiptsRepo,
			ChannelLedgerRepo:        channelLedgerRepo,
			SecretsRepo:              secretsRepo,
			PersonasRepo:             personasRepo,
			ThreadRepo:               threadRepo,
			MessageRepo:              messageRepo,
			RunEventRepo:             runEventRepo,
			JobRepo:                  jobRepo,
			CreditsRepo:              creditsRepo,
			Pool:                     pool,
			EntitlementService:       entitlementSvc,
			DiscordClient:            discordClient,
		})
	}

	server := &http.Server{
		Handler: apihttp.NewHandler(apihttp.HandlerConfig{
			Pool:                         pool,
			DirectPool:                   directPool,
			InvalidationListenerCtx:      ctx,
			DirectPoolAcquireTimeout:     time.Duration(a.config.DirectPoolAcquireTimeoutMs) * time.Millisecond,
			MaxInFlight:                  a.config.MaxInFlight,
			Logger:                       a.logger,
			TrustIncomingTraceID:         a.config.TrustIncomingTraceID,
			TrustXForwardedFor:           a.config.TrustXForwardedFor,
			SchemaRepository:             schemaRepo,
			AuthService:                  authService,
			RegistrationService:          registrationService,
			AccountService:               accountService,
			AccountMembershipRepo:        membershipRepo,
			ThreadRepo:                   threadRepo,
			ThreadStarRepo:               threadStarRepo,
			ThreadShareRepo:              threadShareRepo,
			ThreadReportRepo:             threadReportRepo,
			MessageRepo:                  messageRepo,
			RunEventRepo:                 runEventRepo,
			ShellSessionRepo:             shellSessionRepo,
			AuditWriter:                  auditWriter,
			LlmCredentialsRepo:           llmCredRepo,
			LlmRoutesRepo:                llmRoutesRepo,
			SecretsRepo:                  secretsRepo,
			MCPConfigsRepo:               mcpConfigsRepo,
			ProfileMCPInstallsRepo:       profileMCPInstallsRepo,
			WorkspaceMCPEnableRepo:       workspaceMCPEnableRepo,
			ToolProviderConfigsRepo:      toolProviderConfigsRepo,
			ToolDescriptionOverridesRepo: toolDescriptionOverridesRepo,
			PersonasRepo:                 personasRepo,
			SkillPackagesRepo:            skillPackagesRepo,
			ProfileSkillInstallsRepo:     profileSkillInstallsRepo,
			PlatformSkillOverridesRepo:   platformSkillOverridesRepo,
			WorkspaceSkillEnableRepo:     workspaceSkillEnableRepo,
			ProfileRegistriesRepo:        profileRegistriesRepo,
			WorkspaceRegistriesRepo:      workspaceRegistriesRepo,
			IPRulesRepo:                  ipRulesRepo,
			APIKeysRepo:                  apiKeysRepo,
			TeamRepo:                     teamRepo,
			ProjectRepo:                  projectRepo,
			WebhookRepo:                  webhookRepo,
			ChannelsRepo:                 channelsRepo,
			ChannelIdentitiesRepo:        channelIdentitiesRepo,
			ChannelIdentityLinksRepo:     channelIdentityLinksRepo,
			ChannelBindCodesRepo:         channelBindCodesRepo,
			ChannelDMThreadsRepo:         channelDMThreadsRepo,
			ChannelGroupThreadsRepo:      channelGroupThreadsRepo,
			ChannelReceiptsRepo:          channelReceiptsRepo,
			PlansRepo:                    plansRepo,
			SubscriptionsRepo:            subscriptionsRepo,
			EntitlementsRepo:             entitlementsRepo,
			EntitlementService:           entitlementSvc,
			UsageRepo:                    usageRepo,
			FeatureFlagsRepo:             featureFlagsRepo,
			FeatureFlagService:           featureFlagSvc,
			NotificationsRepo:            notificationsRepo,
			AuditLogRepo:                 auditRepo,
			UsersRepo:                    userRepo,
			AccountRepo:                  accountRepo,
			UserCredentialRepo:           credentialRepo,
			InviteCodesRepo:              inviteCodesRepo,
			ReferralsRepo:                referralsRepo,
			CreditsRepo:                  creditsRepo,
			RedemptionCodesRepo:          redemptionCodesRepo,
			PlatformSettingsRepo:         platformSettingsRepo,
			SmtpProviderRepo:             smtpProviderRepo,
			RedisClient:                  redisClient,
			GatewayRedisClient:           gatewayRedisClient,
			RunLimiter:                   runLimiter,
			AsrCredentialsRepo:           asrCredRepo,
			EmailVerifyService:           emailVerifyService,
			EmailOTPLoginService:         emailOTPLoginService,
			JobRepo:                      jobRepo,
			ArtifactStore:                artifactStore,
			MessageAttachmentStore:       messageAttachmentStore,
			EnvironmentStore:             environmentStore,
			SkillStore:                   skillStore,
			EmailFrom:                    strings.TrimSpace(a.config.EmailFrom),
			AppBaseURL:                   a.config.AppBaseURL,
			DiscordBotClient:             discordClient,
			TurnstileEnvSecretKey:        a.config.TurnstileSecretKey,
			TurnstileEnvSiteKey:          a.config.TurnstileSiteKey,
			TurnstileEnvAllowedHost:      a.config.TurnstileAllowedHost,
			ConfigResolver:               configResolver,
			ConfigInvalidator:            configResolver,
			ConfigRegistry:               configRegistry,
			MCPDiscoveryService:          nil,
			SSEConfig: apihttp.SSEConfig{
				HeartbeatSeconds: a.config.SSE.HeartbeatSeconds,
				BatchLimit:       a.config.SSE.BatchLimit,
			},
			RepoPersonas:       repoPersonas,
			PersonaSyncTrigger: personaSyncManager,
			ACPTokenValidator:  acpTokenValidator,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return err
	}

	err = <-errCh
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func buildStorageBucketOpener(cfg Config) (objectstore.BucketOpener, error) {
	runtimeConfig, err := objectstore.NormalizeRuntimeConfig(objectstore.RuntimeConfig{
		Backend: cfg.StorageBackend,
		RootDir: cfg.StorageRoot,
		S3Config: objectstore.S3Config{
			Endpoint:  cfg.S3Endpoint,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
			Region:    cfg.S3Region,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	if !runtimeConfig.Enabled() {
		return nil, nil
	}
	return runtimeConfig.BucketOpener()
}

func startDBPoolStatsLogger(
	ctx context.Context,
	logger *slog.Logger,
	pool *pgxpool.Pool,
	poolName string,
	interval time.Duration,
) {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil || pool == nil || strings.TrimSpace(poolName) == "" || interval <= 0 {
		return
	}

	go func() {
		prev := pool.Stat()
		prevCount := prev.AcquireCount()
		prevDuration := prev.AcquireDuration()
		logDBPoolStats(logger, poolName, prev, 0, 0, 0)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			stat := pool.Stat()
			count := stat.AcquireCount()
			duration := stat.AcquireDuration()

			deltaCount := count - prevCount
			deltaDuration := duration - prevDuration
			if deltaCount < 0 || deltaDuration < 0 {
				deltaCount = 0
				deltaDuration = 0
			}

			var avgMs float64
			if deltaCount > 0 {
				avgMs = durationMs(deltaDuration) / float64(deltaCount)
			}

			logDBPoolStats(logger, poolName, stat, deltaCount, durationMs(deltaDuration), avgMs)
			prevCount = count
			prevDuration = duration
		}
	}()
}

func logDBPoolStats(
	logger *slog.Logger,
	poolName string,
	stat *pgxpool.Stat,
	deltaCount int64,
	deltaDurationMs float64,
	avgMs float64,
) {
	if logger == nil || stat == nil {
		return
	}

	logger.Info("db_pool",
		"pool", poolName,
		"max_conns", stat.MaxConns(),
		"total_conns", stat.TotalConns(),
		"acquired_conns", stat.AcquiredConns(),
		"idle_conns", stat.IdleConns(),
		"acquire_count_total", stat.AcquireCount(),
		"acquire_duration_ms_total", durationMs(stat.AcquireDuration()),
		"acquire_count_delta", deltaCount,
		"acquire_duration_ms_delta", deltaDurationMs,
		"acquire_avg_ms_delta", avgMs,
	)
}

func durationMs(d time.Duration) float64 {
	return float64(d.Nanoseconds()) / 1e6
}

type bootstrapCredentialRepo interface {
	GetByUserID(ctx context.Context, userID uuid.UUID) (*data.UserCredential, error)
}

type bootstrapMembershipRepo interface {
	SetRoleForUser(ctx context.Context, userID uuid.UUID, role string) error
}

type bootstrapSettingsRepo interface {
	Get(ctx context.Context, key string) (*data.PlatformSetting, error)
	Set(ctx context.Context, key, value string) (*data.PlatformSetting, error)
}

const bootstrapPlatformAdminSettingKey = "bootstrap.platform_admin.user_id"

// bootstrapPlatformAdminOnce 将指定 user_id 的用户提升为 platform_admin，并写入一次性标记。
// 标记存在且不匹配时直接报错，避免“换人再次 bootstrap”。
func bootstrapPlatformAdminOnce(
	ctx context.Context,
	credRepo bootstrapCredentialRepo,
	membershipRepo bootstrapMembershipRepo,
	settingsRepo bootstrapSettingsRepo,
	userID uuid.UUID,
	logger *slog.Logger,
) error {
	if ctx == nil {
		ctx = context.Background()
	}

	existing, err := settingsRepo.Get(ctx, bootstrapPlatformAdminSettingKey)
	if err != nil {
		return fmt.Errorf("get bootstrap marker: %w", err)
	}
	if existing != nil {
		if strings.TrimSpace(existing.Value) == userID.String() {
			return nil
		}
		return fmt.Errorf("bootstrap marker already set")
	}

	cred, err := credRepo.GetByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("lookup credential: %w", err)
	}
	if cred == nil {
		return fmt.Errorf("user %s not found", userID.String())
	}
	if err := membershipRepo.SetRoleForUser(ctx, userID, auth.RolePlatformAdmin); err != nil {
		return fmt.Errorf("set role: %w", err)
	}

	if _, err := settingsRepo.Set(ctx, bootstrapPlatformAdminSettingKey, userID.String()); err != nil {
		return fmt.Errorf("write bootstrap marker: %w", err)
	}
	logger.Info(
		"platform_admin bootstrapped",
		"user_id", userID.String(),
		"login", cred.Login,
	)
	return nil
}

// entitlementAdapter adapts entitlement.Service to the auth.EntitlementResolver interface.
type entitlementAdapter struct {
	svc *entitlement.Service
}

func (a *entitlementAdapter) Resolve(ctx context.Context, accountID uuid.UUID, key string) (auth.EntitlementValue, error) {
	val, err := a.svc.Resolve(ctx, accountID, key)
	if err != nil {
		return auth.EntitlementValue{}, err
	}
	return auth.EntitlementValue{Raw: val.Raw, Type: val.Type}, nil
}
