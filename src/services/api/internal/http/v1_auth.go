package http

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
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

	clientAppHeader = "X-Client-App"
)

var allowedClientApps = map[string]string{
	"web":          "arkloop_rt_web",
	"console":      "arkloop_rt_console",
	"console-lite": "arkloop_rt_console_lite",
}

type tokenSource int

const (
	tokenSourceNone   tokenSource = iota
	tokenSourceApp                // app-specific cookie
	tokenSourceShared             // shared cookie
	tokenSourceBody               // request body (legacy)
)

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
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" {
		return true // not configured, skip
	}

	allowedHost, err := resolver.Resolve(r.Context(), settingTurnstileAllowedHost, sharedconfig.Scope{})
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}
	allowedHost = strings.TrimSpace(allowedHost)

	// RemoteAddr 格式为 "IP:port"，需剥离端口再传给 Cloudflare
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr // 兜底，格式异常时原样传
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		remoteIP = strings.TrimSpace(strings.SplitN(fwd, ",", 2)[0])
	}

	verifyErr := turnstile.Verify(r.Context(), nethttp.DefaultClient, turnstile.VerifyRequest{
		SecretKey:   secretKey,
		Token:       token,
		RemoteIP:    strings.TrimSpace(remoteIP),
		AllowedHost: allowedHost,
	})
	if verifyErr != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "auth.captcha_invalid", "captcha validation failed", traceID, nil)
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
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			siteKey = strings.TrimSpace(val)
		}

		writeJSON(w, traceID, nethttp.StatusOK, captchaConfigResponse{
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

type meResponse struct {
	ID                        string   `json:"id"`
	Username                  string   `json:"username"`
	Email                     *string  `json:"email,omitempty"`
	EmailVerified             bool     `json:"email_verified"`
	EmailVerificationRequired bool     `json:"email_verification_required"`
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
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		var body loginRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		body.Login = strings.TrimSpace(body.Login)
		if body.Login == "" || len(body.Login) > 256 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if body.Password == "" || len(body.Password) > 1024 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
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
				WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_credentials", "invalid credentials", traceID, nil)
				return
			}
			var suspended auth.SuspendedUserError
			if errors.As(err, &suspended) {
				if auditWriter != nil {
					auditWriter.WriteLoginFailed(r.Context(), traceID, body.Login)
				}
				WriteError(w, nethttp.StatusForbidden, "auth.user_suspended", "account suspended", traceID, nil)
				return
			}
			var unverified auth.EmailNotVerifiedError
			if errors.As(err, &unverified) {
				if auditWriter != nil {
					auditWriter.WriteLoginFailed(r.Context(), traceID, body.Login)
				}
				WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_credentials", "invalid credentials", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteLoginSucceeded(r.Context(), traceID, issued.UserID, body.Login)
		}

		setLoginCookies(w, r, resolveClientApp(r), issued.RefreshToken, authService.RefreshTokenTTLSeconds(), authService, issued.UserID)
		writeJSON(w, traceID, nethttp.StatusOK, loginResponse{
			AccessToken: issued.AccessToken,
			TokenType:   "bearer",
		})
	}
}

