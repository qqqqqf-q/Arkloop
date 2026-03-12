package http

import (
	"context"
	"io"
	nethttp "net/http"
	"testing"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

func TestAdminUsersListSearchPatchAndForbidden(t *testing.T) {
	db := setupTestDatabase(t, "api_go_admin_users")

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
		AccountMembershipRepo:   membershipRepo,
		UsersRepo:           userRepo,
		AuditWriter:         auditWriter,
	})

	// 注册管理员用户
	adminReg := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "admin@test.com", "password": "adminpass123", "email": "admin@test.com"}, nil)
	if adminReg.Code != nethttp.StatusCreated {
		t.Fatalf("register admin: %d %s", adminReg.Code, adminReg.Body.String())
	}
	adminPayload := decodeJSONBody[registerResponse](t, adminReg.Body.Bytes())
	adminToken := adminPayload.AccessToken

	// 提升为 platform_admin
	_, err = pool.Exec(ctx, "UPDATE account_memberships SET role = $1 WHERE user_id = $2", auth.RolePlatformAdmin, adminPayload.UserID)
	if err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	// token 中的权限基于 role 实时解析，需要重新登录获取新 token
	loginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login",
		map[string]any{"login": "admin@test.com", "password": "adminpass123"}, nil)
	if loginResp.Code != nethttp.StatusOK {
		t.Fatalf("login admin: %d %s", loginResp.Code, loginResp.Body.String())
	}
	adminToken = decodeJSONBody[loginResponse](t, loginResp.Body.Bytes()).AccessToken

	// 注册普通用户
	regAlice := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "alice@test.com", "password": "alicepass123", "email": "alice@test.com"}, nil)
	if regAlice.Code != nethttp.StatusCreated {
		t.Fatalf("register alice: %d %s", regAlice.Code, regAlice.Body.String())
	}
	alicePayload := decodeJSONBody[registerResponse](t, regAlice.Body.Bytes())

	regBob := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "bob@test.com", "password": "bobpass12345", "email": "bob@test.com"}, nil)
	if regBob.Code != nethttp.StatusCreated {
		t.Fatalf("register bob: %d %s", regBob.Code, regBob.Body.String())
	}

	// 测试: 非 admin 用户请求 403
	t.Run("forbidden for non-admin", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/users", nil, authHeader(alicePayload.AccessToken))
		assertErrorEnvelope(t, resp, nethttp.StatusForbidden, "auth.forbidden")
	})

	// 测试: 列表分页
	t.Run("list users", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/users?limit=50", nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("list: %d %s", resp.Code, resp.Body.String())
		}
		users := decodeJSONBody[[]adminUserResponse](t, resp.Body.Bytes())
		if len(users) < 3 {
			t.Fatalf("expected at least 3 users, got %d", len(users))
		}
	})

	// 测试: 搜索 username
	t.Run("search by username", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/users?q=Alice&limit=50", nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("search: %d %s", resp.Code, resp.Body.String())
		}
		users := decodeJSONBody[[]adminUserResponse](t, resp.Body.Bytes())
		if len(users) != 1 {
			t.Fatalf("expected 1 user, got %d", len(users))
		}
		if users[0].Username != "alice@test.com" {
			t.Fatalf("expected alice@test.com, got %s", users[0].Username)
		}
	})

	// 测试: 搜索 email
	t.Run("search by email", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/users?q=bob@test&limit=50", nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("search: %d %s", resp.Code, resp.Body.String())
		}
		users := decodeJSONBody[[]adminUserResponse](t, resp.Body.Bytes())
		if len(users) != 1 {
			t.Fatalf("expected 1 user, got %d", len(users))
		}
	})

	// 测试: status 过滤
	t.Run("filter by status", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/users?status=active&limit=50", nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("filter: %d %s", resp.Code, resp.Body.String())
		}
		users := decodeJSONBody[[]adminUserResponse](t, resp.Body.Bytes())
		for _, u := range users {
			if u.Status != "active" {
				t.Fatalf("expected active status, got %s", u.Status)
			}
		}
	})

	// 测试: 无效 status 过滤
	t.Run("invalid status filter", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/users?status=invalid&limit=50", nil, authHeader(adminToken))
		assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation.error")
	})

	// 测试: 获取用户详情
	t.Run("get user detail", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/users/"+alicePayload.UserID, nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("get: %d %s", resp.Code, resp.Body.String())
		}
		detail := decodeJSONBody[adminUserDetailResponse](t, resp.Body.Bytes())
		if detail.Username != "alice@test.com" {
			t.Fatalf("expected alice@test.com, got %s", detail.Username)
		}
		if len(detail.Accounts) == 0 {
			t.Fatal("expected at least one org")
		}
	})

	// 测试: 获取不存在的用户
	t.Run("get nonexistent user", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/users/00000000-0000-0000-0000-000000000099", nil, authHeader(adminToken))
		assertErrorEnvelope(t, resp, nethttp.StatusNotFound, "users.not_found")
	})

	// 测试: 封禁用户
	t.Run("suspend user", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPatch, "/v1/admin/users/"+alicePayload.UserID,
			map[string]any{"status": "suspended"}, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("suspend: %d %s", resp.Code, resp.Body.String())
		}
		updated := decodeJSONBody[adminUserResponse](t, resp.Body.Bytes())
		if updated.Status != "suspended" {
			t.Fatalf("expected suspended, got %s", updated.Status)
		}
	})

	// 测试: 封禁后旧 token 立即失效（强一致吊销）
	t.Run("suspended user token revoked", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/me", nil, authHeader(alicePayload.AccessToken))
		assertErrorEnvelope(t, resp, nethttp.StatusUnauthorized, "auth.invalid_token")
	})

	// 测试: 封禁后状态过滤可见
	t.Run("suspended user visible in filter", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/users?status=suspended&limit=50", nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("filter: %d %s", resp.Code, resp.Body.String())
		}
		users := decodeJSONBody[[]adminUserResponse](t, resp.Body.Bytes())
		found := false
		for _, u := range users {
			if u.ID == alicePayload.UserID {
				found = true
			}
		}
		if !found {
			t.Fatal("suspended user not found in filter")
		}
	})

	// 测试: 解封用户
	t.Run("reactivate user", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPatch, "/v1/admin/users/"+alicePayload.UserID,
			map[string]any{"status": "active"}, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("reactivate: %d %s", resp.Code, resp.Body.String())
		}
		updated := decodeJSONBody[adminUserResponse](t, resp.Body.Bytes())
		if updated.Status != "active" {
			t.Fatalf("expected active, got %s", updated.Status)
		}
	})

	// 测试: 无效 status 值
	t.Run("patch invalid status", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPatch, "/v1/admin/users/"+alicePayload.UserID,
			map[string]any{"status": "deleted"}, authHeader(adminToken))
		assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation.error")
	})

	// 测试: PATCH 缺少 status
	t.Run("patch missing status", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPatch, "/v1/admin/users/"+alicePayload.UserID,
			map[string]any{}, authHeader(adminToken))
		assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation.error")
	})

	// 测试: 分页 cursor
	t.Run("pagination with limit=1", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/users?limit=1", nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("page1: %d %s", resp.Code, resp.Body.String())
		}
		page1 := decodeJSONBody[[]adminUserResponse](t, resp.Body.Bytes())
		if len(page1) != 1 {
			t.Fatalf("expected 1 user, got %d", len(page1))
		}

		// 使用 cursor 获取下一页
		cursor := "before_created_at=" + page1[0].CreatedAt + "&before_id=" + page1[0].ID
		resp2 := doJSON(handler, nethttp.MethodGet, "/v1/admin/users?limit=1&"+cursor, nil, authHeader(adminToken))
		if resp2.Code != nethttp.StatusOK {
			t.Fatalf("page2: %d %s", resp2.Code, resp2.Body.String())
		}
		page2 := decodeJSONBody[[]adminUserResponse](t, resp2.Body.Bytes())
		if len(page2) != 1 {
			t.Fatalf("expected 1 user on page 2, got %d", len(page2))
		}
		if page2[0].ID == page1[0].ID {
			t.Fatal("page 2 returned same user as page 1")
		}
	})
}
