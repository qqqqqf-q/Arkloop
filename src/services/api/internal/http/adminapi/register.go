package adminapi

import (
	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	sharedconfig "arkloop/services/shared/config"

)

type Deps struct {
	AuthService           *auth.Service
	AccountMembershipRepo *data.AccountMembershipRepository
	UsersRepo             *data.UserRepository
	RunEventRepo          *data.RunEventRepository
	RunPipelineEventsRepo *data.RunPipelineEventsRepository
	APIKeysRepo           *data.APIKeysRepository
	MessageRepo           *data.MessageRepository
	LlmCredentialsRepo    *data.LlmCredentialsRepository
	ThreadRepo            *data.ThreadRepository
	AuditWriter           *audit.Writer
	PlatformSettingsRepo  *data.PlatformSettingsRepository
	ConfigResolver        sharedconfig.Resolver
	ConfigInvalidator     sharedconfig.Invalidator
	JobRepo               *data.JobRepository
	SmtpProviderRepo      *data.SmtpProviderRepository
}

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("/v1/admin/runs/", adminRunsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.RunEventRepo, deps.RunPipelineEventsRepo, deps.UsersRepo, deps.APIKeysRepo, deps.MessageRepo, deps.LlmCredentialsRepo, deps.ThreadRepo))
	mux.HandleFunc("/v1/admin/email/config", adminEmailConfig(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.PlatformSettingsRepo, deps.ConfigResolver, deps.ConfigInvalidator))
	mux.HandleFunc("/v1/admin/email/test", adminEmailTest(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.JobRepo, deps.PlatformSettingsRepo, deps.ConfigResolver))
	mux.HandleFunc("/v1/admin/smtp-providers", adminSmtpProviders(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.SmtpProviderRepo))
	mux.HandleFunc("/v1/admin/smtp-providers/", adminSmtpProviderEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.SmtpProviderRepo, deps.JobRepo))
}
