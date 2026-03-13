//go:build !desktop

package http

import (
	"context"
	"io"
	"testing"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

func TestCreditsIntegration(t *testing.T) {
	db := setupTestDatabase(t, "api_go_credits")

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
	creditsRepo, err := data.NewCreditsRepository(pool)
	if err != nil {
		t.Fatalf("new credits repo: %v", err)
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
		Pool:                pool,
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		AuditWriter:         auditWriter,
		AccountMembershipRepo:   membershipRepo,
		CreditsRepo:         creditsRepo,
		UsersRepo:           userRepo,
	})

	registerResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "credit_user@test.com", "password": "testpass123", "email": "credit_user@test.com"}, nil)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", registerResp.Code, registerResp.Body.String())
	}
	regPayload := decodeJSONBody[registerResponse](t, registerResp.Body.Bytes())
	token := regPayload.AccessToken

	t.Run("initial grant on registration", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/me/credits", nil, authHeader(token))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("get credits: %d %s", resp.Code, resp.Body.String())
		}

		payload := decodeJSONBody[meCreditsResponse](t, resp.Body.Bytes())
		if payload.Balance != 1000 {
			t.Fatalf("expected balance 1000, got %d", payload.Balance)
		}
		if len(payload.Transactions) != 1 {
			t.Fatalf("expected 1 transaction, got %d", len(payload.Transactions))
		}
		if payload.Transactions[0].Type != "initial_grant" {
			t.Fatalf("expected initial_grant, got %s", payload.Transactions[0].Type)
		}
	})

	adminResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "admin@test.com", "password": "adminpass123", "email": "admin@test.com"}, nil)
	if adminResp.Code != nethttp.StatusCreated {
		t.Fatalf("register admin: %d %s", adminResp.Code, adminResp.Body.String())
	}
	adminPayload := decodeJSONBody[registerResponse](t, adminResp.Body.Bytes())

	if _, err = pool.Exec(ctx, "UPDATE account_memberships SET role = $1 WHERE user_id = $2", auth.RolePlatformAdmin, adminPayload.UserID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	loginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login",
		map[string]any{"login": "admin@test.com", "password": "adminpass123"}, nil)
	if loginResp.Code != nethttp.StatusOK {
		t.Fatalf("login admin: %d %s", loginResp.Code, loginResp.Body.String())
	}
	adminToken := decodeJSONBody[loginResponse](t, loginResp.Body.Bytes()).AccessToken

	meResp := doJSON(handler, nethttp.MethodGet, "/v1/me", nil, authHeader(token))
	if meResp.Code != nethttp.StatusOK {
		t.Fatalf("get me: %d %s", meResp.Code, meResp.Body.String())
	}
	type meMinimalResponse struct {
		AccountID string `json:"account_id"`
	}
	meData := decodeJSONBody[meMinimalResponse](t, meResp.Body.Bytes())
	accountID := meData.AccountID

	t.Run("admin adjust positive writes audit", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/admin/credits/adjust",
			map[string]any{"account_id": accountID, "amount": 500, "note": "test top-up"},
			authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("adjust: %d %s", resp.Code, resp.Body.String())
		}

		payload := decodeJSONBody[creditBalanceResponse](t, resp.Body.Bytes())
		if payload.Balance != 1500 {
			t.Fatalf("expected balance 1500, got %d", payload.Balance)
		}

		log := latestAuditLogByAction(t, ctx, pool, "credits.adjust")
		if log.TargetType == nil || *log.TargetType != "org" {
			t.Fatalf("unexpected target_type: %#v", log.TargetType)
		}
		if log.TargetID == nil || *log.TargetID != accountID {
			t.Fatalf("unexpected target_id: %#v", log.TargetID)
		}
		if log.Metadata["amount"] != float64(500) || log.Metadata["note"] != "test top-up" {
			t.Fatalf("unexpected metadata: %#v", log.Metadata)
		}
		if log.BeforeState["balance"] != float64(1000) || log.AfterState["balance"] != float64(1500) {
			t.Fatalf("unexpected states: before=%#v after=%#v", log.BeforeState, log.AfterState)
		}
	})

	t.Run("admin adjust negative writes audit", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/admin/credits/adjust",
			map[string]any{"account_id": accountID, "amount": -200, "note": "test deduction"},
			authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("adjust: %d %s", resp.Code, resp.Body.String())
		}

		payload := decodeJSONBody[creditBalanceResponse](t, resp.Body.Bytes())
		if payload.Balance != 1300 {
			t.Fatalf("expected balance 1300, got %d", payload.Balance)
		}
		if count := countAuditLogByAction(t, ctx, pool, "credits.adjust"); count != 2 {
			t.Fatalf("expected 2 credits.adjust audit logs, got %d", count)
		}

		log := latestAuditLogByAction(t, ctx, pool, "credits.adjust")
		if log.Metadata["amount"] != float64(-200) || log.Metadata["note"] != "test deduction" {
			t.Fatalf("unexpected metadata: %#v", log.Metadata)
		}
		if log.BeforeState["balance"] != float64(1500) || log.AfterState["balance"] != float64(1300) {
			t.Fatalf("unexpected states: before=%#v after=%#v", log.BeforeState, log.AfterState)
		}
	})

	t.Run("admin adjust requires note", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/admin/credits/adjust",
			map[string]any{"account_id": accountID, "amount": 100, "note": ""},
			authHeader(adminToken))
		assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation.error")
	})

	t.Run("admin view org credits", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/credits?account_id="+accountID, nil,
			authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("admin credits: %d %s", resp.Code, resp.Body.String())
		}

		type adminResp struct {
			AccountID        string                      `json:"account_id"`
			Balance      int64                       `json:"balance"`
			Transactions []creditTransactionResponse `json:"transactions"`
		}
		payload := decodeJSONBody[adminResp](t, resp.Body.Bytes())
		if payload.Balance != 1300 {
			t.Fatalf("expected balance 1300, got %d", payload.Balance)
		}
		if len(payload.Transactions) != 3 {
			t.Fatalf("expected 3 transactions, got %d", len(payload.Transactions))
		}
	})

	t.Run("non-admin forbidden", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/credits?account_id="+accountID, nil,
			authHeader(token))
		if resp.Code != nethttp.StatusForbidden {
			t.Fatalf("expected 403, got %d", resp.Code)
		}
	})

	t.Run("admin bulk adjust writes summary audit", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/admin/credits/bulk-adjust",
			map[string]any{"amount": 100, "note": "batch top-up"},
			authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("bulk adjust: %d %s", resp.Code, resp.Body.String())
		}

		type bulkResp struct {
			Affected int64 `json:"affected"`
		}
		payload := decodeJSONBody[bulkResp](t, resp.Body.Bytes())
		if payload.Affected != 2 {
			t.Fatalf("expected affected 2, got %d", payload.Affected)
		}

		log := latestAuditLogByAction(t, ctx, pool, "credits.bulk_adjust")
		if log.TargetType == nil || *log.TargetType != "credits_batch" {
			t.Fatalf("unexpected target_type: %#v", log.TargetType)
		}
		if log.Metadata["amount"] != float64(100) || log.Metadata["affected_count"] != float64(2) {
			t.Fatalf("unexpected metadata: %#v", log.Metadata)
		}
	})

	t.Run("admin reset all writes summary audit", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/admin/credits/reset-all",
			map[string]any{"note": "cleanup credits"},
			authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("reset all: %d %s", resp.Code, resp.Body.String())
		}

		type resetResp struct {
			Affected int64 `json:"affected"`
		}
		payload := decodeJSONBody[resetResp](t, resp.Body.Bytes())
		if payload.Affected != 2 {
			t.Fatalf("expected affected 2, got %d", payload.Affected)
		}

		log := latestAuditLogByAction(t, ctx, pool, "credits.reset_all")
		if log.TargetType == nil || *log.TargetType != "credits_batch" {
			t.Fatalf("unexpected target_type: %#v", log.TargetType)
		}
		if log.Metadata["operation"] != "reset_all" || log.Metadata["affected_count"] != float64(2) {
			t.Fatalf("unexpected metadata: %#v", log.Metadata)
		}

		checkResp := doJSON(handler, nethttp.MethodGet, "/v1/admin/credits?account_id="+accountID, nil, authHeader(adminToken))
		if checkResp.Code != nethttp.StatusOK {
			t.Fatalf("view after reset: %d %s", checkResp.Code, checkResp.Body.String())
		}
		type adminResp struct {
			Balance int64 `json:"balance"`
		}
		check := decodeJSONBody[adminResp](t, checkResp.Body.Bytes())
		if check.Balance != 0 {
			t.Fatalf("expected balance 0 after reset, got %d", check.Balance)
		}
	})

	t.Run("insufficient credits", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/admin/credits/adjust",
			map[string]any{"account_id": accountID, "amount": -99999, "note": "drain test"},
			authHeader(adminToken))
		if resp.Code != nethttp.StatusConflict {
			t.Fatalf("expected 409, got %d %s", resp.Code, resp.Body.String())
		}
	})
}
