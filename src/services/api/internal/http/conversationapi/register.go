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
	OrgMembershipRepo        *data.OrgMembershipRepository
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
	Pool                     *pgxpool.Pool
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
	FeatureFlagService       *featureflag.Service
}

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("/v1/me/feedback", meFeedback(deps.AuthService, deps.OrgMembershipRepo, deps.ThreadReportRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/threads", threadsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.ThreadRepo, deps.ProjectRepo, deps.Pool, deps.APIKeysRepo, deps.AuditWriter, deps.FeatureFlagService))
	mux.HandleFunc("/v1/threads/search", searchThreads(deps.AuthService, deps.OrgMembershipRepo, deps.ThreadRepo, deps.APIKeysRepo, deps.AuditWriter, deps.FeatureFlagService))
	mux.HandleFunc("/v1/threads/starred", listStarredThreads(deps.AuthService, deps.OrgMembershipRepo, deps.ThreadStarRepo, deps.APIKeysRepo, deps.AuditWriter))
	mux.HandleFunc(
		"/v1/threads/",
		threadEntry(
			deps.AuthService,
			deps.OrgMembershipRepo,
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
			deps.FeatureFlagService,
		),
	)
	mux.HandleFunc("/v1/s/", publicShareEntry(deps.ThreadShareRepo, deps.ThreadRepo, deps.MessageRepo, deps.FeatureFlagService))
	mux.HandleFunc("/v1/runs", listGlobalRuns(deps.AuthService, deps.OrgMembershipRepo, deps.RunEventRepo, deps.ThreadRepo, deps.APIKeysRepo, deps.FeatureFlagService))
	mux.HandleFunc(
		"/v1/runs/",
		runEntry(
			deps.AuthService,
			deps.OrgMembershipRepo,
			deps.RunEventRepo,
			deps.ThreadRepo,
			deps.AuditWriter,
			deps.Pool,
			deps.DirectPool,
			deps.DirectPoolAcquireTimeout,
			deps.SSEConfig,
			deps.APIKeysRepo,
			deps.ConfigResolver,
			deps.RedisClient,
			deps.FeatureFlagService,
		),
	)
	mux.HandleFunc(
		"/v1/artifacts/",
		artifactsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.RunEventRepo, deps.ThreadRepo, deps.ShellSessionRepo, deps.ThreadShareRepo, deps.AuditWriter, deps.ArtifactStore, deps.FeatureFlagService),
	)
	mux.HandleFunc(
		"/v1/attachments/",
		messageAttachmentsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.ThreadRepo, deps.ThreadShareRepo, deps.ProjectRepo, deps.TeamRepo, deps.APIKeysRepo, deps.AuditWriter, deps.MessageAttachmentStore, deps.FeatureFlagService),
	)
}
