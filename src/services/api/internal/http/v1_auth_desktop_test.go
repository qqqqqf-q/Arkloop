//go:build desktop

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
)

func TestDesktopMeUsesOSUsername(t *testing.T) {
	t.Setenv("ARKLOOP_DESKTOP_TOKEN", "desktop-test-token")
	t.Setenv("ARKLOOP_DESKTOP_OS_USERNAME", "qqqqqf")

	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	handler := newDesktopAuthHandler(t, pool)
	accessToken := issueDesktopLocalSessionAccessToken(t, handler)

	req := httptest.NewRequest(nethttp.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Username != "qqqqqf" {
		t.Fatalf("expected username qqqqqf, got %q", body.Username)
	}
}

func TestDesktopSeedDoesNotOverwriteEditedUsername(t *testing.T) {
	t.Setenv("ARKLOOP_DESKTOP_TOKEN", "desktop-test-token")
	t.Setenv("ARKLOOP_DESKTOP_OS_USERNAME", "qqqqqf")

	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET username = $1 WHERE id = $2`, "edited_name", auth.DesktopUserID); err != nil {
		t.Fatalf("edit desktop username: %v", err)
	}

	t.Setenv("ARKLOOP_DESKTOP_OS_USERNAME", "os_changed")
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user again: %v", err)
	}

	handler := newDesktopAuthHandler(t, pool)
	accessToken := issueDesktopLocalSessionAccessToken(t, handler)

	req := httptest.NewRequest(nethttp.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Username != "edited_name" {
		t.Fatalf("expected username edited_name, got %q", body.Username)
	}
}

func TestDesktopRawBearerTokenIsUnauthorized(t *testing.T) {
	t.Setenv("ARKLOOP_DESKTOP_TOKEN", "desktop-test-token")

	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	handler := newDesktopAuthHandler(t, pool)

	req := httptest.NewRequest(nethttp.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+auth.DesktopToken())
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != nethttp.StatusUnauthorized {
		t.Fatalf("status = %d, want %d body = %s", rec.Code, nethttp.StatusUnauthorized, rec.Body.String())
	}
}

func TestDesktopResolvePasswordUnsetReturnsSetupRequired(t *testing.T) {
	t.Setenv("ARKLOOP_DESKTOP_OS_USERNAME", "desktop-owner")

	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}
	handler := newDesktopAuthHandler(t, pool)

	for _, identity := range []string{"desktop-owner", "someone-else"} {
		t.Run(identity, func(t *testing.T) {
			body := bytes.NewBufferString(`{"identity":"` + identity + `"}`)
			req := httptest.NewRequest(nethttp.MethodPost, "/v1/auth/resolve", body)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != nethttp.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			var resp struct {
				NextStep string `json:"next_step"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.NextStep != "setup_required" {
				t.Fatalf("next_step = %q, want setup_required", resp.NextStep)
			}
		})
	}
}

func TestDesktopLocalOwnerPasswordSetsCredentialAndReturnsJWT(t *testing.T) {
	t.Setenv("ARKLOOP_DESKTOP_TOKEN", "desktop-test-token")

	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}
	handler := newDesktopAuthHandler(t, pool)

	setupBody := bytes.NewBufferString(`{"username":"owner-web","password":"abc12345"}`)
	setupReq := httptest.NewRequest(nethttp.MethodPost, "/v1/auth/local-owner-password", setupBody)
	setLocalTrustRequest(setupReq)
	setupReq.Header.Set("Authorization", "Bearer "+auth.DesktopToken())
	setupReq.Header.Set("Content-Type", "application/json")
	setupRec := httptest.NewRecorder()

	handler.ServeHTTP(setupRec, setupReq)

	if setupRec.Code != nethttp.StatusOK {
		t.Fatalf("setup status = %d, body = %s", setupRec.Code, setupRec.Body.String())
	}
	var setupResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(setupRec.Body.Bytes(), &setupResp); err != nil {
		t.Fatalf("decode setup response: %v", err)
	}
	if setupResp.AccessToken == "" {
		t.Fatal("setup response missing access token")
	}

	meReq := httptest.NewRequest(nethttp.MethodGet, "/v1/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+setupResp.AccessToken)
	meRec := httptest.NewRecorder()
	handler.ServeHTTP(meRec, meReq)
	if meRec.Code != nethttp.StatusOK {
		t.Fatalf("me status = %d, body = %s", meRec.Code, meRec.Body.String())
	}

	loginBody := bytes.NewBufferString(`{"login":"owner-web","password":"abc12345"}`)
	loginReq := httptest.NewRequest(nethttp.MethodPost, "/v1/auth/login", loginBody)
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != nethttp.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginRec.Code, loginRec.Body.String())
	}
}

