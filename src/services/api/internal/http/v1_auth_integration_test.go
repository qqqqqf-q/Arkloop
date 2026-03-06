package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
	pool, err := data.NewPool(ctx, db.DSN)
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
	})

	registerBody := map[string]any{"login": "alice", "password": "pwdpwdpwd", "email": "alice@test.com"}
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

	loginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login", map[string]any{"login": "alice", "password": "pwdpwdpwd"}, nil)
	if loginResp.Code != nethttp.StatusOK {
		t.Fatalf("unexpected login status: %d body=%s", loginResp.Code, loginResp.Body.String())
	}
	loginPayload := decodeJSONBody[loginResponse](t, loginResp.Body.Bytes())
	if loginPayload.AccessToken == "" || loginPayload.TokenType != "bearer" {
		t.Fatalf("unexpected login payload: %#v", loginPayload)
	}

	refreshResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/refresh", nil, authHeader(loginPayload.AccessToken))
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

// TestAuthLogoutThenReLoginNewTokenStillValid verifies that a new token obtained
// by re-login/refresh right after logout is still valid.
// Guard: iat is stored as float64 seconds in JWT, tokens_invalid_before is stored
// as Postgres TIMESTAMPTZ (microsecond precision), comparison uses iat.Before(tokens_invalid_before)
// (strict less-than). Float64 round-trip may lose nanosecond precision; if the new iat
// is truncated to before the logout timestamp, the new token would be incorrectly rejected.
func TestAuthLogoutThenReLoginNewTokenStillValid(t *testing.T) {
	db := setupTestDatabase(t, "api_go_auth_relogin")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN)
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
	})

	// register
	registerResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "carol", "password": "pwdpwdpwd", "email": "carol@test.com"}, nil)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", registerResp.Code, registerResp.Body.String())
	}

	// first login to get tokenA
	loginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login",
		map[string]any{"login": "carol", "password": "pwdpwdpwd"}, nil)
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
		map[string]any{"login": "carol", "password": "pwdpwdpwd"}, nil)
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
	refreshResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/refresh", nil, authHeader(tokenB))
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
