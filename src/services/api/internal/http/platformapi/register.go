package platformapi

import (
	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	sharedconfig "arkloop/services/shared/config"
)

type Deps struct {
	AuthService          *auth.Service
	AccountMembershipRepo    *data.AccountMembershipRepository
	APIKeysRepo          *data.APIKeysRepository
	NotificationsRepo    *data.NotificationsRepository
	PlatformSettingsRepo *data.PlatformSettingsRepository
	ConfigInvalidator    sharedconfig.Invalidator
	ConfigRegistry       *sharedconfig.Registry
}

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("/v1/notifications", notificationsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.NotificationsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/notifications/", notificationEntry(deps.AuthService, deps.AccountMembershipRepo, deps.NotificationsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/platform-settings", platformSettingsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.PlatformSettingsRepo, deps.APIKeysRepo, deps.ConfigRegistry))
	mux.HandleFunc("/v1/admin/platform-settings/", platformSettingEntry(deps.AuthService, deps.AccountMembershipRepo, deps.PlatformSettingsRepo, deps.APIKeysRepo, deps.ConfigInvalidator, deps.ConfigRegistry))
}
