package authapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/mail"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/featureflag"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/turnstile"
	sharedconfig "arkloop/services/shared/config"

	"github.com/google/uuid"
)

const (
	settingTurnstileSecretKey   = "turnstile.secret_key"
	settingTurnstileSiteKey     = "turnstile.site_key"
	settingTurnstileAllowedHost = "turnstile.allowed_host"

	refreshTokenCookieName = "arkloop_refresh_token"
	refreshTokenCookiePath = "/v1/auth"
)

var legacyRefreshCookieNames = []string{
	"arkloop_rt_web",
	"arkloop_rt_console",
	"arkloop_rt_console_lite",
}

// verifyTurnstileToken performs Turnstile validation if a secret key is configured.
// Returns false and writes the error response when validation fails.
func verifyTurnstileToken(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	token string,
	resolver sharedconfig.Resolver,
) bool {
	if resolver == nil {
		return true
	}

	secretKey, err := resolver.Resolve(r.Context(), settingTurnstileSecretKey, sharedconfig.Scope{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" {
		return true // not configured, skip
	}

	allowedHost, err := resolver.Resolve(r.Context(), settingTurnstileAllowedHost, sharedconfig.Scope{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}
	allowedHost = strings.TrimSpace(allowedHost)

	verifyErr := turnstile.Verify(r.Context(), nethttp.DefaultClient, turnstile.VerifyRequest{
		SecretKey:   secretKey,
		Token:       token,
		RemoteIP:    requestClientIP(r),
		AllowedHost: allowedHost,
	})
	if verifyErr != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "auth.captcha_invalid", "captcha validation failed", traceID, nil)
		return false
	}
	return true
}

type captchaConfigResponse struct {
	Enabled bool   `json:"enabled"`
	SiteKey string `json:"site_key"`
}

func captchaConfig(
	resolver sharedconfig.Resolver,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		siteKey := ""
		if resolver != nil {
			val, err := resolver.Resolve(r.Context(), settingTurnstileSiteKey, sharedconfig.Scope{})
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			siteKey = strings.TrimSpace(val)
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, captchaConfigResponse{
			Enabled: siteKey != "",
			SiteKey: siteKey,
		})
	}
}

type loginRequest struct {
	Login            string `json:"login"`
	Password         string `json:"password"`
	CfTurnstileToken string `json:"cf_turnstile_token"`
}

type loginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type logoutResponse struct {
	OK bool `json:"ok"`
}

type registerRequest struct {
	Login            string `json:"login"`
	Password         string `json:"password"`
	Email            string `json:"email"`
	InviteCode       string `json:"invite_code"`
	Locale           string `json:"locale"`
	CfTurnstileToken string `json:"cf_turnstile_token"`
}

type registerResponse struct {
	UserID      string  `json:"user_id"`
	AccessToken string  `json:"access_token"`
	TokenType   string  `json:"token_type"`
	Warning     *string `json:"warning,omitempty"`
}

type registrationModeResponse struct {
	Mode string `json:"mode"`
}

type resolveIdentityRequest struct {
	Identity         string `json:"identity"`
	CfTurnstileToken string `json:"cf_turnstile_token"`
}

type resolvePrefillResponse struct {
	Login string `json:"login,omitempty"`
	Email string `json:"email,omitempty"`
}

type resolveIdentityResponse struct {
	NextStep       string                  `json:"next_step"`
	FlowToken      string                  `json:"flow_token,omitempty"`
	MaskedEmail    string                  `json:"masked_email,omitempty"`
	OTPAvailable   bool                    `json:"otp_available"`
	InviteRequired bool                    `json:"invite_required"`
	Prefill        *resolvePrefillResponse `json:"prefill,omitempty"`
}

type resolveEmailOTPSendRequest struct {
	FlowToken        string `json:"flow_token"`
	CfTurnstileToken string `json:"cf_turnstile_token"`
}

type resolveEmailOTPVerifyRequest struct {
	FlowToken string `json:"flow_token"`
	Code      string `json:"code"`
}

