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
	AuthService          *auth.Service
	RegistrationService  *auth.RegistrationService
	EmailVerifyService   *auth.EmailVerifyService
	EmailOTPLoginService *auth.EmailOTPLoginService
	FeatureFlagService   *featureflag.Service
	AuditWriter          *audit.Writer
	AccountMembershipRepo    *data.AccountMembershipRepository
	AccountRepo              *data.AccountRepository
	UserCredentialRepo   *data.UserCredentialRepository
	UsersRepo            *data.UserRepository
	ConfigResolver       sharedconfig.Resolver
}

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/auth/captcha-config", captchaConfig(deps.ConfigResolver))
	mux.HandleFunc("POST /v1/auth/resolve", resolveIdentity(deps.AuthService, deps.FeatureFlagService, deps.AuditWriter, deps.ConfigResolver))
	mux.HandleFunc("/v1/auth/login", login(deps.AuthService, deps.AuditWriter, deps.ConfigResolver))
	mux.HandleFunc("/v1/auth/refresh", refreshToken(deps.AuthService, deps.AuditWriter))
	mux.HandleFunc("/v1/auth/logout", logout(deps.AuthService, deps.AuditWriter))
	mux.HandleFunc("/v1/auth/register", register(deps.RegistrationService, deps.AuthService, deps.FeatureFlagService, deps.AuditWriter, deps.ConfigResolver))
	mux.HandleFunc("/v1/auth/registration-mode", registrationMode(deps.FeatureFlagService))
	mux.HandleFunc("/v1/auth/email/verify/send", emailVerifySend(deps.AuthService, deps.EmailVerifyService))
	mux.HandleFunc("/v1/auth/email/verify/confirm", emailVerifyConfirm(deps.EmailVerifyService))
	mux.HandleFunc("/v1/auth/email/otp/send", emailOTPSend(deps.EmailOTPLoginService, deps.ConfigResolver))
	mux.HandleFunc("/v1/auth/email/otp/verify", emailOTPVerify(deps.EmailOTPLoginService, deps.AuthService, deps.AuditWriter))
	mux.HandleFunc("POST /v1/auth/resolve/otp/send", resolveEmailOTPSend(deps.AuthService, deps.EmailOTPLoginService, deps.AuditWriter, deps.ConfigResolver))
	mux.HandleFunc("POST /v1/auth/resolve/otp/verify", resolveEmailOTPVerify(deps.AuthService, deps.EmailOTPLoginService, deps.AuditWriter))
	mux.HandleFunc("/v1/me", me(deps.AuthService, deps.AccountMembershipRepo, deps.AccountRepo, deps.UserCredentialRepo, deps.UsersRepo, deps.FeatureFlagService))
}
