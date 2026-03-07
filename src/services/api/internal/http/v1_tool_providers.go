package http

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type toolProviderDefinition struct {
	GroupName       string
	ProviderName    string
	RequiresAPIKey  bool
	RequiresBaseURL bool
}

var toolProviderCatalog = []toolProviderDefinition{
	{GroupName: "web_search", ProviderName: "web_search.tavily", RequiresAPIKey: true},
	{GroupName: "web_search", ProviderName: "web_search.searxng", RequiresBaseURL: true},
	{GroupName: "web_fetch", ProviderName: "web_fetch.jina", RequiresAPIKey: true},
	{GroupName: "web_fetch", ProviderName: "web_fetch.firecrawl", RequiresAPIKey: true},
	{GroupName: "web_fetch", ProviderName: "web_fetch.basic"},
	{GroupName: "sandbox", ProviderName: "sandbox.docker", RequiresBaseURL: true},
	{GroupName: "sandbox", ProviderName: "sandbox.firecracker", RequiresBaseURL: true},
	{GroupName: "memory", ProviderName: "memory.openviking", RequiresBaseURL: true, RequiresAPIKey: true},
}

type toolProvidersResponse struct {
	Groups []toolProviderGroupResponse `json:"groups"`
}

type toolProviderGroupResponse struct {
	GroupName string                     `json:"group_name"`
	Providers []toolProviderItemResponse `json:"providers"`
}

type toolProviderItemResponse struct {
	GroupName       string          `json:"group_name"`
	ProviderName    string          `json:"provider_name"`
	IsActive        bool            `json:"is_active"`
	KeyPrefix       *string         `json:"key_prefix,omitempty"`
	BaseURL         *string         `json:"base_url,omitempty"`
	RequiresAPIKey  bool            `json:"requires_api_key"`
	RequiresBaseURL bool            `json:"requires_base_url"`
	Configured      bool            `json:"configured"`
	ConfigJSON      json.RawMessage `json:"config_json,omitempty"`
}

type upsertToolProviderCredentialRequest struct {
	APIKey  *string `json:"api_key"`
	BaseURL *string `json:"base_url"`
}

func toolProvidersEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
	directPool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodGet:
			listToolProviders(w, r, traceID, authService, membershipRepo, toolProvidersRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func toolProviderEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
	directPool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/tool-providers/")
		tail = strings.Trim(tail, "/")
		parts := strings.Split(tail, "/")
		if len(parts) != 3 {
			writeNotFound(w, r)
			return
		}

		group := strings.TrimSpace(parts[0])
		provider := strings.TrimSpace(parts[1])
		action := strings.TrimSpace(parts[2])

		if _, ok := findProviderDef(group, provider); !ok {
			writeNotFound(w, r)
			return
		}

		switch action {
		case "activate":
			if r.Method != nethttp.MethodPut {
				writeMethodNotAllowed(w, r)
				return
			}
			activateToolProvider(w, r, traceID, group, provider, authService, membershipRepo, toolProvidersRepo, pool, directPool)
			return
		case "deactivate":
			if r.Method != nethttp.MethodPut {
				writeMethodNotAllowed(w, r)
				return
			}
			deactivateToolProvider(w, r, traceID, group, provider, authService, membershipRepo, toolProvidersRepo, pool, directPool)
			return
		case "credential":
			switch r.Method {
			case nethttp.MethodPut:
				upsertToolProviderCredential(w, r, traceID, group, provider, authService, membershipRepo, toolProvidersRepo, secretsRepo, pool, directPool)
			case nethttp.MethodDelete:
				clearToolProviderCredential(w, r, traceID, group, provider, authService, membershipRepo, toolProvidersRepo, secretsRepo, pool, directPool)
			default:
				writeMethodNotAllowed(w, r)
			}
			return
		case "config":
			if r.Method != nethttp.MethodPut {
				writeMethodNotAllowed(w, r)
				return
			}
			updateToolProviderConfig(w, r, traceID, group, provider, authService, membershipRepo, toolProvidersRepo, pool, directPool)
			return
		default:
			writeNotFound(w, r)
			return
		}
	}
}

