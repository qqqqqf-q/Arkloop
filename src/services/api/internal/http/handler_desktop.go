//go:build desktop

package http

import (
	"context"
	nethttp "net/http"

	"arkloop/services/api/internal/http/accountapi"
	"arkloop/services/api/internal/http/adminapi"
	"arkloop/services/api/internal/http/authapi"
	"arkloop/services/api/internal/http/billingapi"
	"arkloop/services/api/internal/http/catalogapi"
	"arkloop/services/api/internal/http/conversationapi"
	"arkloop/services/api/internal/http/memoryapi"
	"arkloop/services/api/internal/http/platformapi"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/featureflag"
	"arkloop/services/api/internal/observability"
	repopersonas "arkloop/services/api/internal/personas"
	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/desktop"
	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/objectstore"
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

type artifactStore interface {
	Head(ctx context.Context, key string) (objectstore.ObjectInfo, error)
	GetWithContentType(ctx context.Context, key string) ([]byte, string, error)
}

type messageAttachmentStore interface {
	Head(ctx context.Context, key string) (objectstore.ObjectInfo, error)
	GetWithContentType(ctx context.Context, key string) ([]byte, string, error)
	PutObject(ctx context.Context, key string, data []byte, options objectstore.PutOptions) error
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
	Logger               *observability.JSONLogger
	SchemaRepository     *data.SchemaRepository
	TrustIncomingTraceID bool
	TrustXForwardedFor   bool
	MaxInFlight          int

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

	RunLimiter *data.RunLimiter

	SSEConfig SSEConfig

	ConfigResolver    sharedconfig.Resolver
	ConfigInvalidator sharedconfig.Invalidator
	ConfigRegistry    *sharedconfig.Registry

	RepoPersonas       []repopersonas.RepoPersona
	PersonaSyncTrigger interface{ Trigger() }
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
		AuthService:           cfg.AuthService,
		AccountMembershipRepo: cfg.AccountMembershipRepo,
		TeamRepo:              cfg.TeamRepo,
		ProjectRepo:           cfg.ProjectRepo,
		APIKeysRepo:           cfg.APIKeysRepo,
		AuditWriter:           cfg.AuditWriter,
		EntitlementService:    cfg.EntitlementService,
		Pool:                  cfg.Pool,
		AccountRepo:           cfg.AccountRepo,
		AccountService:        cfg.AccountService,
		WebhookRepo:           cfg.WebhookRepo,
		SecretsRepo:           cfg.SecretsRepo,
		EnvironmentStore:      cfg.EnvironmentStore,
		RunEventRepo:          cfg.RunEventRepo,
		GatewayRedisClient:    nil,
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
		Pool: cfg.Pool,
	})

	notFound := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		WriteError(w, nethttp.StatusNotFound, "http.method_not_allowed", "Not Found", traceID, nil)
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
