package catalogapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/llmproviders"
	"arkloop/services/api/internal/observability"
	sharedoutbound "arkloop/services/shared/outboundurl"

	"github.com/google/uuid"
)

type createLlmProviderRequest struct {
	Scope         string         `json:"scope"`
	Name          string         `json:"name"`
	Provider      string         `json:"provider"`
	APIKey        string         `json:"api_key"`
	BaseURL       *string        `json:"base_url"`
	OpenAIAPIMode *string        `json:"openai_api_mode"`
	AdvancedJSON  map[string]any `json:"advanced_json"`
}

type createLlmProviderModelRequest struct {
	Scope               string          `json:"scope"`
	Model               string          `json:"model"`
	Priority            int             `json:"priority"`
	IsDefault           bool            `json:"is_default"`
	ShowInPicker        *bool           `json:"show_in_picker"`
	Tags                []string        `json:"tags"`
	WhenJSON            json.RawMessage `json:"when"`
	AdvancedJSON        map[string]any  `json:"advanced_json"`
	Multiplier          *float64        `json:"multiplier"`
	CostPer1kInput      *float64        `json:"cost_per_1k_input"`
	CostPer1kOutput     *float64        `json:"cost_per_1k_output"`
	CostPer1kCacheWrite *float64        `json:"cost_per_1k_cache_write"`
	CostPer1kCacheRead  *float64        `json:"cost_per_1k_cache_read"`
}

type llmProviderResponse struct {
	ID            string                     `json:"id"`
	AccountID     *string                    `json:"account_id,omitempty"`
	Scope         string                     `json:"scope"`
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
	ShowInPicker        bool            `json:"show_in_picker"`
	Tags                []string        `json:"tags"`
	WhenJSON            json.RawMessage `json:"when"`
	AdvancedJSON        map[string]any  `json:"advanced_json,omitempty"`
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
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Configured       bool     `json:"configured"`
	Type             string   `json:"type,omitempty"`
	ContextLength    *int     `json:"context_length,omitempty"`
	MaxOutputTokens  *int     `json:"max_output_tokens,omitempty"`
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
}

var validLlmProviders = map[string]bool{
	"openai":    true,
	"anthropic": true,
	"gemini":    true,
}

var validOpenAIAPIModes = map[string]bool{
	"auto":             true,
	"responses":        true,
	"chat_completions": true,
}

func validateAdvancedJSONForProvider(provider string, advancedJSON map[string]any) error {
	return llmproviders.ValidateAdvancedJSONForProvider(provider, advancedJSON)
}

func llmProvidersEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	credRepo *data.LlmCredentialsRepository,
	routeRepo *data.LlmRoutesRepository,
	secretsRepo *data.SecretsRepository,
	projectRepo *data.ProjectRepository,
	pool data.TxStarter,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	service := llmproviders.NewService(pool, credRepo, routeRepo, secretsRepo, projectRepo)
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodGet:
			listLlmProviders(w, r, traceID, authService, membershipRepo, service)
		case nethttp.MethodPost:
			createLlmProvider(w, r, traceID, authService, membershipRepo, service)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func llmProviderEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	credRepo *data.LlmCredentialsRepository,
	routeRepo *data.LlmRoutesRepository,
	secretsRepo *data.SecretsRepository,
	projectRepo *data.ProjectRepository,
	pool data.TxStarter,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	service := llmproviders.NewService(pool, credRepo, routeRepo, secretsRepo, projectRepo)
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		tail := strings.TrimPrefix(r.URL.Path, "/v1/llm-providers/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}
		parts := strings.Split(tail, "/")
		providerID, err := uuid.Parse(parts[0])
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid provider id", traceID, nil)
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
				httpkit.WriteMethodNotAllowed(w, r)
			}
		case len(parts) == 2 && parts[1] == "models":
			if r.Method != nethttp.MethodPost {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			createLlmProviderModel(w, r, traceID, providerID, authService, membershipRepo, service)
		case len(parts) == 2 && parts[1] == "available-models":
			if r.Method != nethttp.MethodGet {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			listLlmProviderAvailableModels(w, r, traceID, providerID, authService, membershipRepo, service)
		case len(parts) == 3 && parts[1] == "models":
			modelID, err := uuid.Parse(parts[2])
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid model id", traceID, nil)
				return
			}
			switch r.Method {
			case nethttp.MethodPatch:
				patchLlmProviderModel(w, r, traceID, providerID, modelID, authService, membershipRepo, service)
			case nethttp.MethodDelete:
				deleteLlmProviderModel(w, r, traceID, providerID, modelID, authService, membershipRepo, service)
			default:
				httpkit.WriteMethodNotAllowed(w, r)
			}
		default:
			httpkit.WriteNotFound(w, r)
		}
	}
}

