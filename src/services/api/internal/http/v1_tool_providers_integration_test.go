package http

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"testing"
	"time"

	"arkloop/services/api/internal/auth"
	apiCrypto "arkloop/services/api/internal/crypto"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

type toolProvidersListResponse struct {
	Groups []toolProvidersGroup `json:"groups"`
}

type toolProvidersGroup struct {
	GroupName string                 `json:"group_name"`
	Providers []toolProviderListItem `json:"providers"`
}

type toolProviderListItem struct {
	GroupName       string  `json:"group_name"`
	ProviderName    string  `json:"provider_name"`
	IsActive        bool    `json:"is_active"`
	KeyPrefix       *string `json:"key_prefix"`
	BaseURL         *string `json:"base_url"`
	RequiresAPIKey  bool    `json:"requires_api_key"`
	RequiresBaseURL bool    `json:"requires_base_url"`
	Configured      bool    `json:"configured"`
}

func TestToolProvidersListActivateCredentialAndClear(t *testing.T) {
	db := setupTestDatabase(t, "api_go_tool_providers")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	logger := observability.NewJSONLogger("test", io.Discard)

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("user repo: %v", err)
	}
	credRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("cred repo: %v", err)
	}
	membershipRepo, err := data.NewAccountMembershipRepository(pool)
	if err != nil {
		t.Fatalf("membership repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("refresh repo: %v", err)
	}
	orgRepo, err := data.NewAccountRepository(pool)
	if err != nil {
		t.Fatalf("org repo: %v", err)
	}
	toolProvidersRepo, err := data.NewToolProviderConfigsRepository(pool)
	if err != nil {
		t.Fatalf("tool providers repo: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 3)
	}
	ring, err := apiCrypto.NewKeyRing(map[int][]byte{1: key})
	if err != nil {
		t.Fatalf("new key ring: %v", err)
	}
	secretsRepo, err := data.NewSecretsRepository(pool, ring)
	if err != nil {
		t.Fatalf("secrets repo: %v", err)
	}

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	authService, err := auth.NewService(userRepo, credRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}

	org, err := orgRepo.Create(ctx, "tool-providers-org", "Tool Providers Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	user, err := userRepo.Create(ctx, "admin", "admin@test.com", "en")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := membershipRepo.Create(ctx, org.ID, user.ID, auth.RolePlatformAdmin); err != nil {
		t.Fatalf("create membership: %v", err)
	}

	token, err := tokenService.Issue(user.ID, org.ID, auth.RolePlatformAdmin, time.Now().UTC())
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	listenerCtx, cancelListener := context.WithCancel(ctx)
	t.Cleanup(cancelListener)

	handler := NewHandler(HandlerConfig{
		Pool:                    pool,
		DirectPool:              pool,
		InvalidationListenerCtx: listenerCtx,
		Logger:                  logger,
		AuthService:             authService,
		AccountMembershipRepo:       membershipRepo,
		ToolProviderConfigsRepo: toolProvidersRepo,
		SecretsRepo:             secretsRepo,
	})

	// 初始列表
	listResp := doJSON(handler, nethttp.MethodGet, "/v1/tool-providers", nil, authHeader(token))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list: %d %s", listResp.Code, listResp.Body.String())
	}
	initial := decodeJSONBody[toolProvidersListResponse](t, listResp.Body.Bytes())
	if len(initial.Groups) == 0 {
		t.Fatal("expected groups, got 0")
	}

	// 激活 tavily
	actTavily := doJSON(handler, nethttp.MethodPut, "/v1/tool-providers/web_search/web_search.tavily/activate", nil, authHeader(token))
	if actTavily.Code != nethttp.StatusNoContent {
		t.Fatalf("activate tavily: %d %s", actTavily.Code, actTavily.Body.String())
	}

	// 同组切换到 searxng
	actSearx := doJSON(handler, nethttp.MethodPut, "/v1/tool-providers/web_search/web_search.searxng/activate", nil, authHeader(token))
	if actSearx.Code != nethttp.StatusNoContent {
		t.Fatalf("activate searxng: %d %s", actSearx.Code, actSearx.Body.String())
	}

	listAfterActivate := doJSON(handler, nethttp.MethodGet, "/v1/tool-providers", nil, authHeader(token))
	if listAfterActivate.Code != nethttp.StatusOK {
		t.Fatalf("list after activate: %d %s", listAfterActivate.Code, listAfterActivate.Body.String())
	}
	afterActivate := decodeJSONBody[toolProvidersListResponse](t, listAfterActivate.Body.Bytes())

	var tavilyActive, searxActive bool
	for _, g := range afterActivate.Groups {
		if g.GroupName != "web_search" {
			continue
		}
		for _, p := range g.Providers {
			if p.ProviderName == "web_search.tavily" {
				tavilyActive = p.IsActive
			}
			if p.ProviderName == "web_search.searxng" {
				searxActive = p.IsActive
			}
		}
	}
	if tavilyActive {
		t.Fatal("expected tavily inactive after switching")
	}
	if !searxActive {
		t.Fatal("expected searxng active after switching")
	}

	// 预置 config_json，确保后续仅更新凭证时不会被覆盖成 {}
	if _, err := pool.Exec(ctx, `
		UPDATE tool_provider_configs
		SET config_json = '{"keep": true}'::jsonb
		WHERE scope = 'platform' AND provider_name = 'web_search.tavily'
	`); err != nil {
		t.Fatalf("seed config_json: %v", err)
	}

	// 配置 tavily key（不激活也应允许配置）
	keyPayload := map[string]any{"api_key": "tvly-1234567890abcdef"}
	upsert := doJSON(handler, nethttp.MethodPut, "/v1/tool-providers/web_search/web_search.tavily/credential", keyPayload, authHeader(token))
	if upsert.Code != nethttp.StatusNoContent {
		t.Fatalf("upsert credential: %d %s", upsert.Code, upsert.Body.String())
	}

	var rawCfg []byte
	if err := pool.QueryRow(ctx, `
		SELECT config_json
		FROM tool_provider_configs
		WHERE scope = 'platform' AND provider_name = 'web_search.tavily'
	`).Scan(&rawCfg); err != nil {
		t.Fatalf("load config_json: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(rawCfg, &cfg); err != nil {
		t.Fatalf("unmarshal config_json: %v (%s)", err, string(rawCfg))
	}
	if v, ok := cfg["keep"]; !ok || v != true {
		t.Fatalf("unexpected config_json after credential upsert: %v", cfg)
	}

	listAfterKey := doJSON(handler, nethttp.MethodGet, "/v1/tool-providers", nil, authHeader(token))
	if listAfterKey.Code != nethttp.StatusOK {
		t.Fatalf("list after key: %d %s", listAfterKey.Code, listAfterKey.Body.String())
	}
	afterKey := decodeJSONBody[toolProvidersListResponse](t, listAfterKey.Body.Bytes())

	var tavilyPrefix *string
	for _, g := range afterKey.Groups {
		for _, p := range g.Providers {
			if p.ProviderName == "web_search.tavily" {
				tavilyPrefix = p.KeyPrefix
			}
		}
	}
	if tavilyPrefix == nil || *tavilyPrefix != "tvly-123" {
		t.Fatalf("unexpected key prefix: %v", tavilyPrefix)
	}

	// 清除凭证会同时停用
	clearResp := doJSON(handler, nethttp.MethodDelete, "/v1/tool-providers/web_search/web_search.tavily/credential", nil, authHeader(token))
	if clearResp.Code != nethttp.StatusNoContent {
		t.Fatalf("clear credential: %d %s", clearResp.Code, clearResp.Body.String())
	}

	listAfterClear := doJSON(handler, nethttp.MethodGet, "/v1/tool-providers", nil, authHeader(token))
	if listAfterClear.Code != nethttp.StatusOK {
		t.Fatalf("list after clear: %d %s", listAfterClear.Code, listAfterClear.Body.String())
	}
	afterClear := decodeJSONBody[toolProvidersListResponse](t, listAfterClear.Body.Bytes())

	var tavily toolProviderListItem
	found := false
	for _, g := range afterClear.Groups {
		for _, p := range g.Providers {
			if p.ProviderName == "web_search.tavily" {
				tavily = p
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected tavily provider in list")
	}
	if tavily.IsActive {
		t.Fatal("expected tavily inactive after clear")
	}
	if tavily.KeyPrefix != nil {
		t.Fatalf("expected key_prefix cleared, got %v", *tavily.KeyPrefix)
	}

	// project scope: account_admin 可配置自身 project，且不会影响 platform scope
	orgAdmin, err := userRepo.Create(ctx, "org-admin", "org-admin@test.com", "en")
	if err != nil {
		t.Fatalf("create org admin user: %v", err)
	}
	if _, err := membershipRepo.Create(ctx, org.ID, orgAdmin.ID, auth.RoleAccountAdmin); err != nil {
		t.Fatalf("create org admin membership: %v", err)
	}
	orgToken, err := tokenService.Issue(orgAdmin.ID, org.ID, auth.RoleAccountAdmin, time.Now().UTC())
	if err != nil {
		t.Fatalf("issue org token: %v", err)
	}

	// account_admin 默认（platform）应 403
	orgListPlatform := doJSON(handler, nethttp.MethodGet, "/v1/tool-providers", nil, authHeader(orgToken))
	if orgListPlatform.Code != nethttp.StatusForbidden {
		t.Fatalf("org list platform: %d %s", orgListPlatform.Code, orgListPlatform.Body.String())
	}

	// account_admin + scope=project 可访问
	orgListResp := doJSON(handler, nethttp.MethodGet, "/v1/tool-providers?scope=project", nil, authHeader(orgToken))
	if orgListResp.Code != nethttp.StatusOK {
		t.Fatalf("org list: %d %s", orgListResp.Code, orgListResp.Body.String())
	}

	// 激活 + 配置 key
	orgAct := doJSON(handler, nethttp.MethodPut, "/v1/tool-providers/web_search/web_search.tavily/activate?scope=project", nil, authHeader(orgToken))
	if orgAct.Code != nethttp.StatusNoContent {
		t.Fatalf("org activate tavily: %d %s", orgAct.Code, orgAct.Body.String())
	}
	orgKeyPayload := map[string]any{"api_key": "tvly-org-abcdef123456"}
	orgUpsert := doJSON(handler, nethttp.MethodPut, "/v1/tool-providers/web_search/web_search.tavily/credential?scope=project", orgKeyPayload, authHeader(orgToken))
	if orgUpsert.Code != nethttp.StatusNoContent {
		t.Fatalf("org upsert credential: %d %s", orgUpsert.Code, orgUpsert.Body.String())
	}

	var orgCfgCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM tool_provider_configs
		WHERE owner_kind = 'user' AND account_id = $1 AND provider_name = 'web_search.tavily'
	`, org.ID).Scan(&orgCfgCount); err != nil {
		t.Fatalf("org cfg count: %v", err)
	}
	if orgCfgCount != 1 {
		t.Fatalf("expected 1 org config, got %d", orgCfgCount)
	}

	// org scope list 应有前缀
	orgListAfterKey := doJSON(handler, nethttp.MethodGet, "/v1/tool-providers?scope=project", nil, authHeader(orgToken))
	if orgListAfterKey.Code != nethttp.StatusOK {
		t.Fatalf("org list after key: %d %s", orgListAfterKey.Code, orgListAfterKey.Body.String())
	}
	orgAfterKey := decodeJSONBody[toolProvidersListResponse](t, orgListAfterKey.Body.Bytes())
	var orgTavilyPrefix *string
	for _, g := range orgAfterKey.Groups {
		for _, p := range g.Providers {
			if p.ProviderName == "web_search.tavily" {
				orgTavilyPrefix = p.KeyPrefix
			}
		}
	}
	if orgTavilyPrefix == nil || *orgTavilyPrefix != "tvly-org" {
		t.Fatalf("unexpected org key prefix: %v", orgTavilyPrefix)
	}

	// platform scope 不应被 org scope 污染（此前已 clear，key_prefix 应为空）
	listPlatformAgain := doJSON(handler, nethttp.MethodGet, "/v1/tool-providers", nil, authHeader(token))
	if listPlatformAgain.Code != nethttp.StatusOK {
		t.Fatalf("list platform again: %d %s", listPlatformAgain.Code, listPlatformAgain.Body.String())
	}
	platformAgain := decodeJSONBody[toolProvidersListResponse](t, listPlatformAgain.Body.Bytes())
	var platformTavilyPrefix *string
	for _, g := range platformAgain.Groups {
		for _, p := range g.Providers {
			if p.ProviderName == "web_search.tavily" {
				platformTavilyPrefix = p.KeyPrefix
			}
		}
	}
	if platformTavilyPrefix != nil {
		t.Fatalf("expected platform key_prefix nil, got %v", *platformTavilyPrefix)
	}

	// org scope clear 会删除 org secret + 停用
	orgClear := doJSON(handler, nethttp.MethodDelete, "/v1/tool-providers/web_search/web_search.tavily/credential?scope=project", nil, authHeader(orgToken))
	if orgClear.Code != nethttp.StatusNoContent {
		t.Fatalf("org clear credential: %d %s", orgClear.Code, orgClear.Body.String())
	}

	var orgSecretCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM secrets
		WHERE owner_kind = 'user' AND account_id = $1 AND name = 'tool_provider:web_search.tavily'
	`, org.ID).Scan(&orgSecretCount); err != nil {
		t.Fatalf("org secret count: %v", err)
	}
	if orgSecretCount != 0 {
		t.Fatalf("expected org secret deleted, got %d", orgSecretCount)
	}
}
