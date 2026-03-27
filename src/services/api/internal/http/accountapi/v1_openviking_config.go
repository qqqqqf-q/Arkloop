package accountapi

import (
	"errors"
	nethttp "net/http"
	"strings"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/llmproviders"
	"arkloop/services/api/internal/observability"
)

type resolveOpenVikingConfigRequest struct {
	VLMSelector            string `json:"vlm_selector"`
	EmbeddingSelector      string `json:"embedding_selector"`
	EmbeddingDimensionHint int    `json:"embedding_dimension_hint"`
}

type resolvedOpenVikingModelResponse struct {
	Selector       string            `json:"selector"`
	CredentialName string            `json:"credential_name"`
	Provider       string            `json:"provider"`
	Model          string            `json:"model"`
	APIBase        string            `json:"api_base"`
	APIKey         string            `json:"api_key"`
	ExtraHeaders   map[string]string `json:"extra_headers,omitempty"`
}

type resolvedOpenVikingEmbeddingResponse struct {
	resolvedOpenVikingModelResponse
	Dimension int `json:"dimension"`
}

type resolveOpenVikingConfigResponse struct {
	VLM       *resolvedOpenVikingModelResponse     `json:"vlm,omitempty"`
	Embedding *resolvedOpenVikingEmbeddingResponse `json:"embedding,omitempty"`
}

func openVikingResolveEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	llmCredentialsRepo *data.LlmCredentialsRepository,
	llmRoutesRepo *data.LlmRoutesRepository,
	secretsRepo *data.SecretsRepository,
	projectRepo *data.ProjectRepository,
	pool data.DB,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	service := llmproviders.NewService(pool, llmCredentialsRepo, llmRoutesRepo, secretsRepo, projectRepo)

	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if r.Method != nethttp.MethodPost {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
		if llmCredentialsRepo == nil || llmRoutesRepo == nil || secretsRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataSecrets, w, traceID) {
			return
		}

		var body resolveOpenVikingConfigRequest
		if err := httpkit.DecodeJSON(r, &body); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid request body", traceID, nil)
			return
		}
		body.VLMSelector = strings.TrimSpace(body.VLMSelector)
		body.EmbeddingSelector = strings.TrimSpace(body.EmbeddingSelector)
		if body.VLMSelector == "" && body.EmbeddingSelector == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "at least one selector is required", traceID, nil)
			return
		}

		var resp resolveOpenVikingConfigResponse
		if body.VLMSelector != "" {
			resolved, err := service.ResolveOpenVikingModel(r.Context(), actor.AccountID, "user", &actor.UserID, body.VLMSelector)
			if err != nil {
				writeOpenVikingResolveError(w, traceID, err)
				return
			}
			resp.VLM = &resolvedOpenVikingModelResponse{
				Selector:       resolved.Selector,
				CredentialName: resolved.CredentialName,
				Provider:       resolved.Provider,
				Model:          resolved.Model,
				APIBase:        resolved.APIBase,
				APIKey:         resolved.APIKey,
				ExtraHeaders:   resolved.ExtraHeaders,
			}
		}
		if body.EmbeddingSelector != "" {
			resolved, err := service.ResolveOpenVikingEmbedding(
				r.Context(),
				actor.AccountID,
				"user",
				&actor.UserID,
				body.EmbeddingSelector,
				body.EmbeddingDimensionHint,
			)
			if err != nil {
				writeOpenVikingResolveError(w, traceID, err)
				return
			}
			resp.Embedding = &resolvedOpenVikingEmbeddingResponse{
				resolvedOpenVikingModelResponse: resolvedOpenVikingModelResponse{
					Selector:       resolved.Selector,
					CredentialName: resolved.CredentialName,
					Provider:       resolved.Provider,
					Model:          resolved.Model,
					APIBase:        resolved.APIBase,
					APIKey:         resolved.APIKey,
					ExtraHeaders:   resolved.ExtraHeaders,
				},
				Dimension: resolved.Dimension,
			}
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
	}
}

func writeOpenVikingResolveError(w nethttp.ResponseWriter, traceID string, err error) {
	var selectorNotFound llmproviders.SelectorNotFoundError
	var selectorAmbiguous llmproviders.SelectorAmbiguousError
	var unsupported llmproviders.UnsupportedOpenVikingProviderError
	var secretMissing llmproviders.ProviderSecretMissingError

	switch {
	case errors.As(err, &selectorNotFound),
		errors.As(err, &selectorAmbiguous),
		errors.As(err, &unsupported),
		errors.As(err, &secretMissing):
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
	default:
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
	}
}