func listToolProviders(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if toolProvidersRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "platform"
	}
	if scope != "org" && scope != "platform" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "scope must be org or platform", traceID, nil)
		return
	}

	orgID := uuid.Nil
	if scope == "platform" {
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}
	} else {
		if !requirePerm(actor, auth.PermDataSecrets, w, traceID) {
			return
		}
		orgID = actor.OrgID
	}

	configs, err := toolProvidersRepo.ListByScope(r.Context(), orgID, scope)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	byProvider := map[string]data.ToolProviderConfig{}
	for _, cfg := range configs {
		byProvider[cfg.ProviderName] = cfg
	}

	groupOrder := []string{"web_search", "web_fetch", "sandbox", "memory"}
	groups := make([]toolProviderGroupResponse, 0, len(groupOrder))
	for _, groupName := range groupOrder {
		items := []toolProviderItemResponse{}
		for _, def := range toolProviderCatalog {
			if def.GroupName != groupName {
				continue
			}
			cfg, has := byProvider[def.ProviderName]

			item := toolProviderItemResponse{
				GroupName:       def.GroupName,
				ProviderName:    def.ProviderName,
				IsActive:        has && cfg.IsActive,
				KeyPrefix:       nil,
				BaseURL:         nil,
				RequiresAPIKey:  def.RequiresAPIKey,
				RequiresBaseURL: def.RequiresBaseURL,
				Configured:      false,
			}

			var secretConfigured bool
			if has && cfg.SecretID != nil {
				secretConfigured = true
				item.KeyPrefix = cfg.KeyPrefix
			}
			baseURLConfigured := false
			if has && cfg.BaseURL != nil && strings.TrimSpace(*cfg.BaseURL) != "" {
				baseURLConfigured = true
				item.BaseURL = cfg.BaseURL
			}

			if has && len(cfg.ConfigJSON) > 2 {
				item.ConfigJSON = cfg.ConfigJSON
			}

			item.Configured = (!def.RequiresAPIKey || secretConfigured) && (!def.RequiresBaseURL || baseURLConfigured)
			items = append(items, item)
		}
		groups = append(groups, toolProviderGroupResponse{
			GroupName: groupName,
			Providers: items,
		})
	}

	writeJSON(w, traceID, nethttp.StatusOK, toolProvidersResponse{Groups: groups})
}

func activateToolProvider(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	groupName string,
	providerName string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	pool *pgxpool.Pool,
	directPool *pgxpool.Pool,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if toolProvidersRepo == nil || pool == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "platform"
	}
	if scope != "org" && scope != "platform" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "scope must be org or platform", traceID, nil)
		return
	}

	orgID := uuid.Nil
	notifyPayload := "platform"
	if scope == "platform" {
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}
	} else {
		if !requirePerm(actor, auth.PermDataSecrets, w, traceID) {
			return
		}
		orgID = actor.OrgID
		notifyPayload = actor.OrgID.String()
	}

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context())

	if err := toolProvidersRepo.WithTx(tx).Activate(r.Context(), orgID, scope, groupName, providerName); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			WriteError(w, nethttp.StatusConflict, "tool_provider.active_conflict", "active tool provider conflict", traceID, nil)
			return
		}
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	notifyToolProviderChanged(r.Context(), directPool, pool, notifyPayload)
	w.WriteHeader(nethttp.StatusNoContent)
}

func deactivateToolProvider(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	groupName string,
	providerName string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	pool *pgxpool.Pool,
	directPool *pgxpool.Pool,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if toolProvidersRepo == nil || pool == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "platform"
	}
	if scope != "org" && scope != "platform" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "scope must be org or platform", traceID, nil)
		return
	}

	orgID := uuid.Nil
	notifyPayload := "platform"
	if scope == "platform" {
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}
	} else {
		if !requirePerm(actor, auth.PermDataSecrets, w, traceID) {
			return
		}
		orgID = actor.OrgID
		notifyPayload = actor.OrgID.String()
	}

	if err := toolProvidersRepo.Deactivate(r.Context(), orgID, scope, groupName, providerName); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	notifyToolProviderChanged(r.Context(), directPool, pool, notifyPayload)
	w.WriteHeader(nethttp.StatusNoContent)
}

