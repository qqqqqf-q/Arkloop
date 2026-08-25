package authapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"errors"
	"net"
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

type resolveIdentityRequest struct {
	Identity         string `json:"identity"`
	CfTurnstileToken string `json:"cf_turnstile_token"`
}

type resolvePrefillResponse struct {
	Login string `json:"login,omitempty"`
	Email string `json:"email,omitempty"`
}

type resolveIdentityResponse struct {
	NextStep string                  `json:"next_step"`
	Prefill  *resolvePrefillResponse `json:"prefill,omitempty"`
}

type meResponse struct {
	ID                        string   `json:"id"`
	Username                  string   `json:"username"`
	Email                     *string  `json:"email,omitempty"`
	EmailVerified             bool     `json:"email_verified"`
	EmailVerificationRequired bool     `json:"email_verification_required"`
	WorkEnabled               bool     `json:"work_enabled"`
	Timezone                  *string  `json:"timezone,omitempty"`
	AccountTimezone           *string  `json:"account_timezone,omitempty"`
	CreatedAt                 string   `json:"created_at"`
	AccountID                 string   `json:"account_id,omitempty"`
	AccountName               string   `json:"account_name,omitempty"`
	Role                      string   `json:"role,omitempty"`
	Permissions               []string `json:"permissions"`
}

type updateMeRequest struct {
	Username *string `json:"username,omitempty"`
	Timezone *string `json:"timezone,omitempty"`
}

type updateMeResponse struct {
	Username string  `json:"username"`
	Timezone *string `json:"timezone,omitempty"`
}

func normalizeResponseTimeZone(value *string) *string {
	if value == nil {
		return nil
	}
	cleaned := strings.TrimSpace(*value)
	if cleaned == "" {
		return nil
	}
	loc, err := time.LoadLocation(cleaned)
	if err != nil {
		return nil
	}
	normalized := loc.String()
	return &normalized
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
			case auth.TokenInvalidError:
				// 旧 refresh token 并发重放时，不能反向清掉已经轮换成功的新 cookie。
				httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", "token invalid or expired", traceID, nil)
				return
			case auth.UserNotFoundError:
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

func resolveIdentity(
	authService *auth.Service,
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

		if auditWriter != nil {
			auditWriter.WriteAuthResolved(r.Context(), traceID, body.Identity, string(resolved.NextStep))
		}

		resp := resolveIdentityResponse{
			NextStep: string(resolved.NextStep),
		}
		if resolved.PrefillLogin != "" || resolved.PrefillEmail != "" {
			resp.Prefill = &resolvePrefillResponse{Login: resolved.PrefillLogin, Email: resolved.PrefillEmail}
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func me(authService *auth.Service, membershipRepo *data.AccountMembershipRepository, accountRepo *data.AccountRepository, usersRepo *data.UserRepository, flagService *featureflag.Service) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
			if !ok {
				return
			}

			user, ok := authenticateUser(w, r, traceID, authService)
			if !ok {
				return
			}

			emailVerifyRequired := false
			workEnabled := false
			if flagService != nil {
				emailVerifyRequired, _ = flagService.IsGloballyEnabled(r.Context(), "auth.require_email_verification")
				workEnabled = featureflag.IsWorkEnabled(r.Context(), flagService)
			}
			resp := meResponse{
				ID:                        user.ID.String(),
				Username:                  user.Username,
				Email:                     user.Email,
				EmailVerified:             user.EmailVerifiedAt != nil,
				EmailVerificationRequired: emailVerifyRequired,
				WorkEnabled:               workEnabled,
				CreatedAt:                 user.CreatedAt.UTC().Format(time.RFC3339Nano),
				AccountID:                 actor.AccountID.String(),
				Role:                      actor.AccountRole,
				Permissions:               actor.Permissions,
			}

			if usersRepo != nil {
				if current, err := usersRepo.GetByID(r.Context(), user.ID); err == nil && current != nil {
					resp.Timezone = normalizeResponseTimeZone(current.Timezone)
				}
			}

			if accountRepo != nil {
				if account, err := accountRepo.GetByID(r.Context(), actor.AccountID); err == nil && account != nil {
					resp.AccountName = account.Name
					resp.AccountTimezone = normalizeResponseTimeZone(account.Timezone)
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
			if body.Username == nil && body.Timezone == nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}

			current, err := usersRepo.GetByID(r.Context(), user.ID)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if current == nil {
				httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.user_not_found", "user not found", traceID, nil)
				return
			}

			nextUsername := current.Username
			if body.Username != nil {
				trimmed := strings.TrimSpace(*body.Username)
				if !isValidPublicUsername(trimmed) {
					httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "username is invalid", traceID, nil)
					return
				}
				nextUsername = trimmed
			}

			nextTimezone := current.Timezone
			if body.Timezone != nil {
				cleaned := strings.TrimSpace(*body.Timezone)
				if cleaned == "" {
					nextTimezone = nil
				} else {
					loc, loadErr := time.LoadLocation(cleaned)
					if loadErr != nil {
						httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "timezone is invalid", traceID, nil)
						return
					}
					normalized := loc.String()
					nextTimezone = &normalized
				}
			}

			updated, err := usersRepo.UpdateProfile(r.Context(), user.ID, data.UpdateProfileParams{
				Username:        nextUsername,
				Email:           current.Email,
				EmailVerifiedAt: current.EmailVerifiedAt,
				Locale:          current.Locale,
				Timezone:        nextTimezone,
			})
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if updated == nil {
				httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.user_not_found", "user not found", traceID, nil)
				return
			}

			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, updateMeResponse{
				Username: updated.Username,
				Timezone: normalizeResponseTimeZone(updated.Timezone),
			})

		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
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

func isValidPublicUsername(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > 256 {
		return false
	}
	return !strings.Contains(trimmed, "@")
}
