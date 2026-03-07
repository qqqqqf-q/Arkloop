package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/llmproviders"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type createLlmProviderRequest struct {
	Name          string         `json:"name"`
	Provider      string         `json:"provider"`
	APIKey        string         `json:"api_key"`
	BaseURL       *string        `json:"base_url"`
	OpenAIAPIMode *string        `json:"openai_api_mode"`
	AdvancedJSON  map[string]any `json:"advanced_json"`
}

type createLlmProviderModelRequest struct {
	Model               string          `json:"model"`
	Priority            int             `json:"priority"`
	IsDefault           bool            `json:"is_default"`
	Tags                []string        `json:"tags"`
	WhenJSON            json.RawMessage `json:"when"`
	Multiplier          *float64        `json:"multiplier"`
	CostPer1kInput      *float64        `json:"cost_per_1k_input"`
	CostPer1kOutput     *float64        `json:"cost_per_1k_output"`
	CostPer1kCacheWrite *float64        `json:"cost_per_1k_cache_write"`
	CostPer1kCacheRead  *float64        `json:"cost_per_1k_cache_read"`
}

type llmProviderResponse struct {
	ID            string                     `json:"id"`
	OrgID         string                     `json:"org_id"`
	Provider      string                     `json:"provider"`
	Name          string                     `json:"name"`
	KeyPrefix     *string                    `json:"key_prefix"`
	BaseURL       *string                    `json:"base_url"`
	OpenAIAPIMode *string                    `json:"openai_api_mode"`
	AdvancedJSON  map[string]any             `json:"advanced_json,omitempty"`
	CreatedAt     string                     `json:"created_at"`
	Models        []llmProviderModelResponse `json:"models"`
}

type llmProviderModelResponse struct {
	ID                  string          `json:"id"`
	ProviderID          string          `json:"provider_id"`
	Model               string          `json:"model"`
	Priority            int             `json:"priority"`
	IsDefault           bool            `json:"is_default"`
	Tags                []string        `json:"tags"`
	WhenJSON            json.RawMessage `json:"when"`
	Multiplier          float64         `json:"multiplier"`
	CostPer1kInput      *float64        `json:"cost_per_1k_input,omitempty"`
	CostPer1kOutput     *float64        `json:"cost_per_1k_output,omitempty"`
	CostPer1kCacheWrite *float64        `json:"cost_per_1k_cache_write,omitempty"`
	CostPer1kCacheRead  *float64        `json:"cost_per_1k_cache_read,omitempty"`
}

type llmProviderAvailableModelsResponse struct {
	Models []llmProviderAvailableModel `json:"models"`
}

type llmProviderAvailableModel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
}

var validLlmProviders = map[string]bool{
	"openai":    true,
	"anthropic": true,
}

var validOpenAIAPIModes = map[string]bool{
	"auto":             true,
	"responses":        true,
	"chat_completions": true,
}

const (
	anthropicAdvancedVersionKey      = "anthropic_version"
	anthropicAdvancedExtraHeadersKey = "extra_headers"
	anthropicBetaHeaderName          = "anthropic-beta"
)

func validateAdvancedJSONForProvider(provider string, advancedJSON map[string]any) error {
	if strings.TrimSpace(provider) != "anthropic" || advancedJSON == nil {
		return nil
	}
	return validateAnthropicAdvancedJSON(advancedJSON)
}

func validateAnthropicAdvancedJSON(advancedJSON map[string]any) error {
	if advancedJSON == nil {
		return nil
	}
	if rawVersion, ok := advancedJSON[anthropicAdvancedVersionKey]; ok {
		version, ok := rawVersion.(string)
		if !ok || strings.TrimSpace(version) == "" {
			return errors.New("advanced_json.anthropic_version must be a non-empty string")
		}
	}

	rawHeaders, ok := advancedJSON[anthropicAdvancedExtraHeadersKey]
	if !ok {
		return nil
	}
	headers, ok := rawHeaders.(map[string]any)
	if !ok {
		return errors.New("advanced_json.extra_headers must be an object")
	}
	for key, value := range headers {
		headerName := strings.ToLower(strings.TrimSpace(key))
		if headerName != anthropicBetaHeaderName {
			return errors.New("advanced_json.extra_headers only supports anthropic-beta")
		}
		headerValue, ok := value.(string)
		if !ok || strings.TrimSpace(headerValue) == "" {
			return errors.New("advanced_json.extra_headers.anthropic-beta must be a non-empty string")
		}
	}
	return nil
}

func llmProvidersEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	credRepo *data.LlmCredentialsRepository,
	routeRepo *data.LlmRoutesRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	service := llmproviders.NewService(pool, credRepo, routeRepo, secretsRepo)
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodGet:
			listLlmProviders(w, r, traceID, authService, membershipRepo, service)
		case nethttp.MethodPost:
			createLlmProvider(w, r, traceID, authService, membershipRepo, service)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func llmProviderEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	credRepo *data.LlmCredentialsRepository,
	routeRepo *data.LlmRoutesRepository,
	secretsRepo *data.SecretsRepository,
	pool *pgxpool.Pool,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	service := llmproviders.NewService(pool, credRepo, routeRepo, secretsRepo)
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		tail := strings.TrimPrefix(r.URL.Path, "/v1/llm-providers/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}
		parts := strings.Split(tail, "/")
		providerID, err := uuid.Parse(parts[0])
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid provider id", traceID, nil)
			return
		}

		switch {
		case len(parts) == 1:
			switch r.Method {
			case nethttp.MethodPatch:
				patchLlmProvider(w, r, traceID, providerID, authService, membershipRepo, service)
			case nethttp.MethodDelete:
				deleteLlmProvider(w, r, traceID, providerID, authService, membershipRepo, service)
			default:
				writeMethodNotAllowed(w, r)
			}
		case len(parts) == 2 && parts[1] == "models":
			if r.Method != nethttp.MethodPost {
				writeMethodNotAllowed(w, r)
				return
			}
			createLlmProviderModel(w, r, traceID, providerID, authService, membershipRepo, service)
		case len(parts) == 2 && parts[1] == "available-models":
			if r.Method != nethttp.MethodGet {
				writeMethodNotAllowed(w, r)
				return
			}
			listLlmProviderAvailableModels(w, r, traceID, providerID, authService, membershipRepo, service)
		case len(parts) == 3 && parts[1] == "models":
			modelID, err := uuid.Parse(parts[2])
			if err != nil {
				WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid model id", traceID, nil)
				return
			}
			switch r.Method {
			case nethttp.MethodPatch:
				patchLlmProviderModel(w, r, traceID, providerID, modelID, authService, membershipRepo, service)
			case nethttp.MethodDelete:
				deleteLlmProviderModel(w, r, traceID, providerID, modelID, authService, membershipRepo, service)
			default:
				writeMethodNotAllowed(w, r)
			}
		default:
			writeNotFound(w, r)
		}
	}
}

