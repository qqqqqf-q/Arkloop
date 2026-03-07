package http

import (
	"context"
	"encoding/json"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/personas"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// liteAgentResponse 是 LITE 聚合 API 返回的统一智能体视图。
type liteAgentResponse struct {
	ID              string          `json:"id"`
	PersonaKey      string          `json:"persona_key"`
	DisplayName     string          `json:"display_name"`
	Description     *string         `json:"description,omitempty"`
	PromptMD        string          `json:"prompt_md"`
	Model           *string         `json:"model,omitempty"`
	AgentConfigName *string         `json:"agent_config_name,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	MaxOutputTokens *int            `json:"max_output_tokens,omitempty"`
	ReasoningMode   string          `json:"reasoning_mode"`
	ToolPolicy      string          `json:"tool_policy"`
	ToolAllowlist   []string        `json:"tool_allowlist"`
	IsActive        bool            `json:"is_active"`
	IsDefault       bool            `json:"is_default"`
	ExecutorType    string          `json:"executor_type"`
	BudgetsJSON     json.RawMessage `json:"budgets"`
	Source          string          `json:"source"` // "db" | "repo"
	AgentConfigID   *string         `json:"agent_config_id,omitempty"`
	CreatedAt       string          `json:"created_at"`
}

type createLiteAgentRequest struct {
	Name            string   `json:"name"`
	PromptMD        string   `json:"prompt_md"`
	Model           *string  `json:"model"`
	Temperature     *float64 `json:"temperature"`
	MaxOutputTokens *int     `json:"max_output_tokens"`
	ReasoningMode   string   `json:"reasoning_mode"`
	ToolAllowlist   []string `json:"tool_allowlist"`
	ExecutorType    string   `json:"executor_type"`
}

type patchLiteAgentRequest struct {
	Name            *string   `json:"name"`
	PromptMD        *string   `json:"prompt_md"`
	Model           *string   `json:"model"`
	Temperature     *float64  `json:"temperature"`
	MaxOutputTokens *int      `json:"max_output_tokens"`
	ReasoningMode   *string   `json:"reasoning_mode"`
	ToolAllowlist   *[]string `json:"tool_allowlist"`
	IsActive        *bool     `json:"is_active"`
	IsDefault       *bool     `json:"is_default"`
}

func liteAgentsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
	agentConfigRepo *data.AgentConfigRepository,
	pool *pgxpool.Pool,
	repoPersonas []personas.RepoPersona,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodGet:
			listLiteAgents(w, r, authService, membershipRepo, personasRepo, agentConfigRepo, repoPersonas)
		case nethttp.MethodPost:
			createLiteAgent(w, r, authService, membershipRepo, personasRepo, agentConfigRepo, pool)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func liteAgentEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
	agentConfigRepo *data.AgentConfigRepository,
	pool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/lite/agents/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}

		personaID, err := uuid.Parse(tail)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodPatch:
			patchLiteAgent(w, r, traceID, personaID, authService, membershipRepo, personasRepo, agentConfigRepo, pool)
		case nethttp.MethodDelete:
			deleteLiteAgent(w, r, traceID, personaID, authService, membershipRepo, personasRepo, agentConfigRepo, pool)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

// ─── List ────────────────────────────────────────────────────────────────────

func listLiteAgents(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
	agentConfigRepo *data.AgentConfigRepository,
	repoPersonas []personas.RepoPersona,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	dbPersonas, err := personasRepo.ListByOrg(r.Context(), actor.OrgID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	configs, err := agentConfigRepo.ListByOrg(r.Context(), actor.OrgID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	// 按 persona_id 索引 agent config
	configByPersonaID := make(map[uuid.UUID]data.AgentConfig, len(configs))
	for _, c := range configs {
		if c.PersonaID != nil {
			configByPersonaID[*c.PersonaID] = c
		}
	}

	// 记录已有 persona_key（DB 覆盖 repo）
	dbPersonaKeys := make(map[string]bool, len(dbPersonas))

	resp := make([]liteAgentResponse, 0, len(dbPersonas)+len(repoPersonas))

	for _, p := range dbPersonas {
		dbPersonaKeys[p.PersonaKey] = true
		agent := toLiteAgentFromDB(p, configByPersonaID[p.ID])
		resp = append(resp, agent)
	}

	// 追加仓库 persona（DB 中不存在的）
	for _, rp := range repoPersonas {
		if dbPersonaKeys[rp.ID] {
			continue
		}
		agent := toLiteAgentFromRepo(rp, configByName(configs, rp.AgentConfigName))
		resp = append(resp, agent)
	}

	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

// ─── Create ──────────────────────────────────────────────────────────────────

func createLiteAgent(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
	agentConfigRepo *data.AgentConfigRepository,
	pool *pgxpool.Pool,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataPersonasManage, w, traceID) {
		return
	}

	var req createLiteAgentRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.PromptMD = strings.TrimSpace(req.PromptMD)
	if req.Name == "" || req.PromptMD == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name and prompt_md are required", traceID, nil)
		return
	}

	ctx := r.Context()
	tx, err := pool.Begin(ctx)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(ctx)

	txPersonas := personasRepo.WithTx(tx)
	txConfigs := agentConfigRepo.WithTx(tx)

	executorType := req.ExecutorType
	if executorType == "" {
		executorType = "agent.simple"
	}

	personaKey := slugify(req.Name)
	var modelStr *string
	if req.Model != nil {
		modelStr = req.Model
	}

	persona, err := txPersonas.Create(
		ctx, actor.OrgID,
		personaKey, "1.0",
		req.Name, nil, req.PromptMD,
		req.ToolAllowlist, nil,
		modelStr, executorType, nil,
	)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	reasoningMode := req.ReasoningMode
	if reasoningMode == "" {
		reasoningMode = "disabled"
	}
	toolPolicy := "allowlist"
	if len(req.ToolAllowlist) == 0 {
		toolPolicy = "none"
	}

	config, err := txConfigs.Create(ctx, actor.OrgID, data.CreateAgentConfigRequest{
		Scope:              "platform",
		Name:               req.Name,
		Model:              req.Model,
		Temperature:        req.Temperature,
		MaxOutputTokens:    req.MaxOutputTokens,
		ToolPolicy:         toolPolicy,
		ToolAllowlist:      req.ToolAllowlist,
		PersonaID:          &persona.ID,
		PromptCacheControl: "none",
		ReasoningMode:      reasoningMode,
	})
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusCreated, toLiteAgentFromDB(persona, config))
}

// ─── Patch ───────────────────────────────────────────────────────────────────

func patchLiteAgent(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	personaID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
	agentConfigRepo *data.AgentConfigRepository,
	pool *pgxpool.Pool,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataPersonasManage, w, traceID) {
		return
	}

	existing, err := personasRepo.GetByID(r.Context(), actor.OrgID, personaID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if existing == nil {
		WriteError(w, nethttp.StatusNotFound, "lite_agents.not_found", "agent not found", traceID, nil)
		return
	}

	var req patchLiteAgentRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	ctx := r.Context()
	tx, err := pool.Begin(ctx)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(ctx)

	txPersonas := personasRepo.WithTx(tx)
	txConfigs := agentConfigRepo.WithTx(tx)

	// patch persona
	personaPatch := data.PersonaPatch{
		DisplayName:         req.Name,
		PromptMD:            req.PromptMD,
		IsActive:            req.IsActive,
		PreferredCredential: req.Model,
	}
	if req.ToolAllowlist != nil {
		personaPatch.ToolAllowlist = *req.ToolAllowlist
	}

	updated, err := txPersonas.Patch(ctx, actor.OrgID, personaID, personaPatch)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if updated == nil {
		WriteError(w, nethttp.StatusNotFound, "lite_agents.not_found", "agent not found", traceID, nil)
		return
	}

	// 查找关联的 agent config
	linkedConfig := findLinkedConfig(ctx, agentConfigRepo, actor.OrgID, personaID)
	var resultConfig data.AgentConfig

	if linkedConfig != nil {
		// patch agent config
		fields := data.AgentConfigUpdateFields{}
		if req.Name != nil {
			fields.SetName = true
			fields.Name = strings.TrimSpace(*req.Name)
		}
		if req.Model != nil {
			fields.SetModel = true
			fields.Model = req.Model
		}
		if req.Temperature != nil {
			fields.SetTemperature = true
			fields.Temperature = req.Temperature
		}
		if req.MaxOutputTokens != nil {
			fields.SetMaxOutputTokens = true
			fields.MaxOutputTokens = req.MaxOutputTokens
		}
		if req.ReasoningMode != nil {
			fields.SetReasoningMode = true
			fields.ReasoningMode = *req.ReasoningMode
		}
		if req.ToolAllowlist != nil {
			fields.SetToolPolicy = true
			fields.SetToolAllowlist = true
			fields.ToolAllowlist = *req.ToolAllowlist
			if len(*req.ToolAllowlist) > 0 {
				fields.ToolPolicy = "allowlist"
			} else {
				fields.ToolPolicy = "none"
			}
		}
		if req.IsDefault != nil {
			fields.SetIsDefault = true
			fields.IsDefault = *req.IsDefault
		}

		hasUpdate := fields.SetName || fields.SetModel || fields.SetTemperature ||
			fields.SetMaxOutputTokens || fields.SetReasoningMode || fields.SetToolAllowlist ||
			fields.SetIsDefault

		if hasUpdate {
			ac, err := txConfigs.Update(ctx, linkedConfig.ID, actor.OrgID, true, fields)
			if err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if ac != nil {
				resultConfig = *ac
			}
		} else {
			resultConfig = *linkedConfig
		}
	}

	if err := tx.Commit(ctx); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusOK, toLiteAgentFromDB(*updated, resultConfig))
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func deleteLiteAgent(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	personaID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	personasRepo *data.PersonasRepository,
	agentConfigRepo *data.AgentConfigRepository,
	pool *pgxpool.Pool,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermDataPersonasManage, w, traceID) {
		return
	}

	existing, err := personasRepo.GetByID(r.Context(), actor.OrgID, personaID)
	if err != nil || existing == nil {
		WriteError(w, nethttp.StatusNotFound, "lite_agents.not_found", "agent not found", traceID, nil)
		return
	}

	ctx := r.Context()
	tx, err2 := pool.Begin(ctx)
	if err2 != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(ctx)

	// 先删 agent config（有 FK 约束方向则先删子表）
	linkedConfig := findLinkedConfig(ctx, agentConfigRepo, actor.OrgID, personaID)
	if linkedConfig != nil {
		txConfigs := agentConfigRepo.WithTx(tx)
		_ = txConfigs.Delete(ctx, linkedConfig.ID, actor.OrgID, true)
	}

	// 删除 persona
	txPersonas := personasRepo.WithTx(tx)
	_, _ = txPersonas.Delete(ctx, actor.OrgID, personaID)

	if err := tx.Commit(ctx); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	writeJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func findLinkedConfig(ctx context.Context, repo *data.AgentConfigRepository, orgID, personaID uuid.UUID) *data.AgentConfig {
	configs, err := repo.ListByOrg(ctx, orgID)
	if err != nil {
		return nil
	}
	for _, c := range configs {
		if c.PersonaID != nil && *c.PersonaID == personaID {
			return &c
		}
	}
	return nil
}

func configByName(configs []data.AgentConfig, name string) *data.AgentConfig {
	if name == "" {
		return nil
	}
	for _, c := range configs {
		if c.Name == name {
			return &c
		}
	}
	return nil
}

func toLiteAgentFromDB(p data.Persona, ac data.AgentConfig) liteAgentResponse {
	allowlist := p.ToolAllowlist
	if allowlist == nil {
		allowlist = []string{}
	}
	budgets := p.BudgetsJSON
	if len(budgets) == 0 {
		budgets = json.RawMessage("{}")
	}
	executorType := p.ExecutorType
	if executorType == "" {
		executorType = "agent.simple"
	}
	reasoningMode := ac.ReasoningMode
	if reasoningMode == "" {
		reasoningMode = "auto"
	}
	toolPolicy := ac.ToolPolicy
	if toolPolicy == "" {
		toolPolicy = "allowlist"
	}

	// 优先使用 agent config 的工具列表
	if len(ac.ToolAllowlist) > 0 {
		allowlist = ac.ToolAllowlist
	}

	var configID *string
	if ac.ID != uuid.Nil {
		s := ac.ID.String()
		configID = &s
	}

	return liteAgentResponse{
		ID:              p.ID.String(),
		PersonaKey:      p.PersonaKey,
		DisplayName:     p.DisplayName,
		Description:     p.Description,
		PromptMD:        p.PromptMD,
		Model:           ac.Model,
		AgentConfigName: optionalLiteTrimmedStringPtr(p.AgentConfigName),
		Temperature:     ac.Temperature,
		MaxOutputTokens: ac.MaxOutputTokens,
		ReasoningMode:   reasoningMode,
		ToolPolicy:      toolPolicy,
		ToolAllowlist:   allowlist,
		IsActive:        p.IsActive,
		IsDefault:       ac.IsDefault,
		ExecutorType:    executorType,
		BudgetsJSON:     budgets,
		Source:          "db",
		AgentConfigID:   configID,
		CreatedAt:       p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func toLiteAgentFromRepo(rp personas.RepoPersona, ac *data.AgentConfig) liteAgentResponse {
	allowlist := rp.ToolAllowlist
	if allowlist == nil {
		allowlist = []string{}
	}

	budgets := json.RawMessage("{}")
	if rp.Budgets != nil {
		if b, err := json.Marshal(rp.Budgets); err == nil {
			budgets = b
		}
	}

	executorType := rp.ExecutorType
	if executorType == "" {
		executorType = "agent.simple"
	}

	desc := rp.Description
	var descPtr *string
	if desc != "" {
		descPtr = &desc
	}

	// 从 budgets 提取 temperature
	var temperature *float64
	if rp.Budgets != nil {
		if t, ok := rp.Budgets["temperature"]; ok {
			if f, ok := t.(float64); ok {
				temperature = &f
			}
		}
	}

	var maxOutputTokens *int
	if rp.Budgets != nil {
		if t, ok := rp.Budgets["max_output_tokens"]; ok {
			switch v := t.(type) {
			case float64:
				i := int(v)
				maxOutputTokens = &i
			case int:
				maxOutputTokens = &v
			}
		}
	}

	resp := liteAgentResponse{
		ID:              rp.ID,
		PersonaKey:      rp.ID,
		DisplayName:     rp.Title,
		Description:     descPtr,
		PromptMD:        rp.PromptMD,
		AgentConfigName: optionalLiteTrimmedString(rp.AgentConfigName),
		Temperature:     temperature,
		MaxOutputTokens: maxOutputTokens,
		ReasoningMode:   "auto",
		ToolPolicy:      "allowlist",
		ToolAllowlist:   allowlist,
		IsActive:        true,
		IsDefault:       false,
		ExecutorType:    executorType,
		BudgetsJSON:     budgets,
		Source:          "repo",
		CreatedAt:       "",
	}

	if ac != nil {
		resp.Model = ac.Model
		resp.AgentConfigID = ptrString(ac.ID.String())
	}

	return resp
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			b.WriteRune('-')
		}
	}
	result := b.String()
	if result == "" {
		result = "agent"
	}
	return result
}

func ptrString(s string) *string {
	return &s
}

func optionalLiteTrimmedStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	return optionalLiteTrimmedString(*value)
}

func optionalLiteTrimmedString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
