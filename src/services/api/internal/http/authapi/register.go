package authapi

import (
	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/featureflag"
	sharedconfig "arkloop/services/shared/config"
)

type Deps struct {
	Pool                  data.DB
	AuthService           *auth.Service
	FeatureFlagService    *featureflag.Service
	AuditWriter           *audit.Writer
	AccountMembershipRepo *data.AccountMembershipRepository
	AccountRepo           *data.AccountRepository
	UserCredentialRepo    *data.UserCredentialRepository
	UsersRepo             *data.UserRepository
	ConfigResolver        sharedconfig.Resolver
}

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	registerLocalSessionRoute(mux, deps)
	mux.HandleFunc("POST /v1/auth/resolve", resolveIdentity(deps.AuthService, deps.AuditWriter, deps.ConfigResolver))
	mux.HandleFunc("/v1/auth/login", login(deps.AuthService, deps.AuditWriter, deps.ConfigResolver))
	mux.HandleFunc("/v1/auth/refresh", refreshToken(deps.AuthService, deps.AuditWriter))
	mux.HandleFunc("/v1/auth/logout", logout(deps.AuthService, deps.AuditWriter))
	mux.HandleFunc("/v1/me", me(deps.AuthService, deps.AccountMembershipRepo, deps.AccountRepo, deps.UsersRepo, deps.FeatureFlagService))
}