func listLlmProviders(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	providers, err := service.ListProviders(r.Context(), actor.OrgID)
	if err != nil {
		writeLlmProviderServiceError(w, traceID, err)
		return
	}
	resp := make([]llmProviderResponse, 0, len(providers))
	for _, provider := range providers {
		resp = append(resp, toLlmProviderResponse(provider))
	}
	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func createLlmProvider(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	var req createLlmProviderRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	if err := validateCreateLlmProviderRequest(req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}
	provider, err := service.CreateProvider(r.Context(), actor.OrgID, llmproviders.CreateProviderInput{
		Provider:      strings.TrimSpace(req.Provider),
		Name:          strings.TrimSpace(req.Name),
		APIKey:        strings.TrimSpace(req.APIKey),
		BaseURL:       req.BaseURL,
		OpenAIAPIMode: normalizeOptionalString(req.OpenAIAPIMode),
		AdvancedJSON:  req.AdvancedJSON,
	})
	if err != nil {
		writeLlmProviderServiceError(w, traceID, err)
		return
	}
	writeJSON(w, traceID, nethttp.StatusCreated, toLlmProviderResponse(provider))
}

func patchLlmProvider(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	providerID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	current, err := service.GetProvider(r.Context(), actor.OrgID, providerID)
	if err != nil {
		writeLlmProviderServiceError(w, traceID, err)
		return
	}
	body, err := decodeRawJSONMap(r)
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	providerText, providerSet, err := readOptionalString(body, "provider")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "provider must be a string", traceID, nil)
		return
	}
	nameText, nameSet, err := readOptionalString(body, "name")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name must be a string", traceID, nil)
		return
	}
	apiKeyText, apiKeySet, err := readOptionalString(body, "api_key")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "api_key must be a string", traceID, nil)
		return
	}
	baseURL, baseURLSet, err := readOptionalNullableString(body, "base_url")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "base_url must be a string or null", traceID, nil)
		return
	}
	openAIAPIMode, openAIAPIModeSet, err := readOptionalNullableString(body, "openai_api_mode")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "openai_api_mode must be a string or null", traceID, nil)
		return
	}
	advancedJSON, advancedJSONSet, err := readOptionalJSONObject(body, "advanced_json")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "advanced_json must be an object or null", traceID, nil)
		return
	}
	mergedProvider := current.Credential.Provider
	if providerSet && providerText != nil {
		mergedProvider = strings.TrimSpace(*providerText)
	}
	mergedName := current.Credential.Name
	if nameSet && nameText != nil {
		mergedName = strings.TrimSpace(*nameText)
	}
	mergedMode := current.Credential.OpenAIAPIMode
	if openAIAPIModeSet {
		mergedMode = normalizeOptionalString(openAIAPIMode)
	}
	mergedAdvanced := current.Credential.AdvancedJSON
	if advancedJSONSet {
		mergedAdvanced = advancedJSON
	}
	if strings.TrimSpace(mergedName) == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name is required", traceID, nil)
		return
	}
	if !validLlmProviders[strings.TrimSpace(mergedProvider)] {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid provider", traceID, nil)
		return
	}
	if apiKeySet && apiKeyText != nil && strings.TrimSpace(*apiKeyText) == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "api_key must not be empty", traceID, nil)
		return
	}
	if err := validateProviderFields(strings.TrimSpace(mergedProvider), mergedMode, mergedAdvanced); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}
	provider, err := service.UpdateProvider(r.Context(), actor.OrgID, providerID, llmproviders.UpdateProviderInput{
		Provider:         normalizeOptionalString(providerText),
		Name:             normalizeOptionalString(nameText),
		BaseURLSet:       baseURLSet,
		BaseURL:          normalizeOptionalString(baseURL),
		OpenAIAPIModeSet: openAIAPIModeSet,
		OpenAIAPIMode:    normalizeOptionalString(openAIAPIMode),
		AdvancedJSONSet:  advancedJSONSet,
		AdvancedJSON:     mergedAdvanced,
		APIKey:           normalizeOptionalString(apiKeyText),
	})
	if err != nil {
		writeLlmProviderServiceError(w, traceID, err)
		return
	}
	writeJSON(w, traceID, nethttp.StatusOK, toLlmProviderResponse(provider))
}

func deleteLlmProvider(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	providerID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if err := service.DeleteProvider(r.Context(), actor.OrgID, providerID); err != nil {
		writeLlmProviderServiceError(w, traceID, err)
		return
	}
	writeJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func createLlmProviderModel(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	providerID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	var req createLlmProviderModelRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "model is required", traceID, nil)
		return
	}
	whenJSON, _, err := normalizeJSONRequest(req.WhenJSON)
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "when must be valid JSON", traceID, nil)
		return
	}
	model, err := service.CreateModel(r.Context(), actor.OrgID, providerID, llmproviders.CreateModelInput{
		Model:               req.Model,
		Priority:            req.Priority,
		IsDefault:           req.IsDefault,
		Tags:                req.Tags,
		WhenJSON:            whenJSON,
		Multiplier:          req.Multiplier,
		CostPer1kInput:      req.CostPer1kInput,
		CostPer1kOutput:     req.CostPer1kOutput,
		CostPer1kCacheWrite: req.CostPer1kCacheWrite,
		CostPer1kCacheRead:  req.CostPer1kCacheRead,
	})
	if err != nil {
		writeLlmProviderServiceError(w, traceID, err)
		return
	}
	writeJSON(w, traceID, nethttp.StatusCreated, toLlmProviderModelResponse(model))
}

