package llmproviders

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	sharedoutbound "arkloop/services/shared/outboundurl"

	"github.com/google/uuid"
)

const (
	openVikingProbeTimeout      = 15 * time.Second
	openVikingProbeResponseSize = 4 << 20
)

type ResolvedOpenVikingModel struct {
	Selector       string
	CredentialName string
	Provider       string
	Model          string
	APIKey         string
	APIBase        string
	ExtraHeaders   map[string]string
}

type ResolvedOpenVikingEmbedding struct {
	ResolvedOpenVikingModel
	Dimension int
}

type SelectorNotFoundError struct {
	Selector string
}

func (e SelectorNotFoundError) Error() string {
	return fmt.Sprintf("selector %q not found", e.Selector)
}

type SelectorAmbiguousError struct {
	Selector string
}

func (e SelectorAmbiguousError) Error() string {
	return fmt.Sprintf("selector %q is ambiguous", e.Selector)
}

type UnsupportedOpenVikingProviderError struct {
	Selector string
	Provider string
	Message  string
}

func (e UnsupportedOpenVikingProviderError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return fmt.Sprintf("selector %q uses unsupported provider %q", e.Selector, e.Provider)
}

func (s *Service) ResolveOpenVikingModel(
	ctx context.Context,
	accountID uuid.UUID,
	scope string,
	userID *uuid.UUID,
	selector string,
) (ResolvedOpenVikingModel, error) {
	if s.credentials == nil || s.routes == nil || s.secrets == nil {
		return ResolvedOpenVikingModel{}, ErrNotConfigured
	}
	provider, route, err := s.findProviderRoute(ctx, accountID, scope, userID, selector)
	if err != nil {
		return ResolvedOpenVikingModel{}, err
	}

	return buildResolvedOpenVikingModel(ctx, s.secrets, provider.Credential, route, selector)
}

func (s *Service) ResolveOpenVikingEmbedding(
	ctx context.Context,
	accountID uuid.UUID,
	scope string,
	userID *uuid.UUID,
	selector string,
	dimensionHint int,
) (ResolvedOpenVikingEmbedding, error) {
	resolved, err := s.ResolveOpenVikingModel(ctx, accountID, scope, userID, selector)
	if err != nil {
		return ResolvedOpenVikingEmbedding{}, err
	}
	if !IsSupportedOpenVikingEmbeddingBackend(resolved.Provider) {
		return ResolvedOpenVikingEmbedding{}, UnsupportedOpenVikingProviderError{
			Selector: selector,
			Provider: resolved.Provider,
			Message:  fmt.Sprintf("selector %q resolves to OpenViking backend %q, which is not supported for embedding models", selector, resolved.Provider),
		}
	}

	dimension, err := probeOpenAIEmbeddingDimension(ctx, resolved.APIBase, resolved.APIKey, resolved.Model)
	if err != nil {
		dimension = inferKnownEmbeddingDimension(resolved.Model)
	}
	if dimension <= 0 {
		dimension = dimensionHint
	}
	if dimension <= 0 {
		return ResolvedOpenVikingEmbedding{}, fmt.Errorf("unable to determine embedding dimension for %q", selector)
	}

	return ResolvedOpenVikingEmbedding{
		ResolvedOpenVikingModel: resolved,
		Dimension:               dimension,
	}, nil
}

func (s *Service) findProviderRoute(
	ctx context.Context,
	accountID uuid.UUID,
	scope string,
	userID *uuid.UUID,
	selector string,
) (Provider, data.LlmRoute, error) {
	providers, err := s.ListProviders(ctx, accountID, scope, userID)
	if err != nil {
		return Provider{}, data.LlmRoute{}, err
	}
	match, ok, err := matchProviderRouteBySelector(providers, selector)
	if err != nil {
		return Provider{}, data.LlmRoute{}, err
	}
	if !ok {
		return Provider{}, data.LlmRoute{}, SelectorNotFoundError{Selector: selector}
	}
	return match.provider, match.route, nil
}

type providerRouteMatch struct {
	provider Provider
	route    data.LlmRoute
}

