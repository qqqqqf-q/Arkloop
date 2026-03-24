//go:build !desktop

package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	nethttp "net/http"
	"testing"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
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

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	if err != nil {
		t.Fatalf("new registration service: %v", err)
	}
	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Pool:                  pool,
		Logger:                logger,
		AuthService:           authService,
		RegistrationService:   registrationService,
		AuditWriter:           auditWriter,
		AccountMembershipRepo: membershipRepo,
		PersonasRepo:          personasRepo,
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
				Roles: map[string]any{
					"worker": map[string]any{"prompt_md": "builtin worker prompt"},
				},
				PromptMD: "builtin prompt",
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
	accountID := uuid.MustParse(me.AccountID)

	ghostID := insertGlobalPersonaHTTP(t, ctx, pool, "ghost", "Ghost Persona")

	_, err = personasRepo.Create(
		ctx,
		accountID,
		"shadowed",
		"1",
		"Custom Shadowed",
		nil,
		"custom shadowed prompt",
		nil,
		nil,
		json.RawMessage(`{"max_output_tokens":512}`),
		nil,
		nil,
		strPtrPersonaLocal("custom-shadow^model"),
		"high",
		true,
		"system_prompt",
		"agent.simple",
		nil,
	)
	if err != nil {
		t.Fatalf("create shadowed persona: %v", err)
	}
	_, err = personasRepo.Create(
		ctx,
		accountID,
		"custom-only",
		"1",
		"Custom Only",
		nil,
		"custom only prompt",
		[]string{"web.search"},
		[]string{"exec_command"},
		json.RawMessage(`{"temperature":0.3}`),
		nil,
		strPtrPersonaLocal("cred-custom"),
		strPtrPersonaLocal("custom-only^gpt-4.1-mini"),
		"high",
		true,
		"system_prompt",
		"agent.simple",
		nil,
	)
	if err != nil {
		t.Fatalf("create custom-only persona: %v", err)
	}

	createResp := doJSON(handler, nethttp.MethodPost, "/v1/personas", map[string]any{
		"scope":                "user",
		"persona_key":          "api-created",
		"version":              "1",
		"display_name":         "API Created",
		"prompt_md":            "api created prompt",
		"tool_allowlist":       []string{"web.search"},
		"tool_denylist":        []string{"exec_command"},
		"budgets":              map[string]any{"max_output_tokens": 1024, "top_p": 0.9},
		"roles":                map[string]any{"worker": map[string]any{"prompt_md": "worker prompt"}},
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
	if created.Scope != "user" {
		t.Fatalf("unexpected created scope: %q", created.Scope)
	}
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
	assertJSONContainsRolePrompt(t, created.RolesJSON, "worker", "worker prompt")

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

	listResp := doJSON(handler, nethttp.MethodGet, "/v1/personas?scope=user", nil, headers)
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
	if builtinOnly.Scope != "user" {
		t.Fatalf("unexpected builtin-only scope: %q", builtinOnly.Scope)
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
	assertJSONContainsRolePrompt(t, builtinOnly.RolesJSON, "worker", "builtin worker prompt")

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

	patchResp := doJSON(handler, nethttp.MethodPatch, "/v1/personas/"+created.ID+"?scope=user", map[string]any{
		"model":                "patched-cred^gpt-5",
		"tool_denylist":        []string{"write_stdin"},
		"reasoning_mode":       "high",
		"prompt_cache_control": "none",
		"roles":                map[string]any{},
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
	assertJSONContainsEmptyObject(t, patched.RolesJSON)

	deleteResp := doJSON(handler, nethttp.MethodDelete, "/v1/personas/"+created.ID+"?scope=user", nil, headers)
	if deleteResp.Code != nethttp.StatusOK {
		t.Fatalf("delete persona: %d %s", deleteResp.Code, deleteResp.Body.String())
	}

	afterDelete := doJSON(handler, nethttp.MethodPatch, "/v1/personas/"+created.ID+"?scope=user", map[string]any{
		"display_name": "after delete",
	}, headers)
	assertErrorEnvelope(t, afterDelete, nethttp.StatusNotFound, "personas.not_found")

	ghostPatchResp := doJSON(handler, nethttp.MethodPatch, "/v1/personas/"+ghostID.String(), map[string]any{
		"display_name": "Ghost Renamed",
	}, headers)
	assertErrorEnvelope(t, ghostPatchResp, nethttp.StatusNotFound, "personas.not_found")
}

func TestPersonasListOrgScopeAllowsMemberReadForBuiltinSelector(t *testing.T) {
	db := setupTestDatabase(t, "api_go_personas_member_read")
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

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	if err != nil {
		t.Fatalf("new registration service: %v", err)
	}
	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Pool:                  pool,
		Logger:                logger,
		AuthService:           authService,
		RegistrationService:   registrationService,
		AuditWriter:           auditWriter,
		AccountMembershipRepo: membershipRepo,
		PersonasRepo:          personasRepo,
		RepoPersonas: []repopersonas.RepoPersona{
			{
				ID:             "normal",
				Version:        "1",
				Title:          "Normal",
				UserSelectable: true,
				SelectorName:   "Normal",
				SelectorOrder:  intPtrPersonaLocal(1),
				PromptMD:       "normal prompt",
			},
			{
				ID:             "extended-search",
				Version:        "1",
				Title:          "Search",
				UserSelectable: true,
				SelectorName:   "Search",
				SelectorOrder:  intPtrPersonaLocal(2),
				PromptMD:       "search prompt",
			},
		},
	})

	reg := doJSON(handler, nethttp.MethodPost, "/v1/auth/register", map[string]any{
		"login":    "member-persona@test.com",
		"password": "personapass123",
		"email":    "member-persona@test.com",
	}, nil)
	if reg.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", reg.Code, reg.Body.String())
	}
	regBody := decodeJSONBody[registerResponse](t, reg.Body.Bytes())
	if err := membershipRepo.SetRoleForUser(ctx, uuid.MustParse(regBody.UserID), auth.RoleAccountMember); err != nil {
		t.Fatalf("set member role: %v", err)
	}

	headers := authHeader(regBody.AccessToken)
	listResp := doJSON(handler, nethttp.MethodGet, "/v1/personas?scope=user", nil, headers)
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list personas: %d %s", listResp.Code, listResp.Body.String())
	}
	platformResp := doJSON(handler, nethttp.MethodGet, "/v1/personas?scope=platform", nil, headers)
	assertErrorEnvelope(t, platformResp, nethttp.StatusForbidden, "auth.forbidden")

	body := decodeJSONBody[[]personaResponse](t, listResp.Body.Bytes())
	if len(body) != 2 {
		t.Fatalf("unexpected persona count: %d", len(body))
	}

	byKey := make(map[string]personaResponse, len(body))
	for _, persona := range body {
		byKey[persona.PersonaKey] = persona
	}
	if byKey["normal"].SelectorName == nil || *byKey["normal"].SelectorName != "Normal" {
		t.Fatalf("unexpected normal selector: %#v", byKey["normal"].SelectorName)
	}
	if byKey["extended-search"].SelectorName == nil || *byKey["extended-search"].SelectorName != "Search" {
		t.Fatalf("unexpected search selector: %#v", byKey["extended-search"].SelectorName)
	}
}