type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func refreshToken(authService *auth.Service, auditWriter *audit.Writer) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		clientApp := resolveClientApp(r)
		token, source := readRefreshTokenFromRequest(r, clientApp)
		if source == tokenSourceNone {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "refresh_token is required", traceID, nil)
			return
		}

		issued, err := authService.ConsumeRefreshToken(r.Context(), token)
		if err != nil {
			clearSourceCookie := func() {
				switch source {
				case tokenSourceApp:
					clearRefreshTokenCookie(w, r, appRefreshCookieName(clientApp))
				case tokenSourceShared:
					clearRefreshTokenCookie(w, r, refreshTokenCookieName)
				}
			}
			switch err.(type) {
			case auth.TokenInvalidError, auth.UserNotFoundError:
				clearSourceCookie()
				WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", "token invalid or expired", traceID, nil)
				return
			case auth.SuspendedUserError:
				clearSourceCookie()
				WriteError(w, nethttp.StatusForbidden, "auth.user_suspended", "account suspended", traceID, nil)
				return
			default:
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
		}

		if auditWriter != nil {
			auditWriter.WriteTokenRefreshed(r.Context(), traceID, issued.UserID)
		}

		ttl := authService.RefreshTokenTTLSeconds()
		appCookie := appRefreshCookieName(clientApp)
		if appCookie != "" {
			setRefreshTokenCookie(w, r, appCookie, issued.RefreshToken, ttl)
			if source == tokenSourceShared {
				// fallback: 从共享 cookie 继承 session，同时更新共享 cookie
				if sharedToken, err := authService.IssueRefreshTokenOnly(r.Context(), issued.UserID); err == nil {
					setRefreshTokenCookie(w, r, refreshTokenCookieName, sharedToken, ttl)
				}
			}
		} else {
			setRefreshTokenCookie(w, r, refreshTokenCookieName, issued.RefreshToken, ttl)
		}
		writeJSON(w, traceID, nethttp.StatusOK, loginResponse{
			AccessToken: issued.AccessToken,
			TokenType:   "bearer",
		})
	}
}

func logout(authService *auth.Service, auditWriter *audit.Writer) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		user, ok := authenticateUser(w, r, traceID, authService)
		if !ok {
			return
		}

		if err := authService.Logout(r.Context(), user.ID, time.Now().UTC()); err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteLogout(r.Context(), traceID, user.ID)
		}

		clearAuthCookies(w, r, resolveClientApp(r))
		writeJSON(w, traceID, nethttp.StatusOK, logoutResponse{OK: true})
	}
}

func registrationMode(flagService *featureflag.Service) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowed(w, r)
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

		writeJSON(w, traceID, nethttp.StatusOK, registrationModeResponse{Mode: mode})
	}
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
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if registrationService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		var body registerRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		body.Login = strings.TrimSpace(body.Login)
		body.Email = strings.TrimSpace(body.Email)
		body.InviteCode = strings.TrimSpace(body.InviteCode)
		body.Locale = strings.TrimSpace(body.Locale)
		if body.Login == "" || len(body.Login) > 256 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if body.Password == "" || len(body.Password) < 8 || len(body.Password) > 1024 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if body.Email == "" || len(body.Email) > 256 || !isValidEmail(body.Email) {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "email is required and must be valid", traceID, nil)
			return
		}

		if !verifyTurnstileToken(w, r, traceID, body.CfTurnstileToken, resolver) {
			return
		}

		// 注册模式检查
		openRegistration := flagService == nil
		if flagService != nil {
			open, err := flagService.IsGloballyEnabled(r.Context(), "registration.open")
			if err == nil {
				openRegistration = open
			}
		}

		if !openRegistration && body.InviteCode == "" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "auth.invite_code_required", "invite code is required", traceID, nil)
			return
		}

		created, err := registrationService.Register(r.Context(), body.Login, body.Password, body.Email, body.Locale, body.InviteCode, !openRegistration)
		if err != nil {
			var loginExists auth.LoginExistsError
			if errors.As(err, &loginExists) {
				WriteError(w, nethttp.StatusConflict, "auth.login_exists", "login already taken", traceID, nil)
				return
			}
			var codeErr auth.InviteCodeInvalidError
			if errors.As(err, &codeErr) {
				WriteError(w, nethttp.StatusUnprocessableEntity, "auth.invite_code_invalid", codeErr.Error(), traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
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
		setLoginCookies(w, r, resolveClientApp(r), created.RefreshToken, registrationService.RefreshTokenTTLSeconds(), authService, created.UserID)
		writeJSON(w, traceID, nethttp.StatusCreated, resp)
	}
}

func me(authService *auth.Service, membershipRepo *data.OrgMembershipRepository, orgRepo *data.OrgRepository, credentialRepo *data.UserCredentialRepository, usersRepo *data.UserRepository, flagService *featureflag.Service) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			user, ok := authenticateUser(w, r, traceID, authService)
			if !ok {
				return
			}

			if membershipRepo == nil {
				WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
				return
			}

			membership, err := membershipRepo.GetDefaultForUser(r.Context(), user.ID)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if membership == nil {
				WriteError(w, nethttp.StatusForbidden, "auth.no_org_membership", "user has no org membership", traceID, nil)
				return
			}

			emailVerifyRequired := false
			if flagService != nil {
				emailVerifyRequired, _ = flagService.IsGloballyEnabled(r.Context(), "auth.require_email_verification")
			}
			resp := meResponse{
				ID:                        user.ID.String(),
				Email:                     user.Email,
				EmailVerified:             user.EmailVerifiedAt != nil,
				EmailVerificationRequired: emailVerifyRequired,
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

			writeJSON(w, traceID, nethttp.StatusOK, resp)

		case nethttp.MethodPatch:
			if usersRepo == nil {
				WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
				return
			}

			user, ok := authenticateUser(w, r, traceID, authService)
			if !ok {
				return
			}

			var body updateMeRequest
			if err := decodeJSON(r, &body); err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
			body.Username = strings.TrimSpace(body.Username)
			if body.Username == "" || len(body.Username) > 256 {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "username is invalid", traceID, nil)
				return
			}

			updated, err := usersRepo.UpdateProfile(r.Context(), user.ID, data.UpdateProfileParams{
				Username: body.Username,
			})
			if err != nil || updated == nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}

			writeJSON(w, traceID, nethttp.StatusOK, updateMeResponse{Username: updated.Username})

		default:
			writeMethodNotAllowed(w, r)
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
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(raw)
}

