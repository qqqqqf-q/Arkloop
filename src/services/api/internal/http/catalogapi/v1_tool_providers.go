package catalogapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
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

func toolProvidersEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	directPool *pgxpool.Pool,
	projectRepo *data.ProjectRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodGet:
			listToolProviders(w, r, traceID, authService, membershipRepo, toolProvidersRepo, projectRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func toolProviderEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	directPool *pgxpool.Pool,
	projectRepo *data.ProjectRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/tool-providers/")
		tail = strings.Trim(tail, "/")
		parts := strings.Split(tail, "/")
		if len(parts) != 3 {
			httpkit.WriteNotFound(w, r)
			return
		}

		group := strings.TrimSpace(parts[0])
		provider := strings.TrimSpace(parts[1])
		action := strings.TrimSpace(parts[2])

		if _, ok := findProviderDef(group, provider); !ok {
			httpkit.WriteNotFound(w, r)
			return
		}

		switch action {
		case "activate":
			if r.Method != nethttp.MethodPut {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			activateToolProvider(w, r, traceID, group, provider, authService, membershipRepo, toolProvidersRepo, pool, directPool, projectRepo)
			return
		case "deactivate":
			if r.Method != nethttp.MethodPut {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			deactivateToolProvider(w, r, traceID, group, provider, authService, membershipRepo, toolProvidersRepo, pool, directPool, projectRepo)
			return
		case "credential":
			switch r.Method {
			case nethttp.MethodPut:
				upsertToolProviderCredential(w, r, traceID, group, provider, authService, membershipRepo, toolProvidersRepo, secretsRepo, pool, directPool, projectRepo)
			case nethttp.MethodDelete:
				clearToolProviderCredential(w, r, traceID, group, provider, authService, membershipRepo, toolProvidersRepo, secretsRepo, pool, directPool, projectRepo)
			default:
				httpkit.WriteMethodNotAllowed(w, r)
			}
			return
		case "config":
			if r.Method != nethttp.MethodPut {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			updateToolProviderConfig(w, r, traceID, group, provider, authService, membershipRepo, toolProvidersRepo, pool, directPool, projectRepo)
			return
		default:
			httpkit.WriteNotFound(w, r)
			return
		}
	}
}

func resolveToolProviderScope(
	ctx context.Context,
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *httpkit.Actor,
	projectRepo *data.ProjectRepository,
) (string, *uuid.UUID, bool) {
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "platform"
	}
	if scope != "user" && scope != "project" && scope != "platform" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "scope must be user or platform", traceID, nil)
		return "", nil, false
	}
	if scope == "project" {
		scope = "user"
	}
	if scope == "platform" {
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return "", nil, false
		}
		return "platform", nil, true
	}
	if !httpkit.RequirePerm(actor, auth.PermDataSecrets, w, traceID) {
		return "", nil, false
	}
	return "user", &actor.UserID, true
}

func listToolProviders(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	projectRepo *data.ProjectRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if toolProvidersRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}

	ownerKind, ownerUserID, ok := resolveToolProviderScope(r.Context(), w, r, traceID, actor, projectRepo)
	if !ok {
		return
	}

	configs, err := toolProvidersRepo.ListByOwner(r.Context(), ownerKind, ownerUserID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	byProvider := map[string]data.ToolProviderConfig{}
	for _, cfg := range configs {
		byProvider[cfg.ProviderName] = cfg
	}

	groupOrder := []string{"web_search", "web_fetch", "image_understanding", "acp", "sandbox", "memory"}
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
				ConfigFields:    def.ConfigFields,
				DefaultBaseURL:  def.DefaultBaseURL,
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

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toolProvidersResponse{Groups: groups})
}

