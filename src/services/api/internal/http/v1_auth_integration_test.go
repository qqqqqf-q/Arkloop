package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	nethttp "net/http"
	"net/http/httptest"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/testutil"
)

func TestAuthRegisterLoginRefreshLogoutFlow(t *testing.T) {
	db := setupTestDatabase(t, "api_go_auth")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	logger := observability.NewJSONLogger("test", io.Discard)

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credentialRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("new credential repo: %v", err)
	}
	membershipRepo, err := data.NewOrgMembershipRepository(pool)
	if err != nil {
		t.Fatalf("new membership repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("new refresh token repo: %v", err)
	}
	auditRepo, err := data.NewAuditLogRepository(pool)
	if err != nil {
		t.Fatalf("new audit repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	jobRepo, err := data.NewJobRepository(pool)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	if err != nil {
		t.Fatalf("new registration service: %v", err)
	}

	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		AuditWriter:         auditWriter,
		OrgMembershipRepo:   membershipRepo,
	})

	registerBody := map[string]any{"login": "alice", "password": "pwd12345", "email": "alice@test.com"}
	registerResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register", registerBody, nil)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected register status: %d body=%s", registerResp.Code, registerResp.Body.String())
	}
	registerPayload := decodeJSONBody[registerResponse](t, registerResp.Body.Bytes())
	if registerPayload.TokenType != "bearer" || registerPayload.AccessToken == "" || registerPayload.UserID == "" {
		t.Fatalf("unexpected register payload: %#v", registerPayload)
	}

	dupResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register", registerBody, nil)
	assertErrorEnvelope(t, dupResp, nethttp.StatusConflict, "auth.login_exists")

	missingMe := doJSON(handler, nethttp.MethodGet, "/v1/me", nil, nil)
	assertErrorEnvelope(t, missingMe, nethttp.StatusUnauthorized, "auth.missing_token")

	meResp := doJSON(handler, nethttp.MethodGet, "/v1/me", nil, authHeader(registerPayload.AccessToken))
	if meResp.Code != nethttp.StatusOK {
		t.Fatalf("unexpected me status: %d body=%s", meResp.Code, meResp.Body.String())
	}

	loginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login", map[string]any{"login": "alice", "password": "pwd12345"}, nil)
	if loginResp.Code != nethttp.StatusOK {
		t.Fatalf("unexpected login status: %d body=%s", loginResp.Code, loginResp.Body.String())
	}
	loginPayload := decodeJSONBody[loginResponse](t, loginResp.Body.Bytes())
	if loginPayload.AccessToken == "" || loginPayload.TokenType != "bearer" {
		t.Fatalf("unexpected login payload: %#v", loginPayload)
	}

	refreshResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/refresh", nil, map[string]string{
		"Cookie": refreshTokenCookieHeader(t, loginResp),
	})
	if refreshResp.Code != nethttp.StatusOK {
		t.Fatalf("unexpected refresh status: %d body=%s", refreshResp.Code, refreshResp.Body.String())
	}
	refreshPayload := decodeJSONBody[loginResponse](t, refreshResp.Body.Bytes())
	if refreshPayload.AccessToken == "" || refreshPayload.TokenType != "bearer" {
		t.Fatalf("unexpected refresh payload: %#v", refreshPayload)
	}

	logoutResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/logout", nil, authHeader(refreshPayload.AccessToken))
	if logoutResp.Code != nethttp.StatusOK {
		t.Fatalf("unexpected logout status: %d body=%s", logoutResp.Code, logoutResp.Body.String())
	}
	logoutPayload := decodeJSONBody[logoutResponse](t, logoutResp.Body.Bytes())
	if !logoutPayload.OK {
		t.Fatalf("unexpected logout payload: %#v", logoutPayload)
	}

	meAfterLogout := doJSON(handler, nethttp.MethodGet, "/v1/me", nil, authHeader(refreshPayload.AccessToken))
	assertErrorEnvelope(t, meAfterLogout, nethttp.StatusUnauthorized, "auth.invalid_token")

	actions, err := countAuditActions(ctx, pool)
	if err != nil {
		t.Fatalf("count audit actions: %v", err)
	}
	for _, action := range []string{"auth.register", "auth.login", "auth.refresh", "auth.logout"} {
		if actions[action] != 1 {
			t.Fatalf("unexpected audit count: action=%s count=%d actions=%#v", action, actions[action], actions)
		}
	}
}

