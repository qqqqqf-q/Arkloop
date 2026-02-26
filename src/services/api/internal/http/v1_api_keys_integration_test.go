package http

import (
	"context"
	"io"
	"testing"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/testutil"
)

func buildAPIKeyHandler(t *testing.T) (nethttp.Handler, *data.APIKeysRepository, string) {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "api_keys")
	if _, err := migrate.Up(context.Background(), db.DSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	logger := observability.NewJSONLogger("test", io.Discard)
	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 7776000)
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
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	apiKeysRepo, err := data.NewAPIKeysRepository(pool)
	if err != nil {
		t.Fatalf("new api keys repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo)
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
		OrgMembershipRepo:   membershipRepo,
		ThreadRepo:          threadRepo,
		AuditWriter:         auditWriter,
		APIKeysRepo:         apiKeysRepo,
	})

	// 注册一个测试用户并获取 JWT token
	regResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "alice", "password": "pwdpwdpwd", "display_name": "Alice"},
		nil,
	)
	if regResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", regResp.Code, regResp.Body.String())
	}
	regPayload := decodeJSONBody[registerResponse](t, regResp.Body.Bytes())

	return handler, apiKeysRepo, regPayload.AccessToken
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
		map[string]any{"name": "ci-key"},
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

func TestAPIKeyAuditLog(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_keys_audit")
	if _, err := migrate.Up(context.Background(), db.DSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	logger := observability.NewJSONLogger("test", io.Discard)
	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 7776000)
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
	membershipRepo, err := data.NewOrgMembershipRepository(pool)
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

	authService, err := auth.NewService(userRepo, credRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo)
	if err != nil {
		t.Fatalf("registration service: %v", err)
	}

	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)
	handler := NewHandler(HandlerConfig{
		Pool:                pool,
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		OrgMembershipRepo:   membershipRepo,
		ThreadRepo:          threadRepo,
		AuditWriter:         auditWriter,
		APIKeysRepo:         apiKeysRepo,
	})

	regResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "bob", "password": "pwdpwdpwd", "display_name": "Bob"},
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