func patchLlmProviderModel(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	providerID uuid.UUID,
	modelID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	body, err := decodeRawJSONMap(r)
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	modelText, modelSet, err := readOptionalString(body, "model")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "model must be a string", traceID, nil)
		return
	}
	priority, prioritySet, err := readOptionalInt(body, "priority")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "priority must be an integer", traceID, nil)
		return
	}
	isDefault, isDefaultSet, err := readOptionalBool(body, "is_default")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "is_default must be a boolean", traceID, nil)
		return
	}
	tags, tagsSet, err := readOptionalStringSlice(body, "tags")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "tags must be an array of strings", traceID, nil)
		return
	}
	whenJSON, whenJSONSet, err := readOptionalJSON(body, "when")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "when must be valid JSON", traceID, nil)
		return
	}
	multiplier, multiplierSet, err := readOptionalFloat(body, "multiplier")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "multiplier must be a number", traceID, nil)
		return
	}
	costPer1kInput, costPer1kInputSet, err := readOptionalFloat(body, "cost_per_1k_input")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "cost_per_1k_input must be a number", traceID, nil)
		return
	}
	costPer1kOutput, costPer1kOutputSet, err := readOptionalFloat(body, "cost_per_1k_output")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "cost_per_1k_output must be a number", traceID, nil)
		return
	}
	costPer1kCacheWrite, costPer1kCacheWriteSet, err := readOptionalFloat(body, "cost_per_1k_cache_write")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "cost_per_1k_cache_write must be a number", traceID, nil)
		return
	}
	costPer1kCacheRead, costPer1kCacheReadSet, err := readOptionalFloat(body, "cost_per_1k_cache_read")
	if err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "cost_per_1k_cache_read must be a number", traceID, nil)
		return
	}
	if modelSet && modelText != nil && strings.TrimSpace(*modelText) == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "model must not be empty", traceID, nil)
		return
	}
	model, err := service.UpdateModel(r.Context(), actor.OrgID, providerID, modelID, llmproviders.UpdateModelInput{
		ModelSet:               modelSet,
		Model:                  normalizeOptionalString(modelText),
		PrioritySet:            prioritySet,
		Priority:               priority,
		IsDefaultSet:           isDefaultSet,
		IsDefault:              isDefault,
		TagsSet:                tagsSet,
		Tags:                   tags,
		WhenJSONSet:            whenJSONSet,
		WhenJSON:               whenJSON,
		MultiplierSet:          multiplierSet,
		Multiplier:             multiplier,
		CostPer1kInputSet:      costPer1kInputSet,
		CostPer1kInput:         costPer1kInput,
		CostPer1kOutputSet:     costPer1kOutputSet,
		CostPer1kOutput:        costPer1kOutput,
		CostPer1kCacheWriteSet: costPer1kCacheWriteSet,
		CostPer1kCacheWrite:    costPer1kCacheWrite,
		CostPer1kCacheReadSet:  costPer1kCacheReadSet,
		CostPer1kCacheRead:     costPer1kCacheRead,
	})
	if err != nil {
		writeLlmProviderServiceError(w, traceID, err)
		return
	}
	writeJSON(w, traceID, nethttp.StatusOK, toLlmProviderModelResponse(model))
}

func deleteLlmProviderModel(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	providerID uuid.UUID,
	modelID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	if err := service.DeleteModel(r.Context(), actor.OrgID, providerID, modelID); err != nil {
		writeLlmProviderServiceError(w, traceID, err)
		return
	}
	writeJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func listLlmProviderAvailableModels(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	providerID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	models, err := service.ListAvailableModels(r.Context(), actor.OrgID, providerID)
	if err != nil {
		writeLlmProviderServiceError(w, traceID, err)
		return
	}
	resp := llmProviderAvailableModelsResponse{Models: make([]llmProviderAvailableModel, 0, len(models))}
	for _, model := range models {
		resp.Models = append(resp.Models, llmProviderAvailableModel{
			ID:         model.ID,
			Name:       model.Name,
			Configured: model.Configured,
		})
	}
	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func authenticateLLMProviderActor(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
) (*actor, bool) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return nil, false
	}
	actor, ok := authenticateActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return nil, false
	}
	if !requirePerm(actor, auth.PermDataSecrets, w, traceID) {
		return nil, false
	}
	return actor, true
}

