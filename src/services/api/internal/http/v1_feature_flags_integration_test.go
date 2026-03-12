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

func TestFeatureFlagsAuditIntegration(t *testing.T) {
	db := setupTestDatabase(t, "api_go_feature_flags")

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
	featureFlagsRepo, err := data.NewFeatureFlagRepository(pool)
	if err != nil {
		t.Fatalf("new feature flag repo: %v", err)
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
		FeatureFlagsRepo:    featureFlagsRepo,
		UsersRepo:           userRepo,
	})

	registerResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "flag-admin@test.com", "password": "adminpass123", "email": "flag-admin@test.com"}, nil)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register admin: %d %s", registerResp.Code, registerResp.Body.String())
	}
	adminPayload := decodeJSONBody[registerResponse](t, registerResp.Body.Bytes())

	if _, err := pool.Exec(ctx, "UPDATE account_memberships SET role = $1 WHERE user_id = $2", auth.RolePlatformAdmin, adminPayload.UserID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	loginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login",
		map[string]any{"login": "flag-admin@test.com", "password": "adminpass123"}, nil)
	if loginResp.Code != nethttp.StatusOK {
		t.Fatalf("login admin: %d %s", loginResp.Code, loginResp.Body.String())
	}
	adminToken := decodeJSONBody[loginResponse](t, loginResp.Body.Bytes()).AccessToken

	var accountID string
	if err := pool.QueryRow(ctx, "SELECT account_id::text FROM account_memberships WHERE user_id = $1 LIMIT 1", adminPayload.UserID).Scan(&accountID); err != nil {
		t.Fatalf("query org id: %v", err)
	}

	type flagResp struct {
		ID           string `json:"id"`
		Key          string `json:"key"`
		DefaultValue bool   `json:"default_value"`
	}
	var flagID string
	flagKey := "beta.new_dashboard"

	t.Run("create flag writes audit", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/feature-flags", map[string]any{
			"key":           flagKey,
			"description":   "new dashboard rollout",
			"default_value": false,
		}, authHeader(adminToken))
		if resp.Code != nethttp.StatusCreated {
			t.Fatalf("create flag: %d %s", resp.Code, resp.Body.String())
		}
		payload := decodeJSONBody[flagResp](t, resp.Body.Bytes())
		flagID = payload.ID

		log := latestAuditLogByAction(t, ctx, pool, "feature_flags.create")
		if log.TargetType == nil || *log.TargetType != "feature_flag" {
			t.Fatalf("unexpected target_type: %#v", log.TargetType)
		}
		if log.TargetID == nil || *log.TargetID != flagID {
			t.Fatalf("unexpected target_id: %#v", log.TargetID)
		}
		if log.Metadata["key"] != flagKey {
			t.Fatalf("unexpected metadata: %#v", log.Metadata)
		}
		if log.AfterState["default_value"] != false {
			t.Fatalf("unexpected after_state: %#v", log.AfterState)
		}
	})

	t.Run("update flag writes before and after", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPatch, "/v1/feature-flags/"+flagKey, map[string]any{"default_value": true}, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("update flag: %d %s", resp.Code, resp.Body.String())
		}

		log := latestAuditLogByAction(t, ctx, pool, "feature_flags.update")
		if log.BeforeState["default_value"] != false {
			t.Fatalf("unexpected before_state: %#v", log.BeforeState)
		}
		if log.AfterState["default_value"] != true {
			t.Fatalf("unexpected after_state: %#v", log.AfterState)
		}
	})

	t.Run("set account override writes audit", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodPost, "/v1/feature-flags/"+flagKey+"/org-overrides", map[string]any{
			"account_id":  accountID,
			"enabled": false,
		}, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("set account override: %d %s", resp.Code, resp.Body.String())
		}

		log := latestAuditLogByAction(t, ctx, pool, "feature_flags.account_override_set")
		if log.TargetType == nil || *log.TargetType != "feature_flag_account_override" {
			t.Fatalf("unexpected target_type: %#v", log.TargetType)
		}
		if log.TargetID == nil || *log.TargetID != accountID+":"+flagKey {
			t.Fatalf("unexpected target_id: %#v", log.TargetID)
		}
		if log.BeforeState != nil {
			t.Fatalf("before_state should be nil, got %#v", log.BeforeState)
		}
		if log.AfterState["enabled"] != false {
			t.Fatalf("unexpected after_state: %#v", log.AfterState)
		}
	})

	t.Run("delete account override writes audit", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodDelete, "/v1/feature-flags/"+flagKey+"/org-overrides/"+accountID, nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("delete account override: %d %s", resp.Code, resp.Body.String())
		}

		log := latestAuditLogByAction(t, ctx, pool, "feature_flags.account_override_delete")
		if log.BeforeState["enabled"] != false {
			t.Fatalf("unexpected before_state: %#v", log.BeforeState)
		}
		if log.AfterState != nil {
			t.Fatalf("after_state should be nil, got %#v", log.AfterState)
		}
	})

	t.Run("delete flag writes audit", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodDelete, "/v1/feature-flags/"+flagKey, nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("delete flag: %d %s", resp.Code, resp.Body.String())
		}

		log := latestAuditLogByAction(t, ctx, pool, "feature_flags.delete")
		if log.TargetID == nil || *log.TargetID != flagID {
			t.Fatalf("unexpected target_id: %#v", log.TargetID)
		}
		if log.BeforeState["default_value"] != true {
			t.Fatalf("unexpected before_state: %#v", log.BeforeState)
		}
		if log.AfterState != nil {
			t.Fatalf("after_state should be nil, got %#v", log.AfterState)
		}
	})

	t.Run("missing delete does not write audit", func(t *testing.T) {
		before := countAuditLogByAction(t, ctx, pool, "feature_flags.delete")
		resp := doJSON(handler, nethttp.MethodDelete, "/v1/feature-flags/missing-flag", nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("delete missing flag: %d %s", resp.Code, resp.Body.String())
		}
		after := countAuditLogByAction(t, ctx, pool, "feature_flags.delete")
		if before != after {
			t.Fatalf("delete audit count changed: before=%d after=%d", before, after)
		}
	})
}
