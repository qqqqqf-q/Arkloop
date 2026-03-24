//go:build !desktop

package http

import (
	"context"
	"io"
	"log/slog"
	"testing"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
)

// TestRBACPermissions 验证权限体系的核心场景：
//   - org_member 无法创建/列出邀请（403）
//   - org_admin (owner) 可以创建/列出/撤销邀请（201/200/204）
//   - 跨 org 操作被拒绝（403 access denied）
//   - 未知角色无任何权限（403）
func TestRBACPermissions(t *testing.T) {
	db := setupTestDatabase(t, "api_go_rbac")
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
	authService, err := auth.NewService(userRepo, credRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}
	jobRepo, err := data.NewJobRepository(pool)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
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
	})

	// 注册 owner (A)
	regA := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "rbac_owner", "password": "pwd12345", "email": "rbac_owner@test.com"},
		nil,
	)
	if regA.Code != nethttp.StatusCreated {
		t.Fatalf("register owner: %d %s", regA.Code, regA.Body.String())
	}
	payloadA := decodeJSONBody[registerResponse](t, regA.Body.Bytes())
	tokenA := payloadA.AccessToken
	userAID := payloadA.UserID

	// 注册用户 B，随后降为 member
	regB := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "rbac_member", "password": "pwd12345", "email": "rbac_member@test.com"},
		nil,
	)
	if regB.Code != nethttp.StatusCreated {
		t.Fatalf("register member: %d %s", regB.Code, regB.Body.String())
	}
	payloadB := decodeJSONBody[registerResponse](t, regB.Body.Bytes())
	var tokenB string
	userBID := payloadB.UserID

	// 查询两人各自的 account_id
	var orgAID, orgBID string
	if err := pool.QueryRow(ctx,
		"SELECT account_id FROM account_memberships WHERE user_id = $1", userAID,
	).Scan(&orgAID); err != nil {
		t.Fatalf("get orgA id: %v", err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT account_id FROM account_memberships WHERE user_id = $1", userBID,
	).Scan(&orgBID); err != nil {
		t.Fatalf("get orgB id: %v", err)
	}

	// 将 B 在 orgB 的角色降为 "member"，模拟受邀普通成员
	if _, err := pool.Exec(ctx,
		"UPDATE account_memberships SET role = 'member' WHERE user_id = $1", userBID,
	); err != nil {
		t.Fatalf("demote B: %v", err)
	}

	// 角色来源于 token claims，需要重新登录获取新 token
	reLoginB := doJSON(handler, nethttp.MethodPost, "/v1/auth/login",
		map[string]any{"login": "rbac_member", "password": "pwd12345"}, nil)
	if reLoginB.Code != nethttp.StatusOK {
		t.Fatalf("re-login B: %d %s", reLoginB.Code, reLoginB.Body.String())
	}
	tokenB = decodeJSONBody[loginResponse](t, reLoginB.Body.Bytes()).AccessToken

	inviteBody := map[string]any{"email": "guest@example.com", "role": "member"}

	// 场景 1：member (B) 尝试在自己的 org 创建邀请 → 403（无 invite 权限）
	memberInvite := doJSON(handler, nethttp.MethodPost,
		"/v1/accounts/"+orgBID+"/invitations",
		inviteBody,
		authHeader(tokenB),
	)
	assertErrorEnvelope(t, memberInvite, nethttp.StatusForbidden, "auth.forbidden")

	// 场景 2：owner (A) 在自己的 org 创建邀请 → 201
	ownerInvite := doJSON(handler, nethttp.MethodPost,
		"/v1/accounts/"+orgAID+"/invitations",
		inviteBody,
		authHeader(tokenA),
	)
	if ownerInvite.Code != nethttp.StatusCreated {
		t.Fatalf("owner invite: expected 201, got %d body=%s", ownerInvite.Code, ownerInvite.Body.String())
	}

	// 场景 3：member (B) 尝试列出自己 org 的邀请 → 403
	memberList := doJSON(handler, nethttp.MethodGet,
		"/v1/accounts/"+orgBID+"/invitations",
		nil,
		authHeader(tokenB),
	)
	assertErrorEnvelope(t, memberList, nethttp.StatusForbidden, "auth.forbidden")

	// 场景 4：owner (A) 可以列出邀请 → 200
	ownerList := doJSON(handler, nethttp.MethodGet,
		"/v1/accounts/"+orgAID+"/invitations",
		nil,
		authHeader(tokenA),
	)
	if ownerList.Code != nethttp.StatusOK {
		t.Fatalf("owner list: expected 200, got %d body=%s", ownerList.Code, ownerList.Body.String())
	}

	// 场景 5：跨 org 隔离 —— owner (A) 尝试操作 B 的 org → 403 access denied（org 不匹配）
	crossOrgInvite := doJSON(handler, nethttp.MethodPost,
		"/v1/accounts/"+orgBID+"/invitations",
		inviteBody,
		authHeader(tokenA),
	)
	assertErrorEnvelope(t, crossOrgInvite, nethttp.StatusForbidden, "auth.forbidden")

	crossOrgList := doJSON(handler, nethttp.MethodGet,
		"/v1/accounts/"+orgBID+"/invitations",
		nil,
		authHeader(tokenA),
	)
	assertErrorEnvelope(t, crossOrgList, nethttp.StatusForbidden, "auth.forbidden")

	// 场景 6：owner (A) 可以撤销自己 org 内的邀请 → 204
	type inviteResp struct {
		ID string `json:"id"`
	}
	created := decodeJSONBody[inviteResp](t, ownerInvite.Body.Bytes())
	revokeResp := doJSON(handler, nethttp.MethodDelete,
		"/v1/account-invitations/"+created.ID,
		nil,
		authHeader(tokenA),
	)
	if revokeResp.Code != nethttp.StatusNoContent {
		t.Fatalf("owner revoke: expected 204, got %d body=%s", revokeResp.Code, revokeResp.Body.String())
	}

	// 场景 7：member (B) 无法撤销邀请 → 403
	// 先让 owner 再建一条邀请
	ownerInvite2 := doJSON(handler, nethttp.MethodPost,
		"/v1/accounts/"+orgAID+"/invitations",
		map[string]any{"email": "guest2@example.com", "role": "member"},
		authHeader(tokenA),
	)
	if ownerInvite2.Code != nethttp.StatusCreated {
		t.Fatalf("owner invite2: expected 201, got %d body=%s", ownerInvite2.Code, ownerInvite2.Body.String())
	}
	// B 无法撤销 A org 内的邀请（org 不匹配先触发）
	created2 := decodeJSONBody[inviteResp](t, ownerInvite2.Body.Bytes())
	memberRevoke := doJSON(handler, nethttp.MethodDelete,
		"/v1/account-invitations/"+created2.ID,
		nil,
		authHeader(tokenB),
	)
	assertErrorEnvelope(t, memberRevoke, nethttp.StatusForbidden, "auth.forbidden")

	// 场景 8：未知角色（无任何权限）无法执行任何邀请操作
	if _, err := pool.Exec(ctx,
		"UPDATE account_memberships SET role = 'legacy_role' WHERE user_id = $1", userBID,
	); err != nil {
		t.Fatalf("set legacy role: %v", err)
	}
	// 同样需要重新登录拿到 role=legacy_role 的 token
	reLoginB2 := doJSON(handler, nethttp.MethodPost, "/v1/auth/login",
		map[string]any{"login": "rbac_member", "password": "pwd12345"}, nil)
	if reLoginB2.Code != nethttp.StatusOK {
		t.Fatalf("re-login B (legacy): %d %s", reLoginB2.Code, reLoginB2.Body.String())
	}
	tokenB = decodeJSONBody[loginResponse](t, reLoginB2.Body.Bytes()).AccessToken

	unknownRoleInvite := doJSON(handler, nethttp.MethodPost,
		"/v1/accounts/"+orgBID+"/invitations",
		inviteBody,
		authHeader(tokenB),
	)
	assertErrorEnvelope(t, unknownRoleInvite, nethttp.StatusForbidden, "auth.forbidden")
}
