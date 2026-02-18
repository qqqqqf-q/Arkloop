package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	nethttp "net/http"
	"net/http/httptest"

	"arkloop/services/api_go/internal/audit"
	"arkloop/services/api_go/internal/auth"
	"arkloop/services/api_go/internal/data"
	"arkloop/services/api_go/internal/observability"
	"arkloop/services/api_go/internal/testutil"
)

func TestAuthRegisterLoginRefreshLogoutFlow(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_auth")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	if err := setupAuthSchema(ctx, pool); err != nil {
		t.Fatalf("setup schema: %v", err)
	}

	logger := observability.NewJSONLogger("test", io.Discard)

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600)
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
	auditRepo, err := data.NewAuditLogRepository(pool)
	if err != nil {
		t.Fatalf("new audit repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, passwordHasher, tokenService)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService)
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

	registerBody := map[string]any{"login": "alice", "password": "pwdpwdpwd", "display_name": "Alice"}
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

func setupAuthSchema(ctx context.Context, db data.Querier) error {
	if ctx == nil {
		ctx = context.Background()
	}

	statements := []string{
		"CREATE EXTENSION IF NOT EXISTS pgcrypto",
		`CREATE TABLE orgs (
		   id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		   slug TEXT NOT NULL,
		   name TEXT NOT NULL,
		   created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		   CONSTRAINT uq_orgs_slug UNIQUE (slug)
		 )`,
		`CREATE TABLE users (
		   id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		   display_name TEXT NOT NULL,
		   tokens_invalid_before TIMESTAMPTZ NOT NULL DEFAULT to_timestamp(0),
		   created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		 )`,
		`CREATE TABLE user_credentials (
		   id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		   user_id UUID NOT NULL,
		   login TEXT NOT NULL,
		   password_hash TEXT NOT NULL,
		   created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		   CONSTRAINT fk_user_credentials_user_id_users FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
		   CONSTRAINT uq_user_credentials_user_id UNIQUE (user_id),
		   CONSTRAINT uq_user_credentials_login UNIQUE (login)
		 )`,
		`CREATE TABLE org_memberships (
		   id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		   org_id UUID NOT NULL,
		   user_id UUID NOT NULL,
		   role TEXT NOT NULL DEFAULT 'member',
		   created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		   CONSTRAINT uq_org_memberships_org_id_user_id UNIQUE (org_id, user_id),
		   CONSTRAINT fk_org_memberships_org_id_orgs FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
		   CONSTRAINT fk_org_memberships_user_id_users FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		 )`,
		`CREATE TABLE audit_logs (
		   id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		   org_id UUID NULL,
		   actor_user_id UUID NULL,
		   action TEXT NOT NULL,
		   target_type TEXT NULL,
		   target_id TEXT NULL,
		   ts TIMESTAMPTZ NOT NULL DEFAULT now(),
		   trace_id TEXT NOT NULL,
		   metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
		   CONSTRAINT fk_audit_logs_org_id_orgs FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
		   CONSTRAINT fk_audit_logs_actor_user_id_users FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL
		 )`,
	}

	for _, stmt := range statements {
		if _, err := db.Exec(ctx, stmt); err != nil {
			return err
		}
	}

	return nil
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