func matchProviderRouteBySelector(providers []Provider, selector string) (providerRouteMatch, bool, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return providerRouteMatch{}, false, SelectorNotFoundError{Selector: selector}
	}

	credentialName, modelName, exact := splitExactSelector(selector)
	if exact {
		for _, provider := range providers {
			if !strings.EqualFold(provider.Credential.Name, credentialName) {
				continue
			}
			for _, route := range provider.Models {
				if route.Model == modelName {
					return providerRouteMatch{provider: provider, route: route}, true, nil
				}
			}
		}

		var legacy []providerRouteMatch
		for _, provider := range providers {
			if !strings.EqualFold(provider.Credential.Provider, credentialName) {
				continue
			}
			for _, route := range provider.Models {
				if route.Model == modelName {
					legacy = append(legacy, providerRouteMatch{provider: provider, route: route})
				}
			}
		}
		if len(legacy) == 1 {
			return legacy[0], true, nil
		}
		if len(legacy) > 1 {
			return providerRouteMatch{}, false, SelectorAmbiguousError{Selector: selector}
		}
		return providerRouteMatch{}, false, nil
	}

	var matches []providerRouteMatch
	for _, provider := range providers {
		for _, route := range provider.Models {
			if route.Model == selector {
				matches = append(matches, providerRouteMatch{provider: provider, route: route})
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	if len(matches) > 1 {
		return providerRouteMatch{}, false, SelectorAmbiguousError{Selector: selector}
	}
	return providerRouteMatch{}, false, nil
}

func buildResolvedOpenVikingModel(
	ctx context.Context,
	secrets *data.SecretsRepository,
	credential data.LlmCredential,
	route data.LlmRoute,
	selector string,
) (ResolvedOpenVikingModel, error) {
	backend := ResolveOpenVikingBackend(credential.Provider, credential.AdvancedJSON)
	if backend == "" {
		return ResolvedOpenVikingModel{}, UnsupportedOpenVikingProviderError{
			Selector: selector,
			Provider: credential.Provider,
			Message:  fmt.Sprintf("selector %q has no OpenViking backend mapping; configure advanced_json.openviking_backend to one of openai, azure, volcengine, openai_compatible", selector),
		}
	}
	if credential.SecretID == nil {
		return ResolvedOpenVikingModel{}, ProviderSecretMissingError{ProviderID: credential.ID}
	}

	apiKey, err := secrets.DecryptByID(ctx, *credential.SecretID)
	if err != nil {
		return ResolvedOpenVikingModel{}, err
	}
	if apiKey == nil || strings.TrimSpace(*apiKey) == "" {
		return ResolvedOpenVikingModel{}, ProviderSecretMissingError{ProviderID: credential.ID}
	}

	return ResolvedOpenVikingModel{
		Selector:       strings.TrimSpace(selector),
		CredentialName: credential.Name,
		Provider:       backend,
		Model:          route.Model,
		APIKey:         strings.TrimSpace(*apiKey),
		APIBase:        defaultModelBaseURL(credential),
		ExtraHeaders:   OpenVikingExtraHeadersFromAdvancedJSON(credential.AdvancedJSON),
	}, nil
}

func defaultModelBaseURL(credential data.LlmCredential) string {
	if credential.BaseURL != nil && strings.TrimSpace(*credential.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(*credential.BaseURL), "/")
	}
	switch strings.ToLower(strings.TrimSpace(credential.Provider)) {
	case "openai":
		return defaultOpenAIBaseURL
	case "anthropic":
		return defaultAnthropicBaseURL
	default:
		return ""
	}
}

func splitExactSelector(selector string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(selector), "^", 2)
	if len(parts) != 2 {
		return "", strings.TrimSpace(selector), false
	}
	left := strings.TrimSpace(parts[0])
	right := strings.TrimSpace(parts[1])
	if left == "" || right == "" {
		return "", strings.TrimSpace(selector), false
	}
	return left, right, true
}

func probeOpenAIEmbeddingDimension(ctx context.Context, apiBase, apiKey, model string) (int, error) {
	apiBase = strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if apiBase == "" {
		apiBase = defaultOpenAIBaseURL
	}

	endpoint := apiBase + "/embeddings"
	policy := sharedoutbound.DefaultPolicy()
	if err := policy.ValidateRequestURL(endpoint); err != nil {
		return 0, err
	}

	body, err := json.Marshal(map[string]any{
		"model": model,
		"input": "dimension probe",
	})
	if err != nil {
		return 0, err
	}

	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := policy.NewHTTPClient(openVikingProbeTimeout).Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, openVikingProbeResponseSize))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("embedding probe failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var payload struct {
		Data []struct {
			Embedding []json.RawMessage `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, err
	}
	if len(payload.Data) == 0 || len(payload.Data[0].Embedding) == 0 {
		return 0, fmt.Errorf("embedding probe returned no vector")
	}
	return len(payload.Data[0].Embedding), nil
}

func inferKnownEmbeddingDimension(model string) int {
	m := strings.ToLower(strings.TrimSpace(model))
	switch m {
	// OpenAI
	case "text-embedding-3-small", "openai/text-embedding-3-small",
		"text-embedding-ada-002", "openai/text-embedding-ada-002":
		return 1536
	case "text-embedding-3-large", "openai/text-embedding-3-large":
		return 3072
	default:
		// 通义 embedding
		if strings.Contains(m, "tongyi-embedding") || strings.HasPrefix(m, "text-embedding-v") {
			switch {
			case strings.Contains(m, "text-embedding-v1"), strings.Contains(m, "text-embedding-v2"):
				return 1536
			default:
				return 1024 // v3, v4, vision-plus 默认 1024
			}
		}
		// 火山引擎 doubao embedding
		if strings.Contains(m, "doubao-embedding") {
			return 1024
		}
		return 0
	}
}