func upsertToolProviderCredential(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	groupName string,
	providerName string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
	directPool *pgxpool.Pool,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if toolProvidersRepo == nil || pool == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	def, _ := findProviderDef(groupName, providerName)

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "platform"
	}
	if scope != "org" && scope != "platform" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "scope must be org or platform", traceID, nil)
		return
	}

	orgID := uuid.Nil
	notifyPayload := "platform"
	if scope == "platform" {
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}
	} else {
		if !requirePerm(actor, auth.PermDataSecrets, w, traceID) {
			return
		}
		orgID = actor.OrgID
		notifyPayload = actor.OrgID.String()
	}

	var req upsertToolProviderCredentialRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	apiKey := ""
	if req.APIKey != nil {
		apiKey = strings.TrimSpace(*req.APIKey)
	}
	baseURLRaw := ""
	if req.BaseURL != nil {
		baseURLRaw = strings.TrimRight(strings.TrimSpace(*req.BaseURL), "/")
		if def.RequiresBaseURL && baseURLRaw == "" {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "base_url is required", traceID, nil)
			return
		}
	}

	baseURL := ""
	if baseURLRaw != "" {
		baseURL = baseURLRaw
	}
	if apiKey == "" && baseURL == "" {
		w.WriteHeader(nethttp.StatusNoContent)
		return
	}

	secretName := "tool_provider:" + providerName

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context())

	txProviders := toolProvidersRepo.WithTx(tx)

	var (
		secretID   *uuid.UUID
		keyPrefix  *string
		baseURLPtr *string
	)
	if apiKey != "" {
		if secretsRepo == nil {
			WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "secrets not configured", traceID, nil)
			return
		}
		var (
			secret data.Secret
			err    error
		)
		if scope == "platform" {
			secret, err = secretsRepo.WithTx(tx).UpsertPlatform(r.Context(), secretName, apiKey)
		} else {
			secret, err = secretsRepo.WithTx(tx).Upsert(r.Context(), orgID, secretName, apiKey)
		}
		if err != nil {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		id := secret.ID
		secretID = &id
		prefix := computeKeyPrefix(apiKey)
		keyPrefix = &prefix
	}

	if baseURL != "" {
		baseURLPtr = &baseURL
	}

	if _, err := txProviders.UpsertConfig(r.Context(), orgID, scope, groupName, providerName, secretID, keyPrefix, baseURLPtr, nil); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	notifyToolProviderChanged(r.Context(), directPool, pool, notifyPayload)
	w.WriteHeader(nethttp.StatusNoContent)
}

func clearToolProviderCredential(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	groupName string,
	providerName string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
	directPool *pgxpool.Pool,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if toolProvidersRepo == nil || pool == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}
	if secretsRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "secrets not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "platform"
	}
	if scope != "org" && scope != "platform" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "scope must be org or platform", traceID, nil)
		return
	}

	orgID := uuid.Nil
	notifyPayload := "platform"
	if scope == "platform" {
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}
	} else {
		if !requirePerm(actor, auth.PermDataSecrets, w, traceID) {
			return
		}
		orgID = actor.OrgID
		notifyPayload = actor.OrgID.String()
	}

	secretName := "tool_provider:" + providerName

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context())

	var delErr error
	if scope == "platform" {
		delErr = secretsRepo.WithTx(tx).DeletePlatform(r.Context(), secretName)
	} else {
		delErr = secretsRepo.WithTx(tx).Delete(r.Context(), orgID, secretName)
	}
	if delErr != nil {
		var notFound data.SecretNotFoundError
		if !errors.As(delErr, &notFound) {
			WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
	}

	if err := toolProvidersRepo.WithTx(tx).ClearCredential(r.Context(), orgID, scope, providerName); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	notifyToolProviderChanged(r.Context(), directPool, pool, notifyPayload)
	w.WriteHeader(nethttp.StatusNoContent)
}

func updateToolProviderConfig(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	groupName string,
	providerName string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	pool *pgxpool.Pool,
	directPool *pgxpool.Pool,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if toolProvidersRepo == nil || pool == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}

	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "platform"
	}
	if scope != "org" && scope != "platform" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "scope must be org or platform", traceID, nil)
		return
	}

	orgID := uuid.Nil
	notifyPayload := "platform"
	if scope == "platform" {
		if !requirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}
	} else {
		if !requirePerm(actor, auth.PermDataSecrets, w, traceID) {
			return
		}
		orgID = actor.OrgID
		notifyPayload = actor.OrgID.String()
	}

	var raw json.RawMessage
	if err := decodeJSON(r, &raw); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid JSON body", traceID, nil)
		return
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}

	if _, err := toolProvidersRepo.UpsertConfig(r.Context(), orgID, scope, groupName, providerName, nil, nil, nil, raw); err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	notifyToolProviderChanged(r.Context(), directPool, pool, notifyPayload)
	w.WriteHeader(nethttp.StatusNoContent)
}

func findProviderDef(groupName string, providerName string) (toolProviderDefinition, bool) {
	group := strings.TrimSpace(groupName)
	provider := strings.TrimSpace(providerName)
	for _, def := range toolProviderCatalog {
		if def.GroupName == group && def.ProviderName == provider {
			return def, true
		}
	}
	return toolProviderDefinition{}, false
}

func notifyToolProviderChanged(ctx context.Context, directPool *pgxpool.Pool, pool *pgxpool.Pool, payload string) {
	db := directPool
	if db == nil {
		db = pool
	}
	if db == nil {
		return
	}
	_, _ = db.Exec(ctx, "SELECT pg_notify('tool_provider_config_changed', $1)", payload)
}
