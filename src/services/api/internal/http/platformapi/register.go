package platformapi

import (
	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	sharedconfig "arkloop/services/shared/config"

	"github.com/redis/go-redis/v9"
)

type Deps struct {
	AuthService          *auth.Service
	AccountMembershipRepo    *data.AccountMembershipRepository
	APIKeysRepo          *data.APIKeysRepository
	NotificationsRepo    *data.NotificationsRepository
	AuditLogRepo         *data.AuditLogRepository
	PlatformSettingsRepo *data.PlatformSettingsRepository
	RedisClient          *redis.Client
	ConfigInvalidator    sharedconfig.Invalidator
	ConfigRegistry       *sharedconfig.Registry
}

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("/v1/notifications", notificationsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.NotificationsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/notifications/", notificationEntry(deps.AuthService, deps.AccountMembershipRepo, deps.NotificationsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/audit-logs", auditLogsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.AuditLogRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/platform-settings", platformSettingsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.PlatformSettingsRepo, deps.APIKeysRepo, deps.ConfigRegistry))
	mux.HandleFunc("/v1/admin/platform-settings/", platformSettingEntry(deps.AuthService, deps.AccountMembershipRepo, deps.PlatformSettingsRepo, deps.APIKeysRepo, deps.RedisClient, deps.ConfigInvalidator, deps.ConfigRegistry))
}
