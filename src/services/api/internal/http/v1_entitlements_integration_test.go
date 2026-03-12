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

func TestEntitlementOverridesAuditIntegration(t *testing.T) {
	db := setupTestDatabase(t, "api_go_entitlements")

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
	entitlementsRepo, err := data.NewEntitlementsRepository(appDB)
	if err != nil {
		t.Fatalf("new entitlements repo: %v", err)
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
		AuditWriter:         auditWriter,
		OrgMembershipRepo:   membershipRepo,
		EntitlementsRepo:    entitlementsRepo,
		UsersRepo:           userRepo,
	})

	registerResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "ent-admin@test.com", "password": "adminpass123", "email": "ent-admin@test.com"}, nil)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register admin: %d %s", registerResp.Code, registerResp.Body.String())
	}
	adminPayload := decodeJSONBody[registerResponse](t, registerResp.Body.Bytes())

	if _, err := appDB.Exec(ctx, "UPDATE org_memberships SET role = $1 WHERE user_id = $2", auth.RolePlatformAdmin, adminPayload.UserID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	loginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login",
		map[string]any{"login": "ent-admin@test.com", "password": "adminpass123"}, nil)
	if loginResp.Code != nethttp.StatusOK {
		t.Fatalf("login admin: %d %s", loginResp.Code, loginResp.Body.String())
	}
	adminToken := decodeJSONBody[loginResponse](t, loginResp.Body.Bytes()).AccessToken

	var orgID string
	if err := appDB.QueryRow(ctx, "SELECT org_id::text FROM org_memberships WHERE user_id = $1 LIMIT 1", adminPayload.UserID).Scan(&orgID); err != nil {
		t.Fatalf("query org id: %v", err)
	}

	type overrideResp struct {
		ID        string  `json:"id"`
		OrgID     string  `json:"org_id"`
		Key       string  `json:"key"`
		Value     string  `json:"value"`
		Reason    *string `json:"reason,omitempty"`
		ValueType string  `json:"value_type"`
	}

	var overrideID string
	t.Run("create override writes audit", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/entitlement-overrides", map[string]any{
			"org_id":     orgID,
			"key":        "credits.monthly_limit",
			"value":      "1500",
			"value_type": "int",
			"reason":     "manual grant",
		}, authHeader(adminToken))
		if resp.Code != nethttp.StatusCreated {
			t.Fatalf("create override: %d %s", resp.Code, resp.Body.String())
		}
		payload := decodeJSONBody[overrideResp](t, resp.Body.Bytes())
		overrideID = payload.ID

		log := latestAuditLogByAction(t, ctx, appDB, "entitlements.override_set")
		if log.TargetType == nil || *log.TargetType != "entitlement_override" {
			t.Fatalf("unexpected target_type: %#v", log.TargetType)
		}
		if log.TargetID == nil || *log.TargetID != overrideID {
			t.Fatalf("unexpected target_id: %#v", log.TargetID)
		}
		if log.Metadata["key"] != "credits.monthly_limit" {
			t.Fatalf("unexpected metadata: %#v", log.Metadata)
		}
		if log.BeforeState != nil {
			t.Fatalf("before_state should be nil, got %#v", log.BeforeState)
		}
		if log.AfterState["value"] != "1500" {
			t.Fatalf("unexpected after_state: %#v", log.AfterState)
		}
	})

	t.Run("update override writes before and after", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/entitlement-overrides", map[string]any{
			"org_id":     orgID,
			"key":        "credits.monthly_limit",
			"value":      "2400",
			"value_type": "int",
			"reason":     "expanded grant",
		}, authHeader(adminToken))
		if resp.Code != nethttp.StatusCreated {
			t.Fatalf("update override: %d %s", resp.Code, resp.Body.String())
		}
		payload := decodeJSONBody[overrideResp](t, resp.Body.Bytes())
		if payload.ID != overrideID {
			t.Fatalf("override id changed: %s != %s", payload.ID, overrideID)
		}
		if count := countAuditLogByAction(t, ctx, appDB, "entitlements.override_set"); count != 2 {
			t.Fatalf("expected 2 override_set audit logs, got %d", count)
		}

		log := latestAuditLogByAction(t, ctx, appDB, "entitlements.override_set")
		if log.BeforeState["value"] != "1500" {
			t.Fatalf("unexpected before_state: %#v", log.BeforeState)
		}
		if log.AfterState["value"] != "2400" {
			t.Fatalf("unexpected after_state: %#v", log.AfterState)
		}
	})

	t.Run("delete override writes delete audit", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodDelete, "/v1/entitlement-overrides/"+overrideID+"?org_id="+orgID, nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("delete override: %d %s", resp.Code, resp.Body.String())
		}

		log := latestAuditLogByAction(t, ctx, appDB, "entitlements.override_delete")
		if log.TargetType == nil || *log.TargetType != "entitlement_override" {
			t.Fatalf("unexpected target_type: %#v", log.TargetType)
		}
		if log.TargetID == nil || *log.TargetID != overrideID {
			t.Fatalf("unexpected target_id: %#v", log.TargetID)
		}
		if log.BeforeState["value"] != "2400" {
			t.Fatalf("unexpected before_state: %#v", log.BeforeState)
		}
		if log.AfterState != nil {
			t.Fatalf("after_state should be nil, got %#v", log.AfterState)
		}
	})
}