func TestAuthRegisterRejectsWeakPasswords(t *testing.T) {
	db := setupTestDatabase(t, "api_go_auth_password_policy")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	logger := observability.NewJSONLogger("test", io.Discard)
	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credentialRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("new credential repo: %v", err)
	}
	membershipRepo, err := data.NewOrgMembershipRepository(pool)
	if err != nil {
		t.Fatalf("new membership repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("new refresh token repo: %v", err)
	}
	auditRepo, err := data.NewAuditLogRepository(pool)
	if err != nil {
		t.Fatalf("new audit repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	jobRepo, err := data.NewJobRepository(pool)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	if err != nil {
		t.Fatalf("new registration service: %v", err)
	}

	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)
	handler := NewHandler(HandlerConfig{
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		AuditWriter:         auditWriter,
		OrgMembershipRepo:   membershipRepo,
	})

	cases := []struct {
		name     string
		login    string
		email    string
		password string
	}{
		{name: "letters_only", login: "letters-only", email: "letters-only@test.com", password: "abcdefgh"},
		{name: "digits_only", login: "digits-only", email: "digits-only@test.com", password: "12345678"},
		{name: "too_short", login: "too-short", email: "too-short@test.com", password: "abc123"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register", map[string]any{
				"login":    tc.login,
				"password": tc.password,
				"email":    tc.email,
			}, nil)
			assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation.error")

			payload := decodeJSONBody[ErrorEnvelope](t, resp.Body.Bytes())
			if payload.Message != "password must be 8-72 characters and include letters and numbers" {
				t.Fatalf("unexpected message: %q", payload.Message)
			}
		})
	}

	strongResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register", map[string]any{
		"login":    "strong-user",
		"password": "abc12345",
		"email":    "strong-user@test.com",
	}, nil)
	if strongResp.Code != nethttp.StatusCreated {
		t.Fatalf("unexpected register status: %d body=%s", strongResp.Code, strongResp.Body.String())
	}
}

func TestAuthLoginAllowsLegacyWeakPassword(t *testing.T) {
	db := setupTestDatabase(t, "api_go_auth_legacy_password")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	logger := observability.NewJSONLogger("test", io.Discard)
	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credentialRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("new credential repo: %v", err)
	}
	membershipRepo, err := data.NewOrgMembershipRepository(pool)
	if err != nil {
		t.Fatalf("new membership repo: %v", err)
	}
	orgRepo, err := data.NewOrgRepository(pool)
	if err != nil {
		t.Fatalf("new org repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("new refresh token repo: %v", err)
	}
	auditRepo, err := data.NewAuditLogRepository(pool)
	if err != nil {
		t.Fatalf("new audit repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	jobRepo, err := data.NewJobRepository(pool)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	if err != nil {
		t.Fatalf("new registration service: %v", err)
	}

	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)
	handler := NewHandler(HandlerConfig{
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		AuditWriter:         auditWriter,
		OrgMembershipRepo:   membershipRepo,
	})

	legacyUser, err := userRepo.Create(ctx, "legacy-user", "legacy-user@test.com", "")
	if err != nil {
		t.Fatalf("create legacy user: %v", err)
	}
	legacyHash, err := passwordHasher.HashPassword("abcdefgh")
	if err != nil {
		t.Fatalf("hash legacy password: %v", err)
	}
	if _, err := credentialRepo.Create(ctx, legacyUser.ID, "legacy-user", legacyHash); err != nil {
		t.Fatalf("create legacy credential: %v", err)
	}
	legacyOrg, err := orgRepo.Create(ctx, "personal-legacy-user", "legacy-user's workspace", "personal")
	if err != nil {
		t.Fatalf("create legacy org: %v", err)
	}
	if _, err := membershipRepo.Create(ctx, legacyOrg.ID, legacyUser.ID, "owner"); err != nil {
		t.Fatalf("create legacy membership: %v", err)
	}

	loginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login", map[string]any{
		"login":    "legacy-user",
		"password": "abcdefgh",
	}, nil)
	if loginResp.Code != nethttp.StatusOK {
		t.Fatalf("unexpected login status: %d body=%s", loginResp.Code, loginResp.Body.String())
	}
	loginPayload := decodeJSONBody[loginResponse](t, loginResp.Body.Bytes())
	if loginPayload.AccessToken == "" || loginPayload.TokenType != "bearer" {
		t.Fatalf("unexpected login payload: %#v", loginPayload)
	}
}

