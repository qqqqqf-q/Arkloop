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
				ID:       "builtin-only",
				Version:  "1",
				Title:    "Builtin Only",
				PromptMD: "builtin prompt",
				Budgets:  map[string]any{"max_output_tokens": 64},
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
	ghostID := insertGlobalPersonaHTTP(t, ctx, pool, "ghost", "Ghost Persona")

	listResp := doJSON(handler, nethttp.MethodGet, "/v1/personas", nil, headers)
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list personas: %d %s", listResp.Code, listResp.Body.String())
	}
	list := decodeJSONBody[[]personaResponse](t, listResp.Body.Bytes())
	if len(list) != 3 {
		t.Fatalf("expected 3 personas, got %d body=%s", len(list), listResp.Body.String())
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

	shadowed := byKey["shadowed"]
	if shadowed.Source != "custom" {
		t.Fatalf("expected custom shadowed persona, got %q", shadowed.Source)
	}
	if shadowed.DisplayName != "Custom Shadowed" {
		t.Fatalf("expected custom display name, got %q", shadowed.DisplayName)
	}

	customOnly := byKey["custom-only"]
	if customOnly.Source != "custom" {
		t.Fatalf("expected custom source, got %q", customOnly.Source)
	}
	if customOnly.CreatedAt == "" {
		t.Fatal("expected created_at on custom persona")
	}

	patchResp := doJSON(handler, nethttp.MethodPatch, "/v1/personas/"+ghostID.String(), map[string]any{
		"display_name": "Ghost Renamed",
	}, headers)
	assertErrorEnvelope(t, patchResp, nethttp.StatusNotFound, "personas.not_found")
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
