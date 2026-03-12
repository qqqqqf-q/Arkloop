package http

import (
	"context"
	"io"
	nethttp "net/http"
	"testing"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

func TestAdminReportsListAndFilters(t *testing.T) {
	db := setupTestDatabase(t, "api_go_admin_reports")

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
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	threadReportRepo, err := data.NewThreadReportRepository(pool)
	if err != nil {
		t.Fatalf("new thread report repo: %v", err)
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

	handler := NewHandler(HandlerConfig{
		Pool:                pool,
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		AccountMembershipRepo:   membershipRepo,
		ThreadRepo:          threadRepo,
		ProjectRepo:         projectRepo,
		ThreadReportRepo:    threadReportRepo,
	})

	adminReg := doJSON(handler, nethttp.MethodPost, "/v1/auth/register", map[string]any{
		"login":    "admin_reports@test.com",
		"password": "adminpass123",
		"email":    "admin_reports@test.com",
	}, nil)
	if adminReg.Code != nethttp.StatusCreated {
		t.Fatalf("register admin: %d body=%s", adminReg.Code, adminReg.Body.String())
	}
	adminPayload := decodeJSONBody[registerResponse](t, adminReg.Body.Bytes())

	_, err = pool.Exec(ctx, "UPDATE account_memberships SET role = $1 WHERE user_id = $2", auth.RolePlatformAdmin, adminPayload.UserID)
	if err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	adminLogin := doJSON(handler, nethttp.MethodPost, "/v1/auth/login", map[string]any{
		"login":    "admin_reports@test.com",
		"password": "adminpass123",
	}, nil)
	if adminLogin.Code != nethttp.StatusOK {
		t.Fatalf("login admin: %d body=%s", adminLogin.Code, adminLogin.Body.String())
	}
	adminToken := decodeJSONBody[loginResponse](t, adminLogin.Body.Bytes()).AccessToken

	aliceReg := doJSON(handler, nethttp.MethodPost, "/v1/auth/register", map[string]any{
		"login":    "alice_report@test.com",
		"password": "alicepass123",
		"email":    "alice_report@test.com",
	}, nil)
	if aliceReg.Code != nethttp.StatusCreated {
		t.Fatalf("register alice: %d body=%s", aliceReg.Code, aliceReg.Body.String())
	}
	alicePayload := decodeJSONBody[registerResponse](t, aliceReg.Body.Bytes())
	aliceHeaders := authHeader(alicePayload.AccessToken)

	bobReg := doJSON(handler, nethttp.MethodPost, "/v1/auth/register", map[string]any{
		"login":    "bob_report@test.com",
		"password": "bobpass12345",
		"email":    "bob_report@test.com",
	}, nil)
	if bobReg.Code != nethttp.StatusCreated {
		t.Fatalf("register bob: %d body=%s", bobReg.Code, bobReg.Body.String())
	}
	bobPayload := decodeJSONBody[registerResponse](t, bobReg.Body.Bytes())
	bobHeaders := authHeader(bobPayload.AccessToken)

	aliceThreadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "alice thread"}, aliceHeaders)
	if aliceThreadResp.Code != nethttp.StatusCreated {
		t.Fatalf("create alice thread: %d body=%s", aliceThreadResp.Code, aliceThreadResp.Body.String())
	}
	aliceThread := decodeJSONBody[threadResponse](t, aliceThreadResp.Body.Bytes())

	bobThreadResp := doJSON(handler, nethttp.MethodPost, "/v1/threads", map[string]any{"title": "bob thread"}, bobHeaders)
	if bobThreadResp.Code != nethttp.StatusCreated {
		t.Fatalf("create bob thread: %d body=%s", bobThreadResp.Code, bobThreadResp.Body.String())
	}
	bobThread := decodeJSONBody[threadResponse](t, bobThreadResp.Body.Bytes())

	aliceReportResp := doJSON(handler, nethttp.MethodPost, "/v1/threads/"+aliceThread.ID+":report", map[string]any{
		"categories": []string{"inaccurate", "wrong_sources"},
		"feedback":   "citation mismatch",
	}, aliceHeaders)
	if aliceReportResp.Code != nethttp.StatusCreated {
		t.Fatalf("create alice report: %d body=%s", aliceReportResp.Code, aliceReportResp.Body.String())
	}
	aliceReport := decodeJSONBody[reportResponse](t, aliceReportResp.Body.Bytes())

	bobReportResp := doJSON(handler, nethttp.MethodPost, "/v1/threads/"+bobThread.ID+":report", map[string]any{
		"categories": []string{"harmful_or_offensive"},
		"feedback":   "abusive output",
	}, bobHeaders)
	if bobReportResp.Code != nethttp.StatusCreated {
		t.Fatalf("create bob report: %d body=%s", bobReportResp.Code, bobReportResp.Body.String())
	}

	t.Run("forbidden for non-admin", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/reports", nil, aliceHeaders)
		assertErrorEnvelope(t, resp, nethttp.StatusForbidden, "auth.forbidden")
	})

	t.Run("list all reports", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/reports?limit=50", nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("list reports: %d body=%s", resp.Code, resp.Body.String())
		}
		body := decodeJSONBody[adminReportsResponse](t, resp.Body.Bytes())
		if body.Total != 2 {
			t.Fatalf("expected total=2, got %d", body.Total)
		}
		if len(body.Data) != 2 {
			t.Fatalf("expected 2 items, got %d", len(body.Data))
		}
	})

	t.Run("filter by report_id", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/reports?report_id="+aliceReport.ID, nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("filter by report_id: %d body=%s", resp.Code, resp.Body.String())
		}
		body := decodeJSONBody[adminReportsResponse](t, resp.Body.Bytes())
		if body.Total != 1 || len(body.Data) != 1 || body.Data[0].ID != aliceReport.ID {
			t.Fatalf("unexpected report_id filter result: %#v total=%d", body.Data, body.Total)
		}
	})

	t.Run("filter by report_id prefix", func(t *testing.T) {
		prefix := aliceReport.ID[:8]
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/reports?report_id="+prefix, nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("filter by report_id prefix: %d body=%s", resp.Code, resp.Body.String())
		}
		body := decodeJSONBody[adminReportsResponse](t, resp.Body.Bytes())
		if body.Total != 1 || len(body.Data) != 1 || body.Data[0].ID != aliceReport.ID {
			t.Fatalf("unexpected report_id prefix result: %#v total=%d", body.Data, body.Total)
		}
	})

	t.Run("filter by thread_id", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/reports?thread_id="+aliceThread.ID, nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("filter by thread_id: %d body=%s", resp.Code, resp.Body.String())
		}
		body := decodeJSONBody[adminReportsResponse](t, resp.Body.Bytes())
		if body.Total != 1 || len(body.Data) != 1 || body.Data[0].ThreadID != aliceThread.ID {
			t.Fatalf("unexpected thread filter result: %#v total=%d", body.Data, body.Total)
		}
	})

	t.Run("filter by thread_id prefix", func(t *testing.T) {
		prefix := aliceThread.ID[:8]
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/reports?thread_id="+prefix, nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("filter by thread_id prefix: %d body=%s", resp.Code, resp.Body.String())
		}
		body := decodeJSONBody[adminReportsResponse](t, resp.Body.Bytes())
		if body.Total != 1 || len(body.Data) != 1 || body.Data[0].ThreadID != aliceThread.ID {
			t.Fatalf("unexpected thread prefix result: %#v total=%d", body.Data, body.Total)
		}
	})

	t.Run("filter by reporter_id", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/reports?reporter_id="+alicePayload.UserID, nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("filter by reporter_id: %d body=%s", resp.Code, resp.Body.String())
		}
		body := decodeJSONBody[adminReportsResponse](t, resp.Body.Bytes())
		if body.Total != 1 || len(body.Data) != 1 || body.Data[0].ReporterID != alicePayload.UserID {
			t.Fatalf("unexpected reporter filter result: %#v total=%d", body.Data, body.Total)
		}
	})

	t.Run("filter by reporter_email", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/reports?reporter_email=alice_report", nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("filter by reporter_email: %d body=%s", resp.Code, resp.Body.String())
		}
		body := decodeJSONBody[adminReportsResponse](t, resp.Body.Bytes())
		if body.Total != 1 || len(body.Data) != 1 || body.Data[0].ReporterID != alicePayload.UserID {
			t.Fatalf("unexpected reporter_email filter result: %#v total=%d", body.Data, body.Total)
		}
	})

	t.Run("filter by category", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/reports?category=harmful_or_offensive", nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("filter by category: %d body=%s", resp.Code, resp.Body.String())
		}
		body := decodeJSONBody[adminReportsResponse](t, resp.Body.Bytes())
		if body.Total != 1 || len(body.Data) != 1 || body.Data[0].ThreadID != bobThread.ID {
			t.Fatalf("unexpected category filter result: %#v total=%d", body.Data, body.Total)
		}
	})

	t.Run("filter by feedback keyword", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/reports?feedback=citation", nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("filter by feedback: %d body=%s", resp.Code, resp.Body.String())
		}
		body := decodeJSONBody[adminReportsResponse](t, resp.Body.Bytes())
		if body.Total != 1 || len(body.Data) != 1 || body.Data[0].ID != aliceReport.ID {
			t.Fatalf("unexpected feedback filter result: %#v total=%d", body.Data, body.Total)
		}
	})

	t.Run("suggestion feedback appears in reports", func(t *testing.T) {
		createResp := doJSON(handler, nethttp.MethodPost, "/v1/me/feedback", map[string]any{
			"feedback": "please add export markdown",
		}, aliceHeaders)
		if createResp.Code != nethttp.StatusCreated {
			t.Fatalf("create feedback: %d body=%s", createResp.Code, createResp.Body.String())
		}

		listResp := doJSON(handler, nethttp.MethodGet, "/v1/admin/reports?category=product_suggestion", nil, authHeader(adminToken))
		if listResp.Code != nethttp.StatusOK {
			t.Fatalf("filter suggestion: %d body=%s", listResp.Code, listResp.Body.String())
		}
		body := decodeJSONBody[adminReportsResponse](t, listResp.Body.Bytes())
		if body.Total != 1 || len(body.Data) != 1 {
			t.Fatalf("unexpected suggestion count: total=%d data=%#v", body.Total, body.Data)
		}
		item := body.Data[0]
		if item.ThreadID != "" {
			t.Fatalf("expected empty thread_id for suggestion, got %q", item.ThreadID)
		}
		if item.Feedback == nil || *item.Feedback != "please add export markdown" {
			t.Fatalf("unexpected suggestion feedback: %#v", item.Feedback)
		}
	})

	t.Run("invalid thread_id returns 422", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/reports?thread_id=invalid", nil, authHeader(adminToken))
		assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation.error")
	})

	t.Run("since after until returns 422", func(t *testing.T) {
		resp := doJSON(
			handler,
			nethttp.MethodGet,
			"/v1/admin/reports?since=2026-03-01T00:00:00Z&until=2026-02-01T00:00:00Z",
			nil,
			authHeader(adminToken),
		)
		assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation.error")
	})
}