func TestAuthMeRequiresMembership(t *testing.T) {
	db := setupTestDatabase(t, "api_go_auth_me_membership")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	logger := observability.NewJSONLogger("test", io.Discard)

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credentialRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("new credential repo: %v", err)
	}
	membershipRepo, err := data.NewOrgMembershipRepository(pool)
	if err != nil {
		t.Fatalf("new membership repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("new refresh token repo: %v", err)
	}
	auditRepo, err := data.NewAuditLogRepository(pool)
	if err != nil {
		t.Fatalf("new audit repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	jobRepo, err := data.NewJobRepository(pool)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	if err != nil {
		t.Fatalf("new registration service: %v", err)
	}

	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		AuditWriter:         auditWriter,
		OrgMembershipRepo:   membershipRepo,
	})

	registerResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "membershipless", "password": "pwd12345", "email": "membershipless@test.com"}, nil)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", registerResp.Code, registerResp.Body.String())
	}
	registerPayload := decodeJSONBody[registerResponse](t, registerResp.Body.Bytes())

	_, err = pool.Exec(ctx, `DELETE FROM org_memberships m
		USING orgs o
		WHERE m.org_id = o.id
		  AND m.user_id = $1::uuid
		  AND o.type = 'personal'`, registerPayload.UserID)
	if err != nil {
		t.Fatalf("delete personal membership: %v", err)
	}

	meResp := doJSON(handler, nethttp.MethodGet, "/v1/me", nil, authHeader(registerPayload.AccessToken))
	assertErrorEnvelope(t, meResp, nethttp.StatusForbidden, "auth.no_org_membership")
}

// TestAuthLogoutThenReLoginNewTokenStillValid verifies that a new token obtained
// by re-login/refresh right after logout is still valid.
// Guard: iat is stored as float64 seconds in JWT, tokens_invalid_before is stored
// as Postgres TIMESTAMPTZ (microsecond precision), comparison uses iat.Before(tokens_invalid_before)
// (strict less-than). Float64 round-trip may lose nanosecond precision; if the new iat
// is truncated to before the logout timestamp, the new token would be incorrectly rejected.
func TestAuthLogoutThenReLoginNewTokenStillValid(t *testing.T) {
	db := setupTestDatabase(t, "api_go_auth_relogin")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	logger := observability.NewJSONLogger("test", io.Discard)

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credentialRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("new credential repo: %v", err)
	}
	membershipRepo, err := data.NewOrgMembershipRepository(pool)
	if err != nil {
		t.Fatalf("new membership repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("new refresh token repo: %v", err)
	}
	auditRepo, err := data.NewAuditLogRepository(pool)
	if err != nil {
		t.Fatalf("new audit repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	jobRepo, err := data.NewJobRepository(pool)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	if err != nil {
		t.Fatalf("new registration service: %v", err)
	}

	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		AuditWriter:         auditWriter,
		OrgMembershipRepo:   membershipRepo,
	})

	// register
	registerResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "carol", "password": "pwd12345", "email": "carol@test.com"}, nil)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", registerResp.Code, registerResp.Body.String())
	}

	// first login to get tokenA
	loginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login",
		map[string]any{"login": "carol", "password": "pwd12345"}, nil)
	if loginResp.Code != nethttp.StatusOK {
		t.Fatalf("login: %d %s", loginResp.Code, loginResp.Body.String())
	}
	tokenA := decodeJSONBody[loginResponse](t, loginResp.Body.Bytes()).AccessToken

	// logout (triggers tokens_invalid_before = now())
	logoutResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/logout", nil, authHeader(tokenA))
	if logoutResp.Code != nethttp.StatusOK {
		t.Fatalf("logout: %d %s", logoutResp.Code, logoutResp.Body.String())
	}

	// old tokenA must be invalid
	assertErrorEnvelope(t, doJSON(handler, nethttp.MethodGet, "/v1/me", nil, authHeader(tokenA)),
		nethttp.StatusUnauthorized, "auth.invalid_token")

	// immediately re-login to get tokenB (iat right after logout, precision boundary scenario)
	reLoginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login",
		map[string]any{"login": "carol", "password": "pwd12345"}, nil)
	if reLoginResp.Code != nethttp.StatusOK {
		t.Fatalf("re-login: %d %s", reLoginResp.Code, reLoginResp.Body.String())
	}
	tokenB := decodeJSONBody[loginResponse](t, reLoginResp.Body.Bytes()).AccessToken

	// tokenB must be valid
	meB := doJSON(handler, nethttp.MethodGet, "/v1/me", nil, authHeader(tokenB))
	if meB.Code != nethttp.StatusOK {
		t.Fatalf("me with tokenB after re-login: %d %s", meB.Code, meB.Body.String())
	}

	// refresh tokenB to get tokenC (refresh also goes through AuthenticateUser)
	refreshResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/refresh", nil, map[string]string{
		"Cookie": refreshTokenCookieHeader(t, reLoginResp),
	})
	if refreshResp.Code != nethttp.StatusOK {
		t.Fatalf("refresh tokenB: %d %s", refreshResp.Code, refreshResp.Body.String())
	}
	tokenC := decodeJSONBody[loginResponse](t, refreshResp.Body.Bytes()).AccessToken

	// tokenC must be valid
	meC := doJSON(handler, nethttp.MethodGet, "/v1/me", nil, authHeader(tokenC))
	if meC.Code != nethttp.StatusOK {
		t.Fatalf("me with tokenC after refresh: %d %s", meC.Code, meC.Body.String())
	}
}