func validateCreateLlmProviderRequest(req createLlmProviderRequest) error {
	name := strings.TrimSpace(req.Name)
	provider := strings.TrimSpace(req.Provider)
	apiKey := strings.TrimSpace(req.APIKey)
	if name == "" || provider == "" || apiKey == "" {
		return errors.New("name, provider and api_key are required")
	}
	if !validLlmProviders[provider] {
		return errors.New("invalid provider")
	}
	return validateProviderFields(provider, normalizeOptionalString(req.OpenAIAPIMode), req.AdvancedJSON)
}

func validateProviderFields(provider string, openAIAPIMode *string, advancedJSON map[string]any) error {
	provider = strings.TrimSpace(provider)
	if openAIAPIMode != nil {
		if provider != "openai" {
			return errors.New("openai_api_mode only applies to openai provider")
		}
		if !validOpenAIAPIModes[*openAIAPIMode] {
			return errors.New("invalid openai_api_mode")
		}
	}
	if provider == "openai" && openAIAPIMode != nil && strings.TrimSpace(*openAIAPIMode) == "" {
		return errors.New("invalid openai_api_mode")
	}
	return validateAdvancedJSONForProvider(provider, advancedJSON)
}

func writeLlmProviderServiceError(w nethttp.ResponseWriter, traceID string, err error) {
	if err == nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if errors.Is(err, llmproviders.ErrNotConfigured) {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}
	var providerNotFound llmproviders.ProviderNotFoundError
	if errors.As(err, &providerNotFound) {
		WriteError(w, nethttp.StatusNotFound, "llm_providers.not_found", "provider not found", traceID, nil)
		return
	}
	var modelNotFound llmproviders.ModelNotFoundError
	if errors.As(err, &modelNotFound) {
		WriteError(w, nethttp.StatusNotFound, "llm_provider_models.not_found", "model not found", traceID, nil)
		return
	}
	var nameConflict data.LlmCredentialNameConflictError
	if errors.As(err, &nameConflict) {
		WriteError(w, nethttp.StatusConflict, "llm_providers.name_conflict", "provider name already exists", traceID, nil)
		return
	}
	var modelConflict data.LlmRouteModelConflictError
	if errors.As(err, &modelConflict) {
		WriteError(w, nethttp.StatusConflict, "llm_provider_models.model_conflict", "model already exists", traceID, nil)
		return
	}
	var secretMissing llmproviders.ProviderSecretMissingError
	if errors.As(err, &secretMissing) {
		WriteError(w, nethttp.StatusUnprocessableEntity, "llm_providers.api_key_missing", "provider api key is missing", traceID, nil)
		return
	}
	var upstreamErr *llmproviders.UpstreamListModelsError
	if errors.As(err, &upstreamErr) {
		switch upstreamErr.Kind {
		case "auth":
			WriteError(w, nethttp.StatusUnprocessableEntity, "llm_providers.upstream_auth_failed", "provider authentication failed", traceID, nil)
		case "request", "unsupported_provider":
			WriteError(w, nethttp.StatusUnprocessableEntity, "llm_providers.upstream_request_failed", "provider request failed", traceID, nil)
		default:
			WriteError(w, nethttp.StatusBadGateway, "llm_providers.upstream_error", "provider upstream error", traceID, nil)
		}
		return
	}
	WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
}

func toLlmProviderResponse(provider llmproviders.Provider) llmProviderResponse {
	models := make([]llmProviderModelResponse, 0, len(provider.Models))
	for _, model := range provider.Models {
		models = append(models, toLlmProviderModelResponse(model))
	}
	return llmProviderResponse{
		ID:            provider.Credential.ID.String(),
		OrgID:         provider.Credential.OrgID.String(),
		Provider:      provider.Credential.Provider,
		Name:          provider.Credential.Name,
		KeyPrefix:     provider.Credential.KeyPrefix,
		BaseURL:       provider.Credential.BaseURL,
		OpenAIAPIMode: provider.Credential.OpenAIAPIMode,
		AdvancedJSON:  provider.Credential.AdvancedJSON,
		CreatedAt:     provider.Credential.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		Models:        models,
	}
}