func TestSelectablePersonasEffectiveForMemberUser(t *testing.T) {
	db := setupTestDatabase(t, "api_go_selectable_personas")
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

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	if err != nil {
		t.Fatalf("new registration service: %v", err)
	}
	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)

	handler := NewHandler(HandlerConfig{
		Pool:                  pool,
		Logger:                logger,
		AuthService:           authService,
		RegistrationService:   registrationService,
		AuditWriter:           auditWriter,
		AccountMembershipRepo: membershipRepo,
		PersonasRepo:          personasRepo,
		RepoPersonas: []repopersonas.RepoPersona{
			{
				ID:             "normal",
				Version:        "1",
				Title:          "Normal",
				UserSelectable: true,
				SelectorName:   "Normal",
				SelectorOrder:  intPtrPersonaLocal(1),
				PromptMD:       "normal prompt",
			},
			{
				ID:             "extended-search",
				Version:        "1",
				Title:          "Search",
				UserSelectable: true,
				SelectorName:   "Search",
				SelectorOrder:  intPtrPersonaLocal(2),
				PromptMD:       "search prompt",
			},
			{
				ID:             "hidden-builtin",
				Version:        "1",
				Title:          "Hidden Builtin",
				UserSelectable: false,
				PromptMD:       "hidden prompt",
			},
		},
	})

	reg := doJSON(handler, nethttp.MethodPost, "/v1/auth/register", map[string]any{
		"login":    "effective-persona@test.com",
		"password": "personapass123",
		"email":    "effective-persona@test.com",
	}, nil)
	if reg.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", reg.Code, reg.Body.String())
	}
	regBody := decodeJSONBody[registerResponse](t, reg.Body.Bytes())
	if err := membershipRepo.SetRoleForUser(ctx, uuid.MustParse(regBody.UserID), auth.RoleAccountMember); err != nil {
		t.Fatalf("set member role: %v", err)
	}

	meResp := doJSON(handler, nethttp.MethodGet, "/v1/me", nil, authHeader(regBody.AccessToken))
	if meResp.Code != nethttp.StatusOK {
		t.Fatalf("me: %d %s", meResp.Code, meResp.Body.String())
	}
	me := decodeJSONBody[meResponse](t, meResp.Body.Bytes())
	accountID := uuid.MustParse(me.AccountID)

	if _, err := personasRepo.CreateInScope(ctx, uuid.Nil, data.PersonaScopePlatform, "extended-search", "1", "Platform Search", nil, "platform search prompt", nil, nil, json.RawMessage(`{"temperature":0.4}`), nil, nil, strPtrPersonaLocal("platform^search"), "auto", true, "none", "agent.simple", nil); err != nil {
		t.Fatalf("create platform search: %v", err)
	}
	if _, err := personasRepo.CreateInScope(ctx, uuid.Nil, data.PersonaScopePlatform, "hidden-builtin", "1", "Platform Hidden", nil, "platform hidden prompt", nil, nil, json.RawMessage(`{"temperature":0.4}`), nil, nil, strPtrPersonaLocal("platform^hidden"), "auto", true, "none", "agent.simple", nil); err != nil {
		t.Fatalf("create platform hidden: %v", err)
	}
	if _, err := personasRepo.CreateInScope(ctx, uuid.Nil, data.PersonaScopePlatform, "platform-custom", "1", "Platform Custom", nil, "platform custom prompt", nil, nil, json.RawMessage(`{"temperature":0.4}`), nil, nil, strPtrPersonaLocal("platform^custom"), "auto", true, "none", "agent.simple", nil); err != nil {
		t.Fatalf("create platform custom: %v", err)
	}
	if _, err := personasRepo.CreateInScope(ctx, accountID, data.PersonaScopeProject, "normal", "1", "Account Normal", nil, "account normal prompt", nil, nil, json.RawMessage(`{"temperature":0.2}`), nil, nil, strPtrPersonaLocal("org^normal"), "high", true, "system_prompt", "agent.simple", nil); err != nil {
		t.Fatalf("create org normal: %v", err)
	}
	if _, err := personasRepo.CreateInScope(ctx, accountID, data.PersonaScopeProject, "org-custom", "1", "Account Custom", nil, "account custom prompt", nil, nil, json.RawMessage(`{"temperature":0.2}`), nil, nil, strPtrPersonaLocal("org^custom"), "high", true, "system_prompt", "agent.simple", nil); err != nil {
		t.Fatalf("create org custom: %v", err)
	}
	inactive, err := personasRepo.CreateInScope(ctx, accountID, data.PersonaScopeProject, "extended-search", "1", "Account Search Inactive", nil, "account inactive search prompt", nil, nil, json.RawMessage(`{"temperature":0.2}`), nil, nil, strPtrPersonaLocal("org^search"), "high", true, "system_prompt", "agent.simple", nil)
	if err != nil {
		t.Fatalf("create org search inactive: %v", err)
	}
	if _, err := personasRepo.PatchInScope(ctx, accountID, inactive.ID, data.PersonaScopeProject, data.PersonaPatch{IsActive: boolPtrPersonaLocal(false)}); err != nil {
		t.Fatalf("deactivate org search: %v", err)
	}

	type selectablePersonaHTTPResponse struct {
		personaResponse
		Scope string `json:"scope"`
	}

	listResp := doJSON(handler, nethttp.MethodGet, "/v1/me/selectable-personas", nil, authHeader(regBody.AccessToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list selectable personas: %d %s", listResp.Code, listResp.Body.String())
	}

	body := decodeJSONBody[[]selectablePersonaHTTPResponse](t, listResp.Body.Bytes())
	if len(body) != 2 {
		t.Fatalf("unexpected persona count: %d", len(body))
	}

	byKey := make(map[string]selectablePersonaHTTPResponse, len(body))
	for _, persona := range body {
		byKey[persona.PersonaKey] = persona
	}
	if _, exists := byKey["hidden-builtin"]; exists {
		t.Fatal("expected hidden builtin excluded from selectable list")
	}
	if _, exists := byKey["platform-custom"]; exists {
		t.Fatal("expected platform custom excluded from selectable list")
	}
	if _, exists := byKey["org-custom"]; exists {
		t.Fatal("expected org custom excluded from selectable list")
	}

	normal, ok := byKey["normal"]
	if !ok {
		t.Fatal("expected normal persona in selectable list")
	}
	if normal.DisplayName != "Account Normal" {
		t.Fatalf("unexpected normal display name: %q", normal.DisplayName)
	}
	if normal.Scope != data.PersonaScopeProject {
		t.Fatalf("unexpected normal scope: %q", normal.Scope)
	}
	if normal.Source != "custom" {
		t.Fatalf("unexpected normal source: %q", normal.Source)
	}
	if !normal.UserSelectable {
		t.Fatal("expected normal to remain selectable")
	}
	if normal.SelectorName == nil || *normal.SelectorName != "Normal" {
		t.Fatalf("unexpected normal selector: %#v", normal.SelectorName)
	}

	search, ok := byKey["extended-search"]
	if !ok {
		t.Fatal("expected search persona in selectable list")
	}
	if search.DisplayName != "Platform Search" {
		t.Fatalf("unexpected search display name: %q", search.DisplayName)
	}
	if search.Scope != data.PersonaScopePlatform {
		t.Fatalf("unexpected search scope: %q", search.Scope)
	}
	if search.Source != "custom" {
		t.Fatalf("unexpected search source: %q", search.Source)
	}
	if !search.UserSelectable {
		t.Fatal("expected search to remain selectable")
	}
	if search.SelectorName == nil || *search.SelectorName != "Search" {
		t.Fatalf("unexpected search selector: %#v", search.SelectorName)
	}
}

func assertJSONContainsRolePrompt(t *testing.T, raw json.RawMessage, role string, prompt string) {
	t.Helper()
	var parsed map[string]map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal roles json failed: %v", err)
	}
	roleObj, ok := parsed[role]
	if !ok {
		t.Fatalf("expected role %q in %#v", role, parsed)
	}
	if roleObj["prompt_md"] != prompt {
		t.Fatalf("unexpected prompt for role %q: %#v", role, roleObj)
	}
}

func assertJSONContainsEmptyObject(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal empty roles json failed: %v", err)
	}
	if len(parsed) != 0 {
		t.Fatalf("expected empty object, got %#v", parsed)
	}
}

func intPtrPersonaLocal(value int) *int {
	return &value
}

func boolPtrPersonaLocal(value bool) *bool {
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
			(account_id, persona_key, version, display_name, prompt_md, tool_allowlist, budgets_json, executor_type, executor_config_json)
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