type meResponse struct {
	ID                        string   `json:"id"`
	Username                  string   `json:"username"`
	Email                     *string  `json:"email,omitempty"`
	EmailVerified             bool     `json:"email_verified"`
	EmailVerificationRequired bool     `json:"email_verification_required"`
	ClawEnabled               bool     `json:"claw_enabled"`
	CreatedAt                 string   `json:"created_at"`
	OrgID                     string   `json:"org_id,omitempty"`
	OrgName                   string   `json:"org_name,omitempty"`
	Role                      string   `json:"role,omitempty"`
	Permissions               []string `json:"permissions"`
}

type updateMeRequest struct {
	Username string `json:"username"`
}

type updateMeResponse struct {
	Username string `json:"username"`
}

func login(authService *auth.Service, auditWriter *audit.Writer, resolver sharedconfig.Resolver) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		var body loginRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		body.Login = strings.TrimSpace(body.Login)
		if body.Login == "" || len(body.Login) > 256 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if body.Password == "" || len(body.Password) > 1024 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		if !verifyTurnstileToken(w, r, traceID, body.CfTurnstileToken, resolver) {
			return
		}

		issued, err := authService.IssueAccessToken(r.Context(), body.Login, body.Password)
		if err != nil {
			var invalid auth.InvalidCredentialsError
			if errors.As(err, &invalid) {
				if auditWriter != nil {
					auditWriter.WriteLoginFailed(r.Context(), traceID, body.Login)
				}
				httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_credentials", "invalid credentials", traceID, nil)
				return
			}
			var suspended auth.SuspendedUserError
			if errors.As(err, &suspended) {
				if auditWriter != nil {
					auditWriter.WriteLoginFailed(r.Context(), traceID, body.Login)
				}
				httpkit.WriteError(w, nethttp.StatusForbidden, "auth.user_suspended", "account suspended", traceID, nil)
				return
			}
			var unverified auth.EmailNotVerifiedError
			if errors.As(err, &unverified) {
				if auditWriter != nil {
					auditWriter.WriteLoginFailed(r.Context(), traceID, body.Login)
				}
				httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_credentials", "invalid credentials", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteLoginSucceeded(r.Context(), traceID, issued.UserID, body.Login)
		}

		setRefreshTokenCookie(w, r, refreshTokenCookieName, issued.RefreshToken, authService.RefreshTokenTTLSeconds())
		clearLegacyRefreshTokenCookies(w, r)
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, loginResponse{
			AccessToken: issued.AccessToken,
			TokenType:   "bearer",
		})
	}
}

func refreshToken(authService *auth.Service, auditWriter *audit.Writer) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		token, ok := readRefreshTokenFromRequest(r)
		if !ok {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "refresh_token is required", traceID, nil)
			return
		}

		issued, err := authService.ConsumeRefreshToken(r.Context(), token)
		if err != nil {
			switch err.(type) {
			case auth.TokenInvalidError, auth.UserNotFoundError:
				clearAuthCookies(w, r)
				httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", "token invalid or expired", traceID, nil)
				return
			case auth.SuspendedUserError:
				clearAuthCookies(w, r)
				httpkit.WriteError(w, nethttp.StatusForbidden, "auth.user_suspended", "account suspended", traceID, nil)
				return
			default:
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
		}

		if auditWriter != nil {
			auditWriter.WriteTokenRefreshed(r.Context(), traceID, issued.UserID)
		}

		setRefreshTokenCookie(w, r, refreshTokenCookieName, issued.RefreshToken, authService.RefreshTokenTTLSeconds())
		clearLegacyRefreshTokenCookies(w, r)
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, loginResponse{
			AccessToken: issued.AccessToken,
			TokenType:   "bearer",
		})
	}
}

func logout(authService *auth.Service, auditWriter *audit.Writer) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		user, ok := authenticateUser(w, r, traceID, authService)
		if !ok {
			return
		}

		if err := authService.Logout(r.Context(), user.ID, time.Now().UTC()); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteLogout(r.Context(), traceID, user.ID)
		}

		clearAuthCookies(w, r)
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, logoutResponse{OK: true})
	}
}

func registrationMode(flagService *featureflag.Service) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		mode := "open"
		if flagService != nil {
			mode = "invite_only"
			open, err := flagService.IsGloballyEnabled(r.Context(), "registration.open")
			if err == nil && open {
				mode = "open"
			}
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, registrationModeResponse{Mode: mode})
	}
}

