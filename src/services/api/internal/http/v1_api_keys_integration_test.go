//go:build !desktop

package http

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"
	"github.com/google/uuid"
)

const apiKeysTestJWTSecret = "test-secret-should-be-long-enough-32chars"

type apiKeyTestEnv struct {
	handler        nethttp.Handler
	apiKeysRepo    *data.APIKeysRepository
	membershipRepo *data.AccountMembershipRepository
	tokenService   *auth.JwtAccessTokenService
	aliceToken     string
	aliceUserID    uuid.UUID
	aliceAccountID     uuid.UUID
}

func buildAPIKeyEnv(t *testing.T) apiKeyTestEnv {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "api_keys")
	if _, err := migrate.Up(context.Background(), db.DSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService(apiKeysTestJWTSecret, 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("new cred repo: %v", err)
	}
	membershipRepo, err := data.NewAccountMembershipRepository(pool)
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
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	apiKeysRepo, err := data.NewAPIKeysRepository(pool)
	if err != nil {
		t.Fatalf("new api keys repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
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
		Pool:                pool,
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		AccountMembershipRepo:   membershipRepo,
		ThreadRepo:          threadRepo,
		AuditWriter:         auditWriter,
		APIKeysRepo:         apiKeysRepo,
	})

	// 注册一个测试用户并获取 JWT token
	regResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "alice", "password": "pwd12345", "email": "alice@test.com"},
		nil,
	)
	if regResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", regResp.Code, regResp.Body.String())
	}
	regPayload := decodeJSONBody[registerResponse](t, regResp.Body.Bytes())
	aliceUserID, err := uuid.Parse(regPayload.UserID)
	if err != nil {
		t.Fatalf("parse user id: %v", err)
	}
	aliceMembership, err := membershipRepo.GetDefaultForUser(ctx, aliceUserID)
	if err != nil {
		t.Fatalf("lookup membership: %v", err)
	}
	if aliceMembership == nil {
		t.Fatal("expected default membership")
	}

	return apiKeyTestEnv{
		handler:        handler,
		apiKeysRepo:    apiKeysRepo,
		membershipRepo: membershipRepo,
		tokenService:   tokenService,
		aliceToken:     regPayload.AccessToken,
		aliceUserID:    aliceUserID,
		aliceAccountID:     aliceMembership.AccountID,
	}
}

func buildAPIKeyHandler(t *testing.T) (nethttp.Handler, *data.APIKeysRepository, string) {
	t.Helper()
	env := buildAPIKeyEnv(t)
	return env.handler, env.apiKeysRepo, env.aliceToken
}