func listLlmProviders(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	scope, ok := resolveLlmProviderScope(w, r, traceID, actor, nil)
	if !ok {
		return
	}
	providers, err := service.ListProviders(r.Context(), actor.AccountID, scope, &actor.UserID)
	if err != nil {
		writeLlmProviderServiceError(r.Context(), w, traceID, err)
		return
	}
	resp := make([]llmProviderResponse, 0, len(providers))
	for _, provider := range providers {
		resp = append(resp, toLlmProviderResponse(provider))
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func createLlmProvider(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	var req createLlmProviderRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	if err := validateCreateLlmProviderRequest(req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}
	normalizedBaseURL, err := normalizeOptionalBaseURL(req.BaseURL)
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
	scope, ok := resolveLlmProviderScope(w, r, traceID, actor, &req.Scope)
	if !ok {
		return
	}
	provider, err := service.CreateProvider(r.Context(), actor.AccountID, scope, &actor.UserID, llmproviders.CreateProviderInput{
		Provider:      strings.TrimSpace(req.Provider),
		Name:          strings.TrimSpace(req.Name),
		APIKey:        strings.TrimSpace(req.APIKey),
		BaseURL:       normalizedBaseURL,
		OpenAIAPIMode: normalizeOptionalString(req.OpenAIAPIMode),
		AdvancedJSON:  req.AdvancedJSON,
	})
	if err != nil {
		writeLlmProviderServiceError(r.Context(), w, traceID, err)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toLlmProviderResponse(provider))
}

func patchLlmProvider(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	providerID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	currentScope := "platform"
	current, err := service.GetProvider(r.Context(), actor.AccountID, providerID, currentScope, &actor.UserID)
	if err != nil {
		currentScope = "user"
		current, err = service.GetProvider(r.Context(), actor.AccountID, providerID, currentScope, &actor.UserID)
	}
	if err != nil {
		writeLlmProviderServiceError(r.Context(), w, traceID, err)
		return
	}
	body, err := decodeRawJSONMap(r)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	providerText, providerSet, err := readOptionalString(body, "provider")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "provider must be a string", traceID, nil)
		return
	}
	if providerSet && providerText == nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "provider cannot be null", traceID, nil)
		return
	}
	nameText, nameSet, err := readOptionalString(body, "name")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name must be a string", traceID, nil)
		return
	}
	if nameSet && nameText == nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name cannot be null", traceID, nil)
		return
	}
	apiKeyText, apiKeySet, err := readOptionalString(body, "api_key")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "api_key must be a string", traceID, nil)
		return
	}
	if apiKeySet && apiKeyText == nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "api_key cannot be null", traceID, nil)
		return
	}
	baseURL, baseURLSet, err := readOptionalNullableString(body, "base_url")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "base_url must be a string or null", traceID, nil)
		return
	}
	openAIAPIMode, openAIAPIModeSet, err := readOptionalNullableString(body, "openai_api_mode")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "openai_api_mode must be a string or null", traceID, nil)
		return
	}
	advancedJSON, advancedJSONSet, err := readOptionalJSONObject(body, "advanced_json")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "advanced_json must be an object or null", traceID, nil)
		return
	}
	scopeText, _, err := readOptionalString(body, "scope")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "scope must be a string", traceID, nil)
		return
	}
	if scopeText == nil {
		scopeText = &currentScope
	}
	scope, ok := resolveLlmProviderScope(w, r, traceID, actor, scopeText)
	if !ok {
		return
	}
	if scope != currentScope {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "scope change is not supported", traceID, nil)
		return
	}
	mergedProvider := current.Credential.Provider
	if providerSet {
		mergedProvider = strings.TrimSpace(*providerText)
	}
	mergedName := current.Credential.Name
	if nameSet {
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
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name is required", traceID, nil)
		return
	}
	if strings.Contains(mergedName, "^") {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name must not contain ^", traceID, nil)
		return
	}
	if !validLlmProviders[strings.TrimSpace(mergedProvider)] {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid provider", traceID, nil)
		return
	}
	if apiKeySet && strings.TrimSpace(*apiKeyText) == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "api_key must not be empty", traceID, nil)
		return
	}
	if err := validateProviderFields(strings.TrimSpace(mergedProvider), mergedMode, mergedAdvanced); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}
	normalizedBaseURL, err := normalizeOptionalBaseURL(baseURL)
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
	provider, err := service.UpdateProvider(r.Context(), actor.AccountID, providerID, scope, &actor.UserID, llmproviders.UpdateProviderInput{
		Provider:         normalizeOptionalString(providerText),
		Name:             normalizeOptionalString(nameText),
		BaseURLSet:       baseURLSet,
		BaseURL:          normalizedBaseURL,
		OpenAIAPIModeSet: openAIAPIModeSet,
		OpenAIAPIMode:    normalizeOptionalString(openAIAPIMode),
		AdvancedJSONSet:  advancedJSONSet,
		AdvancedJSON:     mergedAdvanced,
		APIKey:           normalizeOptionalString(apiKeyText),
	})
	if err != nil {
		writeLlmProviderServiceError(r.Context(), w, traceID, err)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toLlmProviderResponse(provider))
}