func resolveIdentity(
	authService *auth.Service,
	flagService *featureflag.Service,
	auditWriter *audit.Writer,
	resolver sharedconfig.Resolver,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		var body resolveIdentityRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		body.Identity = strings.TrimSpace(body.Identity)
		if body.Identity == "" || len(body.Identity) > 256 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "identity is required", traceID, nil)
			return
		}

		if !verifyTurnstileToken(w, r, traceID, body.CfTurnstileToken, resolver) {
			return
		}

		resolved, err := authService.ResolveIdentity(r.Context(), body.Identity)
		if err != nil {
			var invalid auth.InvalidIdentityError
			if errors.As(err, &invalid) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "identity is invalid", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		inviteRequired := false
		if resolved.NextStep == auth.ResolveNextStepRegister {
			inviteRequired = !isOpenRegistration(r.Context(), flagService)
		}

		if auditWriter != nil {
			auditWriter.WriteAuthResolved(r.Context(), traceID, body.Identity, string(resolved.NextStep))
		}

		resp := resolveIdentityResponse{
			NextStep:       string(resolved.NextStep),
			FlowToken:      resolved.FlowToken,
			MaskedEmail:    resolved.MaskedEmail,
			OTPAvailable:   resolved.OTPAvailable,
			InviteRequired: inviteRequired,
		}
		if resolved.PrefillLogin != "" || resolved.PrefillEmail != "" {
			resp.Prefill = &resolvePrefillResponse{Login: resolved.PrefillLogin, Email: resolved.PrefillEmail}
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func isOpenRegistration(ctx context.Context, flagService *featureflag.Service) bool {
	openRegistration := flagService == nil
	if flagService != nil {
		open, err := flagService.IsGloballyEnabled(ctx, "registration.open")
		if err == nil {
			openRegistration = open
		}
	}
	return openRegistration
}

func register(
	registrationService *auth.RegistrationService,
	authService *auth.Service,
	flagService *featureflag.Service,
	auditWriter *audit.Writer,
	resolver sharedconfig.Resolver,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if registrationService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		var body registerRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		body.Login = strings.TrimSpace(body.Login)
		body.Email = strings.TrimSpace(body.Email)
		body.InviteCode = strings.TrimSpace(body.InviteCode)
		body.Locale = strings.TrimSpace(body.Locale)
		if !isValidPublicUsername(body.Login) {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if body.Password == "" || len(body.Password) > 1024 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if body.Email == "" || len(body.Email) > 256 || !isValidEmail(body.Email) {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "email is required and must be valid", traceID, nil)
			return
		}

		if !verifyTurnstileToken(w, r, traceID, body.CfTurnstileToken, resolver) {
			return
		}

		// 注册模式检查
		openRegistration := isOpenRegistration(r.Context(), flagService)

		if !openRegistration && body.InviteCode == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "auth.invite_code_required", "invite code is required", traceID, nil)
			return
		}

		created, err := registrationService.Register(r.Context(), body.Login, body.Password, body.Email, body.Locale, body.InviteCode, !openRegistration)
		if err != nil {
			var loginExists auth.LoginExistsError
			if errors.As(err, &loginExists) {
				httpkit.WriteError(w, nethttp.StatusConflict, "auth.login_exists", "login already taken", traceID, nil)
				return
			}
			var passwordPolicy auth.PasswordPolicyError
			if errors.As(err, &passwordPolicy) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", passwordPolicy.Error(), traceID, nil)
				return
			}
			var codeErr auth.InviteCodeInvalidError
			if errors.As(err, &codeErr) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "auth.invite_code_invalid", codeErr.Error(), traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteUserRegistered(r.Context(), traceID, created.UserID, body.Login)
			if created.ReferralID != nil {
				auditWriter.WriteReferralCreated(r.Context(), traceID, created.InviterUserID, created.UserID, created.InviteCodeID, *created.ReferralID)
			}
		}

		resp := registerResponse{
			UserID:      created.UserID.String(),
			AccessToken: created.AccessToken,
			TokenType:   "bearer",
		}
		if created.Warning != "" {
			resp.Warning = &created.Warning
		}
		setRefreshTokenCookie(w, r, refreshTokenCookieName, created.RefreshToken, registrationService.RefreshTokenTTLSeconds())
		clearLegacyRefreshTokenCookies(w, r)
		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, resp)
	}
}

