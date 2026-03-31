//go:build !desktop

package http

import (
	"context"
	nethttp "net/http"
	"log/slog"
	"os"
	"time"

	"arkloop/services/api/internal/http/accountapi"
	"arkloop/services/api/internal/http/adminapi"
	"arkloop/services/api/internal/http/authapi"
	"arkloop/services/api/internal/http/billingapi"
	"arkloop/services/api/internal/http/catalogapi"
	"arkloop/services/api/internal/http/conversationapi"
	"arkloop/services/api/internal/http/llmproxyapi"
	"arkloop/services/api/internal/http/platformapi"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/featureflag"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/personas"
	"arkloop/services/shared/acptoken"
	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/discordbot"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/telegrambot"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// SSEConfig controls SSE stream heartbeat behavior.
type SSEConfig struct {
	HeartbeatSeconds float64
	BatchLimit       int
}

func defaultSSEConfig() SSEConfig {
	return SSEConfig{
		HeartbeatSeconds: 15.0,
		BatchLimit:       500,
	}
}

type HandlerConfig struct {
	Pool                     data.DB
	DirectPool               *pgxpool.Pool // LISTEN/NOTIFY 专用，不走 PgBouncer
	InvalidationListenerCtx  context.Context
	DirectPoolAcquireTimeout time.Duration
	Logger                   *slog.Logger
	SchemaRepository         *data.SchemaRepository
	TrustIncomingTraceID     bool
	TrustXForwardedFor       bool
	MaxInFlight              int

	AuthService           *auth.Service
	RegistrationService   *auth.RegistrationService
	EmailVerifyService    *auth.EmailVerifyService
	EmailOTPLoginService  *auth.EmailOTPLoginService
	AccountService        *auth.AccountService
	AccountMembershipRepo *data.AccountMembershipRepository
	ThreadRepo            *data.ThreadRepository
	ThreadStarRepo        *data.ThreadStarRepository
	ThreadShareRepo       *data.ThreadShareRepository
	ThreadReportRepo      *data.ThreadReportRepository
	MessageRepo           *data.MessageRepository
	RunEventRepo          *data.RunEventRepository
	ShellSessionRepo      *data.ShellSessionRepository
	AuditWriter           *audit.Writer

	LlmCredentialsRepo           *data.LlmCredentialsRepository
	LlmRoutesRepo                *data.LlmRoutesRepository
	SecretsRepo                  *data.SecretsRepository
	AsrCredentialsRepo           *data.AsrCredentialsRepository
	MCPConfigsRepo               *data.MCPConfigsRepository
	ProfileMCPInstallsRepo       *data.ProfileMCPInstallsRepository
	WorkspaceMCPEnableRepo       *data.WorkspaceMCPEnablementsRepository
	ToolProviderConfigsRepo      *data.ToolProviderConfigsRepository
	ToolDescriptionOverridesRepo *data.ToolDescriptionOverridesRepository
	PersonasRepo                 *data.PersonasRepository
	SkillPackagesRepo            *data.SkillPackagesRepository
	ProfileSkillInstallsRepo     *data.ProfileSkillInstallsRepository
	PlatformSkillOverridesRepo   *data.PlatformSkillOverridesRepository
	WorkspaceSkillEnableRepo     *data.WorkspaceSkillEnablementsRepository
	ProfileRegistriesRepo        *data.ProfileRegistriesRepository
	WorkspaceRegistriesRepo      *data.WorkspaceRegistriesRepository
	IPRulesRepo                  *data.IPRulesRepository
	APIKeysRepo                  *data.APIKeysRepository
	TeamRepo                     *data.TeamRepository
	ProjectRepo                  *data.ProjectRepository
	WebhookRepo                  *data.WebhookEndpointRepository
	ChannelsRepo                 *data.ChannelsRepository
	ChannelIdentitiesRepo        *data.ChannelIdentitiesRepository
	ChannelIdentityLinksRepo     *data.ChannelIdentityLinksRepository
	ChannelBindCodesRepo         *data.ChannelBindCodesRepository
	ChannelDMThreadsRepo         *data.ChannelDMThreadsRepository
	ChannelGroupThreadsRepo      *data.ChannelGroupThreadsRepository
	ChannelReceiptsRepo          *data.ChannelMessageReceiptsRepository
	PlansRepo                    *data.PlanRepository
	SubscriptionsRepo            *data.SubscriptionRepository
	EntitlementsRepo             *data.EntitlementsRepository
	EntitlementService           *entitlement.Service
	UsageRepo                    *data.UsageRepository

	FeatureFlagsRepo   *data.FeatureFlagRepository
	FeatureFlagService *featureflag.Service

	NotificationsRepo *data.NotificationsRepository
	AuditLogRepo      *data.AuditLogRepository

	InviteCodesRepo *data.InviteCodeRepository
	ReferralsRepo   *data.ReferralRepository

	CreditsRepo         *data.CreditsRepository
	RedemptionCodesRepo *data.RedemptionCodesRepository

	PlatformSettingsRepo *data.PlatformSettingsRepository
	SmtpProviderRepo     *data.SmtpProviderRepository

	UsersRepo   *data.UserRepository
	AccountRepo *data.AccountRepository

	UserCredentialRepo *data.UserCredentialRepository

	JobRepo *data.JobRepository

	ArtifactStore          artifactStore
	MessageAttachmentStore messageAttachmentStore
	EnvironmentStore       environmentStore
	SkillStore             skillStore
	MCPDiscoveryService    catalogapi.MCPDiscoverySourceService

	EmailFrom  string
	AppBaseURL string

	TelegramBotClient *telegrambot.Client
	DiscordBotClient  *discordbot.Client

	TurnstileEnvSecretKey   string
	TurnstileEnvSiteKey     string
	TurnstileEnvAllowedHost string

	RedisClient *redis.Client
	// 网关相关 key 专用 Redis（未设置时回退到 RedisClient）。
	GatewayRedisClient *redis.Client
	RunLimiter         *data.RunLimiter

	SSEConfig SSEConfig

	ConfigResolver    sharedconfig.Resolver
	ConfigInvalidator sharedconfig.Invalidator
	ConfigRegistry    *sharedconfig.Registry

	RepoPersonas       []personas.RepoPersona
	PersonaSyncTrigger interface{ Trigger() }

	ACPTokenValidator *acptoken.Validator
}

type artifactStore interface {
	Head(ctx context.Context, key string) (objectstore.ObjectInfo, error)
	GetWithContentType(ctx context.Context, key string) ([]byte, string, error)
}

type messageAttachmentStore interface {
	Head(ctx context.Context, key string) (objectstore.ObjectInfo, error)
	GetWithContentType(ctx context.Context, key string) ([]byte, string, error)
	PutObject(ctx context.Context, key string, data []byte, options objectstore.PutOptions) error
	Delete(ctx context.Context, key string) error
}

type environmentStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

type skillStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Head(ctx context.Context, key string) (objectstore.ObjectInfo, error)
	PutObject(ctx context.Context, key string, data []byte, options objectstore.PutOptions) error
}