func isSecureCookieRequest(r *nethttp.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
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

// resolveClientApp reads and validates the X-Client-App header.
func resolveClientApp(r *nethttp.Request) string {
	if r == nil {
		return ""
	}
	app := strings.TrimSpace(r.Header.Get(clientAppHeader))
	if _, ok := allowedClientApps[app]; ok {
		return app
	}
	return ""
}

// appRefreshCookieName returns the app-specific cookie name, or empty string.
func appRefreshCookieName(clientApp string) string {
	return allowedClientApps[clientApp]
}

func readRefreshTokenFromRequest(r *nethttp.Request, clientApp string) (string, tokenSource) {
	if r == nil {
		return "", tokenSourceNone
	}

	if appCookie := appRefreshCookieName(clientApp); appCookie != "" {
		if cookie, err := r.Cookie(appCookie); err == nil {
			if token := strings.TrimSpace(cookie.Value); token != "" {
				return token, tokenSourceApp
			}
		}
	}

	if cookie, err := r.Cookie(refreshTokenCookieName); err == nil {
		if token := strings.TrimSpace(cookie.Value); token != "" {
			return token, tokenSourceShared
		}
	}

	var body refreshTokenRequest
	if err := decodeJSON(r, &body); err != nil {
		return "", tokenSourceNone
	}
	if token := strings.TrimSpace(body.RefreshToken); token != "" {
		return token, tokenSourceBody
	}
	return "", tokenSourceNone
}

// setLoginCookies sets the app-specific cookie and issues a separate shared cookie token.
// For legacy clients (no X-Client-App), only the shared cookie is set.
func setLoginCookies(
	w nethttp.ResponseWriter, r *nethttp.Request,
	clientApp string,
	mainToken string, ttlSeconds int,
	issuer interface {
		IssueRefreshTokenOnly(ctx context.Context, userID uuid.UUID) (string, error)
	},
	userID uuid.UUID,
) {
	appCookie := appRefreshCookieName(clientApp)
	if appCookie != "" {
		setRefreshTokenCookie(w, r, appCookie, mainToken, ttlSeconds)
		if sharedToken, err := issuer.IssueRefreshTokenOnly(r.Context(), userID); err == nil {
			setRefreshTokenCookie(w, r, refreshTokenCookieName, sharedToken, ttlSeconds)
		}
	} else {
		setRefreshTokenCookie(w, r, refreshTokenCookieName, mainToken, ttlSeconds)
	}
}

// clearAuthCookies clears the app-specific cookie (if applicable) and the shared cookie.
func clearAuthCookies(w nethttp.ResponseWriter, r *nethttp.Request, clientApp string) {
	if appCookie := appRefreshCookieName(clientApp); appCookie != "" {
		clearRefreshTokenCookie(w, r, appCookie)
	}
	clearRefreshTokenCookie(w, r, refreshTokenCookieName)
}

func parseBearerToken(w nethttp.ResponseWriter, r *nethttp.Request, traceID string) (string, bool) {
	authorization := r.Header.Get("Authorization")
	if strings.TrimSpace(authorization) == "" {
		WriteError(w, nethttp.StatusUnauthorized, "auth.missing_token", "missing Authorization Bearer token", traceID, nil)
		return "", false
	}

	scheme, rest, ok := strings.Cut(authorization, " ")
	if !ok || strings.TrimSpace(rest) == "" || strings.ToLower(scheme) != "bearer" {
		WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_authorization", "Authorization header must be: Bearer <token>", traceID, nil)
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
	token, ok := parseBearerToken(w, r, traceID)
	if !ok {
		return nil, false
	}

	user, err := authService.AuthenticateUser(r.Context(), token)
	if err != nil {
		switch typed := err.(type) {
		case auth.TokenExpiredError:
			WriteError(w, nethttp.StatusUnauthorized, "auth.token_expired", typed.Error(), traceID, nil)
		case auth.TokenInvalidError:
			WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", typed.Error(), traceID, nil)
		case auth.UserNotFoundError:
			WriteError(w, nethttp.StatusUnauthorized, "auth.user_not_found", "user not found", traceID, nil)
		case auth.SuspendedUserError:
			WriteError(w, nethttp.StatusForbidden, "auth.user_suspended", "account suspended", traceID, nil)
		default:
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
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
	WriteError(w, nethttp.StatusMethodNotAllowed, "http.method_not_allowed", "Method Not Allowed", traceID, nil)
}

func writeAuthNotConfigured(w nethttp.ResponseWriter, traceID string) {
	WriteError(w, nethttp.StatusServiceUnavailable, "auth.not_configured", "auth not configured", traceID, nil)
}

type emailVerifyConfirmRequest struct {
	Token string `json:"token"`
}

func emailVerifySend(authService *auth.Service, emailVerifyService *auth.EmailVerifyService) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil || emailVerifyService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		user, ok := authenticateUser(w, r, traceID, authService)
		if !ok {
			return
		}

		if err := emailVerifyService.SendVerification(r.Context(), user.ID, user.Username); err != nil {
			if err.Error() == "user has no email address" {
				WriteError(w, nethttp.StatusUnprocessableEntity, "email.no_address", "user has no email address", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		w.WriteHeader(nethttp.StatusNoContent)
	}
}

func emailVerifyConfirm(emailVerifyService *auth.EmailVerifyService) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if emailVerifyService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		var body emailVerifyConfirmRequest
		if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Token) == "" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "token is required", traceID, nil)
			return
		}

		if err := emailVerifyService.ConfirmVerification(r.Context(), strings.TrimSpace(body.Token)); err != nil {
			var expired auth.TokenAlreadyUsedOrExpiredError
			if errors.As(err, &expired) {
				WriteError(w, nethttp.StatusUnprocessableEntity, "email.token_invalid", "token invalid or expired", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		writeJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
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
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if otpLoginService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		var body emailOTPSendRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		body.Email = strings.TrimSpace(body.Email)
		if body.Email == "" || len(body.Email) > 256 || !isValidEmail(body.Email) {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "valid email is required", traceID, nil)
			return
		}

		if !verifyTurnstileToken(w, r, traceID, body.CfTurnstileToken, resolver) {
			return
		}

		// 静默处理：无论邮箱是否存在都返回 204
		_ = otpLoginService.SendLoginOTP(r.Context(), body.Email)
		w.WriteHeader(nethttp.StatusNoContent)
	}
}

func emailOTPVerify(otpLoginService *auth.EmailOTPLoginService, authService *auth.Service, auditWriter *audit.Writer) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if otpLoginService == nil {
			writeAuthNotConfigured(w, traceID)
			return
		}

		var body emailOTPVerifyRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		body.Email = strings.TrimSpace(body.Email)
		body.Code = strings.TrimSpace(body.Code)
		if body.Email == "" || body.Code == "" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "email and code are required", traceID, nil)
			return
		}

		issued, err := otpLoginService.VerifyLoginOTP(r.Context(), body.Email, body.Code)
		if err != nil {
			var expired auth.OTPExpiredOrUsedError
			if errors.As(err, &expired) {
				WriteError(w, nethttp.StatusUnprocessableEntity, "auth.otp_invalid", "code invalid or expired", traceID, nil)
				return
			}
			var suspended auth.SuspendedUserError
			if errors.As(err, &suspended) {
				WriteError(w, nethttp.StatusForbidden, "auth.user_suspended", "account suspended", traceID, nil)
				return
			}
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		if auditWriter != nil {
			auditWriter.WriteLoginSucceeded(r.Context(), traceID, issued.UserID, body.Email)
		}

		setLoginCookies(w, r, resolveClientApp(r), issued.RefreshToken, otpLoginService.RefreshTokenTTLSeconds(), authService, issued.UserID)
		writeJSON(w, traceID, nethttp.StatusOK, loginResponse{
			AccessToken: issued.AccessToken,
			TokenType:   "bearer",
		})
	}
}

