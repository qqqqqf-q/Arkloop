package http

import (
	"encoding/json"
	"errors"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type createMCPConfigRequest struct {
	Name             string          `json:"name"`
	Transport        string          `json:"transport"`
	URL              *string         `json:"url"`
	BearerToken      *string         `json:"bearer_token"`
	Command          *string         `json:"command"`
	Args             []string        `json:"args"`
	Cwd              *string         `json:"cwd"`
	Env              map[string]string `json:"env"`
	InheritParentEnv bool            `json:"inherit_parent_env"`
	CallTimeoutMs    *int            `json:"call_timeout_ms"`
}

type patchMCPConfigRequest struct {
	Name          *string `json:"name"`
	URL           *string `json:"url"`
	BearerToken   *string `json:"bearer_token"`
	CallTimeoutMs *int    `json:"call_timeout_ms"`
	IsActive      *bool   `json:"is_active"`
}

type mcpConfigResponse struct {
	ID               string          `json:"id"`
	OrgID            string          `json:"org_id"`
	Name             string          `json:"name"`
	Transport        string          `json:"transport"`
	URL              *string         `json:"url,omitempty"`
	HasAuth          bool            `json:"has_auth"`
	Command          *string         `json:"command,omitempty"`
	Args             []string        `json:"args,omitempty"`
	Cwd              *string         `json:"cwd,omitempty"`
	InheritParentEnv bool            `json:"inherit_parent_env"`
	CallTimeoutMs    int             `json:"call_timeout_ms"`
	IsActive         bool            `json:"is_active"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
}

var validMCPTransports = map[string]bool{
	"stdio":           true,
	"http_sse":        true,
	"streamable_http": true,
}

func mcpConfigsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	mcpRepo *data.MCPConfigsRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodPost:
			createMCPConfig(w, r, traceID, authService, membershipRepo, mcpRepo, secretsRepo, pool)
		case nethttp.MethodGet:
			listMCPConfigs(w, r, traceID, authService, membershipRepo, mcpRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func mcpConfigEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	mcpRepo *data.MCPConfigsRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/mcp-configs/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}

		configID, err := uuid.Parse(tail)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodPatch:
			patchMCPConfig(w, r, traceID, configID, authService, membershipRepo, mcpRepo, secretsRepo, pool)
		case nethttp.MethodDelete:
			deleteMCPConfig(w, r, traceID, configID, authService, membershipRepo, mcpRepo, pool)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func createMCPConfig(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	mcpRepo *data.MCPConfigsRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if mcpRepo == nil || pool == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	var req createMCPConfigRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Transport = strings.ToLower(strings.TrimSpace(req.Transport))

	if req.Name == "" || req.Transport == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name and transport are required", traceID, nil)
		return
	}
	if !validMCPTransports[req.Transport] {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "transport must be stdio, http_sse, or streamable_http", traceID, nil)
		return
	}
	if req.Transport == "stdio" && (req.Command == nil || strings.TrimSpace(*req.Command) == "") {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "command is required for stdio transport", traceID, nil)
		return
	}
	if req.Transport != "stdio" && (req.URL == nil || strings.TrimSpace(*req.URL) == "") {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "url is required for HTTP transport", traceID, nil)
		return
	}
	if req.BearerToken != nil && strings.TrimSpace(*req.BearerToken) != "" && secretsRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "secrets not configured", traceID, nil)
		return
	}

	timeoutMs := 10000
	if req.CallTimeoutMs != nil {
		if *req.CallTimeoutMs <= 0 {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "call_timeout_ms must be positive", traceID, nil)
			return
		}
		timeoutMs = *req.CallTimeoutMs
	}

	argsJSON, _ := json.Marshal(req.Args)
	envJSON, _ := json.Marshal(req.Env)

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context())

	txMCP := mcpRepo.WithTx(tx)

	cfg, err := txMCP.Create(
		r.Context(),
		actor.OrgID,
		req.Name,
		req.Transport,
		req.URL,
		nil, // auth_secret_id 在 secret 创建后填充
		req.Command,
		argsJSON,
		req.Cwd,
		envJSON,
		req.InheritParentEnv,
		timeoutMs,
	)
	if err != nil {
		var nameConflict data.MCPConfigNameConflictError
		if errors.As(err, &nameConflict) {
			WriteError(w, nethttp.StatusConflict, "mcp_configs.name_conflict", "config name already exists", traceID, nil)
			return
		}
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if req.BearerToken != nil && strings.TrimSpace(*req.BearerToken) != "" {
		txSecrets := secretsRepo.WithTx(tx)
		secret, err := txSecrets.Create(r.Context(), actor.OrgID, "mcp_cred:"+cfg.ID.String(), *req.BearerToken)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if err := txMCP.UpdateAuthSecret(r.Context(), actor.OrgID, cfg.ID, secret.ID); err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		cfg.AuthSecretID = &secret.ID
	}

	if err := tx.Commit(r.Context()); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	// 主动失效 Worker 侧 MCP 发现缓存
	_, _ = pool.Exec(r.Context(), "SELECT pg_notify('mcp_config_changed', $1)", actor.OrgID.String())

	writeJSON(w, traceID, nethttp.StatusCreated, toMCPConfigResponse(cfg))
}

func listMCPConfigs(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	mcpRepo *data.MCPConfigsRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if mcpRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	configs, err := mcpRepo.ListByOrg(r.Context(), actor.OrgID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]mcpConfigResponse, 0, len(configs))
	for _, cfg := range configs {
		resp = append(resp, toMCPConfigResponse(cfg))
	}

	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func patchMCPConfig(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	configID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	mcpRepo *data.MCPConfigsRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if mcpRepo == nil || pool == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	existing, err := mcpRepo.GetByID(r.Context(), actor.OrgID, configID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if existing == nil {
		WriteError(w, nethttp.StatusNotFound, "mcp_configs.not_found", "config not found", traceID, nil)
		return
	}

	var req patchMCPConfigRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	if req.BearerToken != nil && strings.TrimSpace(*req.BearerToken) != "" && secretsRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "secrets not configured", traceID, nil)
		return
	}

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context())

	txMCP := mcpRepo.WithTx(tx)

	if req.BearerToken != nil && strings.TrimSpace(*req.BearerToken) != "" {
		txSecrets := secretsRepo.WithTx(tx)
		secretName := "mcp_cred:" + configID.String()
		secret, err := txSecrets.Upsert(r.Context(), actor.OrgID, secretName, *req.BearerToken)
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if existing.AuthSecretID == nil || *existing.AuthSecretID != secret.ID {
			if err := txMCP.UpdateAuthSecret(r.Context(), actor.OrgID, configID, secret.ID); err != nil {
				WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
		}
	}

	patch := data.MCPConfigPatch{
		Name:          req.Name,
		URL:           req.URL,
		CallTimeoutMs: req.CallTimeoutMs,
		IsActive:      req.IsActive,
	}

	updated, err := txMCP.Patch(r.Context(), actor.OrgID, configID, patch)
	if err != nil {
		var nameConflict data.MCPConfigNameConflictError
		if errors.As(err, &nameConflict) {
			WriteError(w, nethttp.StatusConflict, "mcp_configs.name_conflict", "config name already exists", traceID, nil)
			return
		}
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if updated == nil {
		WriteError(w, nethttp.StatusNotFound, "mcp_configs.not_found", "config not found", traceID, nil)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	// 主动失效 Worker 侧 MCP 发现缓存
	_, _ = pool.Exec(r.Context(), "SELECT pg_notify('mcp_config_changed', $1)", actor.OrgID.String())

	writeJSON(w, traceID, nethttp.StatusOK, toMCPConfigResponse(*updated))
}

func deleteMCPConfig(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	configID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	mcpRepo *data.MCPConfigsRepository,
	pool *pgxpool.Pool,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if mcpRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	existing, err := mcpRepo.GetByID(r.Context(), actor.OrgID, configID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if existing == nil {
		WriteError(w, nethttp.StatusNotFound, "mcp_configs.not_found", "config not found", traceID, nil)
		return
	}

	if err := mcpRepo.Delete(r.Context(), actor.OrgID, configID); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	// 主动失效 Worker 侧 MCP 发现缓存
	if pool != nil {
		_, _ = pool.Exec(r.Context(), "SELECT pg_notify('mcp_config_changed', $1)", actor.OrgID.String())
	}

	writeJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func toMCPConfigResponse(cfg data.MCPConfig) mcpConfigResponse {
	var args []string
	if len(cfg.ArgsJSON) > 0 {
		_ = json.Unmarshal(cfg.ArgsJSON, &args)
	}
	if args == nil {
		args = []string{}
	}

	return mcpConfigResponse{
		ID:               cfg.ID.String(),
		OrgID:            cfg.OrgID.String(),
		Name:             cfg.Name,
		Transport:        cfg.Transport,
		URL:              cfg.URL,
		HasAuth:          cfg.AuthSecretID != nil,
		Command:          cfg.Command,
		Args:             args,
		Cwd:              cfg.CwdPath,
		InheritParentEnv: cfg.InheritParentEnv,
		CallTimeoutMs:    cfg.CallTimeoutMs,
		IsActive:         cfg.IsActive,
		CreatedAt:        cfg.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:        cfg.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}