func deleteLlmProvider(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	providerID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	scope, ok := resolveLlmProviderScope(w, r, traceID, actor, nil)
	if !ok {
		return
	}
	if err := service.DeleteProvider(r.Context(), actor.AccountID, providerID, scope, &actor.UserID); err != nil {
		writeLlmProviderServiceError(r.Context(), w, traceID, err)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func createLlmProviderModel(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	providerID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	var req createLlmProviderModelRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	scope, ok := resolveLlmProviderScope(w, r, traceID, actor, &req.Scope)
	if !ok {
		return
	}
	provider, err := service.GetProvider(r.Context(), actor.AccountID, providerID, scope, &actor.UserID)
	if err != nil {
		writeLlmProviderServiceError(r.Context(), w, traceID, err)
		return
	}
	req.Model = llmproviders.CanonicalModelIdentifier(provider.Credential.Provider, req.Model)
	if req.Model == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "model is required", traceID, nil)
		return
	}
	if err := validateAdvancedJSONForProvider(provider.Credential.Provider, req.AdvancedJSON); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}
	whenJSON, _, err := normalizeJSONRequest(req.WhenJSON)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "when must be valid JSON", traceID, nil)
		return
	}
	showInPicker := true
	if req.ShowInPicker != nil {
		showInPicker = *req.ShowInPicker
	}
	model, err := service.CreateModel(r.Context(), actor.AccountID, providerID, scope, &actor.UserID, llmproviders.CreateModelInput{
		Model:               req.Model,
		Priority:            req.Priority,
		IsDefault:           req.IsDefault,
		ShowInPicker:        showInPicker,
		Tags:                req.Tags,
		WhenJSON:            whenJSON,
		AdvancedJSON:        req.AdvancedJSON,
		Multiplier:          req.Multiplier,
		CostPer1kInput:      req.CostPer1kInput,
		CostPer1kOutput:     req.CostPer1kOutput,
		CostPer1kCacheWrite: req.CostPer1kCacheWrite,
		CostPer1kCacheRead:  req.CostPer1kCacheRead,
	})
	if err != nil {
		writeLlmProviderServiceError(r.Context(), w, traceID, err)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toLlmProviderModelResponse(model, provider.Credential.Provider))
}