func TestDesktopResolveActorSupportsAPIKey(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}
	ensureAPIKeyColumn(t, ctx, pool, "scopes", "TEXT NOT NULL DEFAULT '[]'")
	ensureAPIKeyColumn(t, ctx, pool, "revoked_at", "TEXT")

	membershipRepo, err := data.NewAccountMembershipRepository(pool)
	if err != nil {
		t.Fatalf("new membership repo: %v", err)
	}
	apiKeysRepo, err := data.NewAPIKeysRepository(pool)
	if err != nil {
		t.Fatalf("new api keys repo: %v", err)
	}
	_, rawKey, err := apiKeysRepo.Create(ctx, auth.DesktopAccountID, auth.DesktopUserID, "desktop test", []string{auth.PermDataThreadsRead})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	req := httptest.NewRequest(nethttp.MethodGet, "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	actor, ok := resolveActor(rec, req, "trace", nil, membershipRepo, apiKeysRepo, nil)
	if !ok {
		t.Fatalf("resolve actor failed: status = %d body = %s", rec.Code, rec.Body.String())
	}
	if actor.AccountID != auth.DesktopAccountID || actor.UserID != auth.DesktopUserID {
		t.Fatalf("unexpected actor account/user: %s/%s", actor.AccountID, actor.UserID)
	}
	if !hasPermission(actor.Permissions, auth.PermDataThreadsRead) {
		t.Fatalf("expected api key scope %q in permissions: %#v", auth.PermDataThreadsRead, actor.Permissions)
	}
	if hasPermission(actor.Permissions, auth.PermDataThreadsWrite) {
		t.Fatalf("unexpected permission outside api key scope: %#v", actor.Permissions)
	}
}

func TestDesktopLocalSessionRequiresLocalTrustRequest(t *testing.T) {
	t.Setenv("ARKLOOP_DESKTOP_TOKEN", "desktop-test-token")

	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}
	handler := newDesktopAuthHandler(t, pool)

	tests := []struct {
		name     string
		host     string
		remote   string
		origin   string
		forward  string
		wantCode int
	}{
		{name: "local", host: "127.0.0.1:19001", remote: "127.0.0.1:50000", wantCode: nethttp.StatusOK},
		{name: "lan host", host: "192.168.1.10:19001", remote: "127.0.0.1:50000", wantCode: nethttp.StatusForbidden},
		{name: "remote address", host: "127.0.0.1:19001", remote: "192.168.1.5:50000", wantCode: nethttp.StatusForbidden},
		{name: "public origin", host: "127.0.0.1:19001", remote: "127.0.0.1:50000", origin: "https://example.com", wantCode: nethttp.StatusForbidden},
		{name: "forwarded public client", host: "127.0.0.1:19001", remote: "127.0.0.1:50000", forward: "203.0.113.10", wantCode: nethttp.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(nethttp.MethodPost, "/v1/auth/local-session", nil)
			req.Host = tt.host
			req.RemoteAddr = tt.remote
			req.Header.Set("Authorization", "Bearer "+auth.DesktopToken())
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.forward != "" {
				req.Header.Set("X-Forwarded-For", tt.forward)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d body = %s", rec.Code, tt.wantCode, rec.Body.String())
			}
		})
	}
}

func TestDesktopWrongBearerTokenIsUnauthorized(t *testing.T) {
	t.Setenv("ARKLOOP_DESKTOP_TOKEN", "desktop-test-token")

	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	handler := newDesktopAuthHandler(t, pool)

	req := httptest.NewRequest(nethttp.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != nethttp.StatusUnauthorized {
		t.Fatalf("status = %d, want %d body = %s", rec.Code, nethttp.StatusUnauthorized, rec.Body.String())
	}
}

func issueDesktopLocalSessionAccessToken(t *testing.T, handler nethttp.Handler) string {
	t.Helper()
	t.Setenv("ARKLOOP_DESKTOP_TOKEN", "desktop-test-token")

	req := httptest.NewRequest(nethttp.MethodPost, "/v1/auth/local-session", nil)
	setLocalTrustRequest(req)
	req.Header.Set("Authorization", "Bearer "+auth.DesktopToken())
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != nethttp.StatusOK {
		t.Fatalf("local session status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode local session response: %v", err)
	}
	if body.AccessToken == "" {
		t.Fatalf("local session response missing access token")
	}
	return body.AccessToken
}

func setLocalTrustRequest(req *nethttp.Request) {
	req.Host = "127.0.0.1:19001"
	req.RemoteAddr = "127.0.0.1:50000"
}

func setDesktopTestAuthHeader(t *testing.T, handler nethttp.Handler, req *nethttp.Request) {
	t.Helper()
	req.Header.Set("Authorization", "Bearer "+issueDesktopLocalSessionAccessToken(t, handler))
}

func hasPermission(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func ensureAPIKeyColumn(t *testing.T, ctx context.Context, pool data.DB, name string, definition string) {
	t.Helper()

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM pragma_table_info('api_keys') WHERE name = $1`, name).Scan(&count); err != nil {
		t.Fatalf("inspect api_keys columns: %v", err)
	}
	if count > 0 {
		return
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE api_keys ADD COLUMN `+name+` `+definition); err != nil {
		t.Fatalf("add api_keys %s column: %v", name, err)
	}
}

func newDesktopAuthHandler(t *testing.T, pool data.DB) nethttp.Handler {
	t.Helper()

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credentialRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("new credential repo: %v", err)
	}
	membershipRepo, err := data.NewAccountMembershipRepository(pool)
	if err != nil {
		t.Fatalf("new membership repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("new refresh token repo: %v", err)
	}
	accountRepo, err := data.NewAccountRepository(pool)
	if err != nil {
		t.Fatalf("new account repo: %v", err)
	}
	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("desktop-auth-test-secret", 3600, 86400)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}
	authService, err := auth.NewService(
		userRepo,
		credentialRepo,
		membershipRepo,
		passwordHasher,
		tokenService,
		refreshTokenRepo,
		nil,
		projectRepo,
	)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}

	return NewHandler(HandlerConfig{
		Logger:                slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Pool:                  pool,
		AuthService:           authService,
		AccountMembershipRepo: membershipRepo,
		UsersRepo:             userRepo,
		UserCredentialRepo:    credentialRepo,
		AccountRepo:           accountRepo,
	})
}