func me(authService *auth.Service, membershipRepo *data.OrgMembershipRepository, orgRepo *data.OrgRepository, credentialRepo *data.UserCredentialRepository, usersRepo *data.UserRepository, flagService *featureflag.Service) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			user, ok := authenticateUser(w, r, traceID, authService)
			if !ok {
				return
			}

			if membershipRepo == nil {
				httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
				return
			}

			membership, err := membershipRepo.GetDefaultForUser(r.Context(), user.ID)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if membership == nil {
				httpkit.WriteError(w, nethttp.StatusForbidden, "auth.no_org_membership", "user has no org membership", traceID, nil)
				return
			}

			emailVerifyRequired := false
			clawEnabled := false
			if flagService != nil {
				emailVerifyRequired, _ = flagService.IsGloballyEnabled(r.Context(), "auth.require_email_verification")
				clawEnabled = featureflag.IsClawEnabled(r.Context(), flagService)
			}
			resp := meResponse{
				ID:                        user.ID.String(),
				Email:                     user.Email,
				EmailVerified:             user.EmailVerifiedAt != nil,
				EmailVerificationRequired: emailVerifyRequired,
				ClawEnabled:               clawEnabled,
				CreatedAt:                 user.CreatedAt.UTC().Format(time.RFC3339Nano),
				OrgID:                     membership.OrgID.String(),
				Role:                      membership.Role,
				Permissions:               auth.PermissionsForRole(membership.Role),
			}

			if credentialRepo != nil {
				if cred, err := credentialRepo.GetByUserID(r.Context(), user.ID); err == nil && cred != nil {
					resp.Username = cred.Login
				}
			}

			if orgRepo != nil {
				if org, err := orgRepo.GetByID(r.Context(), membership.OrgID); err == nil && org != nil {
					resp.OrgName = org.Name
				}
			}

			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)

		case nethttp.MethodPatch:
			if usersRepo == nil {
				httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
				return
			}

			user, ok := authenticateUser(w, r, traceID, authService)
			if !ok {
				return
			}

			var body updateMeRequest
			if err := httpkit.DecodeJSON(r, &body); err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
			body.Username = strings.TrimSpace(body.Username)
			if !isValidPublicUsername(body.Username) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "username is invalid", traceID, nil)
				return
			}

			updated, err := usersRepo.UpdateProfile(r.Context(), user.ID, data.UpdateProfileParams{
				Username: body.Username,
			})
			if err != nil || updated == nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}

			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, updateMeResponse{Username: updated.Username})

		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

// maxJSONBodySize 限制 JSON 请求体最大 1 MiB，防止大 payload DoS。
const maxJSONBodySize = 1 << 20

