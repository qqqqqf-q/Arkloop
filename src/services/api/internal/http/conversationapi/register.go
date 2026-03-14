package conversationapi

import (
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/featureflag"
	sharedconfig "arkloop/services/shared/config"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Deps struct {
	AuthService              *auth.Service
	AccountMembershipRepo        *data.AccountMembershipRepository
	ThreadRepo               *data.ThreadRepository
	ThreadStarRepo           *data.ThreadStarRepository
	ThreadShareRepo          *data.ThreadShareRepository
	ThreadReportRepo         *data.ThreadReportRepository
	MessageRepo              *data.MessageRepository
	RunEventRepo             *data.RunEventRepository
	ShellSessionRepo         *data.ShellSessionRepository
	ProjectRepo              *data.ProjectRepository
	TeamRepo                 *data.TeamRepository
	AuditWriter              *audit.Writer
	Pool                     data.DB
	DirectPool               *pgxpool.Pool
	DirectPoolAcquireTimeout time.Duration
	APIKeysRepo              *data.APIKeysRepository
	RunLimiter               *data.RunLimiter
	EntitlementService       *entitlement.Service
	RedisClient              *redis.Client
	ConfigResolver           sharedconfig.Resolver
	SSEConfig                SSEConfig
	MessageAttachmentStore   messageAttachmentStore
	ArtifactStore            artifactStore
	FlagService              *featureflag.Service
}

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("/v1/me/feedback", meFeedback(deps.AuthService, deps.AccountMembershipRepo, deps.ThreadReportRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/threads", threadsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ThreadRepo, deps.ProjectRepo, deps.Pool, deps.APIKeysRepo, deps.AuditWriter))
	mux.HandleFunc("/v1/threads/search", searchThreads(deps.AuthService, deps.AccountMembershipRepo, deps.ThreadRepo, deps.APIKeysRepo, deps.AuditWriter))
	mux.HandleFunc("/v1/threads/starred", listStarredThreads(deps.AuthService, deps.AccountMembershipRepo, deps.ThreadStarRepo, deps.APIKeysRepo, deps.AuditWriter))
	mux.HandleFunc(
		"/v1/threads/",
		threadEntry(
			deps.AuthService,
			deps.AccountMembershipRepo,
			deps.ThreadRepo,
			deps.ThreadStarRepo,
			deps.ThreadShareRepo,
			deps.ThreadReportRepo,
			deps.MessageRepo,
			deps.RunEventRepo,
			deps.ProjectRepo,
			deps.TeamRepo,
			deps.AuditWriter,
			deps.Pool,
			deps.APIKeysRepo,
			deps.RunLimiter,
			deps.EntitlementService,
			deps.RedisClient,
			deps.MessageAttachmentStore,
			deps.FlagService,
		),
	)
	mux.HandleFunc("/v1/s/", publicShareEntry(deps.ThreadShareRepo, deps.ThreadRepo, deps.MessageRepo, deps.FlagService))
	mux.HandleFunc("/v1/runs", listGlobalRuns(deps.AuthService, deps.AccountMembershipRepo, deps.RunEventRepo, deps.APIKeysRepo))
	mux.HandleFunc(
		"/v1/runs/",
		runEntry(
			deps.AuthService,
			deps.AccountMembershipRepo,
			deps.RunEventRepo,
			deps.AuditWriter,
			deps.Pool,
			deps.DirectPool,
			deps.DirectPoolAcquireTimeout,
			deps.SSEConfig,
			deps.APIKeysRepo,
			deps.ConfigResolver,
			deps.RedisClient,
		),
	)
	mux.HandleFunc(
		"/v1/artifacts/",
		artifactsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.RunEventRepo, deps.ThreadRepo, deps.ShellSessionRepo, deps.ThreadShareRepo, deps.AuditWriter, deps.ArtifactStore, deps.FlagService),
	)
	mux.HandleFunc(
		"/v1/attachments/",
		messageAttachmentsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ThreadRepo, deps.ThreadShareRepo, deps.ProjectRepo, deps.TeamRepo, deps.APIKeysRepo, deps.AuditWriter, deps.MessageAttachmentStore, deps.FlagService),
	)
}