func patchLlmProviderModel(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	providerID uuid.UUID,
	modelID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	body, err := decodeRawJSONMap(r)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	modelText, modelSet, err := readOptionalString(body, "model")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "model must be a string", traceID, nil)
		return
	}
	if modelSet && modelText == nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "model cannot be null", traceID, nil)
		return
	}
	priority, prioritySet, err := readOptionalInt(body, "priority")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "priority must be an integer", traceID, nil)
		return
	}
	if prioritySet && priority == nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "priority cannot be null", traceID, nil)
		return
	}
	isDefault, isDefaultSet, err := readOptionalBool(body, "is_default")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "is_default must be a boolean", traceID, nil)
		return
	}
	if isDefaultSet && isDefault == nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "is_default cannot be null", traceID, nil)
		return
	}
	tags, tagsSet, err := readOptionalStringSlice(body, "tags")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "tags must be an array of strings", traceID, nil)
		return
	}
	whenJSON, whenJSONSet, err := readOptionalJSON(body, "when")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "when must be valid JSON", traceID, nil)
		return
	}
	advancedJSON, advancedJSONSet, err := readOptionalJSONObject(body, "advanced_json")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "advanced_json must be an object or null", traceID, nil)
		return
	}
	showInPicker, showInPickerSet, err := readOptionalBool(body, "show_in_picker")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "show_in_picker must be a boolean", traceID, nil)
		return
	}
	scopeText, _, err := readOptionalString(body, "scope")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "scope must be a string", traceID, nil)
		return
	}
	currentScope := "platform"
	provider, err := service.GetProvider(r.Context(), actor.AccountID, providerID, currentScope, &actor.UserID)
	if err != nil {
		currentScope = "user"
		provider, err = service.GetProvider(r.Context(), actor.AccountID, providerID, currentScope, &actor.UserID)
	}
	if err != nil {
		writeLlmProviderServiceError(r.Context(), w, traceID, err)
		return
	}
	if scopeText == nil {
		scopeText = &currentScope
	}
	scope, ok := resolveLlmProviderScope(w, r, traceID, actor, scopeText)
	if !ok {
		return
	}
	multiplier, multiplierSet, err := readOptionalFloat(body, "multiplier")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "multiplier must be a number", traceID, nil)
		return
	}
	costPer1kInput, costPer1kInputSet, err := readOptionalFloat(body, "cost_per_1k_input")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "cost_per_1k_input must be a number", traceID, nil)
		return
	}
	costPer1kOutput, costPer1kOutputSet, err := readOptionalFloat(body, "cost_per_1k_output")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "cost_per_1k_output must be a number", traceID, nil)
		return
	}
	costPer1kCacheWrite, costPer1kCacheWriteSet, err := readOptionalFloat(body, "cost_per_1k_cache_write")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "cost_per_1k_cache_write must be a number", traceID, nil)
		return
	}
	costPer1kCacheRead, costPer1kCacheReadSet, err := readOptionalFloat(body, "cost_per_1k_cache_read")
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "cost_per_1k_cache_read must be a number", traceID, nil)
		return
	}
	if modelSet {
		normalizedModel := llmproviders.CanonicalModelIdentifier(provider.Credential.Provider, *modelText)
		modelText = &normalizedModel
	}
	if modelSet && strings.TrimSpace(*modelText) == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "model must not be empty", traceID, nil)
		return
	}
	if scope != currentScope {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "scope change is not supported", traceID, nil)
		return
	}
	if advancedJSONSet {
		if err := validateAdvancedJSONForProvider(provider.Credential.Provider, advancedJSON); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}
	}
	model, err := service.UpdateModel(r.Context(), actor.AccountID, providerID, modelID, scope, &actor.UserID, llmproviders.UpdateModelInput{
		ModelSet:               modelSet,
		Model:                  normalizeOptionalString(modelText),
		PrioritySet:            prioritySet,
		Priority:               priority,
		IsDefaultSet:           isDefaultSet,
		IsDefault:              isDefault,
		ShowInPickerSet:        showInPickerSet,
		ShowInPicker:           showInPicker,
		TagsSet:                tagsSet,
		Tags:                   tags,
		WhenJSONSet:            whenJSONSet,
		WhenJSON:               whenJSON,
		AdvancedJSONSet:        advancedJSONSet,
		AdvancedJSON:           advancedJSON,
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
		writeLlmProviderServiceError(r.Context(), w, traceID, err)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toLlmProviderModelResponse(model, provider.Credential.Provider))
}