func decodeJSON(r *nethttp.Request, dst any) error {
	reader := nethttp.MaxBytesReader(nil, r.Body, maxJSONBodySize)
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func isValidEmail(value string) bool {
	if strings.ContainsAny(value, "\r\n") {
		return false
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil || addr == nil {
		return false
	}
	return addr.Address == trimmed
}

func writeJSON(w nethttp.ResponseWriter, traceID string, statusCode int, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(raw)
}

func requestClientIP(r *nethttp.Request) string {
	if r == nil {
		return ""
	}
	if ip := observability.ClientIPFromContext(r.Context()); ip != "" {
		return ip
	}
	if fwd := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); fwd != "" {
		if ip, _, _ := strings.Cut(fwd, ","); ip != "" {
			if parsed := net.ParseIP(strings.TrimSpace(ip)); parsed != nil {
				return parsed.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		if parsed := net.ParseIP(strings.TrimSpace(r.RemoteAddr)); parsed != nil {
			return parsed.String()
		}
		return ""
	}
	return host
}

func requestHTTPS(r *nethttp.Request) bool {
	if r == nil {
		return false
	}
	if enabled, ok := observability.RequestHTTPSFromContext(r.Context()); ok {
		return enabled
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func isSecureCookieRequest(r *nethttp.Request) bool {
	return requestHTTPS(r)
}

func setRefreshTokenCookie(w nethttp.ResponseWriter, r *nethttp.Request, cookieName string, token string, ttlSeconds int) {
	if w == nil || r == nil {
		return
	}
	token = strings.TrimSpace(token)
	if token == "" || ttlSeconds <= 0 || cookieName == "" {
		return
	}

	expiresAt := time.Now().UTC().Add(time.Duration(ttlSeconds) * time.Second)
	nethttp.SetCookie(w, &nethttp.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     refreshTokenCookiePath,
		HttpOnly: true,
		SameSite: nethttp.SameSiteLaxMode,
		Secure:   isSecureCookieRequest(r),
		Expires:  expiresAt,
		MaxAge:   ttlSeconds,
	})
}

func clearRefreshTokenCookie(w nethttp.ResponseWriter, r *nethttp.Request, cookieName string) {
	if w == nil || r == nil || cookieName == "" {
		return
	}
	nethttp.SetCookie(w, &nethttp.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     refreshTokenCookiePath,
		HttpOnly: true,
		SameSite: nethttp.SameSiteLaxMode,
		Secure:   isSecureCookieRequest(r),
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
	})
}

func readRefreshTokenFromRequest(r *nethttp.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	if cookie, err := r.Cookie(refreshTokenCookieName); err == nil {
		if token := strings.TrimSpace(cookie.Value); token != "" {
			return token, true
		}
	}
	return "", false
}

func clearLegacyRefreshTokenCookies(w nethttp.ResponseWriter, r *nethttp.Request) {
	for _, cookieName := range legacyRefreshCookieNames {
		clearRefreshTokenCookie(w, r, cookieName)
	}
}

func clearAuthCookies(w nethttp.ResponseWriter, r *nethttp.Request) {
	clearRefreshTokenCookie(w, r, refreshTokenCookieName)
	clearLegacyRefreshTokenCookies(w, r)
}

func parseBearerToken(w nethttp.ResponseWriter, r *nethttp.Request, traceID string) (string, bool) {
	authorization := r.Header.Get("Authorization")
	if strings.TrimSpace(authorization) == "" {
		httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.missing_token", "missing Authorization Bearer token", traceID, nil)
		return "", false
	}

	scheme, rest, ok := strings.Cut(authorization, " ")
	if !ok || strings.TrimSpace(rest) == "" || strings.ToLower(scheme) != "bearer" {
		httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_authorization", "Authorization header must be: Bearer <token>", traceID, nil)
		return "", false
	}

	return strings.TrimSpace(rest), true
}

func authenticateUser(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
) (*authUser, bool) {
	token, ok := httpkit.ParseBearerToken(w, r, traceID)
	if !ok {
		return nil, false
	}

	user, err := authService.AuthenticateUser(r.Context(), token)
	if err != nil {
		switch typed := err.(type) {
		case auth.TokenExpiredError:
			httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.token_expired", typed.Error(), traceID, nil)
		case auth.TokenInvalidError:
			httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", typed.Error(), traceID, nil)
		case auth.UserNotFoundError:
			httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.user_not_found", "user not found", traceID, nil)
		case auth.SuspendedUserError:
			httpkit.WriteError(w, nethttp.StatusForbidden, "auth.user_suspended", "account suspended", traceID, nil)
		default:
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		}
		return nil, false
	}

	return &authUser{
		ID:              user.ID,
		Username:        user.Username,
		Email:           user.Email,
		EmailVerifiedAt: user.EmailVerifiedAt,
		CreatedAt:       user.CreatedAt,
	}, true
}

type authUser struct {
	ID              uuid.UUID
	Username        string
	Email           *string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
}

func writeMethodNotAllowed(w nethttp.ResponseWriter, r *nethttp.Request) {
	traceID := observability.TraceIDFromContext(r.Context())
	httpkit.WriteError(w, nethttp.StatusMethodNotAllowed, "http.method_not_allowed", "Method Not Allowed", traceID, nil)
}

func writeAuthNotConfigured(w nethttp.ResponseWriter, traceID string) {
	httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "auth.not_configured", "auth not configured", traceID, nil)
}

type emailVerifyConfirmRequest struct {
	Token string `json:"token"`
}

func emailVerifySend(authService *auth.Service, emailVerifyService *auth.EmailVerifyService) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil || emailVerifyService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		user, ok := authenticateUser(w, r, traceID, authService)
		if !ok {
			return
		}

		if err := emailVerifyService.SendVerification(r.Context(), user.ID, user.Username); err != nil {
			if err.Error() == "user has no email address" {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "email.no_address", "user has no email address", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		w.WriteHeader(nethttp.StatusNoContent)
	}
}

func emailVerifyConfirm(emailVerifyService *auth.EmailVerifyService) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if emailVerifyService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		var body emailVerifyConfirmRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil || strings.TrimSpace(body.Token) == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "token is required", traceID, nil)
			return
		}

		if err := emailVerifyService.ConfirmVerification(r.Context(), strings.TrimSpace(body.Token)); err != nil {
			var expired auth.TokenAlreadyUsedOrExpiredError
			if errors.As(err, &expired) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "email.token_invalid", "token invalid or expired", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
	}
}

