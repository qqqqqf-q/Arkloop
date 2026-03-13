//go:build !desktop

package http

import (
	"context"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"arkloop/services/api/internal/auth"
	apiCrypto "arkloop/services/api/internal/crypto"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/jackc/pgx/v5/pgxpool"
)

type llmProvidersTestEnv struct {
	handler     nethttp.Handler
	pool        *pgxpool.Pool
	adminToken  string
	memberToken string
}

func setupLlmProvidersTestEnv(t *testing.T) llmProvidersTestEnv {
	t.Helper()

	db := setupTestDatabase(t, "api_go_llm_providers")
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
	userCredRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("user cred repo: %v", err)
	}
	membershipRepo, err := data.NewAccountMembershipRepository(pool)
	if err != nil {
		t.Fatalf("membership repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("refresh token repo: %v", err)
	}
	orgRepo, err := data.NewAccountRepository(pool)
	if err != nil {
		t.Fatalf("org repo: %v", err)
	}
	llmCredentialsRepo, err := data.NewLlmCredentialsRepository(pool)
	if err != nil {
		t.Fatalf("llm credentials repo: %v", err)
	}
	llmRoutesRepo, err := data.NewLlmRoutesRepository(pool)
	if err != nil {
		t.Fatalf("llm routes repo: %v", err)
	}

	key := make([]byte, 32)
	for idx := range key {
		key[idx] = byte(idx + 11)
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
	authService, err := auth.NewService(userRepo, userCredRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}

	org, err := orgRepo.Create(ctx, "llm-providers-org", "LLM Providers Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	adminUser, err := userRepo.Create(ctx, "org-admin", "org-admin@test.com", "en")
	if err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	memberUser, err := userRepo.Create(ctx, "org-member", "org-member@test.com", "en")
	if err != nil {
		t.Fatalf("create member user: %v", err)
	}
	if _, err := membershipRepo.Create(ctx, org.ID, adminUser.ID, auth.RolePlatformAdmin); err != nil {
		t.Fatalf("create admin membership: %v", err)
	}
	if _, err := membershipRepo.Create(ctx, org.ID, memberUser.ID, auth.RoleAccountMember); err != nil {
		t.Fatalf("create member membership: %v", err)
	}
	adminToken, err := tokenService.Issue(adminUser.ID, org.ID, auth.RolePlatformAdmin, time.Now().UTC())
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	memberToken, err := tokenService.Issue(memberUser.ID, org.ID, auth.RoleAccountMember, time.Now().UTC())
	if err != nil {
		t.Fatalf("issue member token: %v", err)
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
		LlmCredentialsRepo:      llmCredentialsRepo,
		LlmRoutesRepo:           llmRoutesRepo,
		SecretsRepo:             secretsRepo,
	})

	return llmProvidersTestEnv{
		handler:     handler,
		pool:        pool,
		adminToken:  adminToken,
		memberToken: memberToken,
	}
}

func TestLlmProvidersRequireSecretsPermission(t *testing.T) {
	env := setupLlmProvidersTestEnv(t)
	resp := doJSON(env.handler, nethttp.MethodGet, "/v1/llm-providers", nil, authHeader(env.memberToken))
	assertErrorEnvelope(t, resp, nethttp.StatusForbidden, "auth.forbidden")
}

func TestLlmProvidersCRUDAndDefaultPromotion(t *testing.T) {
	env := setupLlmProvidersTestEnv(t)

	createProviderResp := doJSON(env.handler, nethttp.MethodPost, "/v1/llm-providers", map[string]any{
		"name":            "openai-prod",
		"provider":        "openai",
		"api_key":         "sk-test-1234567890",
		"openai_api_mode": "responses",
	}, authHeader(env.adminToken))
	if createProviderResp.Code != nethttp.StatusCreated {
		t.Fatalf("create provider: %d %s", createProviderResp.Code, createProviderResp.Body.String())
	}
	provider := decodeJSONBody[llmProviderResponse](t, createProviderResp.Body.Bytes())
	if provider.Name != "openai-prod" || provider.Provider != "openai" {
		t.Fatalf("unexpected provider payload: %#v", provider)
	}
	if provider.KeyPrefix == nil || *provider.KeyPrefix != "sk-test-1" {
		t.Fatalf("unexpected key prefix: %#v", provider.KeyPrefix)
	}

	listResp := doJSON(env.handler, nethttp.MethodGet, "/v1/llm-providers", nil, authHeader(env.adminToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list providers: %d %s", listResp.Code, listResp.Body.String())
	}
	providers := decodeJSONBody[[]llmProviderResponse](t, listResp.Body.Bytes())
	if len(providers) != 1 || len(providers[0].Models) != 0 {
		t.Fatalf("unexpected provider list: %#v", providers)
	}

	patchProviderResp := doJSON(env.handler, nethttp.MethodPatch, "/v1/llm-providers/"+provider.ID, map[string]any{
		"name":     "openai-primary",
		"base_url": "https://example.test/v1",
		"api_key":  "sk-rotated-abcdef",
	}, authHeader(env.adminToken))
	if patchProviderResp.Code != nethttp.StatusOK {
		t.Fatalf("patch provider: %d %s", patchProviderResp.Code, patchProviderResp.Body.String())
	}
	provider = decodeJSONBody[llmProviderResponse](t, patchProviderResp.Body.Bytes())
	if provider.Name != "openai-primary" {
		t.Fatalf("unexpected patched name: %#v", provider)
	}
	if provider.BaseURL == nil || *provider.BaseURL != "https://example.test/v1" {
		t.Fatalf("unexpected patched base_url: %#v", provider.BaseURL)
	}
	if provider.KeyPrefix == nil || *provider.KeyPrefix != "sk-rotat" {
		t.Fatalf("unexpected rotated key prefix: %#v", provider.KeyPrefix)
	}

	createModelOneResp := doJSON(env.handler, nethttp.MethodPost, "/v1/llm-providers/"+provider.ID+"/models", map[string]any{
		"model":    "gpt-4o",
		"priority": 1,
		"tags":     []string{"chat"},
		"advanced_json": map[string]any{
			"provider": "primary",
		},
	}, authHeader(env.adminToken))
	if createModelOneResp.Code != nethttp.StatusCreated {
		t.Fatalf("create model one: %d %s", createModelOneResp.Code, createModelOneResp.Body.String())
	}
	modelOne := decodeJSONBody[llmProviderModelResponse](t, createModelOneResp.Body.Bytes())
	if !modelOne.IsDefault {
		t.Fatal("expected first model to become default")
	}
	if len(modelOne.Tags) != 1 || modelOne.Tags[0] != "chat" {
		t.Fatalf("unexpected model tags: %#v", modelOne.Tags)
	}
	if modelOne.AdvancedJSON["provider"] != "primary" {
		t.Fatalf("unexpected model advanced_json: %#v", modelOne.AdvancedJSON)
	}

	createModelTwoResp := doJSON(env.handler, nethttp.MethodPost, "/v1/llm-providers/"+provider.ID+"/models", map[string]any{
		"model":    "gpt-4.1",
		"priority": 9,
	}, authHeader(env.adminToken))
	if createModelTwoResp.Code != nethttp.StatusCreated {
		t.Fatalf("create model two: %d %s", createModelTwoResp.Code, createModelTwoResp.Body.String())
	}
	modelTwo := decodeJSONBody[llmProviderModelResponse](t, createModelTwoResp.Body.Bytes())
	if modelTwo.IsDefault {
		t.Fatal("expected second model not to be default before patch")
	}

	patchModelResp := doJSON(env.handler, nethttp.MethodPatch, "/v1/llm-providers/"+provider.ID+"/models/"+modelTwo.ID, map[string]any{
		"is_default": true,
		"tags":       []string{"fast", "chat"},
		"advanced_json": map[string]any{
			"provider": "backup",
		},
	}, authHeader(env.adminToken))
	if patchModelResp.Code != nethttp.StatusOK {
		t.Fatalf("patch model: %d %s", patchModelResp.Code, patchModelResp.Body.String())
	}
	modelTwo = decodeJSONBody[llmProviderModelResponse](t, patchModelResp.Body.Bytes())
	if !modelTwo.IsDefault {
		t.Fatal("expected second model to become default")
	}
	if modelTwo.AdvancedJSON["provider"] != "backup" {
		t.Fatalf("unexpected patched model advanced_json: %#v", modelTwo.AdvancedJSON)
	}

	listAfterModelsResp := doJSON(env.handler, nethttp.MethodGet, "/v1/llm-providers", nil, authHeader(env.adminToken))
	if listAfterModelsResp.Code != nethttp.StatusOK {
		t.Fatalf("list after models: %d %s", listAfterModelsResp.Code, listAfterModelsResp.Body.String())
	}
	providers = decodeJSONBody[[]llmProviderResponse](t, listAfterModelsResp.Body.Bytes())
	if len(providers) != 1 || len(providers[0].Models) != 2 {
		t.Fatalf("unexpected provider list after models: %#v", providers)
	}
	var listedOne, listedTwo llmProviderModelResponse
	for _, item := range providers[0].Models {
		if item.ID == modelOne.ID {
			listedOne = item
		}
		if item.ID == modelTwo.ID {
			listedTwo = item
		}
	}
	if listedOne.AdvancedJSON["provider"] != "primary" {
		t.Fatalf("unexpected listed modelOne advanced_json: %#v", listedOne.AdvancedJSON)
	}
	if listedTwo.AdvancedJSON["provider"] != "backup" {
		t.Fatalf("unexpected listed modelTwo advanced_json: %#v", listedTwo.AdvancedJSON)
	}
	if listedOne.IsDefault {
		t.Fatal("expected first model default cleared")
	}
	if !listedTwo.IsDefault {
		t.Fatal("expected second model default set")
	}

	deleteModelResp := doJSON(env.handler, nethttp.MethodDelete, "/v1/llm-providers/"+provider.ID+"/models/"+modelTwo.ID, nil, authHeader(env.adminToken))
	if deleteModelResp.Code != nethttp.StatusOK {
		t.Fatalf("delete model: %d %s", deleteModelResp.Code, deleteModelResp.Body.String())
	}

	listAfterDeleteResp := doJSON(env.handler, nethttp.MethodGet, "/v1/llm-providers", nil, authHeader(env.adminToken))
	if listAfterDeleteResp.Code != nethttp.StatusOK {
		t.Fatalf("list after delete model: %d %s", listAfterDeleteResp.Code, listAfterDeleteResp.Body.String())
	}
	providers = decodeJSONBody[[]llmProviderResponse](t, listAfterDeleteResp.Body.Bytes())
	if len(providers[0].Models) != 1 || !providers[0].Models[0].IsDefault {
		t.Fatalf("expected remaining model promoted to default: %#v", providers[0].Models)
	}

	deleteProviderResp := doJSON(env.handler, nethttp.MethodDelete, "/v1/llm-providers/"+provider.ID, nil, authHeader(env.adminToken))
	if deleteProviderResp.Code != nethttp.StatusOK {
		t.Fatalf("delete provider: %d %s", deleteProviderResp.Code, deleteProviderResp.Body.String())
	}

	listFinalResp := doJSON(env.handler, nethttp.MethodGet, "/v1/llm-providers", nil, authHeader(env.adminToken))
	if listFinalResp.Code != nethttp.StatusOK {
		t.Fatalf("list final: %d %s", listFinalResp.Code, listFinalResp.Body.String())
	}
	providers = decodeJSONBody[[]llmProviderResponse](t, listFinalResp.Body.Bytes())
	if len(providers) != 0 {
		t.Fatalf("expected empty providers after delete, got %#v", providers)
	}
}

func TestLlmProvidersAvailableModelsOpenAI(t *testing.T) {
	env := setupLlmProvidersTestEnv(t)
	var authorization string
	upstream := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		authorization = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		writeJSON(w, "", nethttp.StatusOK, map[string]any{
			"data": []map[string]any{{"id": "gpt-4.1"}, {"id": "gpt-4o"}},
		})
	}))
	defer upstream.Close()

	createProviderResp := doJSON(env.handler, nethttp.MethodPost, "/v1/llm-providers", map[string]any{
		"name":            "openai-import",
		"provider":        "openai",
		"api_key":         "sk-import-123456",
		"base_url":        upstream.URL + "/v1",
		"openai_api_mode": "responses",
	}, authHeader(env.adminToken))
	if createProviderResp.Code != nethttp.StatusCreated {
		t.Fatalf("create provider: %d %s", createProviderResp.Code, createProviderResp.Body.String())
	}
	provider := decodeJSONBody[llmProviderResponse](t, createProviderResp.Body.Bytes())

	createModelResp := doJSON(env.handler, nethttp.MethodPost, "/v1/llm-providers/"+provider.ID+"/models", map[string]any{"model": "gpt-4o"}, authHeader(env.adminToken))
	if createModelResp.Code != nethttp.StatusCreated {
		t.Fatalf("create model: %d %s", createModelResp.Code, createModelResp.Body.String())
	}

	availableResp := doJSON(env.handler, nethttp.MethodGet, "/v1/llm-providers/"+provider.ID+"/available-models", nil, authHeader(env.adminToken))
	if availableResp.Code != nethttp.StatusOK {
		t.Fatalf("available models: %d %s", availableResp.Code, availableResp.Body.String())
	}
	if authorization != "Bearer sk-import-123456" {
		t.Fatalf("unexpected authorization header: %q", authorization)
	}
	payload := decodeJSONBody[llmProviderAvailableModelsResponse](t, availableResp.Body.Bytes())
	if len(payload.Models) != 2 {
		t.Fatalf("unexpected available models payload: %#v", payload)
	}
	var configured map[string]bool = map[string]bool{}
	for _, item := range payload.Models {
		configured[item.ID] = item.Configured
	}
	if !configured["gpt-4o"] || configured["gpt-4.1"] {
		t.Fatalf("unexpected configured flags: %#v", configured)
	}
}