func TestAuthCookieIsolation(t *testing.T) {
	db := setupTestDatabase(t, "api_go_auth_cookie_iso")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	logger := observability.NewJSONLogger("test", io.Discard)
	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	userRepo, _ := data.NewUserRepository(pool)
	credentialRepo, _ := data.NewUserCredentialRepository(pool)
	membershipRepo, _ := data.NewOrgMembershipRepository(pool)
	refreshTokenRepo, _ := data.NewRefreshTokenRepository(pool)
	auditRepo, _ := data.NewAuditLogRepository(pool)
	jobRepo, _ := data.NewJobRepository(pool)

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	if err != nil {
		t.Fatalf("new registration service: %v", err)
	}

	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)
	handler := NewHandler(HandlerConfig{
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		AuditWriter:         auditWriter,
		OrgMembershipRepo:   membershipRepo,
	})

	// register two users
	doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "alice", "password": "pwd12345", "email": "alice@test.com"}, nil)
	doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "bob", "password": "pwd12345", "email": "bob@test.com"}, nil)

	// login alice on web app
	webLoginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login",
		map[string]any{"login": "alice", "password": "pwd12345"},
		map[string]string{clientAppHeader: "web"})
	if webLoginResp.Code != nethttp.StatusOK {
		t.Fatalf("web login alice: %d %s", webLoginResp.Code, webLoginResp.Body.String())
	}

	// verify both app-specific and shared cookies are set
	webAppCookie := findRefreshCookie(t, webLoginResp, "arkloop_rt_web")
	sharedCookie := findRefreshCookie(t, webLoginResp, refreshTokenCookieName)

	// login bob on console app
	consoleLoginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login",
		map[string]any{"login": "bob", "password": "pwd12345"},
		map[string]string{clientAppHeader: "console"})
	if consoleLoginResp.Code != nethttp.StatusOK {
		t.Fatalf("console login bob: %d %s", consoleLoginResp.Code, consoleLoginResp.Body.String())
	}
	_ = findRefreshCookie(t, consoleLoginResp, "arkloop_rt_console")
	_ = findRefreshCookie(t, consoleLoginResp, refreshTokenCookieName)

	// refresh on web using alice's app-specific cookie -> still alice
	webRefreshResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/refresh", nil,
		map[string]string{
			clientAppHeader: "web",
			"Cookie":        webAppCookie,
		})
	if webRefreshResp.Code != nethttp.StatusOK {
		t.Fatalf("web refresh: %d %s", webRefreshResp.Code, webRefreshResp.Body.String())
	}
	webToken := decodeJSONBody[loginResponse](t, webRefreshResp.Body.Bytes())
	meWeb := doJSON(handler, nethttp.MethodGet, "/v1/me", nil, authHeader(webToken.AccessToken))
	if meWeb.Code != nethttp.StatusOK {
		t.Fatalf("me web after refresh: %d %s", meWeb.Code, meWeb.Body.String())
	}

	// no X-Client-App -> legacy behavior, uses shared cookie
	legacyRefreshResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/refresh", nil,
		map[string]string{"Cookie": sharedCookie})
	if legacyRefreshResp.Code != nethttp.StatusOK {
		t.Fatalf("legacy refresh: %d %s", legacyRefreshResp.Code, legacyRefreshResp.Body.String())
	}
	_ = findRefreshCookie(t, legacyRefreshResp, refreshTokenCookieName)

	// console-lite fallback: no arkloop_rt_console_lite, should fallback to shared cookie
	// (shared cookie was updated in the legacy refresh above)
	newSharedCookie := findRefreshCookie(t, legacyRefreshResp, refreshTokenCookieName)
	consoleLiteRefreshResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/refresh", nil,
		map[string]string{
			clientAppHeader: "console-lite",
			"Cookie":        newSharedCookie,
		})
	if consoleLiteRefreshResp.Code != nethttp.StatusOK {
		t.Fatalf("console-lite fallback refresh: %d %s", consoleLiteRefreshResp.Code, consoleLiteRefreshResp.Body.String())
	}
	// should now have app-specific cookie for console-lite
	_ = findRefreshCookie(t, consoleLiteRefreshResp, "arkloop_rt_console_lite")
}