type emailOTPSendRequest struct {
	Email            string `json:"email"`
	CfTurnstileToken string `json:"cf_turnstile_token"`
}

type emailOTPVerifyRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func emailOTPSend(otpLoginService *auth.EmailOTPLoginService, resolver sharedconfig.Resolver) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if otpLoginService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		var body emailOTPSendRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		body.Email = strings.TrimSpace(body.Email)
		if body.Email == "" || len(body.Email) > 256 || !isValidEmail(body.Email) {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "valid email is required", traceID, nil)
			return
		}

		if !verifyTurnstileToken(w, r, traceID, body.CfTurnstileToken, resolver) {
			return
		}

		// 静默处理：无论邮箱是否存在都返回 204
		if err := otpLoginService.SendLoginOTP(r.Context(), body.Email); err != nil {
			var rateLimited auth.OTPRateLimitedError
			if errors.As(err, &rateLimited) {
				httpkit.WriteError(w, nethttp.StatusTooManyRequests, "auth.otp_rate_limited", "otp rate limited", traceID, nil)
				return
			}
			var unavailable auth.OTPProtectionUnavailableError
			if errors.As(err, &unavailable) {
				httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "auth.otp_protection_unavailable", "otp protection unavailable", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		w.WriteHeader(nethttp.StatusNoContent)
	}
}

func resolveEmailOTPSend(
	authService *auth.Service,
	otpLoginService *auth.EmailOTPLoginService,
	auditWriter *audit.Writer,
	resolver sharedconfig.Resolver,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil || otpLoginService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		var body resolveEmailOTPSendRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		body.FlowToken = strings.TrimSpace(body.FlowToken)
		if body.FlowToken == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "flow token is required", traceID, nil)
			return
		}

		if !verifyTurnstileToken(w, r, traceID, body.CfTurnstileToken, resolver) {
			return
		}

		resolved, err := authService.ResolveFlow(r.Context(), body.FlowToken)
		if err != nil {
			var invalid auth.FlowTokenInvalidError
			var unavailable auth.OTPUnavailableError
			if errors.As(err, &invalid) {
				httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.flow_token_invalid", invalid.Error(), traceID, nil)
				return
			}
			if errors.As(err, &unavailable) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "auth.otp_unavailable", unavailable.Error(), traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if err := otpLoginService.SendLoginOTP(r.Context(), resolved.Email); err != nil {
			var rateLimited auth.OTPRateLimitedError
			if errors.As(err, &rateLimited) {
				httpkit.WriteError(w, nethttp.StatusTooManyRequests, "auth.otp_rate_limited", "otp rate limited", traceID, nil)
				return
			}
			var protectionUnavailable auth.OTPProtectionUnavailableError
			if errors.As(err, &protectionUnavailable) {
				httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "auth.otp_protection_unavailable", "otp protection unavailable", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteLoginOTPSent(r.Context(), traceID, resolved.UserID, resolved.Email)
		}
		w.WriteHeader(nethttp.StatusNoContent)
	}
}

