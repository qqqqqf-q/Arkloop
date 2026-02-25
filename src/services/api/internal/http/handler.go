package http

import (
	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/featureflag"
	"arkloop/services/api/internal/observability"

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
	Pool       *pgxpool.Pool
	DirectPool *pgxpool.Pool // LISTEN/NOTIFY 专用，不走 PgBouncer
	Logger               *observability.JSONLogger
	SchemaRepository     *data.SchemaRepository
	TrustIncomingTraceID bool
	TrustXForwardedFor   bool

	AuthService         *auth.Service
	RegistrationService *auth.RegistrationService
	OrgMembershipRepo   *data.OrgMembershipRepository
	ThreadRepo          *data.ThreadRepository
	MessageRepo         *data.MessageRepository
	RunEventRepo        *data.RunEventRepository
	AuditWriter         *audit.Writer

	LlmCredentialsRepo  *data.LlmCredentialsRepository
	LlmRoutesRepo       *data.LlmRoutesRepository
	SecretsRepo         *data.SecretsRepository
	AsrCredentialsRepo  *data.AsrCredentialsRepository
	MCPConfigsRepo      *data.MCPConfigsRepository
	SkillsRepo          *data.SkillsRepository
	IPRulesRepo         *data.IPRulesRepository
	APIKeysRepo         *data.APIKeysRepository
	OrgInvitationsRepo  *data.OrgInvitationsRepository
	TeamRepo            *data.TeamRepository
	ProjectRepo         *data.ProjectRepository
	WebhookRepo         *data.WebhookEndpointRepository
	PromptTemplatesRepo *data.PromptTemplateRepository
	AgentConfigsRepo    *data.AgentConfigRepository
	PlansRepo           *data.PlanRepository
	SubscriptionsRepo   *data.SubscriptionRepository
	EntitlementsRepo    *data.EntitlementsRepository
	EntitlementService  *entitlement.Service
	UsageRepo           *data.UsageRepository

	FeatureFlagsRepo   *data.FeatureFlagRepository
	FeatureFlagService *featureflag.Service

	NotificationsRepo *data.NotificationsRepository
	AuditLogRepo      *data.AuditLogRepository

	InviteCodesRepo *data.InviteCodeRepository
	ReferralsRepo   *data.ReferralRepository

	CreditsRepo         *data.CreditsRepository
	RedemptionCodesRepo *data.RedemptionCodesRepository

	PlatformSettingsRepo *data.PlatformSettingsRepository

	UsersRepo *data.UserRepository
	OrgRepo   *data.OrgRepository

	UserCredentialRepo *data.UserCredentialRepository

	RedisClient *redis.Client
	RunLimiter  *data.RunLimiter

	SSEConfig SSEConfig
}

