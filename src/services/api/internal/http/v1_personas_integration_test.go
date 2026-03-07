package http

import (
	"context"
	"io"
	"testing"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	repopersonas "arkloop/services/api/internal/personas"

	nethttp "net/http"

	"github.com/google/uuid"
)

func TestPersonasListMergesBuiltinAndCustom(t *testing.T) {
	db := setupTestDatabase(t, "api_go_personas_http")
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
		t.Fatalf("new refresh token repo: %v", err)
	}
	auditRepo, err := data.NewAuditLogRepository(pool)
	if err != nil {
		t.Fatalf("new audit repo: %v", err)
	}
	personasRepo, err := data.NewPersonasRepository(pool)
	if err != nil {
		t.Fatalf("new personas repo: %v", err)
	}
	jobRepo, err := data.NewJobRepository(pool)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
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
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		AuditWriter:         auditWriter,
		OrgMembershipRepo:   membershipRepo,
		PersonasRepo:        personasRepo,
		RepoPersonas: []repopersonas.RepoPersona{
			{
				ID:              "builtin-only",
				Version:         "1",
				Title:           "Builtin Only",
				PromptMD:        "builtin prompt",
				Budgets:         map[string]any{"max_output_tokens": 64},
				UserSelectable:  true,
				SelectorName:    "Builtin",
				SelectorOrder:   intPtr(3),
				AgentConfigName: "builtin-default",
			},
			{
				ID:              "hybrid-bound",
				Version:         "1",
				Title:           "Hybrid Bound",
				PromptMD:        "hybrid bound prompt",
				ExecutorType:    "agent.lua",
				AgentConfigName: "lua-default",
			},
			{
				ID:           "hybrid-default",
				Version:      "1",
				Title:        "Hybrid Default",
				PromptMD:     "hybrid default prompt",
				ExecutorType: "agent.lua",
			},
			{
				ID:       "shadowed",
				Version:  "1",
				Title:    "Builtin Shadowed",
				PromptMD: "builtin shadowed prompt",
			},
		},
	})

	registerResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register", map[string]any{
		"login":    "persona-owner",
		"password": "pwdpwdpwd",
		"email":    "persona-owner@test.com",
	}, nil)
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", registerResp.Code, registerResp.Body.String())
	}
	accessToken := decodeJSONBody[registerResponse](t, registerResp.Body.Bytes()).AccessToken
	headers := authHeader(accessToken)

	meResp := doJSON(handler, nethttp.MethodGet, "/v1/me", nil, headers)
	if meResp.Code != nethttp.StatusOK {
		t.Fatalf("me: %d %s", meResp.Code, meResp.Body.String())
	}
	me := decodeJSONBody[meResponse](t, meResp.Body.Bytes())
	orgID := uuid.MustParse(me.OrgID)

	if _, err := personasRepo.Create(ctx, orgID, "custom-only", "1", "Custom Only", nil, "custom prompt", nil, nil, nil, "agent.simple", nil); err != nil {
		t.Fatalf("create custom-only persona: %v", err)
	}
	if _, err := personasRepo.Create(ctx, orgID, "shadowed", "1", "Custom Shadowed", nil, "custom shadowed prompt", nil, nil, nil, "agent.simple", nil); err != nil {
		t.Fatalf("create shadowed persona: %v", err)
	}
	customBoundID := insertOrgPersonaHTTP(t, ctx, pool, orgID, "custom-bound", "Custom Bound", strPtr("custom-default"))
	ghostID := insertGlobalPersonaHTTP(t, ctx, pool, "ghost", "Ghost Persona")

	listResp := doJSON(handler, nethttp.MethodGet, "/v1/personas", nil, headers)
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list personas: %d %s", listResp.Code, listResp.Body.String())
	}
	list := decodeJSONBody[[]personaResponse](t, listResp.Body.Bytes())
	if len(list) != 6 {
		t.Fatalf("expected 6 personas, got %d body=%s", len(list), listResp.Body.String())
	}

	byKey := make(map[string]personaResponse, len(list))
	for _, persona := range list {
		byKey[persona.PersonaKey] = persona
	}

	if _, exists := byKey["ghost"]; exists {
		t.Fatal("expected ghost persona hidden from list")
	}
	builtinOnly, exists := byKey["builtin-only"]
	if !exists {
		t.Fatal("expected builtin-only persona in list")
	}
	if builtinOnly.Source != "builtin" {
		t.Fatalf("expected builtin source, got %q", builtinOnly.Source)
	}
	if builtinOnly.ID != "builtin:builtin-only:1" {
		t.Fatalf("unexpected builtin id: %q", builtinOnly.ID)
	}
	if builtinOnly.CreatedAt != "" {
		t.Fatalf("expected empty created_at for builtin persona, got %q", builtinOnly.CreatedAt)
	}
	if builtinOnly.OrgID != nil {
		t.Fatalf("expected nil org_id for builtin persona, got %v", *builtinOnly.OrgID)
	}
	if !builtinOnly.UserSelectable {
		t.Fatal("expected builtin persona selectable")
	}
	if builtinOnly.SelectorName == nil || *builtinOnly.SelectorName != "Builtin" {
		t.Fatalf("unexpected selector_name: %#v", builtinOnly.SelectorName)
	}
	if builtinOnly.SelectorOrder == nil || *builtinOnly.SelectorOrder != 3 {
		t.Fatalf("unexpected selector_order: %#v", builtinOnly.SelectorOrder)
	}
	if builtinOnly.AgentConfigName == nil || *builtinOnly.AgentConfigName != "builtin-default" {
		t.Fatalf("unexpected builtin agent_config_name: %#v", builtinOnly.AgentConfigName)
	}

	hybridBound, exists := byKey["hybrid-bound"]
	if !exists {
		t.Fatal("expected hybrid-bound persona in list")
	}
	if hybridBound.Source != "builtin" {
		t.Fatalf("expected builtin hybrid-bound source, got %q", hybridBound.Source)
	}
	if hybridBound.ExecutorType != "agent.lua" {
		t.Fatalf("unexpected hybrid-bound executor_type: %q", hybridBound.ExecutorType)
	}
	if hybridBound.AgentConfigName == nil || *hybridBound.AgentConfigName != "lua-default" {
		t.Fatalf("unexpected hybrid-bound agent_config_name: %#v", hybridBound.AgentConfigName)
	}

	hybridDefault, exists := byKey["hybrid-default"]
	if !exists {
		t.Fatal("expected hybrid-default persona in list")
	}
	if hybridDefault.Source != "builtin" {
		t.Fatalf("expected builtin hybrid-default source, got %q", hybridDefault.Source)
	}
	if hybridDefault.ExecutorType != "agent.lua" {
		t.Fatalf("unexpected hybrid-default executor_type: %q", hybridDefault.ExecutorType)
	}
	if hybridDefault.AgentConfigName != nil {
		t.Fatalf("expected nil hybrid-default agent_config_name, got %#v", hybridDefault.AgentConfigName)
	}

	shadowed := byKey["shadowed"]
	if shadowed.Source != "custom" {
		t.Fatalf("expected custom shadowed persona, got %q", shadowed.Source)
	}
	if shadowed.DisplayName != "Custom Shadowed" {
		t.Fatalf("expected custom display name, got %q", shadowed.DisplayName)
	}
	if shadowed.AgentConfigName != nil {
		t.Fatalf("expected nil shadowed agent_config_name, got %#v", shadowed.AgentConfigName)
	}

	customOnly := byKey["custom-only"]
	if customOnly.Source != "custom" {
		t.Fatalf("expected custom-only source, got %q", customOnly.Source)
	}
	if customOnly.UserSelectable {
		t.Fatal("expected custom persona not selectable")
	}
	if customOnly.SelectorName != nil {
		t.Fatalf("expected nil selector_name for custom persona, got %#v", customOnly.SelectorName)
	}
	if customOnly.SelectorOrder != nil {
		t.Fatalf("expected nil selector_order for custom persona, got %#v", customOnly.SelectorOrder)
	}
	if customOnly.AgentConfigName != nil {
		t.Fatalf("expected nil custom-only agent_config_name, got %#v", customOnly.AgentConfigName)
	}
	if customOnly.CreatedAt == "" {
		t.Fatal("expected created_at on custom-only persona")
	}

	customBound := byKey["custom-bound"]
	if customBound.Source != "custom" {
		t.Fatalf("expected custom-bound source, got %q", customBound.Source)
	}
	if customBound.UserSelectable {
		t.Fatal("expected custom-bound persona not selectable")
	}
	if customBound.SelectorName != nil {
		t.Fatalf("expected nil selector_name for custom-bound persona, got %#v", customBound.SelectorName)
	}
	if customBound.SelectorOrder != nil {
		t.Fatalf("expected nil selector_order for custom-bound persona, got %#v", customBound.SelectorOrder)
	}
	if customBound.AgentConfigName == nil || *customBound.AgentConfigName != "custom-default" {
		t.Fatalf("unexpected custom agent_config_name: %#v", customBound.AgentConfigName)
	}
	if customBound.CreatedAt == "" {
		t.Fatal("expected created_at on custom-bound persona")
	}
	if customBound.ID != customBoundID.String() {
		t.Fatalf("unexpected custom-bound id: %q", customBound.ID)
	}

	patchResp := doJSON(handler, nethttp.MethodPatch, "/v1/personas/"+ghostID.String(), map[string]any{
		"display_name": "Ghost Renamed",
	}, headers)
	assertErrorEnvelope(t, patchResp, nethttp.StatusNotFound, "personas.not_found")
}

