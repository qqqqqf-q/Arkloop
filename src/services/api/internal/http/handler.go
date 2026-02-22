package http

import (
	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
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
	Pool                 *pgxpool.Pool
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

	LlmCredentialsRepo *data.LlmCredentialsRepository
	LlmRoutesRepo      *data.LlmRoutesRepository
	SecretsRepo        *data.SecretsRepository
	MCPConfigsRepo     *data.MCPConfigsRepository
	SkillsRepo         *data.SkillsRepository
	IPRulesRepo        *data.IPRulesRepository
	APIKeysRepo        *data.APIKeysRepository
	OrgInvitationsRepo *data.OrgInvitationsRepository
	TeamRepo           *data.TeamRepository
	ProjectRepo        *data.ProjectRepository

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
	mux.HandleFunc("/v1/auth/register", register(cfg.RegistrationService, cfg.AuditWriter))
	mux.HandleFunc("/v1/me", me(cfg.AuthService))
	mux.HandleFunc("/v1/threads", threadsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.ThreadRepo, cfg.APIKeysRepo, cfg.AuditWriter))
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
			cfg.AuditWriter,
			cfg.Pool,
			cfg.APIKeysRepo,
			cfg.RunLimiter,
		),
	)
	sseConfig := cfg.SSEConfig
	if sseConfig.BatchLimit <= 0 {
		sseConfig = defaultSSEConfig()
	}

	mux.HandleFunc(
		"/v1/runs/",
		runEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.RunEventRepo, cfg.AuditWriter, cfg.Pool, sseConfig, cfg.APIKeysRepo),
	)

	mux.HandleFunc(
		"/v1/llm-credentials",
		llmCredentialsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.LlmCredentialsRepo, cfg.LlmRoutesRepo, cfg.SecretsRepo, cfg.Pool),
	)
	mux.HandleFunc(
		"/v1/llm-credentials/",
		llmCredentialEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.LlmCredentialsRepo),
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
		teamsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.TeamRepo, cfg.APIKeysRepo),
	)
	mux.HandleFunc(
		"/v1/teams/",
		teamEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.TeamRepo, cfg.APIKeysRepo),
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