func NewHandler(cfg HandlerConfig) nethttp.Handler {
	registry := cfg.ConfigRegistry
	if registry == nil {
		registry = sharedconfig.DefaultRegistry()
	}
	resolver := cfg.ConfigResolver
	if resolver == nil {
		var cache sharedconfig.Cache
		cacheTTL := sharedconfig.CacheTTLFromEnv()
		if cfg.RedisClient != nil && cacheTTL > 0 {
			cache = sharedconfig.NewRedisCache(cfg.RedisClient)
		}
		fallback, _ := sharedconfig.NewResolver(registry, sharedconfig.NewPGXStoreQuerier(cfg.Pool), cache, cacheTTL)
		resolver = fallback
	}
	invalidator := cfg.ConfigInvalidator
	if invalidator == nil {
		if inv, ok := resolver.(sharedconfig.Invalidator); ok {
			invalidator = inv
		}
	}

	telegramClient := cfg.TelegramBotClient
	if telegramClient == nil {
		telegramClient = telegrambot.NewClient(os.Getenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL"), nil)
	}
	discordClient := cfg.DiscordBotClient
	if discordClient == nil {
		discordClient = discordbot.NewClient("", nil)
	}

	gatewayRedis := cfg.GatewayRedisClient
	if gatewayRedis == nil {
		gatewayRedis = cfg.RedisClient
	}

	effectiveToolCatalogCache := catalogapi.NewEffectiveToolCatalogCache(catalogapi.EffectiveToolCatalogTTL)
	listenerCtx := cfg.InvalidationListenerCtx
	if listenerCtx == nil {
		listenerCtx = context.Background()
	}
	effectiveToolCatalogCache.StartInvalidationListener(listenerCtx, cfg.DirectPool)

	mux := nethttp.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/readyz", readyz(cfg.SchemaRepository, cfg.Logger))
	sseConfig := cfg.SSEConfig
	if sseConfig.BatchLimit <= 0 {
		sseConfig = defaultSSEConfig()
	}

	authapi.RegisterRoutes(mux, authapi.Deps{
		AuthService:           cfg.AuthService,
		RegistrationService:   cfg.RegistrationService,
		EmailVerifyService:    cfg.EmailVerifyService,
		EmailOTPLoginService:  cfg.EmailOTPLoginService,
		FeatureFlagService:    cfg.FeatureFlagService,
		AuditWriter:           cfg.AuditWriter,
		AccountMembershipRepo: cfg.AccountMembershipRepo,
		AccountRepo:           cfg.AccountRepo,
		UserCredentialRepo:    cfg.UserCredentialRepo,
		UsersRepo:             cfg.UsersRepo,
		ConfigResolver:        resolver,
	})

	conversationapi.RegisterRoutes(mux, conversationapi.Deps{
		AuthService:              cfg.AuthService,
		AccountMembershipRepo:    cfg.AccountMembershipRepo,
		ThreadRepo:               cfg.ThreadRepo,
		ThreadStarRepo:           cfg.ThreadStarRepo,
		ThreadShareRepo:          cfg.ThreadShareRepo,
		ThreadReportRepo:         cfg.ThreadReportRepo,
		MessageRepo:              cfg.MessageRepo,
		RunEventRepo:             cfg.RunEventRepo,
		ShellSessionRepo:         cfg.ShellSessionRepo,
		ProjectRepo:              cfg.ProjectRepo,
		TeamRepo:                 cfg.TeamRepo,
		AuditWriter:              cfg.AuditWriter,
		Pool:                     cfg.Pool,
		DirectPool:               cfg.DirectPool,
		DirectPoolAcquireTimeout: cfg.DirectPoolAcquireTimeout,
		APIKeysRepo:              cfg.APIKeysRepo,
		RunLimiter:               cfg.RunLimiter,
		EntitlementService:       cfg.EntitlementService,
		RedisClient:              cfg.RedisClient,
		ConfigResolver:           resolver,
		SSEConfig:                conversationapi.SSEConfig(sseConfig),
		MessageAttachmentStore:   cfg.MessageAttachmentStore,
		ArtifactStore:            cfg.ArtifactStore,
	})

	catalogapi.RegisterRoutes(mux, catalogapi.Deps{
		AuthService:                  cfg.AuthService,
		AccountMembershipRepo:        cfg.AccountMembershipRepo,
		LlmCredentialsRepo:           cfg.LlmCredentialsRepo,
		LlmRoutesRepo:                cfg.LlmRoutesRepo,
		SecretsRepo:                  cfg.SecretsRepo,
		Pool:                         cfg.Pool,
		DirectPool:                   cfg.DirectPool,
		AsrCredentialsRepo:           cfg.AsrCredentialsRepo,
		MCPConfigsRepo:               cfg.MCPConfigsRepo,
		ProfileMCPInstallsRepo:       cfg.ProfileMCPInstallsRepo,
		WorkspaceMCPEnableRepo:       cfg.WorkspaceMCPEnableRepo,
		ToolProviderConfigsRepo:      cfg.ToolProviderConfigsRepo,
		ToolDescriptionOverridesRepo: cfg.ToolDescriptionOverridesRepo,
		PersonasRepo:                 cfg.PersonasRepo,
		SkillPackagesRepo:            cfg.SkillPackagesRepo,
		ProfileSkillInstallsRepo:     cfg.ProfileSkillInstallsRepo,
		PlatformSkillOverridesRepo:   cfg.PlatformSkillOverridesRepo,
		WorkspaceSkillEnableRepo:     cfg.WorkspaceSkillEnableRepo,
		ProfileRegistriesRepo:        cfg.ProfileRegistriesRepo,
		WorkspaceRegistriesRepo:      cfg.WorkspaceRegistriesRepo,
		PlatformSettingsRepo:         cfg.PlatformSettingsRepo,
		APIKeysRepo:                  cfg.APIKeysRepo,
		ProjectRepo:                  cfg.ProjectRepo,
		AuditWriter:                  cfg.AuditWriter,
		SkillStore:                   cfg.SkillStore,
		RepoPersonas:                 cfg.RepoPersonas,
		PersonaSyncTrigger:           cfg.PersonaSyncTrigger,
		EffectiveToolCatalogCache:    effectiveToolCatalogCache,
		ArtifactStoreAvailable:       cfg.ArtifactStore != nil,
		Logger:                       cfg.Logger,
		MCPDiscoveryService:          cfg.MCPDiscoveryService,
	})

	billingapi.RegisterRoutes(mux, billingapi.Deps{
		AuthService:           cfg.AuthService,
		AccountMembershipRepo: cfg.AccountMembershipRepo,
		PlansRepo:             cfg.PlansRepo,
		EntitlementsRepo:      cfg.EntitlementsRepo,
		APIKeysRepo:           cfg.APIKeysRepo,
		SubscriptionsRepo:     cfg.SubscriptionsRepo,
		EntitlementService:    cfg.EntitlementService,
		UsageRepo:             cfg.UsageRepo,
		CreditsRepo:           cfg.CreditsRepo,
		InviteCodesRepo:       cfg.InviteCodesRepo,
		ReferralsRepo:         cfg.ReferralsRepo,
		RedemptionCodesRepo:   cfg.RedemptionCodesRepo,
		AuditWriter:           cfg.AuditWriter,
		Pool:                  cfg.Pool,
	})

	accountapi.RegisterRoutes(mux, accountapi.Deps{
		AuthService:             cfg.AuthService,
		AccountMembershipRepo:   cfg.AccountMembershipRepo,
		ThreadRepo:              cfg.ThreadRepo,
		TeamRepo:                cfg.TeamRepo,
		ProjectRepo:             cfg.ProjectRepo,
		APIKeysRepo:             cfg.APIKeysRepo,
		AuditWriter:             cfg.AuditWriter,
		EntitlementService:      cfg.EntitlementService,
		Pool:                    cfg.Pool,
		AccountRepo:             cfg.AccountRepo,
		AccountService:          cfg.AccountService,
		WebhookRepo:             cfg.WebhookRepo,
		SecretsRepo:             cfg.SecretsRepo,
		LlmCredentialsRepo:      cfg.LlmCredentialsRepo,
		LlmRoutesRepo:           cfg.LlmRoutesRepo,
		ChannelsRepo:            cfg.ChannelsRepo,
		ChannelIdentitiesRepo:   cfg.ChannelIdentitiesRepo,
		ChannelIdentityLinksRepo: cfg.ChannelIdentityLinksRepo,
		ChannelBindCodesRepo:    cfg.ChannelBindCodesRepo,
		ChannelDMThreadsRepo:    cfg.ChannelDMThreadsRepo,
		ChannelGroupThreadsRepo: cfg.ChannelGroupThreadsRepo,
		ChannelReceiptsRepo:     cfg.ChannelReceiptsRepo,
		UsersRepo:               cfg.UsersRepo,
		MessageRepo:             cfg.MessageRepo,
		JobRepo:                 cfg.JobRepo,
		CreditsRepo:             cfg.CreditsRepo,
		PersonasRepo:            cfg.PersonasRepo,
		TelegramBotClient:       telegramClient,
		DiscordBotClient:        discordClient,
		TelegramMode:            "webhook",
		AppBaseURL:              cfg.AppBaseURL,
		EnvironmentStore:        cfg.EnvironmentStore,
		RunEventRepo:            cfg.RunEventRepo,
		GatewayRedisClient:      gatewayRedis,
		EntitlementsRepo:        cfg.EntitlementsRepo,
		ConfigResolver:          resolver,
		MessageAttachmentStore:  cfg.MessageAttachmentStore,
	})

	platformapi.RegisterRoutes(mux, platformapi.Deps{
		AuthService:           cfg.AuthService,
		AccountMembershipRepo: cfg.AccountMembershipRepo,
		FeatureFlagsRepo:      cfg.FeatureFlagsRepo,
		FeatureFlagService:    cfg.FeatureFlagService,
		APIKeysRepo:           cfg.APIKeysRepo,
		AuditWriter:           cfg.AuditWriter,
		IPRulesRepo:           cfg.IPRulesRepo,
		GatewayRedisClient:    gatewayRedis,
		NotificationsRepo:     cfg.NotificationsRepo,
		AuditLogRepo:          cfg.AuditLogRepo,
		PlatformSettingsRepo:  cfg.PlatformSettingsRepo,
		RedisClient:           cfg.RedisClient,
		ConfigInvalidator:     invalidator,
		ConfigRegistry:        registry,
	})

	adminapi.RegisterRoutes(mux, adminapi.Deps{
		AuthService:           cfg.AuthService,
		AccountMembershipRepo: cfg.AccountMembershipRepo,
		UsersRepo:             cfg.UsersRepo,
		RunEventRepo:          cfg.RunEventRepo,
		UsageRepo:             cfg.UsageRepo,
		AccountRepo:           cfg.AccountRepo,
		APIKeysRepo:           cfg.APIKeysRepo,
		MessageRepo:           cfg.MessageRepo,
		LlmCredentialsRepo:    cfg.LlmCredentialsRepo,
		ThreadRepo:            cfg.ThreadRepo,
		ThreadReportRepo:      cfg.ThreadReportRepo,
		AuditWriter:           cfg.AuditWriter,
		InviteCodesRepo:       cfg.InviteCodesRepo,
		ReferralsRepo:         cfg.ReferralsRepo,
		CreditsRepo:           cfg.CreditsRepo,
		RedemptionCodesRepo:   cfg.RedemptionCodesRepo,
		NotificationsRepo:     cfg.NotificationsRepo,
		Pool:                  cfg.Pool,
		Logger:                cfg.Logger,
		GatewayRedisClient:    gatewayRedis,
		PlatformSettingsRepo:  cfg.PlatformSettingsRepo,
		ConfigResolver:        resolver,
		ConfigInvalidator:     invalidator,
		ConfigRegistry:        registry,
		PersonasRepo:          cfg.PersonasRepo,
		RepoPersonas:          cfg.RepoPersonas,
		JobRepo:               cfg.JobRepo,
		SmtpProviderRepo:      cfg.SmtpProviderRepo,
		UserCredentialRepo:    cfg.UserCredentialRepo,
	})

	llmproxyapi.RegisterRoutes(mux, llmproxyapi.Deps{
		TokenValidator: cfg.ACPTokenValidator,
		LlmCredRepo:    cfg.LlmCredentialsRepo,
		LlmRoutesRepo:  cfg.LlmRoutesRepo,
		SecretsRepo:    cfg.SecretsRepo,
		Pool:           cfg.Pool,
		RedisClient:    cfg.RedisClient,
		RunEventRepo:   cfg.RunEventRepo,
	})

	notFound := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		WriteError(w, nethttp.StatusNotFound, "http.not_found", "Not Found", traceID, nil)
	})

	base := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		handler, pattern := mux.Handler(r)
		if pattern == "" {
			notFound.ServeHTTP(w, r)
			return
		}
		handler.ServeHTTP(w, r)
	})

	handler := RecoverMiddleware(base, cfg.Logger)
	handler = InFlightMiddleware(handler, cfg.MaxInFlight)
	handler = TraceMiddleware(handler, cfg.Logger, cfg.TrustIncomingTraceID, cfg.TrustXForwardedFor)
	return handler
}