func toLlmProviderModelResponse(model data.LlmRoute) llmProviderModelResponse {
	whenJSON := model.WhenJSON
	if len(whenJSON) == 0 {
		whenJSON = json.RawMessage("{}")
	}
	return llmProviderModelResponse{
		ID:                  model.ID.String(),
		ProviderID:          model.CredentialID.String(),
		Model:               model.Model,
		Priority:            model.Priority,
		IsDefault:           model.IsDefault,
		Tags:                model.Tags,
		WhenJSON:            whenJSON,
		Multiplier:          model.Multiplier,
		CostPer1kInput:      model.CostPer1kInput,
		CostPer1kOutput:     model.CostPer1kOutput,
		CostPer1kCacheWrite: model.CostPer1kCacheWrite,
		CostPer1kCacheRead:  model.CostPer1kCacheRead,
	}
}

func decodeRawJSONMap(r *nethttp.Request) (map[string]json.RawMessage, error) {
	var body map[string]json.RawMessage
	if err := decodeJSON(r, &body); err != nil {
		return nil, err
	}
	return body, nil
}

func readOptionalString(body map[string]json.RawMessage, key string) (*string, bool, error) {
	raw, ok := body[key]
	if !ok {
		return nil, false, nil
	}
	if bytes.Equal(raw, []byte("null")) {
		return nil, true, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true, err
	}
	return &value, true, nil
}

func readOptionalNullableString(body map[string]json.RawMessage, key string) (*string, bool, error) {
	return readOptionalString(body, key)
}

func readOptionalBool(body map[string]json.RawMessage, key string) (*bool, bool, error) {
	raw, ok := body[key]
	if !ok {
		return nil, false, nil
	}
	if bytes.Equal(raw, []byte("null")) {
		return nil, true, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true, err
	}
	return &value, true, nil
}

func readOptionalInt(body map[string]json.RawMessage, key string) (*int, bool, error) {
	raw, ok := body[key]
	if !ok {
		return nil, false, nil
	}
	if bytes.Equal(raw, []byte("null")) {
		return nil, true, nil
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true, err
	}
	return &value, true, nil
}

func readOptionalFloat(body map[string]json.RawMessage, key string) (*float64, bool, error) {
	raw, ok := body[key]
	if !ok {
		return nil, false, nil
	}
	if bytes.Equal(raw, []byte("null")) {
		return nil, true, nil
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true, err
	}
	return &value, true, nil
}

func readOptionalStringSlice(body map[string]json.RawMessage, key string) ([]string, bool, error) {
	raw, ok := body[key]
	if !ok {
		return nil, false, nil
	}
	if bytes.Equal(raw, []byte("null")) {
		return []string{}, true, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, true, err
	}
	return values, true, nil
}

func readOptionalJSON(body map[string]json.RawMessage, key string) (json.RawMessage, bool, error) {
	raw, ok := body[key]
	if !ok {
		return nil, false, nil
	}
	return normalizeJSONRequest(raw)
}

func readOptionalJSONObject(body map[string]json.RawMessage, key string) (map[string]any, bool, error) {
	raw, ok := body[key]
	if !ok {
		return nil, false, nil
	}
	if bytes.Equal(raw, []byte("null")) {
		return map[string]any{}, true, nil
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true, err
	}
	if value == nil {
		value = map[string]any{}
	}
	return value, true, nil
}

func normalizeJSONRequest(raw json.RawMessage) (json.RawMessage, bool, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return json.RawMessage("{}"), true, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true, err
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return nil, true, err
	}
	return normalized, true, nil
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func readOptionalJSONNumber(raw json.RawMessage) (float64, error) {
	text := strings.TrimSpace(string(raw))
	return strconv.ParseFloat(text, 64)
}