func TestAPIKeyCreateListRevoke(t *testing.T) {
	handler, _, jwtToken := buildAPIKeyHandler(t)

	// 未认证时拒绝
	resp := doJSON(handler, nethttp.MethodPost, "/v1/api-keys", map[string]any{"name": "test"}, nil)
	assertErrorEnvelope(t, resp, nethttp.StatusUnauthorized, "auth.missing_token")

	// 创建 API Key
	createResp := doJSON(handler, nethttp.MethodPost, "/v1/api-keys",
		map[string]any{"name": "my-key", "scopes": []string{}},
		authHeader(jwtToken),
	)
	if createResp.Code != nethttp.StatusCreated {
		t.Fatalf("create api key: %d %s", createResp.Code, createResp.Body.String())
	}

	type createRespBody struct {
		ID        string `json:"id"`
		KeyPrefix string `json:"key_prefix"`
		Name      string `json:"name"`
		Key       string `json:"key"`
	}
	created := decodeJSONBody[createRespBody](t, createResp.Body.Bytes())
	if created.Key == "" {
		t.Fatal("expected key in create response")
	}
	if created.ID == "" {
		t.Fatal("expected id in create response")
	}
	if len(created.Key) != 67 {
		t.Fatalf("unexpected key length: %d key=%s", len(created.Key), created.Key)
	}
	if created.Key[:3] != "ak-" {
		t.Fatalf("expected ak- prefix, got %s", created.Key[:3])
	}
	if created.KeyPrefix[:3] != "ak-" {
		t.Fatalf("expected prefix to start with ak-, got %s", created.KeyPrefix)
	}
	if created.Name != "my-key" {
		t.Fatalf("unexpected name: %s", created.Name)
	}

	// 列表
	listResp := doJSON(handler, nethttp.MethodGet, "/v1/api-keys", nil, authHeader(jwtToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list api keys: %d %s", listResp.Code, listResp.Body.String())
	}

	type listRespItem struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	items := decodeJSONBody[[]listRespItem](t, listResp.Body.Bytes())
	if len(items) != 1 {
		t.Fatalf("expected 1 key, got %d", len(items))
	}
	if items[0].Key != "" {
		t.Fatal("list response must not include plaintext key")
	}

	// 吊销
	revokeResp := doJSON(handler, nethttp.MethodDelete, "/v1/api-keys/"+created.ID, nil, authHeader(jwtToken))
	if revokeResp.Code != nethttp.StatusNoContent {
		t.Fatalf("revoke api key: %d %s", revokeResp.Code, revokeResp.Body.String())
	}

	// 重复吊销返回 404
	revokeAgain := doJSON(handler, nethttp.MethodDelete, "/v1/api-keys/"+created.ID, nil, authHeader(jwtToken))
	assertErrorEnvelope(t, revokeAgain, nethttp.StatusNotFound, "api_keys.not_found")
}

func TestAPIKeyAuthenticatesRequests(t *testing.T) {
	handler, _, jwtToken := buildAPIKeyHandler(t)

	// 创建 API Key
	createResp := doJSON(handler, nethttp.MethodPost, "/v1/api-keys",
		map[string]any{"name": "ci-key", "scopes": []string{auth.PermDataThreadsRead}},
		authHeader(jwtToken),
	)
	if createResp.Code != nethttp.StatusCreated {
		t.Fatalf("create api key: %d %s", createResp.Code, createResp.Body.String())
	}
	type createBody struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	created := decodeJSONBody[createBody](t, createResp.Body.Bytes())

	// 用 API Key 访问 /v1/threads
	threadsResp := doJSON(handler, nethttp.MethodGet, "/v1/threads", nil, authHeader(created.Key))
	if threadsResp.Code != nethttp.StatusOK {
		t.Fatalf("list threads with api key: %d %s", threadsResp.Code, threadsResp.Body.String())
	}

	// 用无效 API Key 被拒绝
	invalidResp := doJSON(handler, nethttp.MethodGet, "/v1/threads", nil, authHeader("ak-deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"))
	assertErrorEnvelope(t, invalidResp, nethttp.StatusUnauthorized, "auth.invalid_api_key")

	// 吊销后访问被拒绝
	revokeResp := doJSON(handler, nethttp.MethodDelete, "/v1/api-keys/"+created.ID, nil, authHeader(jwtToken))
	if revokeResp.Code != nethttp.StatusNoContent {
		t.Fatalf("revoke: %d %s", revokeResp.Code, revokeResp.Body.String())
	}

	afterRevokeResp := doJSON(handler, nethttp.MethodGet, "/v1/threads", nil, authHeader(created.Key))
	assertErrorEnvelope(t, afterRevokeResp, nethttp.StatusUnauthorized, "auth.invalid_api_key")
}

func TestAPIKeyEmptyScopesDenied(t *testing.T) {
	handler, _, jwtToken := buildAPIKeyHandler(t)

	createResp := doJSON(handler, nethttp.MethodPost, "/v1/api-keys",
		map[string]any{"name": "empty-scope", "scopes": []string{}},
		authHeader(jwtToken),
	)
	if createResp.Code != nethttp.StatusCreated {
		t.Fatalf("create api key: %d %s", createResp.Code, createResp.Body.String())
	}
	type createBody struct {
		Key string `json:"key"`
	}
	created := decodeJSONBody[createBody](t, createResp.Body.Bytes())

	threadsResp := doJSON(handler, nethttp.MethodGet, "/v1/threads", nil, authHeader(created.Key))
	assertErrorEnvelope(t, threadsResp, nethttp.StatusForbidden, "auth.forbidden")
}

func TestAPIKeyOwnershipVisibility(t *testing.T) {
	env := buildAPIKeyEnv(t)

	register := func(login string) uuid.UUID {
		resp := doJSON(env.handler, nethttp.MethodPost, "/v1/auth/register",
			map[string]any{"login": login, "password": "pwd12345", "email": login + "@test.com"},
			nil,
		)
		if resp.Code != nethttp.StatusCreated {
			t.Fatalf("register %s: %d %s", login, resp.Code, resp.Body.String())
		}
		payload := decodeJSONBody[registerResponse](t, resp.Body.Bytes())
		id, err := uuid.Parse(payload.UserID)
		if err != nil {
			t.Fatalf("parse user id: %v", err)
		}
		return id
	}

	memberAID := register("member-a")
	memberBID := register("member-b")
	for _, userID := range []uuid.UUID{memberAID, memberBID} {
		if _, err := env.membershipRepo.Create(context.Background(), env.aliceAccountID, userID, auth.RoleAccountMember); err != nil {
			t.Fatalf("add membership: %v", err)
		}
	}

	memberAToken, err := env.tokenService.Issue(memberAID, env.aliceAccountID, auth.RoleAccountMember, time.Now().UTC())
	if err != nil {
		t.Fatalf("issue member token: %v", err)
	}

	memberAKey, _, err := env.apiKeysRepo.Create(context.Background(), env.aliceAccountID, memberAID, "member-a-key", []string{auth.PermDataThreadsRead})
	if err != nil {
		t.Fatalf("create member A key: %v", err)
	}
	memberBKey, _, err := env.apiKeysRepo.Create(context.Background(), env.aliceAccountID, memberBID, "member-b-key", []string{auth.PermDataThreadsRead})
	if err != nil {
		t.Fatalf("create member B key: %v", err)
	}

	listResp := doJSON(env.handler, nethttp.MethodGet, "/v1/api-keys", nil, authHeader(memberAToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list api keys: %d %s", listResp.Code, listResp.Body.String())
	}
	items := decodeJSONBody[[]apiKeyResponse](t, listResp.Body.Bytes())
	if len(items) != 1 || items[0].ID != memberAKey.ID.String() {
		t.Fatalf("unexpected visible keys: %#v", items)
	}

	revokeOther := doJSON(env.handler, nethttp.MethodDelete, "/v1/api-keys/"+memberBKey.ID.String(), nil, authHeader(memberAToken))
	assertErrorEnvelope(t, revokeOther, nethttp.StatusNotFound, "api_keys.not_found")

	adminList := doJSON(env.handler, nethttp.MethodGet, "/v1/api-keys", nil, authHeader(env.aliceToken))
	if adminList.Code != nethttp.StatusOK {
		t.Fatalf("admin list api keys: %d %s", adminList.Code, adminList.Body.String())
	}
	adminItems := decodeJSONBody[[]apiKeyResponse](t, adminList.Body.Bytes())
	if len(adminItems) != 2 {
		t.Fatalf("expected 2 keys for admin view, got %d", len(adminItems))
	}

	adminRevoke := doJSON(env.handler, nethttp.MethodDelete, "/v1/api-keys/"+memberBKey.ID.String(), nil, authHeader(env.aliceToken))
	if adminRevoke.Code != nethttp.StatusNoContent {
		t.Fatalf("admin revoke api key: %d %s", adminRevoke.Code, adminRevoke.Body.String())
	}
}

func TestAPIKeyUpdatesLastUsedAtAsync(t *testing.T) {
	handler, apiKeysRepo, jwtToken := buildAPIKeyHandler(t)

	createResp := doJSON(handler, nethttp.MethodPost, "/v1/api-keys",
		map[string]any{"name": "touch-key"},
		authHeader(jwtToken),
	)
	if createResp.Code != nethttp.StatusCreated {
		t.Fatalf("create api key: %d %s", createResp.Code, createResp.Body.String())
	}

	type createBody struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	created := decodeJSONBody[createBody](t, createResp.Body.Bytes())

	threadsResp := doJSON(handler, nethttp.MethodGet, "/v1/threads", nil, authHeader(created.Key))
	if threadsResp.Code != nethttp.StatusOK {
		t.Fatalf("list threads with api key: %d %s", threadsResp.Code, threadsResp.Body.String())
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		apiKey, err := apiKeysRepo.GetByHash(context.Background(), data.HashAPIKey(created.Key))
		if err != nil {
			t.Fatalf("get api key: %v", err)
		}
		if apiKey == nil {
			t.Fatal("expected api key to exist")
		}
		if apiKey.LastUsedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expected last_used_at to be updated")
		}
		time.Sleep(50 * time.Millisecond)
	}

	invalidResp := doJSON(handler, nethttp.MethodGet, "/v1/threads", nil, authHeader("ak-deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"))
	assertErrorEnvelope(t, invalidResp, nethttp.StatusUnauthorized, "auth.invalid_api_key")

	revokeResp := doJSON(handler, nethttp.MethodDelete, "/v1/api-keys/"+created.ID, nil, authHeader(jwtToken))
	if revokeResp.Code != nethttp.StatusNoContent {
		t.Fatalf("revoke: %d %s", revokeResp.Code, revokeResp.Body.String())
	}

	afterRevokeResp := doJSON(handler, nethttp.MethodGet, "/v1/threads", nil, authHeader(created.Key))
	assertErrorEnvelope(t, afterRevokeResp, nethttp.StatusUnauthorized, "auth.invalid_api_key")
}

func TestAPIKeyAuditLog(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_keys_audit")
	if _, err := migrate.Up(context.Background(), db.DSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
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
		t.Fatalf("new refresh token repo: %v", err)
	}
	auditRepo, err := data.NewAuditLogRepository(pool)
	if err != nil {
		t.Fatalf("audit repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("thread repo: %v", err)
	}
	apiKeysRepo, err := data.NewAPIKeysRepository(pool)
	if err != nil {
		t.Fatalf("api keys repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}
	jobRepo, err := data.NewJobRepository(pool)
	if err != nil {
		t.Fatalf("job repo: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	if err != nil {
		t.Fatalf("registration service: %v", err)
	}

	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)
	handler := NewHandler(HandlerConfig{
		Pool:                pool,
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		AccountMembershipRepo:   membershipRepo,
		ThreadRepo:          threadRepo,
		AuditWriter:         auditWriter,
		APIKeysRepo:         apiKeysRepo,
	})

	regResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "bob", "password": "pwd12345", "email": "bob@test.com"},
		nil,
	)
	if regResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", regResp.Code, regResp.Body.String())
	}
	regPayload := decodeJSONBody[registerResponse](t, regResp.Body.Bytes())
	jwtToken := regPayload.AccessToken

	createResp := doJSON(handler, nethttp.MethodPost, "/v1/api-keys",
		map[string]any{"name": "audit-key"},
		authHeader(jwtToken),
	)
	if createResp.Code != nethttp.StatusCreated {
		t.Fatalf("create api key: %d %s", createResp.Code, createResp.Body.String())
	}
	type createBody struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	created := decodeJSONBody[createBody](t, createResp.Body.Bytes())

	var createCount int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE action = 'api_keys.create'").Scan(&createCount); err != nil {
		t.Fatalf("count create audit: %v", err)
	}
	if createCount != 1 {
		t.Fatalf("expected 1 api_keys.create audit log, got %d", createCount)
	}

	// 吊销并检查审计
	revokeResp := doJSON(handler, nethttp.MethodDelete, "/v1/api-keys/"+created.ID, nil, authHeader(jwtToken))
	if revokeResp.Code != nethttp.StatusNoContent {
		t.Fatalf("revoke: %d %s", revokeResp.Code, revokeResp.Body.String())
	}

	var revokeCount int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE action = 'api_keys.revoke'").Scan(&revokeCount); err != nil {
		t.Fatalf("count revoke audit: %v", err)
	}
	if revokeCount != 1 {
		t.Fatalf("expected 1 api_keys.revoke audit log, got %d", revokeCount)
	}
}