func emailOTPVerify(otpLoginService *auth.EmailOTPLoginService, authService *auth.Service, auditWriter *audit.Writer) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if otpLoginService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		var body emailOTPVerifyRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		body.Email = strings.TrimSpace(body.Email)
		body.Code = strings.TrimSpace(body.Code)
		if body.Email == "" || body.Code == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "email and code are required", traceID, nil)
			return
		}

		issued, err := otpLoginService.VerifyLoginOTP(r.Context(), body.Email, body.Code)
		if err != nil {
			var locked auth.OTPLockedError
			if errors.As(err, &locked) {
				httpkit.WriteError(w, nethttp.StatusTooManyRequests, "auth.otp_locked", "too many attempts", traceID, nil)
				return
			}
			var unavailable auth.OTPProtectionUnavailableError
			if errors.As(err, &unavailable) {
				httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "auth.otp_protection_unavailable", "otp protection unavailable", traceID, nil)
				return
			}
			var expired auth.OTPExpiredOrUsedError
			if errors.As(err, &expired) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "auth.otp_invalid", "code invalid or expired", traceID, nil)
				return
			}
			var suspended auth.SuspendedUserError
			if errors.As(err, &suspended) {
				httpkit.WriteError(w, nethttp.StatusForbidden, "auth.user_suspended", "account suspended", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteLoginSucceeded(r.Context(), traceID, issued.UserID, body.Email)
		}

		setRefreshTokenCookie(w, r, refreshTokenCookieName, issued.RefreshToken, otpLoginService.RefreshTokenTTLSeconds())
		clearLegacyRefreshTokenCookies(w, r)
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, loginResponse{
			AccessToken: issued.AccessToken,
			TokenType:   "bearer",
		})
	}
}

func resolveEmailOTPVerify(authService *auth.Service, otpLoginService *auth.EmailOTPLoginService, auditWriter *audit.Writer) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil || otpLoginService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		var body resolveEmailOTPVerifyRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		body.FlowToken = strings.TrimSpace(body.FlowToken)
		body.Code = strings.TrimSpace(body.Code)
		if body.FlowToken == "" || body.Code == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "flow token and code are required", traceID, nil)
			return
		}

		resolved, err := authService.ResolveFlow(r.Context(), body.FlowToken)
		if err != nil {
			var invalid auth.FlowTokenInvalidError
			var unavailable auth.OTPUnavailableError
			if errors.As(err, &invalid) {
				httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.flow_token_invalid", invalid.Error(), traceID, nil)
				return
			}
			if errors.As(err, &unavailable) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "auth.otp_unavailable", unavailable.Error(), traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		issued, err := otpLoginService.VerifyLoginOTP(r.Context(), resolved.Email, body.Code)
		if err != nil {
			var locked auth.OTPLockedError
			if errors.As(err, &locked) {
				httpkit.WriteError(w, nethttp.StatusTooManyRequests, "auth.otp_locked", "too many attempts", traceID, nil)
				return
			}
			var protectionUnavailable auth.OTPProtectionUnavailableError
			if errors.As(err, &protectionUnavailable) {
				httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "auth.otp_protection_unavailable", "otp protection unavailable", traceID, nil)
				return
			}
			var expired auth.OTPExpiredOrUsedError
			if errors.As(err, &expired) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "auth.otp_invalid", "code invalid or expired", traceID, nil)
				return
			}
			var suspended auth.SuspendedUserError
			if errors.As(err, &suspended) {
				httpkit.WriteError(w, nethttp.StatusForbidden, "auth.user_suspended", "account suspended", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteLoginSucceeded(r.Context(), traceID, issued.UserID, resolved.Email)
		}

		setRefreshTokenCookie(w, r, refreshTokenCookieName, issued.RefreshToken, otpLoginService.RefreshTokenTTLSeconds())
		clearLegacyRefreshTokenCookies(w, r)
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, loginResponse{
			AccessToken: issued.AccessToken,
			TokenType:   "bearer",
		})
	}
}

func isValidPublicUsername(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > 256 {
		return false
	}
	return !strings.Contains(trimmed, "@")
}
