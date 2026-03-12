package orgapi

import (
	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/featureflag"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Deps struct {
	AuthService        *auth.Service
	OrgMembershipRepo  *data.OrgMembershipRepository
	TeamRepo           *data.TeamRepository
	ProjectRepo        *data.ProjectRepository
	APIKeysRepo        *data.APIKeysRepository
	AuditWriter        *audit.Writer
	EntitlementService *entitlement.Service
	Pool               *pgxpool.Pool
	OrgRepo            *data.OrgRepository
	OrgService         *auth.OrgService
	OrgInvitationsRepo *data.OrgInvitationsRepository
	WebhookRepo        *data.WebhookEndpointRepository
	SecretsRepo        *data.SecretsRepository
	EnvironmentStore   environmentStore
	RunEventRepo       *data.RunEventRepository
	GatewayRedisClient *redis.Client
	ThreadRepo         *data.ThreadRepository
	FeatureFlagService *featureflag.Service
}

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("/v1/api-keys", apiKeysEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.GatewayRedisClient))
	mux.HandleFunc("/v1/api-keys/", apiKeyEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.GatewayRedisClient))
	mux.HandleFunc("/v1/teams", teamsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.TeamRepo, deps.APIKeysRepo, deps.EntitlementService, deps.Pool))
	mux.HandleFunc("/v1/teams/", teamEntry(deps.AuthService, deps.OrgMembershipRepo, deps.TeamRepo, deps.APIKeysRepo, deps.EntitlementService, deps.Pool))
	mux.HandleFunc("/v1/projects", projectsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.ProjectRepo, deps.TeamRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/projects/", projectEntry(deps.AuthService, deps.OrgMembershipRepo, deps.ProjectRepo, deps.APIKeysRepo, deps.AuditWriter, deps.Pool, deps.EnvironmentStore, deps.FeatureFlagService))
	mux.HandleFunc("/v1/orgs", orgsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.OrgRepo, deps.OrgService, deps.APIKeysRepo))
	mux.HandleFunc("/v1/orgs/me", orgsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.OrgRepo, deps.OrgService, deps.APIKeysRepo))
	mux.HandleFunc("/v1/orgs/", orgsInvitationsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.OrgInvitationsRepo, deps.AuditWriter, deps.OrgRepo))
	mux.HandleFunc("/v1/org-invitations/", orgInvitationEntry(deps.AuthService, deps.OrgMembershipRepo, deps.OrgInvitationsRepo, deps.AuditWriter, deps.Pool))
	mux.HandleFunc("GET /v1/workspace-files", workspaceFilesEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.RunEventRepo, deps.ThreadRepo, deps.AuditWriter, deps.Pool, deps.EnvironmentStore, deps.FeatureFlagService))
	mux.HandleFunc("/v1/webhook-endpoints", webhookEndpointsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.WebhookRepo, deps.APIKeysRepo, deps.SecretsRepo, deps.Pool))
	mux.HandleFunc("/v1/webhook-endpoints/", webhookEndpointEntry(deps.AuthService, deps.OrgMembershipRepo, deps.WebhookRepo, deps.APIKeysRepo))
}
