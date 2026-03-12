//go:build !desktop

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

func TestAdminExecutionGovernanceReturnsPersonaCentricView(t *testing.T) {
	db := setupTestDatabase(t, "api_go_execution_governance")
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
		t.Fatalf("new refresh repo: %v", err)
	}
	auditRepo, err := data.NewAuditLogRepository(appDB)
	if err != nil {
		t.Fatalf("new audit repo: %v", err)
	}
	jobRepo, err := data.NewJobRepository(appDB)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
	}
	personasRepo, err := data.NewPersonasRepository(appDB)
	if err != nil {
		t.Fatalf("new personas repo: %v", err)
	}

	authService, err := auth.NewService(userRepo, credentialRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
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
		PersonasRepo:        personasRepo,
		RepoPersonas: []repopersonas.RepoPersona{
			{
				ID:                  "builtin-ops",
				Version:             "1",
				Title:               "Builtin Ops",
				SoulMD:              "builtin soul",
				PromptMD:            "builtin prompt",
				PreferredCredential: "cred-builtin",
				Model:               "builtin-cred^gpt-builtin",
				ReasoningMode:       "low",
				PromptCacheControl:  "none",
				Budgets: map[string]any{
					"tool_continuation_budget": 5,
					"max_output_tokens":        9999,
					"per_tool_soft_limits": map[string]any{
						"write_stdin": map[string]any{"max_continuations": 7},
					},
				},
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
	if _, err := appDB.Exec(ctx, "UPDATE org_memberships SET role = $1 WHERE user_id = $2", auth.RolePlatformAdmin, adminPayload.UserID); err != nil {
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

	if _, err := appDB.Exec(ctx, `INSERT INTO platform_settings (key, value, updated_at) VALUES
		('limit.agent_reasoning_iterations', '12', now()),
		('limit.tool_continuation_budget', '30', now()),
		('limit.thread_message_history', '250', now()),
		('title_summarizer.model', 'summary-cred^gpt-summary', now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`); err != nil {
		t.Fatalf("seed platform settings: %v", err)
	}
	if _, err := appDB.Exec(ctx, `INSERT INTO org_settings (org_id, key, value, updated_at) VALUES
		($1, 'limit.agent_reasoning_iterations', '9', now()),
		($1, 'limit.tool_continuation_budget', '18', now())
		ON CONFLICT (org_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, orgID); err != nil {
		t.Fatalf("seed org settings: %v", err)
	}

	customBudgets := json.RawMessage(`{"max_output_tokens":2048,"tool_continuation_budget":15,"temperature":0.7}`)
	_, err = personasRepo.Create(
		ctx,
		orgID,
		"custom-bound",
		"1",
		"Custom Bound",
		nil,
		"custom prompt",
		nil,
		nil,
		customBudgets,
		strPtrLocal("cred-custom"),
		strPtrLocal("custom-cred^claude-3-7-sonnet"),
		"high",
		"system_prompt",
		"agent.simple",
		nil,
	)
	if err != nil {
		t.Fatalf("create custom persona: %v", err)
	}

	noOrgResp := doJSON(handler, nethttp.MethodGet, "/v1/admin/execution-governance", nil, authHeader(adminToken))
	if noOrgResp.Code != nethttp.StatusOK {
		t.Fatalf("execution governance without org: %d %s", noOrgResp.Code, noOrgResp.Body.String())
	}
	noOrgBody := decodeJSONBody[executionGovernanceResponse](t, noOrgResp.Body.Bytes())
	if len(noOrgBody.Personas) != 0 {
		t.Fatalf("expected no personas without org_id, got %d", len(noOrgBody.Personas))
	}
	if noOrgBody.TitleSummarizerModel == nil || *noOrgBody.TitleSummarizerModel != "summary-cred^gpt-summary" {
		t.Fatalf("unexpected title summarizer model without org: %#v", noOrgBody.TitleSummarizerModel)
	}

	resp := doJSON(handler, nethttp.MethodGet, "/v1/admin/execution-governance?org_id="+orgID.String(), nil, authHeader(adminToken))
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("execution governance: %d %s", resp.Code, resp.Body.String())
	}
	body := decodeJSONBody[executionGovernanceResponse](t, resp.Body.Bytes())

	if body.TitleSummarizerModel == nil || *body.TitleSummarizerModel != "summary-cred^gpt-summary" {
		t.Fatalf("unexpected title summarizer model: %#v", body.TitleSummarizerModel)
	}
	if len(body.Personas) != 2 {
		t.Fatalf("unexpected persona count: %d", len(body.Personas))
	}

	byKey := make(map[string]executionGovernancePersona, len(body.Personas))
	for _, persona := range body.Personas {
		byKey[persona.PersonaKey] = persona
	}

	custom, ok := byKey["custom-bound"]
	if !ok {
		t.Fatal("expected custom-bound persona")
	}
	if custom.Source != "custom" {
		t.Fatalf("unexpected custom source: %q", custom.Source)
	}
	if custom.Model == nil || *custom.Model != "custom-cred^claude-3-7-sonnet" {
		t.Fatalf("unexpected custom model: %#v", custom.Model)
	}
	if custom.PreferredCredential == nil || *custom.PreferredCredential != "cred-custom" {
		t.Fatalf("unexpected preferred credential: %#v", custom.PreferredCredential)
	}
	if custom.ReasoningMode != "high" {
		t.Fatalf("unexpected custom reasoning_mode: %q", custom.ReasoningMode)
	}
	if custom.PromptCacheControl != "system_prompt" {
		t.Fatalf("unexpected custom prompt_cache_control: %q", custom.PromptCacheControl)
	}
	if custom.Requested.MaxOutputTokens == nil || *custom.Requested.MaxOutputTokens != 2048 {
		t.Fatalf("unexpected custom requested max_output_tokens: %#v", custom.Requested.MaxOutputTokens)
	}
	if custom.Requested.ToolContinuationBudget == nil || *custom.Requested.ToolContinuationBudget != 15 {
		t.Fatalf("unexpected custom requested tool_continuation_budget: %#v", custom.Requested.ToolContinuationBudget)
	}
	if custom.Effective.ReasoningIterations != 9 {
		t.Fatalf("unexpected custom effective reasoning_iterations: %d", custom.Effective.ReasoningIterations)
	}
	if custom.Effective.ToolContinuationBudget != 15 {
		t.Fatalf("unexpected custom effective tool_continuation_budget: %d", custom.Effective.ToolContinuationBudget)
	}
	if custom.Effective.MaxOutputTokens == nil || *custom.Effective.MaxOutputTokens != 2048 {
		t.Fatalf("unexpected custom effective max_output_tokens: %#v", custom.Effective.MaxOutputTokens)
	}
	if custom.Effective.Temperature == nil || *custom.Effective.Temperature != 0.7 {
		t.Fatalf("unexpected custom effective temperature: %#v", custom.Effective.Temperature)
	}
	if custom.Effective.ReasoningMode != "high" {
		t.Fatalf("unexpected custom effective reasoning_mode: %q", custom.Effective.ReasoningMode)
	}

	builtin, ok := byKey["builtin-ops"]
	if !ok {
		t.Fatal("expected builtin-ops persona")
	}
	if builtin.Source != "builtin" {
		t.Fatalf("unexpected builtin source: %q", builtin.Source)
	}
	if builtin.Model == nil || *builtin.Model != "builtin-cred^gpt-builtin" {
		t.Fatalf("unexpected builtin model: %#v", builtin.Model)
	}
	if builtin.PreferredCredential == nil || *builtin.PreferredCredential != "cred-builtin" {
		t.Fatalf("unexpected builtin preferred credential: %#v", builtin.PreferredCredential)
	}
	if builtin.Requested.ToolContinuationBudget == nil || *builtin.Requested.ToolContinuationBudget != 5 {
		t.Fatalf("unexpected builtin requested tool_continuation_budget: %#v", builtin.Requested.ToolContinuationBudget)
	}
	if builtin.Requested.MaxOutputTokens == nil || *builtin.Requested.MaxOutputTokens != 9999 {
		t.Fatalf("unexpected builtin requested max_output_tokens: %#v", builtin.Requested.MaxOutputTokens)
	}
	if builtin.Effective.ReasoningIterations != 9 {
		t.Fatalf("unexpected builtin effective reasoning_iterations: %d", builtin.Effective.ReasoningIterations)
	}
	if builtin.Effective.ToolContinuationBudget != 5 {
		t.Fatalf("unexpected builtin effective tool_continuation_budget: %d", builtin.Effective.ToolContinuationBudget)
	}
	if builtin.Effective.MaxOutputTokens == nil || *builtin.Effective.MaxOutputTokens != 9999 {
		t.Fatalf("unexpected builtin effective max_output_tokens: %#v", builtin.Effective.MaxOutputTokens)
	}
	if builtin.Effective.ReasoningMode != "low" {
		t.Fatalf("unexpected builtin effective reasoning_mode: %q", builtin.Effective.ReasoningMode)
	}
	if builtin.Effective.SystemPrompt != "builtin soul\n\nbuiltin prompt" {
		t.Fatalf("unexpected builtin effective system_prompt: %q", builtin.Effective.SystemPrompt)
	}
	limit := builtin.Effective.PerToolSoftLimits["write_stdin"]
	if limit.MaxContinuations == nil || *limit.MaxContinuations != 7 {
		t.Fatalf("unexpected builtin write_stdin limit: %#v", limit)
	}
}

func strPtrLocal(value string) *string {
	return &value
}