func NewHandler(cfg HandlerConfig) nethttp.Handler {
	mux := nethttp.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/readyz", readyz(cfg.SchemaRepository, cfg.Logger))

	mux.HandleFunc("/v1/auth/login", login(cfg.AuthService, cfg.AuditWriter))
	mux.HandleFunc("/v1/auth/refresh", refreshToken(cfg.AuthService, cfg.AuditWriter))
	mux.HandleFunc("/v1/auth/logout", logout(cfg.AuthService, cfg.AuditWriter))
	mux.HandleFunc("/v1/auth/register", register(cfg.RegistrationService, cfg.FeatureFlagService, cfg.AuditWriter))
	mux.HandleFunc("/v1/auth/registration-mode", registrationMode(cfg.FeatureFlagService))
	mux.HandleFunc("/v1/me", me(cfg.AuthService, cfg.OrgMembershipRepo, cfg.OrgRepo))
	mux.HandleFunc("/v1/me/usage", meUsage(cfg.AuthService, cfg.OrgMembershipRepo, cfg.UsageRepo, cfg.APIKeysRepo))
	mux.HandleFunc("/v1/me/usage/daily", meDailyUsage(cfg.AuthService, cfg.OrgMembershipRepo, cfg.UsageRepo, cfg.APIKeysRepo))
	mux.HandleFunc("/v1/me/usage/by-model", meUsageByModel(cfg.AuthService, cfg.OrgMembershipRepo, cfg.UsageRepo, cfg.APIKeysRepo))
	mux.HandleFunc("/v1/threads", threadsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.ThreadRepo, cfg.APIKeysRepo, cfg.AuditWriter))
	mux.HandleFunc("/v1/threads/search", searchThreads(cfg.AuthService, cfg.OrgMembershipRepo, cfg.ThreadRepo, cfg.APIKeysRepo, cfg.AuditWriter))
	mux.HandleFunc(
		"/v1/threads/",
		threadEntry(
			cfg.AuthService,
			cfg.OrgMembershipRepo,
			cfg.ThreadRepo,
			cfg.MessageRepo,
			cfg.RunEventRepo,
			cfg.ProjectRepo,
			cfg.TeamRepo,
			cfg.AgentConfigsRepo,
			cfg.AuditWriter,
			cfg.Pool,
			cfg.APIKeysRepo,
			cfg.RunLimiter,
			cfg.EntitlementService,
			cfg.RedisClient,
		),
	)
	sseConfig := cfg.SSEConfig
	if sseConfig.BatchLimit <= 0 {
		sseConfig = defaultSSEConfig()
	}

	mux.HandleFunc(
		"/v1/runs",
		listGlobalRuns(cfg.AuthService, cfg.OrgMembershipRepo, cfg.RunEventRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/runs/",
		runEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.RunEventRepo, cfg.AuditWriter, cfg.Pool, cfg.DirectPool, sseConfig, cfg.APIKeysRepo, cfg.RedisClient),
	)

	mux.HandleFunc(
		"/v1/llm-credentials",
		llmCredentialsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.LlmCredentialsRepo, cfg.LlmRoutesRepo, cfg.SecretsRepo, cfg.Pool),
	)
	mux.HandleFunc(
		"/v1/llm-credentials/",
		llmCredentialEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.LlmCredentialsRepo, cfg.LlmRoutesRepo, cfg.SecretsRepo, cfg.Pool),
	)

	mux.HandleFunc(
		"/v1/asr-credentials",
		asrCredentialsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.AsrCredentialsRepo, cfg.SecretsRepo, cfg.Pool),
	)
	mux.HandleFunc(
		"/v1/asr-credentials/",
		asrCredentialEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.AsrCredentialsRepo),
	)
	mux.HandleFunc(
		"/v1/asr/transcribe",
		asrTranscribeEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.AsrCredentialsRepo, cfg.SecretsRepo),
	)

	mux.HandleFunc(
		"/v1/mcp-configs",
		mcpConfigsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.MCPConfigsRepo, cfg.SecretsRepo, cfg.Pool),
	)
	mux.HandleFunc(
		"/v1/mcp-configs/",
		mcpConfigEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.MCPConfigsRepo, cfg.SecretsRepo, cfg.Pool),
	)

	mux.HandleFunc(
		"/v1/skills",
		skillsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.SkillsRepo),
	)
	mux.HandleFunc(
		"/v1/skills/",
		skillEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.SkillsRepo),
	)

	mux.HandleFunc(
		"/v1/ip-rules",
		ipRulesEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.IPRulesRepo, cfg.RedisClient),
	)
	mux.HandleFunc(
		"/v1/ip-rules/",
		ipRuleEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.IPRulesRepo, cfg.RedisClient),
	)

	mux.HandleFunc(
		"/v1/api-keys",
		apiKeysEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.APIKeysRepo, cfg.AuditWriter, cfg.RedisClient),
	)
	mux.HandleFunc(
		"/v1/api-keys/",
		apiKeyEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.APIKeysRepo, cfg.AuditWriter, cfg.RedisClient),
	)

	mux.HandleFunc(
		"/v1/teams",
		teamsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.TeamRepo, cfg.APIKeysRepo, cfg.EntitlementService, cfg.Pool),
	)
	mux.HandleFunc(
		"/v1/teams/",
		teamEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.TeamRepo, cfg.APIKeysRepo, cfg.EntitlementService, cfg.Pool),
	)

	mux.HandleFunc(
		"/v1/projects",
		projectsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.ProjectRepo, cfg.TeamRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/projects/",
		projectEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.ProjectRepo, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/orgs/",
		orgsInvitationsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.OrgInvitationsRepo, cfg.AuditWriter),
	)
	mux.HandleFunc(
		"/v1/org-invitations/",
		orgInvitationEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.OrgInvitationsRepo, cfg.AuditWriter, cfg.Pool),
	)

	mux.HandleFunc(
		"/v1/webhook-endpoints",
		webhookEndpointsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.WebhookRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/webhook-endpoints/",
		webhookEndpointEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.WebhookRepo, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/prompt-templates",
		promptTemplatesEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.PromptTemplatesRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/prompt-templates/",
		promptTemplateEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.PromptTemplatesRepo, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/agent-configs",
		agentConfigsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.AgentConfigsRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/agent-configs/",
		agentConfigEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.AgentConfigsRepo, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/plans",
		plansEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.PlansRepo, cfg.EntitlementsRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/plans/",
		planEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.PlansRepo, cfg.EntitlementsRepo, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/subscriptions",
		subscriptionsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.SubscriptionsRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/subscriptions/",
		subscriptionEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.SubscriptionsRepo, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/entitlement-overrides",
		entitlementOverridesEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.EntitlementsRepo, cfg.EntitlementService, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/entitlement-overrides/",
		entitlementOverrideEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.EntitlementsRepo, cfg.EntitlementService, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/feature-flags",
		featureFlagsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.FeatureFlagsRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/feature-flags/",
		featureFlagEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.FeatureFlagsRepo, cfg.FeatureFlagService, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/orgs/{id}/usage",
		orgUsageEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.UsageRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/orgs/{id}/usage/daily",
		orgDailyUsage(cfg.AuthService, cfg.OrgMembershipRepo, cfg.UsageRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/orgs/{id}/usage/by-model",
		orgUsageByModel(cfg.AuthService, cfg.OrgMembershipRepo, cfg.UsageRepo, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/notifications",
		notificationsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.NotificationsRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/notifications/",
		notificationEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.NotificationsRepo, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/audit-logs",
		auditLogsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.AuditLogRepo, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/admin/dashboard",
		adminDashboard(cfg.AuthService, cfg.OrgMembershipRepo, cfg.UsersRepo, cfg.RunEventRepo, cfg.UsageRepo, cfg.OrgRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/admin/usage/daily",
		adminGlobalDailyUsage(cfg.AuthService, cfg.OrgMembershipRepo, cfg.UsageRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/admin/usage/summary",
		adminGlobalUsageSummary(cfg.AuthService, cfg.OrgMembershipRepo, cfg.UsageRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/admin/usage/by-model",
		adminGlobalUsageByModel(cfg.AuthService, cfg.OrgMembershipRepo, cfg.UsageRepo, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/admin/users",
		adminUsersEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.UsersRepo, cfg.APIKeysRepo, cfg.UserCredentialRepo),
	)
	mux.HandleFunc(
		"/v1/admin/users/",
		adminUserEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.UsersRepo, cfg.APIKeysRepo, cfg.AuditWriter, cfg.InviteCodesRepo, cfg.UserCredentialRepo),
	)

	mux.HandleFunc(
		"/v1/me/invite-code/reset",
		meInviteCodeReset(cfg.AuthService, cfg.OrgMembershipRepo, cfg.InviteCodesRepo, cfg.EntitlementService, cfg.APIKeysRepo, cfg.AuditWriter),
	)
	mux.HandleFunc(
		"/v1/me/invite-code",
		meInviteCode(cfg.AuthService, cfg.OrgMembershipRepo, cfg.InviteCodesRepo, cfg.EntitlementService, cfg.APIKeysRepo, cfg.AuditWriter),
	)
	mux.HandleFunc(
		"/v1/admin/invite-codes",
		adminInviteCodesEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.InviteCodesRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/admin/invite-codes/",
		adminInviteCodeEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.InviteCodesRepo, cfg.APIKeysRepo, cfg.AuditWriter),
	)
	mux.HandleFunc(
		"/v1/admin/referrals/tree",
		adminReferralTree(cfg.AuthService, cfg.OrgMembershipRepo, cfg.ReferralsRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/admin/referrals",
		adminReferralsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.ReferralsRepo, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/me/credits",
		meCredits(cfg.AuthService, cfg.OrgMembershipRepo, cfg.CreditsRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/admin/credits/adjust",
		adminCreditsAdjust(cfg.AuthService, cfg.OrgMembershipRepo, cfg.CreditsRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/admin/credits/bulk-adjust",
		adminCreditsBulkAdjust(cfg.AuthService, cfg.OrgMembershipRepo, cfg.CreditsRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/admin/credits/reset-all",
		adminCreditsResetAll(cfg.AuthService, cfg.OrgMembershipRepo, cfg.CreditsRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/admin/credits",
		adminCreditsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.CreditsRepo, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/admin/redemption-codes/batch",
		adminRedemptionCodesBatch(cfg.AuthService, cfg.OrgMembershipRepo, cfg.RedemptionCodesRepo, cfg.APIKeysRepo, cfg.AuditWriter, cfg.Pool),
	)
	mux.HandleFunc(
		"/v1/admin/redemption-codes/",
		adminRedemptionCodeEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.RedemptionCodesRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/admin/redemption-codes",
		adminRedemptionCodesEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.RedemptionCodesRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/me/redeem",
		meRedeem(cfg.AuthService, cfg.OrgMembershipRepo, cfg.RedemptionCodesRepo, cfg.CreditsRepo, cfg.APIKeysRepo, cfg.AuditWriter, cfg.Pool),
	)

	mux.HandleFunc(
		"/v1/admin/notifications/broadcasts/",
		adminBroadcastEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.NotificationsRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/admin/notifications/broadcasts",
		adminBroadcastsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.NotificationsRepo, cfg.APIKeysRepo, cfg.AuditWriter, cfg.Pool, cfg.Logger),
	)

	mux.HandleFunc(
		"/v1/admin/platform-settings",
		platformSettingsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.PlatformSettingsRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/admin/platform-settings/",
		platformSettingEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.PlatformSettingsRepo, cfg.APIKeysRepo),
	)

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
	handler = TraceMiddleware(handler, cfg.Logger, cfg.TrustIncomingTraceID, cfg.TrustXForwardedFor)
	return handler
}
