package platformapi

import (
	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/featureflag"
	sharedconfig "arkloop/services/shared/config"

	"github.com/redis/go-redis/v9"
)

type Deps struct {
	AuthService          *auth.Service
	AccountMembershipRepo    *data.AccountMembershipRepository
	FeatureFlagsRepo     *data.FeatureFlagRepository
	FeatureFlagService   *featureflag.Service
	APIKeysRepo          *data.APIKeysRepository
	AuditWriter          *audit.Writer
	IPRulesRepo          *data.IPRulesRepository
	GatewayRedisClient   *redis.Client
	NotificationsRepo    *data.NotificationsRepository
	AuditLogRepo         *data.AuditLogRepository
	PlatformSettingsRepo *data.PlatformSettingsRepository
	RedisClient          *redis.Client
	ConfigInvalidator    sharedconfig.Invalidator
	ConfigRegistry       *sharedconfig.Registry
}

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("/v1/ip-rules", ipRulesEntry(deps.AuthService, deps.AccountMembershipRepo, deps.IPRulesRepo, deps.GatewayRedisClient))
	mux.HandleFunc("/v1/ip-rules/", ipRuleEntry(deps.AuthService, deps.AccountMembershipRepo, deps.IPRulesRepo, deps.GatewayRedisClient))
	mux.HandleFunc("/v1/feature-flags", featureFlagsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.FeatureFlagsRepo, deps.APIKeysRepo, deps.AuditWriter))
	mux.HandleFunc("/v1/feature-flags/", featureFlagEntry(deps.AuthService, deps.AccountMembershipRepo, deps.FeatureFlagsRepo, deps.FeatureFlagService, deps.APIKeysRepo, deps.AuditWriter))
	mux.HandleFunc("/v1/notifications", notificationsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.NotificationsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/notifications/", notificationEntry(deps.AuthService, deps.AccountMembershipRepo, deps.NotificationsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/audit-logs", auditLogsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.AuditLogRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/platform-settings", platformSettingsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.PlatformSettingsRepo, deps.APIKeysRepo, deps.ConfigRegistry))
	mux.HandleFunc("/v1/admin/platform-settings/", platformSettingEntry(deps.AuthService, deps.AccountMembershipRepo, deps.PlatformSettingsRepo, deps.APIKeysRepo, deps.RedisClient, deps.ConfigInvalidator, deps.ConfigRegistry))
	mux.HandleFunc("GET /v1/config/schema", configSchemaEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.ConfigRegistry))
}
