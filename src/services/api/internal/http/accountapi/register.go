package accountapi

import (
	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"

	"github.com/redis/go-redis/v9"
)

type Deps struct {
	AuthService        *auth.Service
	AccountMembershipRepo *data.AccountMembershipRepository
	TeamRepo           *data.TeamRepository
	ProjectRepo        *data.ProjectRepository
	APIKeysRepo        *data.APIKeysRepository
	AuditWriter        *audit.Writer
	EntitlementService *entitlement.Service
	Pool               data.DB
	AccountRepo        *data.AccountRepository
	AccountService     *auth.AccountService
	WebhookRepo        *data.WebhookEndpointRepository
	SecretsRepo        *data.SecretsRepository
	EnvironmentStore   environmentStore
	RunEventRepo       *data.RunEventRepository
	GatewayRedisClient *redis.Client
}

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("/v1/api-keys", apiKeysEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.GatewayRedisClient))
	mux.HandleFunc("/v1/api-keys/", apiKeyEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.GatewayRedisClient))
	mux.HandleFunc("/v1/teams", teamsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.TeamRepo, deps.APIKeysRepo, deps.EntitlementService, deps.Pool))
	mux.HandleFunc("/v1/teams/", teamEntry(deps.AuthService, deps.AccountMembershipRepo, deps.TeamRepo, deps.APIKeysRepo, deps.EntitlementService, deps.Pool))
	mux.HandleFunc("/v1/projects", projectsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ProjectRepo, deps.TeamRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/projects/", projectEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ProjectRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/accounts", accountsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.AccountRepo, deps.AccountService, deps.APIKeysRepo))
	mux.HandleFunc("/v1/accounts/me", accountsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.AccountRepo, deps.AccountService, deps.APIKeysRepo))
	mux.HandleFunc("GET /v1/workspace-files", workspaceFilesEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.RunEventRepo, deps.AuditWriter, deps.Pool, deps.EnvironmentStore))
	mux.HandleFunc("/v1/webhook-endpoints", webhookEndpointsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.WebhookRepo, deps.APIKeysRepo, deps.SecretsRepo, deps.Pool))
	mux.HandleFunc("/v1/webhook-endpoints/", webhookEndpointEntry(deps.AuthService, deps.AccountMembershipRepo, deps.WebhookRepo, deps.APIKeysRepo))
}
