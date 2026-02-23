package app

import (
	"context"
	"errors"
	"fmt"
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
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/objectstore"
	sharedredis "arkloop/services/shared/redis"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Application struct {
	config Config
	logger *observability.JSONLogger
}

func NewApplication(config Config, logger *observability.JSONLogger) (*Application, error) {
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
		schemaRepo *data.SchemaRepository
	)
	var poolCloser func()

	dsn := strings.TrimSpace(a.config.DatabaseDSN)
	if dsn != "" {
		createdPool, err := data.NewPool(ctx, dsn)
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
			a.logger.Error("schema version check skipped",
				observability.LogFields{},
				map[string]any{"reason": vErr.Error()},
			)
		} else if schemaVersion != migrate.ExpectedVersion {
			a.logger.Error("schema version mismatch",
				observability.LogFields{},
				map[string]any{
					"current":  schemaVersion,
					"expected": migrate.ExpectedVersion,
				},
			)
		}
	}
	if poolCloser != nil {
		defer poolCloser()
	}

	var redisClient *redis.Client
	if strings.TrimSpace(a.config.RedisURL) != "" {
		rc, err := sharedredis.NewClient(ctx, a.config.RedisURL)
		if err != nil {
			return fmt.Errorf("redis: %w", err)
		}
		defer rc.Close()
		redisClient = rc
		a.logger.Info("redis connected", observability.LogFields{}, nil)
	}

	var runLimiter *data.RunLimiter
	if redisClient != nil && a.config.MaxConcurrentRunsPerOrg > 0 {
		rl, err := data.NewRunLimiter(redisClient, a.config.MaxConcurrentRunsPerOrg)
		if err != nil {
			return fmt.Errorf("run limiter: %w", err)
		}
		runLimiter = rl
		a.logger.Info("run limiter enabled", observability.LogFields{}, map[string]any{
			"max_per_org": a.config.MaxConcurrentRunsPerOrg,
		})
	}

	if strings.TrimSpace(a.config.S3Endpoint) != "" {
		_, err := objectstore.New(
			ctx,
			a.config.S3Endpoint,
			a.config.S3AccessKey,
			a.config.S3SecretKey,
			a.config.S3Bucket,
			a.config.S3Region,
		)
		if err != nil {
			return fmt.Errorf("objectstore: %w", err)
		}
		a.logger.Info("objectstore connected", observability.LogFields{}, map[string]any{"bucket": a.config.S3Bucket})
	}

	var (
		userRepo       *data.UserRepository
		credentialRepo *data.UserCredentialRepository
		membershipRepo *data.OrgMembershipRepository
		orgRepo        *data.OrgRepository
		threadRepo     *data.ThreadRepository
		messageRepo    *data.MessageRepository
		runEventRepo   *data.RunEventRepository
		auditRepo      *data.AuditLogRepository

		secretsRepo         *data.SecretsRepository
		llmCredRepo         *data.LlmCredentialsRepository
		llmRoutesRepo       *data.LlmRoutesRepository
		mcpConfigsRepo      *data.MCPConfigsRepository
		skillsRepo          *data.SkillsRepository
		ipRulesRepo         *data.IPRulesRepository
		apiKeysRepo         *data.APIKeysRepository
		orgInvitationsRepo  *data.OrgInvitationsRepository
		teamRepo            *data.TeamRepository
		projectRepo         *data.ProjectRepository
		webhookRepo         *data.WebhookEndpointRepository
		promptTemplatesRepo *data.PromptTemplateRepository
		agentConfigsRepo    *data.AgentConfigRepository
		plansRepo           *data.PlanRepository
		subscriptionsRepo   *data.SubscriptionRepository
		entitlementsRepo    *data.EntitlementsRepository
		entitlementSvc      *entitlement.Service
		usageRepo           *data.UsageRepository

		featureFlagsRepo *data.FeatureFlagRepository
		featureFlagSvc   *featureflag.Service

		notificationsRepo *data.NotificationsRepository

		inviteCodesRepo *data.InviteCodeRepository
		referralsRepo   *data.ReferralRepository
		creditsRepo     *data.CreditsRepository

		authService         *auth.Service
		registrationService *auth.RegistrationService
		auditWriter         *audit.Writer
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
		membershipRepo, err = data.NewOrgMembershipRepository(pool)
		if err != nil {
			return err
		}
		orgRepo, err = data.NewOrgRepository(pool)
		if err != nil {
			return err
		}
		threadRepo, err = data.NewThreadRepository(pool)
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
		skillsRepo, err = data.NewSkillsRepository(pool)
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
		orgInvitationsRepo, err = data.NewOrgInvitationsRepository(pool)
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
		promptTemplatesRepo, err = data.NewPromptTemplateRepository(pool)
		if err != nil {
			return err
		}
		agentConfigsRepo, err = data.NewAgentConfigRepository(pool)
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
		entitlementSvc, err = entitlement.NewService(entitlementsRepo, subscriptionsRepo, plansRepo, redisClient)
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

		// 加密 key 未配置时 secrets/llm-credentials 端点不可用，但不影响其他功能启动
		keyRing, keyRingErr := crypto.NewKeyRingFromEnv()
		if keyRingErr == nil {
			secretsRepo, err = data.NewSecretsRepository(pool, keyRing)
			if err != nil {
				return err
			}
		} else {
			a.logger.Error("encryption key not configured, secrets disabled",
				observability.LogFields{},
				map[string]any{"reason": keyRingErr.Error()},
			)
		}
	}

	if pool != nil && a.config.Auth != nil {
		passwordHasher, err := auth.NewBcryptPasswordHasher(0)
		if err != nil {
			return err
		}
		tokenService, err := auth.NewJwtAccessTokenService(a.config.Auth.JWTSecret, a.config.Auth.AccessTokenTTLSeconds)
		if err != nil {
			return err
		}
		authService, err = auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService)
		if err != nil {
			return err
		}
		registrationService, err = auth.NewRegistrationService(pool, passwordHasher, tokenService)
		if err != nil {
			return err
		}
		if entitlementSvc != nil {
			registrationService.SetEntitlementResolver(&entitlementAdapter{svc: entitlementSvc})
		}

		if auditRepo != nil {
			auditWriter = audit.NewWriter(auditRepo, membershipRepo, a.logger)
		}

		if login := strings.TrimSpace(a.config.BootstrapPlatformAdmin); login != "" {
			if err := bootstrapPlatformAdmin(ctx, credentialRepo, membershipRepo, login, a.logger); err != nil {
				a.logger.Error("bootstrap platform admin failed", observability.LogFields{}, map[string]any{"login": login, "error": err.Error()})
			}
		}
	}

	listener, err := net.Listen("tcp", a.config.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

	server := &http.Server{
		Handler: apihttp.NewHandler(apihttp.HandlerConfig{
			Pool:                 pool,
			Logger:               a.logger,
			TrustIncomingTraceID: a.config.TrustIncomingTraceID,
			TrustXForwardedFor:   a.config.TrustXForwardedFor,
			SchemaRepository:     schemaRepo,
			AuthService:          authService,
			RegistrationService:  registrationService,
			OrgMembershipRepo:    membershipRepo,
			ThreadRepo:           threadRepo,
			MessageRepo:          messageRepo,
			RunEventRepo:         runEventRepo,
			AuditWriter:          auditWriter,
			LlmCredentialsRepo:   llmCredRepo,
			LlmRoutesRepo:        llmRoutesRepo,
			SecretsRepo:          secretsRepo,
			MCPConfigsRepo:       mcpConfigsRepo,
			SkillsRepo:           skillsRepo,
			IPRulesRepo:          ipRulesRepo,
			APIKeysRepo:          apiKeysRepo,
			OrgInvitationsRepo:   orgInvitationsRepo,
			TeamRepo:             teamRepo,
			ProjectRepo:          projectRepo,
			WebhookRepo:          webhookRepo,
			PromptTemplatesRepo:  promptTemplatesRepo,
			AgentConfigsRepo:     agentConfigsRepo,
			PlansRepo:            plansRepo,
			SubscriptionsRepo:    subscriptionsRepo,
			EntitlementsRepo:     entitlementsRepo,
			EntitlementService:   entitlementSvc,
			UsageRepo:            usageRepo,
			FeatureFlagsRepo:     featureFlagsRepo,
			FeatureFlagService:   featureFlagSvc,
			NotificationsRepo:    notificationsRepo,
			AuditLogRepo:         auditRepo,
			UsersRepo:            userRepo,
			OrgRepo:              orgRepo,
			InviteCodesRepo:      inviteCodesRepo,
			ReferralsRepo:        referralsRepo,
			CreditsRepo:          creditsRepo,
			RedisClient:          redisClient,
			RunLimiter:           runLimiter,
			SSEConfig: apihttp.SSEConfig{
				HeartbeatSeconds: a.config.SSE.HeartbeatSeconds,
				BatchLimit:       a.config.SSE.BatchLimit,
			},
		}),
		ReadHeaderTimeout: 5 * time.Second,
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

// bootstrapPlatformAdmin 将指定 login 的用户提升为 platform_admin。
// 幂等：每次启动都会确认，若已是 platform_admin 则 SQL UPDATE 无实际变化。
func bootstrapPlatformAdmin(
	ctx context.Context,
	credRepo *data.UserCredentialRepository,
	membershipRepo *data.OrgMembershipRepository,
	login string,
	logger *observability.JSONLogger,
) error {
	cred, err := credRepo.GetByLogin(ctx, login)
	if err != nil {
		return fmt.Errorf("lookup credential: %w", err)
	}
	if cred == nil {
		return fmt.Errorf("login %q not found", login)
	}
	if err := membershipRepo.SetRoleForUser(ctx, cred.UserID, "platform_admin"); err != nil {
		return fmt.Errorf("set role: %w", err)
	}
	logger.Info("platform_admin promoted", observability.LogFields{}, map[string]any{"login": login})
	return nil
}

// entitlementAdapter 将 entitlement.Service 适配为 auth.EntitlementResolver 接口。
type entitlementAdapter struct {
	svc *entitlement.Service
}

func (a *entitlementAdapter) Resolve(ctx context.Context, orgID uuid.UUID, key string) (auth.EntitlementValue, error) {
	val, err := a.svc.Resolve(ctx, orgID, key)
	if err != nil {
		return auth.EntitlementValue{}, err
	}
	return auth.EntitlementValue{Raw: val.Raw, Type: val.Type}, nil
}