type checkUserRequest struct {
	Login string `json:"login"`
}

type checkUserResponse struct {
	Exists      bool   `json:"exists"`
	MaskedEmail string `json:"masked_email,omitempty"`
}

// maskEmail 脱敏：john.doe@example.com → j***e@example.com
func maskEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return email
	}
	local, domain := parts[0], parts[1]
	if len(local) <= 2 {
		return local[:1] + "***@" + domain
	}
	return local[:1] + "***" + local[len(local)-1:] + "@" + domain
}

func checkUser(credentialRepo *data.UserCredentialRepository, usersRepo *data.UserRepository) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if credentialRepo == nil || usersRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "service_unavailable", "service unavailable", traceID, nil)
			return
		}
		var body checkUserRequest
		if err := decodeJSON(r, &body); err != nil {
			WriteError(w, nethttp.StatusBadRequest, "bad_request", "invalid request body", traceID, nil)
			return
		}
		body.Login = strings.TrimSpace(body.Login)

		var maskedEmail string

		if cred, err := credentialRepo.GetByLogin(r.Context(), body.Login); err == nil && cred != nil {
			if user, err := usersRepo.GetByID(r.Context(), cred.UserID); err == nil && user != nil && user.Email != nil && *user.Email != "" {
				maskedEmail = maskEmail(*user.Email)
			}
		} else if cred, err := credentialRepo.GetByUserEmail(r.Context(), body.Login); err == nil && cred != nil {
			maskedEmail = maskEmail(body.Login)
		} else {
			// 用户不存在时生成占位 masked email，防止通过响应差异枚举用户
			if strings.Contains(body.Login, "@") {
				maskedEmail = maskEmail(body.Login)
			}
		}

		// 随机延时 50-150ms，防止时序攻击
		jitter, _ := rand.Int(rand.Reader, big.NewInt(100))
		time.Sleep(time.Duration(50+jitter.Int64()) * time.Millisecond)

		writeJSON(w, traceID, nethttp.StatusOK, checkUserResponse{Exists: true, MaskedEmail: maskedEmail})
	}
}