func deleteLlmProviderModel(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	providerID uuid.UUID,
	modelID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	scope, ok := resolveLlmProviderScope(w, r, traceID, actor, nil)
	if !ok {
		return
	}
	if err := service.DeleteModel(r.Context(), actor.AccountID, providerID, modelID, scope, &actor.UserID); err != nil {
		writeLlmProviderServiceError(r.Context(), w, traceID, err)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func listLlmProviderAvailableModels(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	providerID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	service *llmproviders.Service,
) {
	actor, ok := authenticateLLMProviderActor(w, r, traceID, authService, membershipRepo)
	if !ok {
		return
	}
	scope, ok := resolveLlmProviderScope(w, r, traceID, actor, nil)
	if !ok {
		return
	}
	models, err := service.ListAvailableModels(r.Context(), actor.AccountID, providerID, scope, &actor.UserID)
	if err != nil {
		writeLlmProviderServiceError(r.Context(), w, traceID, err)
		return
	}
	resp := llmProviderAvailableModelsResponse{Models: make([]llmProviderAvailableModel, 0, len(models))}
	for _, model := range models {
		resp.Models = append(resp.Models, llmProviderAvailableModel{
			ID:               model.ID,
			Name:             model.Name,
			Configured:       model.Configured,
			Type:             model.Type,
			ContextLength:    model.ContextLength,
			MaxOutputTokens:  model.MaxOutputTokens,
			InputModalities:  model.InputModalities,
			OutputModalities: model.OutputModalities,
		})
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func authenticateLLMProviderActor(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
) (*httpkit.Actor, bool) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return nil, false
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return nil, false
	}
	return actor, true
}

func resolveLlmProviderScope(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *httpkit.Actor,
	bodyScope *string,
) (string, bool) {
	if strings.TrimSpace(r.URL.Query().Get("project_id")) != "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "project_id is not supported", traceID, nil)
		return "", false
	}

	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" && bodyScope != nil {
		scope = strings.TrimSpace(*bodyScope)
	}
	if scope == "" {
		scope = "platform"
	}
	if scope != "user" && scope != "project" && scope != "platform" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "scope must be user or platform", traceID, nil)
		return "", false
	}
	if scope == "project" {
		scope = "user"
	}
	if scope == "platform" {
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return "", false
		}
		return scope, true
	}
	if !httpkit.RequirePerm(actor, auth.PermDataSecrets, w, traceID) {
		return "", false
	}
	return scope, true
}

