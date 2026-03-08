package http

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"testing"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	repopersonas "arkloop/services/api/internal/personas"

	"github.com/google/uuid"
)

func TestPersonasListCreateAndPatchUsePersonaFields(t *testing.T) {
	db := setupTestDatabase(t, "api_go_personas")
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
		PersonasRepo:        personasRepo,
		RepoPersonas: []repopersonas.RepoPersona{
			{
				ID:                 "builtin-only",
				Version:            "1",
				Title:              "Builtin Only",
				UserSelectable:     true,
				SelectorName:       "Builtin",
				SelectorOrder:      intPtrPersonaLocal(3),
				Model:              "builtin-cred^gpt-builtin",
				ReasoningMode:      "low",
				PromptCacheControl: "none",
				ToolAllowlist:      []string{"web.search"},
				ToolDenylist:       []string{"exec_command"},
				Budgets:            map[string]any{"max_output_tokens": 2048},
				PromptMD:           "builtin prompt",
			},
			{
				ID:                 "shadowed",
				Version:            "1",
				Title:              "Builtin Shadowed",
				Model:              "builtin-shadow^model",
				ReasoningMode:      "auto",
				PromptCacheControl: "none",
				PromptMD:           "shadowed builtin prompt",
			},
		},
	})

	reg := doJSON(handler, nethttp.MethodPost, "/v1/auth/register", map[string]any{
		"login":    "personas-user@test.com",
		"password": "personapass123",
		"email":    "personas-user@test.com",
	}, nil)
	if reg.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", reg.Code, reg.Body.String())
	}
	regBody := decodeJSONBody[registerResponse](t, reg.Body.Bytes())
	headers := authHeader(regBody.AccessToken)
	meResp := doJSON(handler, nethttp.MethodGet, "/v1/me", nil, headers)
	if meResp.Code != nethttp.StatusOK {
		t.Fatalf("me: %d %s", meResp.Code, meResp.Body.String())
	}
	me := decodeJSONBody[meResponse](t, meResp.Body.Bytes())
	orgID := uuid.MustParse(me.OrgID)

	ghostID := insertGlobalPersonaHTTP(t, ctx, pool, "ghost", "Ghost Persona")

	_, err = personasRepo.Create(
		ctx,
		orgID,
		"shadowed",
		"1",
		"Custom Shadowed",
		nil,
		"custom shadowed prompt",
		nil,
		nil,
		json.RawMessage(`{"max_output_tokens":512}`),
		nil,
		strPtrPersonaLocal("custom-shadow^model"),
		"high",
		"system_prompt",
		"agent.simple",
		nil,
	)
	if err != nil {
		t.Fatalf("create shadowed persona: %v", err)
	}
	_, err = personasRepo.Create(
		ctx,
		orgID,
		"custom-only",
		"1",
		"Custom Only",
		nil,
		"custom only prompt",
		[]string{"web.search"},
		[]string{"exec_command"},
		json.RawMessage(`{"temperature":0.3}`),
		strPtrPersonaLocal("cred-custom"),
		strPtrPersonaLocal("custom-only^gpt-4.1-mini"),
		"high",
		"system_prompt",
		"agent.simple",
		nil,
	)
	if err != nil {
		t.Fatalf("create custom-only persona: %v", err)
	}

	createResp := doJSON(handler, nethttp.MethodPost, "/v1/personas", map[string]any{
		"persona_key":          "api-created",
		"version":              "1",
		"display_name":         "API Created",
		"prompt_md":            "api created prompt",
		"tool_allowlist":       []string{"web.search"},
		"tool_denylist":        []string{"exec_command"},
		"budgets":              map[string]any{"max_output_tokens": 1024, "top_p": 0.9},
		"preferred_credential": "cred-api",
		"model":                "api-cred^gpt-5-mini",
		"reasoning_mode":       "medium",
		"prompt_cache_control": "system_prompt",
		"executor_type":        "agent.simple",
	}, headers)
	if createResp.Code != nethttp.StatusCreated {
		t.Fatalf("create persona: %d %s", createResp.Code, createResp.Body.String())
	}
	created := decodeJSONBody[personaResponse](t, createResp.Body.Bytes())
	if created.Model == nil || *created.Model != "api-cred^gpt-5-mini" {
		t.Fatalf("unexpected created model: %#v", created.Model)
	}
	if created.ReasoningMode != "medium" {
		t.Fatalf("unexpected created reasoning_mode: %q", created.ReasoningMode)
	}
	if created.PromptCacheControl != "system_prompt" {
		t.Fatalf("unexpected created prompt_cache_control: %q", created.PromptCacheControl)
	}
	if len(created.ToolDenylist) != 1 || created.ToolDenylist[0] != "exec_command" {
		t.Fatalf("unexpected created tool_denylist: %#v", created.ToolDenylist)
	}

	copyResp := doJSON(handler, nethttp.MethodPost, "/v1/personas", map[string]any{
		"copy_from_repo_persona_key": "builtin-only",
		"persona_key":                "builtin-only",
		"version":                    "1",
		"display_name":               "Builtin Customized",
		"prompt_md":                  "builtin customized prompt",
		"tool_allowlist":             []string{"web_search"},
		"tool_denylist":              []string{"exec_command"},
		"model":                      "builtin-custom^gpt-5-mini",
		"reasoning_mode":             "medium",
	}, headers)
	if copyResp.Code != nethttp.StatusCreated {
		t.Fatalf("copy builtin persona: %d %s", copyResp.Code, copyResp.Body.String())
	}

	listResp := doJSON(handler, nethttp.MethodGet, "/v1/personas", nil, headers)
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list personas: %d %s", listResp.Code, listResp.Body.String())
	}
	body := decodeJSONBody[[]personaResponse](t, listResp.Body.Bytes())
	if len(body) != 4 {
		t.Fatalf("unexpected persona count: %d", len(body))
	}

	byKey := make(map[string]personaResponse, len(body))
	for _, persona := range body {
		byKey[persona.PersonaKey] = persona
	}
	if _, exists := byKey["ghost"]; exists {
		t.Fatal("expected ghost persona hidden from list")
	}

	builtinOnly, ok := byKey["builtin-only"]
	if !ok {
		t.Fatal("expected builtin-only persona in list")
	}
	if builtinOnly.Source != "custom" {
		t.Fatalf("unexpected builtin-only source after copy: %q", builtinOnly.Source)
	}
	if builtinOnly.DisplayName != "Builtin Customized" {
		t.Fatalf("unexpected builtin-only display name: %q", builtinOnly.DisplayName)
	}
	if builtinOnly.Model == nil || *builtinOnly.Model != "builtin-custom^gpt-5-mini" {
		t.Fatalf("unexpected builtin model: %#v", builtinOnly.Model)
	}
	if builtinOnly.ReasoningMode != "medium" {
		t.Fatalf("unexpected builtin reasoning_mode: %q", builtinOnly.ReasoningMode)
	}
	if builtinOnly.PromptCacheControl != "none" {
		t.Fatalf("unexpected builtin prompt_cache_control: %q", builtinOnly.PromptCacheControl)
	}
	if len(builtinOnly.ToolDenylist) != 1 || builtinOnly.ToolDenylist[0] != "exec_command" {
		t.Fatalf("unexpected builtin tool_denylist: %#v", builtinOnly.ToolDenylist)
	}

	shadowed, ok := byKey["shadowed"]
	if !ok {
		t.Fatal("expected shadowed persona in list")
	}
	if shadowed.Source != "custom" {
		t.Fatalf("expected custom shadowed persona, got %q", shadowed.Source)
	}
	if shadowed.Model == nil || *shadowed.Model != "custom-shadow^model" {
		t.Fatalf("unexpected shadowed model: %#v", shadowed.Model)
	}
	if shadowed.PromptCacheControl != "system_prompt" {
		t.Fatalf("unexpected shadowed prompt_cache_control: %q", shadowed.PromptCacheControl)
	}

	customOnly, ok := byKey["custom-only"]
	if !ok {
		t.Fatal("expected custom-only persona in list")
	}
	if customOnly.Source != "custom" {
		t.Fatalf("unexpected custom-only source: %q", customOnly.Source)
	}
	if customOnly.PreferredCredential == nil || *customOnly.PreferredCredential != "cred-custom" {
		t.Fatalf("unexpected custom-only preferred_credential: %#v", customOnly.PreferredCredential)
	}
	if customOnly.Model == nil || *customOnly.Model != "custom-only^gpt-4.1-mini" {
		t.Fatalf("unexpected custom-only model: %#v", customOnly.Model)
	}
	if customOnly.ReasoningMode != "high" {
		t.Fatalf("unexpected custom-only reasoning_mode: %q", customOnly.ReasoningMode)
	}
	if customOnly.PromptCacheControl != "system_prompt" {
		t.Fatalf("unexpected custom-only prompt_cache_control: %q", customOnly.PromptCacheControl)
	}
	if len(customOnly.ToolDenylist) != 1 || customOnly.ToolDenylist[0] != "exec_command" {
		t.Fatalf("unexpected custom-only tool_denylist: %#v", customOnly.ToolDenylist)
	}

	patchResp := doJSON(handler, nethttp.MethodPatch, "/v1/personas/"+created.ID, map[string]any{
		"model":                "patched-cred^gpt-5",
		"tool_denylist":        []string{"write_stdin"},
		"reasoning_mode":       "high",
		"prompt_cache_control": "none",
	}, headers)
	if patchResp.Code != nethttp.StatusOK {
		t.Fatalf("patch persona: %d %s", patchResp.Code, patchResp.Body.String())
	}
	patched := decodeJSONBody[personaResponse](t, patchResp.Body.Bytes())
	if patched.Model == nil || *patched.Model != "patched-cred^gpt-5" {
		t.Fatalf("unexpected patched model: %#v", patched.Model)
	}
	if patched.ReasoningMode != "high" {
		t.Fatalf("unexpected patched reasoning_mode: %q", patched.ReasoningMode)
	}
	if patched.PromptCacheControl != "none" {
		t.Fatalf("unexpected patched prompt_cache_control: %q", patched.PromptCacheControl)
	}
	if len(patched.ToolDenylist) != 1 || patched.ToolDenylist[0] != "write_stdin" {
		t.Fatalf("unexpected patched tool_denylist: %#v", patched.ToolDenylist)
	}

	ghostPatchResp := doJSON(handler, nethttp.MethodPatch, "/v1/personas/"+ghostID.String(), map[string]any{
		"display_name": "Ghost Renamed",
	}, headers)
	assertErrorEnvelope(t, ghostPatchResp, nethttp.StatusNotFound, "personas.not_found")
}

func intPtrPersonaLocal(value int) *int {
	return &value
}

func strPtrPersonaLocal(value string) *string {
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
