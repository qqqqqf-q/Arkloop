//go:build desktop

package authapi

import (
	"context"
	"errors"
	"net"
	nethttp "net/http"
	"net/url"
	"strings"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func registerLocalSessionRoute(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("POST /v1/auth/local-session", localSession(deps.AuthService))
	mux.HandleFunc("POST /v1/auth/local-owner-password", localOwnerPassword(deps.AuthService, deps.Pool))
}

func localSession(authService *auth.Service) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if !localTrustRequestAllowed(r) {
			httpkit.WriteError(w, nethttp.StatusForbidden, "auth.local_trust_required", "local trust required", traceID, nil)
			return
		}
		token, ok := httpkit.ParseBearerToken(w, r, traceID)
		if !ok {
			return
		}
		if !auth.DesktopTokenMatches(token) {
			httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", "token invalid or expired", traceID, nil)
			return
		}

		issued, err := authService.IssueTokenPairForUser(r.Context(), auth.DesktopUserID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		setRefreshTokenCookie(w, r, refreshTokenCookieName, issued.RefreshToken, authService.RefreshTokenTTLSeconds())
		clearLegacyRefreshTokenCookies(w, r)
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, loginResponse{
			AccessToken: issued.AccessToken,
			TokenType:   "bearer",
		})
	}
}

type localOwnerPasswordRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func localOwnerPassword(authService *auth.Service, db data.DB) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if db == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}
		if !localTrustRequestAllowed(r) {
			httpkit.WriteError(w, nethttp.StatusForbidden, "auth.local_trust_required", "local trust required", traceID, nil)
			return
		}
		token, ok := httpkit.ParseBearerToken(w, r, traceID)
		if !ok {
			return
		}
		if !auth.DesktopTokenMatches(token) {
			httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", "token invalid or expired", traceID, nil)
			return
		}

		var body localOwnerPasswordRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		body.Username = strings.TrimSpace(body.Username)
		if body.Username == "" || body.Password == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		if err := auth.ValidateRegistrationPassword(body.Password); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}

		if err := setDesktopOwnerPasswordCredential(r.Context(), db, body.Username, body.Password); err != nil {
			if isLocalOwnerLoginConflict(err) {
				httpkit.WriteError(w, nethttp.StatusConflict, "auth.login_exists", "login already taken", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}

		issued, err := authService.IssueTokenPairForUser(r.Context(), auth.DesktopUserID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		setRefreshTokenCookie(w, r, refreshTokenCookieName, issued.RefreshToken, authService.RefreshTokenTTLSeconds())
		clearLegacyRefreshTokenCookies(w, r)
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, loginResponse{
			AccessToken: issued.AccessToken,
			TokenType:   "bearer",
		})
	}
}

func setDesktopOwnerPasswordCredential(ctx context.Context, db data.DB, username string, password string) error {
	hasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		return err
	}
	hash, err := hasher.HashPassword(password)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `UPDATE users SET username = $1 WHERE id = $2`, username, auth.DesktopUserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return auth.UserNotFoundError{UserID: auth.DesktopUserID}
	}

	if _, err = tx.Exec(ctx, `DELETE FROM user_credentials WHERE user_id = $1`, auth.DesktopUserID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO user_credentials (user_id, login, password_hash) VALUES ($1, $2, $3)`,
		auth.DesktopUserID, username, hash,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func isLocalOwnerLoginConflict(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") &&
		strings.Contains(msg, "user_credentials") &&
		strings.Contains(msg, "login")
}

func localTrustRequestAllowed(r *nethttp.Request) bool {
	if !isLoopbackHost(r.Host) {
		return false
	}
	if !localForwardedHeadersAllowed(r.Header) {
		return false
	}
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}
	ip := net.ParseIP(strings.TrimSpace(remoteHost))
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	return localTrustHeaderAllowed(r.Header.Get("Origin")) &&
		localTrustHeaderAllowed(r.Header.Get("Referer"))
}

func localForwardedHeadersAllowed(header nethttp.Header) bool {
	if !forwardedHostValuesAllowed(header.Values("X-Forwarded-Host")) {
		return false
	}
	if !forwardedIPValuesAllowed(header.Values("X-Forwarded-For")) {
		return false
	}
	if !forwardedIPValuesAllowed(header.Values("X-Real-IP")) {
		return false
	}
	for _, value := range header.Values("Forwarded") {
		for _, part := range strings.Split(value, ",") {
			for _, param := range strings.Split(part, ";") {
				key, raw, ok := strings.Cut(param, "=")
				if !ok {
					continue
				}
				switch strings.ToLower(strings.TrimSpace(key)) {
				case "host":
					if !forwardedHostAllowed(raw) {
						return false
					}
				case "for":
					if !forwardedIPAllowed(raw) {
						return false
					}
				}
			}
		}
	}
	return true
}

func forwardedHostValuesAllowed(values []string) bool {
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			if !forwardedHostAllowed(raw) {
				return false
			}
		}
	}
	return true
}

func forwardedHostAllowed(raw string) bool {
	host := trimForwardedHeaderValue(raw)
	if host == "" {
		return true
	}
	return isLoopbackHost(host)
}

func forwardedIPValuesAllowed(values []string) bool {
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			if !forwardedIPAllowed(raw) {
				return false
			}
		}
	}
	return true
}

func forwardedIPAllowed(raw string) bool {
	host := trimForwardedHeaderValue(raw)
	if host == "" {
		return true
	}
	if strings.EqualFold(host, "unknown") {
		return false
	}
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func trimForwardedHeaderValue(raw string) string {
	return strings.Trim(strings.TrimSpace(raw), `"`)
}

func localTrustHeaderAllowed(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return true
	}
	return isLoopbackHost(parsed.Host)
}

func isLoopbackHost(hostport string) bool {
	host := strings.TrimSpace(hostport)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