func validateCreateLlmProviderRequest(req createLlmProviderRequest) error {
	name := strings.TrimSpace(req.Name)
	provider := strings.TrimSpace(req.Provider)
	apiKey := strings.TrimSpace(req.APIKey)
	scope := strings.TrimSpace(req.Scope)
	if name == "" || provider == "" || apiKey == "" {
		return errors.New("name, provider and api_key are required")
	}
	if scope != "" && scope != "user" && scope != "project" && scope != "platform" {
		return errors.New("scope must be user or platform")
	}
	if strings.Contains(name, "^") {
		return errors.New("name must not contain ^")
	}
	if !validLlmProviders[provider] {
		return errors.New("invalid provider")
	}
	if _, err := normalizeOptionalBaseURL(req.BaseURL); err != nil {
		wrappedErr := wrapDeniedError(err)
		var deniedErr *deniedURLError
		if errors.As(wrappedErr, &deniedErr) {
			return errors.New(wrappedErr.Error())
		}
		return errors.New("base_url is invalid")
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

func writeLlmProviderServiceError(ctx context.Context, w nethttp.ResponseWriter, traceID string, err error) {
	if err == nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if errors.Is(err, llmproviders.ErrNotConfigured) {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}
	var providerNotFound llmproviders.ProviderNotFoundError
	if errors.As(err, &providerNotFound) {
		httpkit.WriteError(w, nethttp.StatusNotFound, "llm_providers.not_found", "provider not found", traceID, nil)
		return
	}
	var modelNotFound llmproviders.ModelNotFoundError
	if errors.As(err, &modelNotFound) {
		httpkit.WriteError(w, nethttp.StatusNotFound, "llm_provider_models.not_found", "model not found", traceID, nil)
		return
	}
	var nameConflict data.LlmCredentialNameConflictError
	if errors.As(err, &nameConflict) {
		httpkit.WriteError(w, nethttp.StatusConflict, "llm_providers.name_conflict", "provider name already exists", traceID, nil)
		return
	}
	var modelConflict data.LlmRouteModelConflictError
	if errors.As(err, &modelConflict) {
		httpkit.WriteError(w, nethttp.StatusConflict, "llm_provider_models.model_conflict", "model already exists", traceID, nil)
		return
	}
	var secretMissing llmproviders.ProviderSecretMissingError
	if errors.As(err, &secretMissing) {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "llm_providers.api_key_missing", "provider api key is missing", traceID, nil)
		return
	}
	var upstreamErr *llmproviders.UpstreamListModelsError
	if errors.As(err, &upstreamErr) {
		details := buildLlmUpstreamErrorDetails(upstreamErr)
		switch upstreamErr.Kind {
		case "auth":
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "llm_providers.upstream_auth_failed", "provider authentication failed", traceID, details)
		case "request", "unsupported_provider":
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "llm_providers.upstream_request_failed", "provider request failed", traceID, details)
		default:
			httpkit.WriteError(w, nethttp.StatusBadGateway, "llm_providers.upstream_error", "provider upstream error", traceID, details)
		}
		return
	}
	slog.ErrorContext(ctx, "unhandled llm provider error", "err", err, "trace_id", traceID)
	httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
}

func buildLlmUpstreamErrorDetails(err *llmproviders.UpstreamListModelsError) map[string]any {
	if err == nil {
		return nil
	}

	details := map[string]any{
		"kind": err.Kind,
	}
	if err.StatusCode > 0 {
		details["status_code"] = err.StatusCode
	}
	if err.Err != nil {
		details["raw_error"] = err.Err.Error()
	}

	var denied sharedoutbound.DeniedError
	if errors.As(err.Err, &denied) {
		details["denied_reason"] = denied.Reason
		if len(denied.Details) > 0 {
			details["denied_details"] = denied.Details
		}
	}

	return details
}

func toLlmProviderResponse(provider llmproviders.Provider) llmProviderResponse {
	models := make([]llmProviderModelResponse, 0, len(provider.Models))
	for _, model := range provider.Models {
		models = append(models, toLlmProviderModelResponse(model, provider.Credential.Provider))
	}
	var accountID *string
	if provider.Credential.OwnerUserID != nil {
		value := provider.Credential.OwnerUserID.String()
		accountID = &value
	}
	scope := provider.Credential.OwnerKind
	return llmProviderResponse{
		ID:            provider.Credential.ID.String(),
		AccountID:     accountID,
		Scope:         scope,
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

func toLlmProviderModelResponse(model data.LlmRoute, providerName string) llmProviderModelResponse {
	whenJSON := model.WhenJSON
	if len(whenJSON) == 0 {
		whenJSON = json.RawMessage("{}")
	}
	return llmProviderModelResponse{
		ID:                  model.ID.String(),
		ProviderID:          model.CredentialID.String(),
		Model:               llmproviders.CanonicalModelIdentifier(providerName, model.Model),
		Priority:            model.Priority,
		IsDefault:           model.IsDefault,
		ShowInPicker:        model.ShowInPicker,
		Tags:                model.Tags,
		WhenJSON:            whenJSON,
		AdvancedJSON:        model.AdvancedJSON,
		Multiplier:          model.Multiplier,
		CostPer1kInput:      model.CostPer1kInput,
		CostPer1kOutput:     model.CostPer1kOutput,
		CostPer1kCacheWrite: model.CostPer1kCacheWrite,
		CostPer1kCacheRead:  model.CostPer1kCacheRead,
	}
}

func decodeRawJSONMap(r *nethttp.Request) (map[string]json.RawMessage, error) {
	var body map[string]json.RawMessage
	if err := httpkit.DecodeJSON(r, &body); err != nil {
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
