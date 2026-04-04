package accountapi

import (
	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/discordbot"
	"arkloop/services/shared/telegrambot"

	"github.com/redis/go-redis/v9"
)

type Deps struct {
	AuthService             *auth.Service
	AccountMembershipRepo   *data.AccountMembershipRepository
	ThreadRepo              *data.ThreadRepository
	TeamRepo                *data.TeamRepository
	ProjectRepo             *data.ProjectRepository
	APIKeysRepo             *data.APIKeysRepository
	AuditWriter             *audit.Writer
	EntitlementService      *entitlement.Service
	Pool                    data.DB
	AccountRepo             *data.AccountRepository
	AccountService          *auth.AccountService
	WebhookRepo             *data.WebhookEndpointRepository
	SecretsRepo             *data.SecretsRepository
	LlmCredentialsRepo      *data.LlmCredentialsRepository
	LlmRoutesRepo           *data.LlmRoutesRepository
	ChannelsRepo             *data.ChannelsRepository
	ChannelIdentitiesRepo    *data.ChannelIdentitiesRepository
	ChannelIdentityLinksRepo *data.ChannelIdentityLinksRepository
	ChannelBindCodesRepo    *data.ChannelBindCodesRepository
	ChannelDMThreadsRepo    *data.ChannelDMThreadsRepository
	ChannelGroupThreadsRepo *data.ChannelGroupThreadsRepository
	ChannelReceiptsRepo     *data.ChannelMessageReceiptsRepository
	UsersRepo               *data.UserRepository
	MessageRepo             *data.MessageRepository
	JobRepo                 *data.JobRepository
	CreditsRepo             *data.CreditsRepository
	PersonasRepo            *data.PersonasRepository
	TelegramBotClient       *telegrambot.Client
	DiscordBotClient        *discordbot.Client
	TelegramMode            string
	AppBaseURL              string
	EnvironmentStore        environmentStore
	RunEventRepo            *data.RunEventRepository
	GatewayRedisClient      *redis.Client
	EntitlementsRepo        *data.EntitlementsRepository
	ConfigResolver          sharedconfig.Resolver
	MessageAttachmentStore  MessageAttachmentPutStore
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
	mux.HandleFunc("/v1/accounts/me/spawn-profiles", spawnProfilesEntry(deps.AuthService, deps.AccountMembershipRepo, deps.EntitlementsRepo, deps.EntitlementService, deps.APIKeysRepo, deps.ConfigResolver))
	mux.HandleFunc("/v1/accounts/me/spawn-profiles/", spawnProfileEntry(deps.AuthService, deps.AccountMembershipRepo, deps.EntitlementsRepo, deps.EntitlementService, deps.APIKeysRepo, deps.ConfigResolver))
	mux.HandleFunc("/v1/account/openviking/resolve", openVikingResolveEntry(
		deps.AuthService,
		deps.AccountMembershipRepo,
		deps.APIKeysRepo,
		deps.LlmCredentialsRepo,
		deps.LlmRoutesRepo,
		deps.SecretsRepo,
		deps.ProjectRepo,
		deps.Pool,
	))
	mux.HandleFunc("GET /v1/account/memory/errors", memoryErrorsEntry(
		deps.AuthService,
		deps.AccountMembershipRepo,
		deps.APIKeysRepo,
		deps.Pool,
	))
	if telegramModeUsesWebhook(deps.TelegramMode) {
		mux.HandleFunc("/v1/channels/telegram/", telegramWebhookEntry(
			deps.ChannelsRepo,
			deps.ChannelIdentitiesRepo,
			deps.ChannelIdentityLinksRepo,
			deps.ChannelBindCodesRepo,
			deps.ChannelDMThreadsRepo,
			deps.ChannelGroupThreadsRepo,
			deps.ChannelReceiptsRepo,
			deps.SecretsRepo,
			deps.PersonasRepo,
			deps.UsersRepo,
			deps.AccountRepo,
			deps.AccountMembershipRepo,
			deps.ProjectRepo,
			deps.ThreadRepo,
			deps.MessageRepo,
			deps.RunEventRepo,
			deps.JobRepo,
			deps.CreditsRepo,
			deps.Pool,
			deps.EntitlementService,
			deps.TelegramBotClient,
			deps.MessageAttachmentStore,
		))
	}
	mux.HandleFunc("/v1/channels", channelsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ChannelsRepo, deps.PersonasRepo, deps.EntitlementService, deps.APIKeysRepo, deps.SecretsRepo, deps.Pool, deps.AppBaseURL, deps.TelegramBotClient, deps.DiscordBotClient, deps.TelegramMode))
	mux.HandleFunc("/v1/channels/", channelEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ChannelsRepo, deps.ChannelIdentityLinksRepo, deps.ChannelIdentitiesRepo, deps.ChannelDMThreadsRepo, deps.PersonasRepo, deps.EntitlementService, deps.APIKeysRepo, deps.SecretsRepo, deps.Pool, deps.TelegramBotClient, deps.DiscordBotClient, deps.TelegramMode))
	mux.HandleFunc("/v1/me/channel-binds", channelBindsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ChannelBindCodesRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/me/channel-identities", channelIdentitiesEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ChannelIdentitiesRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/me/channel-identities/", channelIdentityEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ChannelIdentitiesRepo, deps.ChannelIdentityLinksRepo, deps.APIKeysRepo, deps.Pool))
}