func TestLlmProvidersAvailableModelsAnthropicHeadersAndAuthFailure(t *testing.T) {
	env := setupLlmProvidersTestEnv(t)
	requestCount := 0
	var lastAPIKey string
	var lastVersion string
	var lastBeta string
	upstream := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		requestCount++
		lastAPIKey = r.Header.Get("x-api-key")
		lastVersion = r.Header.Get("anthropic-version")
		lastBeta = r.Header.Get("anthropic-beta")
		if requestCount == 1 {
			writeJSON(w, "", nethttp.StatusOK, map[string]any{
				"data": []map[string]any{{"id": "claude-3-7-sonnet-latest", "display_name": "Claude 3.7 Sonnet"}},
			})
			return
		}
		w.WriteHeader(nethttp.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer upstream.Close()

	createProviderResp := doJSON(env.handler, nethttp.MethodPost, "/v1/llm-providers", map[string]any{
		"name":     "anthropic-import",
		"provider": "anthropic",
		"api_key":  "sk-ant-123456",
		"base_url": upstream.URL + "/v1",
		"advanced_json": map[string]any{
			"anthropic_version": "2023-06-01",
			"extra_headers": map[string]any{
				"anthropic-beta": "beta-test",
			},
		},
	}, authHeader(env.adminToken))
	if createProviderResp.Code != nethttp.StatusCreated {
		t.Fatalf("create provider: %d %s", createProviderResp.Code, createProviderResp.Body.String())
	}
	provider := decodeJSONBody[llmProviderResponse](t, createProviderResp.Body.Bytes())

	availableResp := doJSON(env.handler, nethttp.MethodGet, "/v1/llm-providers/"+provider.ID+"/available-models", nil, authHeader(env.adminToken))
	if availableResp.Code != nethttp.StatusOK {
		t.Fatalf("available models success: %d %s", availableResp.Code, availableResp.Body.String())
	}
	payload := decodeJSONBody[llmProviderAvailableModelsResponse](t, availableResp.Body.Bytes())
	if len(payload.Models) != 1 || payload.Models[0].Name != "Claude 3.7 Sonnet" {
		t.Fatalf("unexpected anthropic models payload: %#v", payload)
	}

	failedResp := doJSON(env.handler, nethttp.MethodGet, "/v1/llm-providers/"+provider.ID+"/available-models", nil, authHeader(env.adminToken))
	assertErrorEnvelope(t, failedResp, nethttp.StatusUnprocessableEntity, "llm_providers.upstream_auth_failed")
	if lastAPIKey != "sk-ant-123456" {
		t.Fatalf("unexpected anthropic api key header: %q", lastAPIKey)
	}
	if lastVersion != "2023-06-01" {
		t.Fatalf("unexpected anthropic version header: %q", lastVersion)
	}
	if lastBeta != "beta-test" {
		t.Fatalf("unexpected anthropic beta header: %q", lastBeta)
	}
}

func TestLlmProvidersModelAdvancedJSONValidation(t *testing.T) {
	env := setupLlmProvidersTestEnv(t)

	createProviderResp := doJSON(env.handler, nethttp.MethodPost, "/v1/llm-providers", map[string]any{
		"name":     "anthropic-model-advanced",
		"provider": "anthropic",
		"api_key":  "sk-ant-model-123456",
	}, authHeader(env.adminToken))
	if createProviderResp.Code != nethttp.StatusCreated {
		t.Fatalf("create provider: %d %s", createProviderResp.Code, createProviderResp.Body.String())
	}
	provider := decodeJSONBody[llmProviderResponse](t, createProviderResp.Body.Bytes())

	resp := doJSON(env.handler, nethttp.MethodPost, "/v1/llm-providers/"+provider.ID+"/models", map[string]any{
		"model": "claude-sonnet-4",
		"advanced_json": map[string]any{
			"extra_headers": map[string]any{
				"x-custom": "bad",
			},
		},
	}, authHeader(env.adminToken))
	assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation.error")
}

func TestLlmProvidersAvailableModelsUpstreamRequestFailure(t *testing.T) {
	env := setupLlmProvidersTestEnv(t)
	upstream := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.WriteHeader(nethttp.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer upstream.Close()

	createProviderResp := doJSON(env.handler, nethttp.MethodPost, "/v1/llm-providers", map[string]any{
		"name":     "openai-bad-request",
		"provider": "openai",
		"api_key":  "sk-bad-request-123",
		"base_url": upstream.URL + "/v1",
	}, authHeader(env.adminToken))
	if createProviderResp.Code != nethttp.StatusCreated {
		t.Fatalf("create provider: %d %s", createProviderResp.Code, createProviderResp.Body.String())
	}
	provider := decodeJSONBody[llmProviderResponse](t, createProviderResp.Body.Bytes())

	resp := doJSON(env.handler, nethttp.MethodGet, "/v1/llm-providers/"+provider.ID+"/available-models", nil, authHeader(env.adminToken))
	assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "llm_providers.upstream_request_failed")
}

func TestLlmProvidersDeleteRemovesSecret(t *testing.T) {
	env := setupLlmProvidersTestEnv(t)

	createProviderResp := doJSON(env.handler, nethttp.MethodPost, "/v1/llm-providers", map[string]any{
		"name":     "secret-check",
		"provider": "openai",
		"api_key":  "sk-secret-check-123",
	}, authHeader(env.adminToken))
	if createProviderResp.Code != nethttp.StatusCreated {
		t.Fatalf("create provider: %d %s", createProviderResp.Code, createProviderResp.Body.String())
	}
	provider := decodeJSONBody[llmProviderResponse](t, createProviderResp.Body.Bytes())

	deleteProviderResp := doJSON(env.handler, nethttp.MethodDelete, "/v1/llm-providers/"+provider.ID, nil, authHeader(env.adminToken))
	if deleteProviderResp.Code != nethttp.StatusOK {
		t.Fatalf("delete provider: %d %s", deleteProviderResp.Code, deleteProviderResp.Body.String())
	}

	var count int
	if err := env.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM secrets WHERE name = $1`, "llm_cred:"+provider.ID).Scan(&count); err != nil {
		t.Fatalf("count secret: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected secret removed, got %d rows", count)
	}
}
