package http

import (
	nethttp "net/http"

	"arkloop/services/api_go/internal/audit"
	"arkloop/services/api_go/internal/auth"
	"arkloop/services/api_go/internal/data"
	"arkloop/services/api_go/internal/observability"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SSEConfig 控制 SSE 流的轮询与心跳行为，对标 Python 端的 SseConfig。
type SSEConfig struct {
	PollSeconds      float64
	HeartbeatSeconds float64
	BatchLimit       int
}

func defaultSSEConfig() SSEConfig {
	return SSEConfig{
		PollSeconds:      0.25,
		HeartbeatSeconds: 15.0,
		BatchLimit:       500,
	}
}

type HandlerConfig struct {
	Pool                 *pgxpool.Pool
	Logger               *observability.JSONLogger
	SchemaRepository     *data.SchemaRepository
	TrustIncomingTraceID bool

	AuthService         *auth.Service
	RegistrationService *auth.RegistrationService
	OrgMembershipRepo   *data.OrgMembershipRepository
	ThreadRepo          *data.ThreadRepository
	MessageRepo         *data.MessageRepository
	RunEventRepo        *data.RunEventRepository
	AuditWriter         *audit.Writer

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
	mux.HandleFunc("/v1/threads", threadsEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.ThreadRepo))
	mux.HandleFunc(
		"/v1/threads/",
		threadEntry(
			cfg.AuthService,
			cfg.OrgMembershipRepo,
			cfg.ThreadRepo,
			cfg.MessageRepo,
			cfg.RunEventRepo,
			cfg.AuditWriter,
			cfg.Pool,
		),
	)
	sseConfig := cfg.SSEConfig
	if sseConfig.BatchLimit <= 0 {
		sseConfig = defaultSSEConfig()
	}

	mux.HandleFunc(
		"/v1/runs/",
		runEntry(cfg.AuthService, cfg.OrgMembershipRepo, cfg.RunEventRepo, cfg.AuditWriter, cfg.Pool, sseConfig),
	)

	notFound := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		WriteError(w, nethttp.StatusNotFound, "http_error", "Not Found", traceID, nil)
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
	handler = TraceMiddleware(handler, cfg.Logger, cfg.TrustIncomingTraceID)
	return handler
}
