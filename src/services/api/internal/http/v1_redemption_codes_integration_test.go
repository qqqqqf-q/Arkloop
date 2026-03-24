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

func TestRedemptionCodesIntegration(t *testing.T) {
	db := setupTestDatabase(t, "api_go_redemption")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

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
	creditsRepo, err := data.NewCreditsRepository(pool)
	if err != nil {
		t.Fatalf("new credits repo: %v", err)
	}
	redemptionRepo, err := data.NewRedemptionCodesRepository(pool)
	if err != nil {
		t.Fatalf("new redemption repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
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
		AuditWriter:         auditWriter,
		AccountMembershipRepo:   membershipRepo,
		CreditsRepo:         creditsRepo,
		RedemptionCodesRepo: redemptionRepo,
		UsersRepo:           userRepo,
	})

	// 注册普通用户
	registerResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "redeem_user@test.com", "password": "testpass123", "email": "redeem_user@test.com"}, nil)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", registerResp.Code, registerResp.Body.String())
	}
	userPayload := decodeJSONBody[registerResponse](t, registerResp.Body.Bytes())
	userToken := userPayload.AccessToken

	// 注册管理员
	adminResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "redeem_admin@test.com", "password": "adminpass123", "email": "redeem_admin@test.com"}, nil)
	if adminResp.Code != nethttp.StatusCreated {
		t.Fatalf("register admin: %d %s", adminResp.Code, adminResp.Body.String())
	}
	adminPayload := decodeJSONBody[registerResponse](t, adminResp.Body.Bytes())

	_, err = pool.Exec(ctx, "UPDATE account_memberships SET role = $1 WHERE user_id = $2", auth.RolePlatformAdmin, adminPayload.UserID)
	if err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	loginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login",
		map[string]any{"login": "redeem_admin@test.com", "password": "adminpass123"}, nil)
	if loginResp.Code != nethttp.StatusOK {
		t.Fatalf("login admin: %d %s", loginResp.Code, loginResp.Body.String())
	}
	adminToken := decodeJSONBody[loginResponse](t, loginResp.Body.Bytes()).AccessToken

	// 批量生成 10 个积分兑换码
	var generatedCodes []redemptionCodeResponse

	t.Run("batch create redemption codes", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/admin/redemption-codes/batch",
			map[string]any{
				"count":    10,
				"type":     "credit",
				"value":    "100",
				"max_uses": 2,
				"batch_id": "test_batch_01",
			}, authHeader(adminToken))
		if resp.Code != nethttp.StatusCreated {
			t.Fatalf("batch create: %d %s", resp.Code, resp.Body.String())
		}

		generatedCodes = decodeJSONBody[[]redemptionCodeResponse](t, resp.Body.Bytes())
		if len(generatedCodes) != 10 {
			t.Fatalf("expected 10 codes, got %d", len(generatedCodes))
		}
	})

	t.Run("list redemption codes", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/redemption-codes?limit=20", nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("list: %d %s", resp.Code, resp.Body.String())
		}

		items := decodeJSONBody[[]redemptionCodeResponse](t, resp.Body.Bytes())
		if len(items) != 10 {
			t.Fatalf("expected 10 items, got %d", len(items))
		}
	})

	t.Run("redeem code and balance increases", func(t *testing.T) {
		// 获取兑换前余额
		beforeResp := doJSON(handler, nethttp.MethodGet, "/v1/me/credits", nil, authHeader(userToken))
		if beforeResp.Code != nethttp.StatusOK {
			t.Fatalf("get credits before: %d %s", beforeResp.Code, beforeResp.Body.String())
		}
		beforeCredits := decodeJSONBody[meCreditsResponse](t, beforeResp.Body.Bytes())
		beforeBalance := beforeCredits.Balance

		// 兑换
		code := generatedCodes[0].Code
		resp := doJSON(handler, nethttp.MethodPost, "/v1/me/redeem",
			map[string]any{"code": code}, authHeader(userToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("redeem: %d %s", resp.Code, resp.Body.String())
		}

		// 检查余额增加了 100
		afterResp := doJSON(handler, nethttp.MethodGet, "/v1/me/credits", nil, authHeader(userToken))
		if afterResp.Code != nethttp.StatusOK {
			t.Fatalf("get credits after: %d %s", afterResp.Code, afterResp.Body.String())
		}
		afterCredits := decodeJSONBody[meCreditsResponse](t, afterResp.Body.Bytes())
		if afterCredits.Balance != beforeBalance+100 {
			t.Fatalf("expected balance %d, got %d", beforeBalance+100, afterCredits.Balance)
		}
	})

	t.Run("duplicate redeem rejected", func(t *testing.T) {
		code := generatedCodes[0].Code
		resp := doJSON(handler, nethttp.MethodPost, "/v1/me/redeem",
			map[string]any{"code": code}, authHeader(userToken))
		assertErrorEnvelope(t, resp, nethttp.StatusConflict, "redemption_codes.already_redeemed")
	})

	t.Run("expired code rejected", func(t *testing.T) {
		// 生成一个过期码
		expiredResp := doJSON(handler, nethttp.MethodPost, "/v1/admin/redemption-codes/batch",
			map[string]any{
				"count":      1,
				"type":       "credit",
				"value":      "50",
				"max_uses":   1,
				"expires_at": "2020-01-01T00:00:00Z",
			}, authHeader(adminToken))
		if expiredResp.Code != nethttp.StatusCreated {
			t.Fatalf("batch create expired: %d %s", expiredResp.Code, expiredResp.Body.String())
		}

		expiredCodes := decodeJSONBody[[]redemptionCodeResponse](t, expiredResp.Body.Bytes())
		resp := doJSON(handler, nethttp.MethodPost, "/v1/me/redeem",
			map[string]any{"code": expiredCodes[0].Code}, authHeader(userToken))
		assertErrorEnvelope(t, resp, nethttp.StatusConflict, "redemption_codes.expired")
	})

	t.Run("deactivate code", func(t *testing.T) {
		codeID := generatedCodes[1].ID
		resp := doJSON(handler, nethttp.MethodPatch, "/v1/admin/redemption-codes/"+codeID,
			map[string]any{"is_active": false}, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("deactivate: %d %s", resp.Code, resp.Body.String())
		}

		result := decodeJSONBody[redemptionCodeResponse](t, resp.Body.Bytes())
		if result.IsActive {
			t.Fatal("expected is_active=false")
		}

		// 兑换已停用的码应被拒绝
		redeemResp := doJSON(handler, nethttp.MethodPost, "/v1/me/redeem",
			map[string]any{"code": generatedCodes[1].Code}, authHeader(userToken))
		assertErrorEnvelope(t, redeemResp, nethttp.StatusConflict, "redemption_codes.inactive")
	})

	t.Run("non-admin cannot batch create", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/admin/redemption-codes/batch",
			map[string]any{"count": 1, "type": "credit", "value": "100", "max_uses": 1},
			authHeader(userToken))
		if resp.Code != nethttp.StatusForbidden {
			t.Fatalf("expected 403, got %d", resp.Code)
		}
	})

	t.Run("invalid code returns not found", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/me/redeem",
			map[string]any{"code": "XXXX-XXXX-XXXX-XXXX"}, authHeader(userToken))
		assertErrorEnvelope(t, resp, nethttp.StatusNotFound, "redemption_codes.not_found")
	})
}