func activateToolProvider(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	groupName string,
	providerName string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	pool data.DB,
	directPool *pgxpool.Pool,
	projectRepo *data.ProjectRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if toolProvidersRepo == nil || pool == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}

	ownerKind, ownerUserID, ok := resolveToolProviderScope(r.Context(), w, r, traceID, actor, projectRepo)
	if !ok {
		return
	}

	notifyPayload := "platform"
	if ownerKind != "platform" && ownerUserID != nil {
		notifyPayload = ownerUserID.String()
	}

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context())

	if err := toolProvidersRepo.WithTx(tx).Activate(r.Context(), ownerKind, ownerUserID, groupName, providerName); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httpkit.WriteError(w, nethttp.StatusConflict, "tool_provider.active_conflict", "active tool provider conflict", traceID, nil)
			return
		}
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	applyProviderDefaults(r.Context(), toolProvidersRepo.WithTx(tx), ownerKind, ownerUserID, groupName, providerName)

	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
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
	membershipRepo *data.AccountMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	pool data.DB,
	directPool *pgxpool.Pool,
	projectRepo *data.ProjectRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if toolProvidersRepo == nil || pool == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}

	ownerKind, ownerUserID, ok := resolveToolProviderScope(r.Context(), w, r, traceID, actor, projectRepo)
	if !ok {
		return
	}

	if err := toolProvidersRepo.Deactivate(r.Context(), ownerKind, ownerUserID, groupName, providerName); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	notifyPayload := "platform"
	if ownerKind != "platform" && ownerUserID != nil {
		notifyPayload = ownerUserID.String()
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
	membershipRepo *data.AccountMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	directPool *pgxpool.Pool,
	projectRepo *data.ProjectRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if toolProvidersRepo == nil || pool == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	def, _ := findProviderDef(groupName, providerName)

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}

	ownerKind, ownerUserID, ok := resolveToolProviderScope(r.Context(), w, r, traceID, actor, projectRepo)
	if !ok {
		return
	}

	notifyPayload := "platform"
	if ownerKind != "platform" && ownerUserID != nil {
		notifyPayload = ownerUserID.String()
	}

	var req upsertToolProviderCredentialRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	apiKey := ""
	if req.APIKey != nil {
		apiKey = strings.TrimSpace(*req.APIKey)
	}
	baseURLRaw := ""
	var baseURLPtr *string
	if req.BaseURL != nil {
		var (
			normalizedBaseURL *string
			err               error
		)
		allowInternalHTTP := def.AllowsInternalHTTP
		if req.AllowInternalHTTP != nil {
			allowInternalHTTP = *req.AllowInternalHTTP
		}
		if allowInternalHTTP {
			normalizedBaseURL, err = normalizeOptionalInternalBaseURL(req.BaseURL)
		} else {
			normalizedBaseURL, err = normalizeOptionalBaseURL(req.BaseURL)
		}
		if err != nil {
			wrappedErr := wrapDeniedError(err)
			var deniedErr *deniedURLError
			if errors.As(wrappedErr, &deniedErr) {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", wrappedErr.Error(), traceID, map[string]any{"reason": deniedErr.Reason()})
				return
			}
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "base_url is invalid", traceID, nil)
			return
		}
		if normalizedBaseURL != nil {
			baseURLRaw = *normalizedBaseURL
			baseURLPtr = normalizedBaseURL
		}
		if def.RequiresBaseURL && baseURLRaw == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "base_url is required", traceID, nil)
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
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context())

	txProviders := toolProvidersRepo.WithTx(tx)

	var (
		secretID  *uuid.UUID
		keyPrefix *string
	)
	if apiKey != "" {
		if secretsRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "secrets not configured", traceID, nil)
			return
		}
		var (
			secret data.Secret
			err    error
		)
		if ownerKind == "platform" {
			secret, err = secretsRepo.WithTx(tx).UpsertPlatform(r.Context(), secretName, apiKey)
		} else {
			secret, err = secretsRepo.WithTx(tx).Upsert(r.Context(), *ownerUserID, secretName, apiKey)
		}
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		id := secret.ID
		secretID = &id
		prefix := computeKeyPrefix(apiKey)
		keyPrefix = &prefix
	}

	if _, err := txProviders.UpsertConfig(r.Context(), ownerKind, ownerUserID, groupName, providerName, secretID, keyPrefix, baseURLPtr, nil); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
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
	membershipRepo *data.AccountMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	directPool *pgxpool.Pool,
	projectRepo *data.ProjectRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if toolProvidersRepo == nil || pool == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}
	if secretsRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "secrets not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}

	ownerKind, ownerUserID, ok := resolveToolProviderScope(r.Context(), w, r, traceID, actor, projectRepo)
	if !ok {
		return
	}

	secretName := "tool_provider:" + providerName

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context())

	var delErr error
	if ownerKind == "platform" {
		delErr = secretsRepo.WithTx(tx).DeletePlatform(r.Context(), secretName)
	} else {
		delErr = secretsRepo.WithTx(tx).Delete(r.Context(), *ownerUserID, secretName)
	}
	if delErr != nil {
		var notFound data.SecretNotFoundError
		if !errors.As(delErr, &notFound) {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
	}

	if err := toolProvidersRepo.WithTx(tx).ClearCredential(r.Context(), ownerKind, ownerUserID, providerName); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	notifyPayload := "platform"
	if ownerKind != "platform" && ownerUserID != nil {
		notifyPayload = ownerUserID.String()
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
	membershipRepo *data.AccountMembershipRepository,
	toolProvidersRepo *data.ToolProviderConfigsRepository,
	pool data.DB,
	directPool *pgxpool.Pool,
	projectRepo *data.ProjectRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if toolProvidersRepo == nil || pool == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}

	ownerKind, ownerUserID, ok := resolveToolProviderScope(r.Context(), w, r, traceID, actor, projectRepo)
	if !ok {
		return
	}

	var raw json.RawMessage
	if err := httpkit.DecodeJSON(r, &raw); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid JSON body", traceID, nil)
		return
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}

	if _, err := toolProvidersRepo.UpsertConfig(r.Context(), ownerKind, ownerUserID, groupName, providerName, nil, nil, nil, raw); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	notifyPayload := "platform"
	if ownerKind != "platform" && ownerUserID != nil {
		notifyPayload = ownerUserID.String()
	}
	notifyToolProviderChanged(r.Context(), directPool, pool, notifyPayload)
	w.WriteHeader(nethttp.StatusNoContent)
}
