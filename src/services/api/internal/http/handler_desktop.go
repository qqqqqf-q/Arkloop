//go:build desktop

package http

import (
	"context"
	"fmt"
	"log/slog"
	nethttp "net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"arkloop/services/api/internal/http/accountapi"
	"arkloop/services/api/internal/http/adminapi"
	"arkloop/services/api/internal/http/authapi"
	"arkloop/services/api/internal/http/billingapi"
	"arkloop/services/api/internal/http/catalogapi"
	"arkloop/services/api/internal/http/conversationapi"
	"arkloop/services/api/internal/http/memoryapi"
	"arkloop/services/api/internal/http/platformapi"
	"arkloop/services/api/internal/http/scheduledjobsapi"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/featureflag"
	"arkloop/services/api/internal/observability"
	repopersonas "arkloop/services/api/internal/personas"
	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/desktop"
	"arkloop/services/shared/discordbot"
	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/telegrambot"
)

// SSEConfig controls SSE stream heartbeat behavior.
type SSEConfig struct {
	HeartbeatSeconds float64
	BatchLimit       int
}

func defaultSSEConfig() SSEConfig {
	return SSEConfig{
		HeartbeatSeconds: 1.0,
		BatchLimit:       500,
	}
}

func atoiDefault(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
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

// HandlerConfig for desktop mode.
// No *redis.Client: all dependencies go through
// repository interfaces or can accept nil gracefully.
type HandlerConfig struct {
	Pool                 data.DB // *sqlitepgx.Pool in desktop mode
	Logger               *slog.Logger
	SchemaRepository     *data.SchemaRepository
	TrustIncomingTraceID bool
	TrustXForwardedFor   bool
	MaxInFlight          int

	AuthService           *auth.Service
	RegistrationService   *auth.RegistrationService
	EmailVerifyService    *auth.EmailVerifyService
	EmailOTPLoginService  *auth.EmailOTPLoginService
	AccountService        *auth.AccountService
	AppBaseURL            string
	AccountMembershipRepo *data.AccountMembershipRepository
	ThreadRepo            *data.ThreadRepository
	ThreadStarRepo        *data.ThreadStarRepository
	ThreadShareRepo       *data.ThreadShareRepository
	ThreadReportRepo      *data.ThreadReportRepository
	MessageRepo           *data.MessageRepository
	RunEventRepo          *data.RunEventRepository
	RunPipelineEventsRepo *data.RunPipelineEventsRepository
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
	WorkspaceSkillEnableRepo     *data.WorkspaceSkillEnablementsRepository
	PlatformSkillOverridesRepo   *data.PlatformSkillOverridesRepository
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

	ScheduledJobsRepo *data.ScheduledJobsRepository

	ArtifactStore          artifactStore
	MessageAttachmentStore messageAttachmentStore
	EnvironmentStore       environmentStore
	SkillStore             skillStore
	MCPDiscoveryService    catalogapi.MCPDiscoverySourceService

	RunLimiter *data.RunLimiter

	SSEConfig SSEConfig

	ConfigResolver    sharedconfig.Resolver
	ConfigInvalidator sharedconfig.Invalidator
	ConfigRegistry    *sharedconfig.Registry

	RepoPersonas       []repopersonas.RepoPersona
	PersonaSyncTrigger interface{ Trigger() }

	TelegramBotClient *telegrambot.Client
	DiscordBotClient  *discordbot.Client
}

func NewHandler(cfg HandlerConfig) nethttp.Handler {
	registry := cfg.ConfigRegistry
	if registry == nil {
		registry = sharedconfig.DefaultRegistry()
	}

	resolver := cfg.ConfigResolver
	if resolver == nil {
		// Desktop: env vars + registry defaults, no DB store, no Redis cache.
		fallback, _ := sharedconfig.NewResolver(registry, nil, nil, 0)
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

	effectiveToolCatalogCache := catalogapi.NewEffectiveToolCatalogCache(catalogapi.EffectiveToolCatalogTTL)
	// nil directPool: StartInvalidationListener returns immediately.

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

	var bus eventbus.EventBus
	if b, ok := desktop.GetEventBus().(eventbus.EventBus); ok {
		bus = b
	}

	conversationapi.RegisterRoutes(mux, conversationapi.Deps{
		AuthService:            cfg.AuthService,
		AccountMembershipRepo:  cfg.AccountMembershipRepo,
		ThreadRepo:             cfg.ThreadRepo,
		ThreadStarRepo:         cfg.ThreadStarRepo,
		ThreadShareRepo:        cfg.ThreadShareRepo,
		ThreadReportRepo:       cfg.ThreadReportRepo,
		MessageRepo:            cfg.MessageRepo,
		RunEventRepo:           cfg.RunEventRepo,
		ShellSessionRepo:       cfg.ShellSessionRepo,
		ProjectRepo:            cfg.ProjectRepo,
		TeamRepo:               cfg.TeamRepo,
		AuditWriter:            cfg.AuditWriter,
		Pool:                   cfg.Pool, // desktop: SQLite via sqlitepgx
		DirectPool:             nil,      // desktop: no LISTEN/NOTIFY
		APIKeysRepo:            cfg.APIKeysRepo,
		RunLimiter:             cfg.RunLimiter,
		EntitlementService:     cfg.EntitlementService,
		RedisClient:            nil, // desktop: no Redis
		ConfigResolver:         resolver,
		SSEConfig:              conversationapi.SSEConfig(sseConfig),
		EventBus:               bus,
		MessageAttachmentStore: cfg.MessageAttachmentStore,
		ArtifactStore:          cfg.ArtifactStore,
	})

	catalogapi.RegisterRoutes(mux, catalogapi.Deps{
		AuthService:                  cfg.AuthService,
		AccountMembershipRepo:        cfg.AccountMembershipRepo,
		LlmCredentialsRepo:           cfg.LlmCredentialsRepo,
		LlmRoutesRepo:                cfg.LlmRoutesRepo,
		SecretsRepo:                  cfg.SecretsRepo,
		Pool:                         cfg.Pool,
		DirectPool:                   nil,
		AsrCredentialsRepo:           cfg.AsrCredentialsRepo,
		MCPConfigsRepo:               cfg.MCPConfigsRepo,
		ProfileMCPInstallsRepo:       cfg.ProfileMCPInstallsRepo,
		WorkspaceMCPEnableRepo:       cfg.WorkspaceMCPEnableRepo,
		ToolProviderConfigsRepo:      cfg.ToolProviderConfigsRepo,
		ToolDescriptionOverridesRepo: cfg.ToolDescriptionOverridesRepo,
		PersonasRepo:                 cfg.PersonasRepo,
		SkillPackagesRepo:            cfg.SkillPackagesRepo,
		ProfileSkillInstallsRepo:     cfg.ProfileSkillInstallsRepo,
		WorkspaceSkillEnableRepo:     cfg.WorkspaceSkillEnableRepo,
		PlatformSkillOverridesRepo:   cfg.PlatformSkillOverridesRepo,
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
		AuthService:              cfg.AuthService,
		AccountMembershipRepo:    cfg.AccountMembershipRepo,
		ThreadRepo:               cfg.ThreadRepo,
		TeamRepo:                 cfg.TeamRepo,
		ProjectRepo:              cfg.ProjectRepo,
		APIKeysRepo:              cfg.APIKeysRepo,
		AuditWriter:              cfg.AuditWriter,
		EntitlementService:       cfg.EntitlementService,
		Pool:                     cfg.Pool,
		AccountRepo:              cfg.AccountRepo,
		AccountService:           cfg.AccountService,
		WebhookRepo:              cfg.WebhookRepo,
		SecretsRepo:              cfg.SecretsRepo,
		LlmCredentialsRepo:       cfg.LlmCredentialsRepo,
		LlmRoutesRepo:            cfg.LlmRoutesRepo,
		ChannelsRepo:             cfg.ChannelsRepo,
		ChannelIdentitiesRepo:    cfg.ChannelIdentitiesRepo,
		ChannelIdentityLinksRepo: cfg.ChannelIdentityLinksRepo,
		ChannelBindCodesRepo:     cfg.ChannelBindCodesRepo,
		ChannelDMThreadsRepo:     cfg.ChannelDMThreadsRepo,
		ChannelGroupThreadsRepo:  cfg.ChannelGroupThreadsRepo,
		ChannelReceiptsRepo:      cfg.ChannelReceiptsRepo,
		UsersRepo:                cfg.UsersRepo,
		MessageRepo:              cfg.MessageRepo,
		JobRepo:                  cfg.JobRepo,
		CreditsRepo:              cfg.CreditsRepo,
		PersonasRepo:             cfg.PersonasRepo,
		TelegramBotClient:        telegramClient,
		DiscordBotClient:         discordClient,
		TelegramMode:             "polling",
		AppBaseURL:               cfg.AppBaseURL,
		EnvironmentStore:         cfg.EnvironmentStore,
		RunEventRepo:             cfg.RunEventRepo,
		GatewayRedisClient:       nil,
		EntitlementsRepo:         cfg.EntitlementsRepo,
		ConfigResolver:           resolver,
		MessageAttachmentStore:   cfg.MessageAttachmentStore,
	})

	platformapi.RegisterRoutes(mux, platformapi.Deps{
		AuthService:           cfg.AuthService,
		AccountMembershipRepo: cfg.AccountMembershipRepo,
		FeatureFlagsRepo:      cfg.FeatureFlagsRepo,
		FeatureFlagService:    cfg.FeatureFlagService,
		APIKeysRepo:           cfg.APIKeysRepo,
		AuditWriter:           cfg.AuditWriter,
		IPRulesRepo:           cfg.IPRulesRepo,
		GatewayRedisClient:    nil,
		NotificationsRepo:     cfg.NotificationsRepo,
		AuditLogRepo:          cfg.AuditLogRepo,
		PlatformSettingsRepo:  cfg.PlatformSettingsRepo,
		RedisClient:           nil,
		ConfigInvalidator:     invalidator,
		ConfigRegistry:        registry,
	})

	adminapi.RegisterRoutes(mux, adminapi.Deps{
		AuthService:           cfg.AuthService,
		AccountMembershipRepo: cfg.AccountMembershipRepo,
		UsersRepo:             cfg.UsersRepo,
		RunEventRepo:          cfg.RunEventRepo,
		RunPipelineEventsRepo: cfg.RunPipelineEventsRepo,
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
		GatewayRedisClient:    nil,
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

	memoryapi.RegisterRoutes(mux, memoryapi.Deps{
		Pool:                     cfg.Pool,
		MemoryProvider:           os.Getenv("ARKLOOP_MEMORY_PROVIDER"),
		OpenVikingBaseURL:        os.Getenv("ARKLOOP_OPENVIKING_BASE_URL"),
		OpenVikingAPIKey:         os.Getenv("ARKLOOP_OPENVIKING_ROOT_API_KEY"),
		NowledgeBaseURL:          os.Getenv("ARKLOOP_NOWLEDGE_BASE_URL"),
		NowledgeAPIKey:           os.Getenv("ARKLOOP_NOWLEDGE_API_KEY"),
		NowledgeRequestTimeoutMs: atoiDefault(os.Getenv("ARKLOOP_NOWLEDGE_REQUEST_TIMEOUT_MS"), 30000),
	})

	// NapCat (QQ channel) lifecycle API -- desktop only
	napCatBaseDir, napCatErr := desktop.ResolveDataDir("")
	if napCatErr != nil {
		cfg.Logger.Warn("napcat: failed to resolve data dir", "err", napCatErr)
	}
	napCatAPIPort := 19001
	if parts := strings.SplitN(strings.TrimSpace(os.Getenv("ARKLOOP_GO_ADDR")), ":", 2); len(parts) == 2 {
		if p, err := strconv.Atoi(parts[1]); err == nil && p > 0 {
			napCatAPIPort = p
		}
	}
	accountapi.RegisterNapCatRoutes(mux, accountapi.NapCatDeps{
		AuthService: cfg.AuthService,
		DataDir:     filepath.Join(napCatBaseDir, "napcat"),
		APIPort:     napCatAPIPort,
	})

	// QQ OneBot11 HTTP callback (NapCat -> Arkloop)
	accountapi.RegisterQQCallbackRoute(mux, accountapi.QQCallbackDeps{
		ChannelsRepo:             cfg.ChannelsRepo,
		ChannelIdentitiesRepo:    cfg.ChannelIdentitiesRepo,
		ChannelBindCodesRepo:     cfg.ChannelBindCodesRepo,
		ChannelIdentityLinksRepo: cfg.ChannelIdentityLinksRepo,
		ChannelDMThreadsRepo:     cfg.ChannelDMThreadsRepo,
		ChannelGroupThreadsRepo:  cfg.ChannelGroupThreadsRepo,
		ChannelReceiptsRepo:      cfg.ChannelReceiptsRepo,
		PersonasRepo:             cfg.PersonasRepo,
		ThreadRepo:               cfg.ThreadRepo,
		MessageRepo:              cfg.MessageRepo,
		RunEventRepo:             cfg.RunEventRepo,
		JobRepo:                  cfg.JobRepo,
		Pool:                     cfg.Pool,
		AttachmentStore:          cfg.MessageAttachmentStore,
	})

	if cfg.ScheduledJobsRepo != nil {
		scheduledjobsapi.RegisterRoutes(mux, scheduledjobsapi.Deps{
			AuthService:           cfg.AuthService,
			AccountMembershipRepo: cfg.AccountMembershipRepo,
			APIKeysRepo:           cfg.APIKeysRepo,
			ScheduledJobsRepo:     cfg.ScheduledJobsRepo,
			ThreadRepo:            cfg.ThreadRepo,
			Pool:                  cfg.Pool,
		})
	}

	notFound := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		slog.Warn("http.not_found", "method", r.Method, "path", r.URL.Path, "trace_id", traceID)
		WriteError(w, nethttp.StatusNotFound, "http.not_found", fmt.Sprintf("Not Found: %s %s", r.Method, r.URL.Path), traceID, nil)
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
	handler = desktopCORSMiddleware(handler)
	handler = TraceMiddleware(handler, cfg.Logger, cfg.TrustIncomingTraceID, cfg.TrustXForwardedFor)
	return handler
}
