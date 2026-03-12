package adminapi

import (
	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	repopersonas "arkloop/services/api/internal/personas"
	sharedconfig "arkloop/services/shared/config"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Deps struct {
	AuthService          *auth.Service
	AccountMembershipRepo    *data.AccountMembershipRepository
	UsersRepo            *data.UserRepository
	RunEventRepo         *data.RunEventRepository
	UsageRepo            *data.UsageRepository
	AccountRepo              *data.AccountRepository
	APIKeysRepo          *data.APIKeysRepository
	MessageRepo          *data.MessageRepository
	LlmCredentialsRepo   *data.LlmCredentialsRepository
	ThreadRepo           *data.ThreadRepository
	ThreadReportRepo     *data.ThreadReportRepository
	AuditWriter          *audit.Writer
	InviteCodesRepo      *data.InviteCodeRepository
	ReferralsRepo        *data.ReferralRepository
	CreditsRepo          *data.CreditsRepository
	RedemptionCodesRepo  *data.RedemptionCodesRepository
	NotificationsRepo    *data.NotificationsRepository
	Pool                 *pgxpool.Pool
	Logger               *observability.JSONLogger
	GatewayRedisClient   *redis.Client
	PlatformSettingsRepo *data.PlatformSettingsRepository
	ConfigResolver       sharedconfig.Resolver
	ConfigInvalidator    sharedconfig.Invalidator
	ConfigRegistry       *sharedconfig.Registry
	PersonasRepo         *data.PersonasRepository
	RepoPersonas         []repopersonas.RepoPersona
	JobRepo              *data.JobRepository
	SmtpProviderRepo     *data.SmtpProviderRepository
	UserCredentialRepo   *data.UserCredentialRepository
}

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("/v1/admin/dashboard", adminDashboard(deps.AuthService, deps.AccountMembershipRepo, deps.UsersRepo, deps.RunEventRepo, deps.UsageRepo, deps.AccountRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/runs/", adminRunsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.RunEventRepo, deps.UsersRepo, deps.APIKeysRepo, deps.MessageRepo, deps.LlmCredentialsRepo, deps.ThreadRepo))
	mux.HandleFunc("/v1/admin/reports", adminReportsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ThreadReportRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/users", adminUsersEntry(deps.AuthService, deps.AccountMembershipRepo, deps.UsersRepo, deps.APIKeysRepo, deps.UserCredentialRepo))
	mux.HandleFunc("/v1/admin/users/", adminUserEntry(deps.AuthService, deps.AccountMembershipRepo, deps.UsersRepo, deps.APIKeysRepo, deps.AuditWriter, deps.InviteCodesRepo, deps.UserCredentialRepo))
	mux.HandleFunc("/v1/admin/notifications/broadcasts/", adminBroadcastEntry(deps.AuthService, deps.AccountMembershipRepo, deps.NotificationsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/notifications/broadcasts", adminBroadcastsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.NotificationsRepo, deps.APIKeysRepo, deps.AuditWriter, deps.Pool, deps.Logger))
	mux.HandleFunc("/v1/admin/gateway-config", adminGatewayConfigEntry(deps.AuthService, deps.AccountMembershipRepo, deps.PlatformSettingsRepo, deps.APIKeysRepo, deps.GatewayRedisClient, deps.ConfigResolver, deps.ConfigInvalidator))
	mux.HandleFunc("/v1/admin/access-log", adminAccessLogEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.UsersRepo, deps.GatewayRedisClient))
	mux.HandleFunc("/v1/admin/execution-governance", adminExecutionGovernance(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.PersonasRepo, deps.RepoPersonas, deps.ConfigRegistry, deps.Pool))
	mux.HandleFunc("/v1/admin/email/status", adminEmailStatus(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.ConfigResolver))
	mux.HandleFunc("/v1/admin/email/config", adminEmailConfig(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.PlatformSettingsRepo, deps.ConfigResolver, deps.ConfigInvalidator))
	mux.HandleFunc("/v1/admin/email/test", adminEmailTest(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.JobRepo, deps.PlatformSettingsRepo, deps.ConfigResolver))
	mux.HandleFunc("/v1/admin/smtp-providers", adminSmtpProviders(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.SmtpProviderRepo))
	mux.HandleFunc("/v1/admin/smtp-providers/", adminSmtpProviderEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.SmtpProviderRepo, deps.JobRepo))
}
