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
	repopersonas "arkloop/services/api/internal/personas"
	sharedconfig "arkloop/services/shared/config"

	"github.com/google/uuid"
)

func TestAdminExecutionGovernanceShowsLayersAndEffectiveProfiles(t *testing.T) {
	t.Setenv("ARKLOOP_LIMIT_AGENT_REASONING_ITERATIONS", "14")

	db := setupTestDatabase(t, "api_go_execution_governance")
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
	membershipRepo, err := data.NewOrgMembershipRepository(pool)
	if err != nil {
		t.Fatalf("new membership repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("new refresh repo: %v", err)
	}
	auditRepo, err := data.NewAuditLogRepository(pool)
	if err != nil {
		t.Fatalf("new audit repo: %v", err)
	}
	jobRepo, err := data.NewJobRepository(pool)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
	}
	personasRepo, err := data.NewPersonasRepository(pool)
	if err != nil {
		t.Fatalf("new personas repo: %v", err)
	}
	agentConfigsRepo, err := data.NewAgentConfigRepository(pool)
	if err != nil {
		t.Fatalf("new agent config repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
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
		OrgMembershipRepo:   membershipRepo,
		APIKeysRepo:         nil,
		PersonasRepo:        personasRepo,
		AgentConfigsRepo:    agentConfigsRepo,
		RepoPersonas: []repopersonas.RepoPersona{
			{
				ID:                  "builtin-ops",
				Version:             "1",
				Title:               "Builtin Ops",
				Budgets:             map[string]any{"tool_continuation_budget": 5, "max_output_tokens": 9999, "per_tool_soft_limits": map[string]any{"write_stdin": map[string]any{"max_continuations": 7}}},
				PreferredCredential: "cred-builtin",
			},
		},
	})

	adminReg := doJSON(handler, nethttp.MethodPost, "/v1/auth/register", map[string]any{
		"login":    "governance-admin@test.com",
		"password": "adminpass123",
		"email":    "governance-admin@test.com",
	}, nil)
	if adminReg.Code != nethttp.StatusCreated {
		t.Fatalf("register admin: %d %s", adminReg.Code, adminReg.Body.String())
	}
	adminPayload := decodeJSONBody[registerResponse](t, adminReg.Body.Bytes())
	if _, err := pool.Exec(ctx, "UPDATE org_memberships SET role = $1 WHERE user_id = $2", auth.RolePlatformAdmin, adminPayload.UserID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	adminLogin := doJSON(handler, nethttp.MethodPost, "/v1/auth/login", map[string]any{
		"login":    "governance-admin@test.com",
		"password": "adminpass123",
	}, nil)
	if adminLogin.Code != nethttp.StatusOK {
		t.Fatalf("login admin: %d %s", adminLogin.Code, adminLogin.Body.String())
	}
	adminToken := decodeJSONBody[loginResponse](t, adminLogin.Body.Bytes()).AccessToken

	meResp := doJSON(handler, nethttp.MethodGet, "/v1/me", nil, authHeader(adminToken))
	if meResp.Code != nethttp.StatusOK {
		t.Fatalf("me: %d %s", meResp.Code, meResp.Body.String())
	}
	me := decodeJSONBody[meResponse](t, meResp.Body.Bytes())
	orgID := uuid.MustParse(me.OrgID)

	if _, err := pool.Exec(ctx, `INSERT INTO platform_settings (key, value, updated_at) VALUES
		('limit.agent_reasoning_iterations', '12', now()),
		('limit.tool_continuation_budget', '30', now()),
		('limit.thread_message_history', '250', now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`); err != nil {
		t.Fatalf("seed platform settings: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO org_settings (org_id, key, value, updated_at) VALUES
		($1, 'limit.agent_reasoning_iterations', '9', now()),
		($1, 'limit.tool_continuation_budget', '18', now())
		ON CONFLICT (org_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, orgID); err != nil {
		t.Fatalf("seed org settings: %v", err)
	}

	platformDefault, err := agentConfigsRepo.Create(ctx, uuid.Nil, data.CreateAgentConfigRequest{
		Scope:           "platform",
		Name:            "platform-default",
		Model:           strPtrLocal("gpt-platform"),
		MaxOutputTokens: intPtrLocal(1024),
		ReasoningMode:   "enabled",
		IsDefault:       true,
	})
	if err != nil {
		t.Fatalf("create platform default: %v", err)
	}
	_ = platformDefault
	orgDefault, err := agentConfigsRepo.Create(ctx, orgID, data.CreateAgentConfigRequest{
		Scope:              "org",
		Name:               "org-default",
		Model:              strPtrLocal("gpt-org"),
		MaxOutputTokens:    intPtrLocal(2048),
		ReasoningMode:      "auto",
		PromptCacheControl: "system_prompt",
		ToolPolicy:         "allowlist",
		IsDefault:          true,
	})
	if err != nil {
		t.Fatalf("create org default: %v", err)
	}
	_ = orgDefault
	namedConfig, err := agentConfigsRepo.Create(ctx, orgID, data.CreateAgentConfigRequest{
		Scope:           "org",
		Name:            "named-config",
		Model:           strPtrLocal("gpt-named"),
		MaxOutputTokens: intPtrLocal(4096),
		ReasoningMode:   "disabled",
		ToolPolicy:      "denylist",
	})
	if err != nil {
		t.Fatalf("create named config: %v", err)
	}
	_ = namedConfig

	if _, err := pool.Exec(ctx, `INSERT INTO personas
		(org_id, persona_key, version, display_name, prompt_md, tool_allowlist, budgets_json, preferred_credential, agent_config_name, executor_type, executor_config_json)
		VALUES ($1, 'custom-shell', '1', 'Custom Shell', 'custom prompt', '{}', $2::jsonb, 'cred-custom', 'named-config', 'agent.simple', '{}'::jsonb)`,
		orgID,
		`{"reasoning_iterations":20,"tool_continuation_budget":10,"max_output_tokens":8192,"per_tool_soft_limits":{"write_stdin":{"max_continuations":9}}}`,
	); err != nil {
		t.Fatalf("insert custom persona: %v", err)
	}

	t.Run("without org filter only shows limits", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/execution-governance", nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("execution governance: %d %s", resp.Code, resp.Body.String())
		}
		body := decodeJSONBody[executionGovernanceResponse](t, resp.Body.Bytes())
		if len(body.Limits) != len(executionGovernanceKeys) {
			t.Fatalf("unexpected limit count: %d", len(body.Limits))
		}
		if len(body.AgentConfigs) != 0 || len(body.Personas) != 0 {
			t.Fatalf("expected empty org-bound blocks, got agent_configs=%d personas=%d", len(body.AgentConfigs), len(body.Personas))
		}
	})

	t.Run("with org filter shows layers and effective profiles", func(t *testing.T) {
		resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/execution-governance?org_id="+orgID.String(), nil, authHeader(adminToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("execution governance: %d %s", resp.Code, resp.Body.String())
		}
		body := decodeJSONBody[executionGovernanceResponse](t, resp.Body.Bytes())
		limits := make(map[string]sharedconfig.SettingInspection, len(body.Limits))
		for _, item := range body.Limits {
			limits[item.Key] = item
		}
		reasoning := limits["limit.agent_reasoning_iterations"]
		if reasoning.Effective.Source != "env" || reasoning.Effective.Value != "14" {
			t.Fatalf("unexpected reasoning effective: %#v", reasoning.Effective)
		}
		if reasoning.Layers.OrgDB == nil || *reasoning.Layers.OrgDB != "9" {
			t.Fatalf("unexpected reasoning org layer: %#v", reasoning.Layers.OrgDB)
		}
		if reasoning.Layers.PlatformDB == nil || *reasoning.Layers.PlatformDB != "12" {
			t.Fatalf("unexpected reasoning platform layer: %#v", reasoning.Layers.PlatformDB)
		}
		if reasoning.Layers.Default != "0" {
			t.Fatalf("unexpected reasoning default: %q", reasoning.Layers.Default)
		}

		continuation := limits["limit.tool_continuation_budget"]
		if continuation.Effective.Source != "org_db" || continuation.Effective.Value != "18" {
			t.Fatalf("unexpected continuation effective: %#v", continuation.Effective)
		}

		if body.AgentConfigDefaults.OrgDefault == nil || body.AgentConfigDefaults.OrgDefault.Name != "org-default" {
			t.Fatalf("unexpected org default: %#v", body.AgentConfigDefaults.OrgDefault)
		}
		if body.AgentConfigDefaults.PlatformDefault == nil || body.AgentConfigDefaults.PlatformDefault.Name != "platform-default" {
			t.Fatalf("unexpected platform default: %#v", body.AgentConfigDefaults.PlatformDefault)
		}
		if len(body.AgentConfigs) != 3 {
			t.Fatalf("unexpected agent config count: %d", len(body.AgentConfigs))
		}

		personasByKey := make(map[string]executionGovernancePersona, len(body.Personas))
		for _, item := range body.Personas {
			personasByKey[item.PersonaKey] = item
		}
		custom := personasByKey["custom-shell"]
		if custom.Source != "custom" {
			t.Fatalf("unexpected custom source: %q", custom.Source)
		}
		if custom.AgentConfigName == nil || *custom.AgentConfigName != "named-config" {
			t.Fatalf("unexpected custom agent_config_name: %#v", custom.AgentConfigName)
		}
		if custom.Effective.ResolvedAgentConfig.Source != "persona_binding" {
			t.Fatalf("unexpected custom resolved source: %#v", custom.Effective.ResolvedAgentConfig)
		}
		if custom.Effective.ResolvedAgentConfig.Config == nil || custom.Effective.ResolvedAgentConfig.Config.Name != "named-config" {
			t.Fatalf("unexpected custom resolved config: %#v", custom.Effective.ResolvedAgentConfig.Config)
		}
		if custom.Effective.ReasoningIterations != 14 {
			t.Fatalf("unexpected custom reasoning_iterations: %d", custom.Effective.ReasoningIterations)
		}
		if custom.Effective.ToolContinuationBudget != 10 {
			t.Fatalf("unexpected custom tool_continuation_budget: %d", custom.Effective.ToolContinuationBudget)
		}
		if custom.Effective.MaxOutputTokens == nil || *custom.Effective.MaxOutputTokens != 4096 {
			t.Fatalf("unexpected custom max_output_tokens: %#v", custom.Effective.MaxOutputTokens)
		}

		builtin := personasByKey["builtin-ops"]
		if builtin.Source != "builtin" {
			t.Fatalf("unexpected builtin source: %q", builtin.Source)
		}
		if builtin.PreferredCredential == nil || *builtin.PreferredCredential != "cred-builtin" {
			t.Fatalf("unexpected builtin preferred credential: %#v", builtin.PreferredCredential)
		}
		if builtin.Effective.ResolvedAgentConfig.Source != "org_default" {
			t.Fatalf("unexpected builtin resolved source: %#v", builtin.Effective.ResolvedAgentConfig)
		}
		if builtin.Effective.MaxOutputTokens == nil || *builtin.Effective.MaxOutputTokens != 2048 {
			t.Fatalf("unexpected builtin max_output_tokens: %#v", builtin.Effective.MaxOutputTokens)
		}
		if builtin.Effective.ToolContinuationBudget != 5 {
			t.Fatalf("unexpected builtin tool_continuation_budget: %d", builtin.Effective.ToolContinuationBudget)
		}
		if writeLimit := builtin.Effective.PerToolSoftLimits["write_stdin"]; writeLimit.MaxContinuations == nil || *writeLimit.MaxContinuations != 7 {
			t.Fatalf("unexpected builtin write_stdin limit: %#v", writeLimit)
		}
	})
}

func intPtrLocal(value int) *int {
	return &value
}

func strPtrLocal(value string) *string {
	return &value
}