func setupTestDatabase(t *testing.T, prefix string) *testutil.PostgresDatabase {
	t.Helper()
	db := testutil.SetupPostgresDatabase(t, prefix)
	if _, err := migrate.Up(context.Background(), db.DSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func doJSON(handler nethttp.Handler, method string, path string, payload any, headers map[string]string) *httptest.ResponseRecorder {
	var body io.Reader
	if payload != nil && method != nethttp.MethodGet {
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func decodeJSONBody[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var dst T
	if err := json.Unmarshal(raw, &dst); err != nil {
		t.Fatalf("decode json: %v raw=%s", err, string(raw))
	}
	return dst
}

func assertErrorEnvelope(t *testing.T, recorder *httptest.ResponseRecorder, statusCode int, code string) {
	t.Helper()

	if recorder.Code != statusCode {
		t.Fatalf("unexpected status: %d raw=%s", recorder.Code, recorder.Body.String())
	}
	traceID := recorder.Header().Get(observability.TraceIDHeader)
	if traceID == "" {
		t.Fatalf("missing %s header", observability.TraceIDHeader)
	}

	var payload ErrorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != code {
		t.Fatalf("unexpected code: %q raw=%s", payload.Code, recorder.Body.String())
	}
	if payload.TraceID != traceID {
		t.Fatalf("trace_id mismatch: header=%q payload=%q", traceID, payload.TraceID)
	}
}

func authHeader(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func refreshTokenCookieHeader(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()
	return findRefreshCookie(t, resp, refreshTokenCookieName)
}

func findRefreshCookie(t *testing.T, resp *httptest.ResponseRecorder, name string) string {
	t.Helper()
	for _, cookie := range resp.Result().Cookies() {
		if cookie.Name == name && strings.TrimSpace(cookie.Value) != "" {
			return cookie.Name + "=" + cookie.Value
		}
	}
	t.Fatalf("missing %s cookie", name)
	return ""
}

func countAuditActions(ctx context.Context, db data.Querier) (map[string]int, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	counts := map[string]int{}
	for _, action := range []string{"auth.register", "auth.login", "auth.refresh", "auth.logout"} {
		var count int
		if err := db.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE action = $1", action).Scan(&count); err != nil {
			return nil, err
		}
		counts[action] = count
	}
	return counts, nil
}
