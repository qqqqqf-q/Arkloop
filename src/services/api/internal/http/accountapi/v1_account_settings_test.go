//go:build !desktop

package accountapi

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"testing"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"
)

type accountSettingsTestEnv struct {
	handler     nethttp.Handler
	accountRepo *data.AccountRepository
	accessToken string
	accountID   string
}

func setupAccountSettingsTestEnv(t *testing.T) accountSettingsTestEnv {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "api_go_account_settings")
	if _, err := migrate.Up(context.Background(), db.DSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := data.NewPool(context.Background(), db.DSN, data.PoolLimits{MaxConns: 8, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("user repo: %v", err)
	}
	userCredRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("user credential repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("refresh token repo: %v", err)
	}
	membershipRepo, err := data.NewAccountMembershipRepository(pool)
	if err != nil {
		t.Fatalf("membership repo: %v", err)
	}
	accountRepo, err := data.NewAccountRepository(pool)
	if err != nil {
		t.Fatalf("account repo: %v", err)
	}
	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("project repo: %v", err)
	}

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("token service: %v", err)
	}
	authService, err := auth.NewService(userRepo, userCredRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, projectRepo)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}

	account, err := accountRepo.Create(context.Background(), "account-settings", "Account Settings", "personal")
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	user, err := userRepo.Create(context.Background(), "settings-owner", "settings-owner@test.com", "zh")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := membershipRepo.Create(context.Background(), account.ID, user.ID, auth.RoleAccountAdmin); err != nil {
		t.Fatalf("create membership: %v", err)
	}

	accessToken, err := tokenService.Issue(user.ID, account.ID, auth.RoleAccountAdmin, time.Now().UTC())
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	mux := nethttp.NewServeMux()
	RegisterRoutes(mux, Deps{
		AuthService:           authService,
		AccountMembershipRepo: membershipRepo,
		AccountRepo:           accountRepo,
		Pool:                  pool,
	})

	return accountSettingsTestEnv{
		handler:     mux,
		accountRepo: accountRepo,
		accessToken: accessToken,
		accountID:   account.ID.String(),
	}
}

func TestAccountSettingsGetAndPatchPipelineTraceEnabled(t *testing.T) {
	env := setupAccountSettingsTestEnv(t)

	getResp := doJSONAccount(env.handler, nethttp.MethodGet, "/v1/account/settings", nil, authHeader(env.accessToken))
	if getResp.Code != nethttp.StatusOK {
		t.Fatalf("get status = %d body=%s", getResp.Code, getResp.Body.String())
	}
	initial := decodeJSONBodyAccount[accountSettingsResponse](t, getResp.Body.Bytes())
	if initial.PipelineTraceEnabled {
		t.Fatal("expected pipeline trace to be disabled by default")
	}

	patchResp := doJSONAccount(env.handler, nethttp.MethodPatch, "/v1/account/settings", map[string]any{
		"pipeline_trace_enabled": true,
	}, authHeader(env.accessToken))
	if patchResp.Code != nethttp.StatusOK {
		t.Fatalf("patch status = %d body=%s", patchResp.Code, patchResp.Body.String())
	}

	accountID := mustUUID(t, env.accountID)
	account, err := env.accountRepo.GetByID(context.Background(), accountID)
	if err != nil {
		t.Fatalf("load account: %v", err)
	}
	if account == nil || !pipelineTraceEnabledFromJSON(account.SettingsJSON) {
		t.Fatalf("expected pipeline trace to be stored in settings_json")
	}
}

func TestAccountSettingsPatchPreservesOtherKeys(t *testing.T) {
	env := setupAccountSettingsTestEnv(t)
	accountID := mustUUID(t, env.accountID)

	if err := env.accountRepo.UpdateSettings(context.Background(), accountID, "existing_flag", "keep"); err != nil {
		t.Fatalf("seed existing flag: %v", err)
	}

	resp := doJSONAccount(env.handler, nethttp.MethodPatch, "/v1/account/settings", map[string]any{
		"pipeline_trace_enabled": true,
	}, authHeader(env.accessToken))
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("patch status = %d body=%s", resp.Code, resp.Body.String())
	}

	account, err := env.accountRepo.GetByID(context.Background(), accountID)
	if err != nil {
		t.Fatalf("load account: %v", err)
	}
	if account == nil {
		t.Fatal("expected account")
	}

	var payload map[string]any
	if err := json.Unmarshal(account.SettingsJSON, &payload); err != nil {
		t.Fatalf("decode settings_json: %v", err)
	}
	if payload["existing_flag"] != "keep" {
		t.Fatalf("existing flag lost: %#v", payload)
	}
	if payload[pipelineTraceEnabledSettingKey] != true {
		t.Fatalf("pipeline trace flag missing: %#v", payload)
	}
}

func TestAccountSettingsPatchRejectsInvalidBody(t *testing.T) {
	env := setupAccountSettingsTestEnv(t)

	resp := doJSONAccount(env.handler, nethttp.MethodPatch, "/v1/account/settings", map[string]any{
		"unexpected": true,
	}, authHeader(env.accessToken))
	if resp.Code != nethttp.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestAccountSettingsRequiresAuthentication(t *testing.T) {
	env := setupAccountSettingsTestEnv(t)

	resp := doJSONAccount(env.handler, nethttp.MethodGet, "/v1/account/settings", nil, nil)
	if resp.Code != nethttp.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
}