func intPtr(value int) *int {
	return &value
}

func insertGlobalPersonaHTTP(t *testing.T, ctx context.Context, pool data.Querier, personaKey string, displayName string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(
		ctx,
		`INSERT INTO personas
			(org_id, persona_key, version, display_name, prompt_md, tool_allowlist, budgets_json, executor_type, executor_config_json)
		 VALUES (NULL, $1, '1', $2, 'ghost prompt', '{}', '{}'::jsonb, 'agent.simple', '{}'::jsonb)
		 RETURNING id`,
		personaKey,
		displayName,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert global persona failed: %v", err)
	}
	return id
}

func insertOrgPersonaHTTP(t *testing.T, ctx context.Context, pool data.Querier, orgID uuid.UUID, personaKey string, displayName string, agentConfigName *string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(
		ctx,
		`INSERT INTO personas
			(org_id, persona_key, version, display_name, prompt_md, tool_allowlist, budgets_json, executor_type, executor_config_json, agent_config_name)
		 VALUES ($1, $2, '1', $3, 'custom bound prompt', '{}', '{}'::jsonb, 'agent.simple', '{}'::jsonb, $4)
		 RETURNING id`,
		orgID,
		personaKey,
		displayName,
		agentConfigName,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert org persona failed: %v", err)
	}
	return id
}

func strPtr(value string) *string {
	return &value
}
