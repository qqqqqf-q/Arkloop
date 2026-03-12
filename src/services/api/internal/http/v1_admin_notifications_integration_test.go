//go:build !desktop

package http

import (
	"context"
	"io"
	nethttp "net/http"
	"testing"
	"time"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

func TestAdminBroadcastsCreateListAndForbidden(t *testing.T) {
	db := setupTestDatabase(t, "api_go_admin_broadcasts")

	ctx := context.Background()
	appDB, _, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer appDB.Close()

	logger := observability.NewJSONLogger("test", io.Discard)

	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	userRepo, err := data.NewUserRepository(appDB)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credentialRepo, err := data.NewUserCredentialRepository(appDB)
	if err != nil {
		t.Fatalf("new credential repo: %v", err)
	}
	membershipRepo, err := data.NewOrgMembershipRepository(appDB)
	if err != nil {
		t.Fatalf("new membership repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(appDB)
	if err != nil {
		t.Fatalf("new refresh token repo: %v", err)
	}
	auditRepo, err := data.NewAuditLogRepository(appDB)
	if err != nil {
		t.Fatalf("new audit repo: %v", err)
	}
	notifRepo, err := data.NewNotificationsRepository(appDB)
	if err != nil {
		t.Fatalf("new notifications repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	jobRepo, err := data.NewJobRepository(appDB)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(appDB, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	if err != nil {
		t.Fatalf("new registration service: %v", err)
	}

	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		DB:                appDB,
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		OrgMembershipRepo:   membershipRepo,
		NotificationsRepo:   notifRepo,
		AuditWriter:         auditWriter,
	})

	// 注册管理员
	adminReg := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "admin@test.com", "password": "adminpass123", "email": "admin@test.com"}, nil)
	if adminReg.Code != nethttp.StatusCreated {
		t.Fatalf("register admin: %d %s", adminReg.Code, adminReg.Body.String())
	}
	adminPayload := decodeJSONBody[registerResponse](t, adminReg.Body.Bytes())

	// 提升为 platform_admin
	_, err = appDB.Exec(ctx, "UPDATE org_memberships SET role = $1 WHERE user_id = $2", auth.RolePlatformAdmin, adminPayload.UserID)
	if err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	loginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login",
		map[string]any{"login": "admin@test.com", "password": "adminpass123"}, nil)
	if loginResp.Code != nethttp.StatusOK {
		t.Fatalf("login admin: %d %s", loginResp.Code, loginResp.Body.String())
	}
	adminToken := decodeJSONBody[loginResponse](t, loginResp.Body.Bytes()).AccessToken

	// 注册普通用户
	aliceReg := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "alice@test.com", "password": "alicepass123", "email": "alice@test.com"}, nil)
	if aliceReg.Code != nethttp.StatusCreated {
		t.Fatalf("register alice: %d %s", aliceReg.Code, aliceReg.Body.String())
	}
	alicePayload := decodeJSONBody[registerResponse](t, aliceReg.Body.Bytes())

	// 非 admin 被拒
	t.Run("forbidden for non-admin", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/admin/notifications/broadcasts",
			map[string]any{"type": "announcement", "title": "test", "body": "test body", "target": "all"},
			authHeader(alicePayload.AccessToken))
		assertErrorEnvelope(t, resp, nethttp.StatusForbidden, "auth.forbidden")
	})

	// 缺少 title
	t.Run("validation: missing title", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/admin/notifications/broadcasts",
			map[string]any{"type": "announcement", "body": "test body", "target": "all"},
			authHeader(adminToken))
		assertErrorEnvelope(t, resp, nethttp.StatusUnprocessableEntity, "validation.error")
	})

	// 广播到所有用户
	t.Run("broadcast to all", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/admin/notifications/broadcasts",
			map[string]any{"type": "announcement", "title": "System Update", "body": "v2.0 released", "target": "all"},
			authHeader(adminToken))
		if resp.Code != nethttp.StatusAccepted {
			t.Fatalf("create broadcast: %d %s", resp.Code, resp.Body.String())
		}
		b := decodeJSONBody[broadcastResponse](t, resp.Body.Bytes())
		if b.Title != "System Update" {
			t.Fatalf("expected title 'System Update', got %q", b.Title)
		}
		if b.TargetType != "all" {
			t.Fatalf("expected target_type 'all', got %q", b.TargetType)
		}

		// 等待后台 goroutine 完成
		time.Sleep(500 * time.Millisecond)

		// 检查用户通知
		notifResp := doJSON(handler, nethttp.MethodGet, "/v1/notifications", nil, authHeader(alicePayload.AccessToken))
		if notifResp.Code != nethttp.StatusOK {
			t.Fatalf("list notifications: %d %s", notifResp.Code, notifResp.Body.String())
		}
		type notifListResp struct {
			Data []notificationResponse `json:"data"`
		}
		notifs := decodeJSONBody[notifListResp](t, notifResp.Body.Bytes())
		found := false
		for _, n := range notifs.Data {
			if n.Title == "System Update" {
				found = true
			}
		}
		if !found {
			t.Fatal("alice did not receive broadcast notification")
		}
	})

	// 列表
	t.Run("list broadcasts", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/notifications/broadcasts", nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("list: %d %s", resp.Code, resp.Body.String())
		}
		items := decodeJSONBody[[]broadcastResponse](t, resp.Body.Bytes())
		if len(items) < 1 {
			t.Fatal("expected at least 1 broadcast")
		}
	})

	// 获取单条
	t.Run("get broadcast by id", func(t *testing.T) {
		listResp := doJSON(handler, nethttp.MethodGet, "/v1/admin/notifications/broadcasts", nil, authHeader(adminToken))
		items := decodeJSONBody[[]broadcastResponse](t, listResp.Body.Bytes())
		if len(items) == 0 {
			t.Fatal("no broadcasts to get")
		}
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/notifications/broadcasts/"+items[0].ID, nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("get: %d %s", resp.Code, resp.Body.String())
		}
		b := decodeJSONBody[broadcastResponse](t, resp.Body.Bytes())
		if b.ID != items[0].ID {
			t.Fatalf("id mismatch: %s vs %s", b.ID, items[0].ID)
		}
	})

	// 广播到指定 org
	t.Run("broadcast to org", func(t *testing.T) {
		// 获取 alice 的 org_id
		var aliceOrgID string
		err := appDB.QueryRow(ctx, "SELECT org_id FROM org_memberships WHERE user_id = $1 LIMIT 1", alicePayload.UserID).Scan(&aliceOrgID)
		if err != nil {
			t.Fatalf("get alice org: %v", err)
		}

		resp := doJSON(handler, nethttp.MethodPost, "/v1/admin/notifications/broadcasts",
			map[string]any{"type": "maintenance", "title": "Org Maintenance", "body": "scheduled downtime", "target": aliceOrgID},
			authHeader(adminToken))
		if resp.Code != nethttp.StatusAccepted {
			t.Fatalf("create org broadcast: %d %s", resp.Code, resp.Body.String())
		}

		time.Sleep(500 * time.Millisecond)

		// alice 应收到
		notifResp := doJSON(handler, nethttp.MethodGet, "/v1/notifications", nil, authHeader(alicePayload.AccessToken))
		if notifResp.Code != nethttp.StatusOK {
			t.Fatalf("list notifications: %d %s", notifResp.Code, notifResp.Body.String())
		}
		type notifListResp struct {
			Data []notificationResponse `json:"data"`
		}
		notifs := decodeJSONBody[notifListResp](t, notifResp.Body.Bytes())
		found := false
		for _, n := range notifs.Data {
			if n.Title == "Org Maintenance" {
				found = true
			}
		}
		if !found {
			t.Fatal("alice did not receive org broadcast notification")
		}
	})
}
