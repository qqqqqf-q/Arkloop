package llmproviders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"strings"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"
)

const (
	availableModelsTimeout   = 15 * time.Second
	availableModelsRespBytes = 8 << 20

	defaultOpenAIBaseURL        = "https://api.openai.com/v1"
	defaultAnthropicVersion     = "2023-06-01"
	defaultGeminiCatalogBaseURL = "https://generativelanguage.googleapis.com/v1beta"
)

type CatalogAdapter interface {
	ListModels(ctx context.Context, cfg CatalogProtocolConfig) ([]AvailableModel, error)
}

func CanonicalModelIdentifier(provider string, model string) string {
	model = strings.TrimSpace(model)
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "gemini":
		return canonicalGeminiModelIdentifier(model)
	default:
		return model
	}
}

func canonicalGeminiModelIdentifier(model string) string {
	model = strings.TrimSpace(model)
	for {
		lowerModel := strings.ToLower(model)
		if !strings.HasPrefix(lowerModel, "models/") {
			return model
		}
		model = strings.TrimSpace(model[len("models/"):])
	}
}

func ListProtocolModels(ctx context.Context, cfg CatalogProtocolConfig) ([]AvailableModel, error) {
	adapter, err := catalogAdapterForProtocol(cfg.Kind)
	if err != nil {
		return nil, &UpstreamListModelsError{Kind: "request", Err: err}
	}
	return adapter.ListModels(ctx, cfg)
}

func catalogAdapterForProtocol(kind ProtocolKind) (CatalogAdapter, error) {
	switch kind {
	case ProtocolKindOpenAIChatCompletions, ProtocolKindOpenAIResponses:
		return openAICatalogAdapter{}, nil
	case ProtocolKindAnthropicMessages:
		return anthropicCatalogAdapter{}, nil
	case ProtocolKindGeminiGenerateContent:
		return geminiCatalogAdapter{}, nil
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", kind)
	}
}

type openAICatalogAdapter struct{}

func (openAICatalogAdapter) ListModels(ctx context.Context, cfg CatalogProtocolConfig) ([]AvailableModel, error) {
	modelsURL := strings.TrimRight(cfg.BaseURL, "/") + "/models"
	if err := sharedoutbound.DefaultPolicy().ValidateRequestURL(modelsURL); err != nil {
		return nil, &UpstreamListModelsError{Kind: "request", Err: err}
	}

	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, &UpstreamListModelsError{Kind: "request", Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := sharedoutbound.DefaultPolicy().NewHTTPClient(availableModelsTimeout).Do(req)
	if err != nil {
		return nil, upstreamNetworkError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, availableModelsRespBytes))
	if err != nil {
		return nil, &UpstreamListModelsError{Kind: "network", Err: err}
	}
	if err := classifyCatalogStatus(resp.StatusCode, body); err != nil {
		return nil, err
	}

	var payload struct {
		Data []openAIModelEntry `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &UpstreamListModelsError{Kind: "invalid_response", StatusCode: resp.StatusCode, Err: err}
	}
	models := make([]AvailableModel, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = id
		}

		am := AvailableModel{ID: id, Name: name}
		if item.ContextLength > 0 {
			cl := item.ContextLength
			am.ContextLength = &cl
		}
		if item.TopProvider.MaxCompletionTokens > 0 {
			mot := item.TopProvider.MaxCompletionTokens
			am.MaxOutputTokens = &mot
		}
		am.Type, am.InputModalities, am.OutputModalities = classifyOpenAIModel(id, item.Architecture)
		models = append(models, am)
	}
	sortAvailableModels(models)
	return models, nil
}

type anthropicCatalogAdapter struct{}

func (anthropicCatalogAdapter) ListModels(ctx context.Context, cfg CatalogProtocolConfig) ([]AvailableModel, error) {
	path := anthropicCatalogPath(cfg.BaseURL)
	modelsURL := strings.TrimRight(cfg.BaseURL, "/") + path
	if err := sharedoutbound.DefaultPolicy().ValidateRequestURL(modelsURL); err != nil {
		return nil, &UpstreamListModelsError{Kind: "request", Err: err}
	}

	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, &UpstreamListModelsError{Kind: "request", Err: err}
	}
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", cfg.Anthropic.Version)
	req.Header.Set("Accept", "application/json")
	for key, value := range cfg.Anthropic.ExtraHeaders {
		req.Header.Set(key, value)
	}

	resp, err := sharedoutbound.DefaultPolicy().NewHTTPClient(availableModelsTimeout).Do(req)
	if err != nil {
		return nil, upstreamNetworkError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, availableModelsRespBytes))
	if err != nil {
		return nil, &UpstreamListModelsError{Kind: "network", Err: err}
	}
	if err := classifyCatalogStatus(resp.StatusCode, body); err != nil {
		return nil, err
	}

	var payload struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Name        string `json:"name"`
			Type        string `json:"type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &UpstreamListModelsError{Kind: "invalid_response", StatusCode: resp.StatusCode, Err: err}
	}
	models := make([]AvailableModel, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(item.DisplayName)
		if name == "" {
			name = strings.TrimSpace(item.Name)
		}
		if name == "" {
			name = id
		}
		am := AvailableModel{ID: id, Name: name, Type: "chat"}
		if strings.Contains(strings.ToLower(id), "embed") {
			am.Type = "embedding"
			am.InputModalities = []string{"text"}
			am.OutputModalities = []string{"embedding"}
		}
		models = append(models, am)
	}
	sortAvailableModels(models)
	return models, nil
}

type geminiCatalogAdapter struct{}

func (geminiCatalogAdapter) ListModels(ctx context.Context, cfg CatalogProtocolConfig) ([]AvailableModel, error) {
	modelsURL := strings.TrimRight(cfg.BaseURL, "/") + "/models"
	if err := sharedoutbound.DefaultPolicy().ValidateRequestURL(modelsURL); err != nil {
		return nil, &UpstreamListModelsError{Kind: "request", Err: err}
	}

	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, &UpstreamListModelsError{Kind: "request", Err: err}
	}
	req.Header.Set("x-goog-api-key", cfg.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := sharedoutbound.DefaultPolicy().NewHTTPClient(availableModelsTimeout).Do(req)
	if err != nil {
		return nil, upstreamNetworkError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, availableModelsRespBytes))
	if err != nil {
		return nil, &UpstreamListModelsError{Kind: "network", Err: err}
	}
	if err := classifyCatalogStatus(resp.StatusCode, body); err != nil {
		return nil, err
	}

	var payload struct {
		Models []struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			Description                string   `json:"description"`
			InputTokenLimit            int      `json:"inputTokenLimit"`
			OutputTokenLimit           int      `json:"outputTokenLimit"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &UpstreamListModelsError{Kind: "invalid_response", StatusCode: resp.StatusCode, Err: err}
	}

	models := make([]AvailableModel, 0, len(payload.Models))
	for _, item := range payload.Models {
		id := CanonicalModelIdentifier("gemini", item.Name)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(item.DisplayName)
		if name == "" {
			name = id
		}

		am := AvailableModel{
			ID:   id,
			Name: name,
			Type: "chat",
		}
		if item.InputTokenLimit > 0 {
			cl := item.InputTokenLimit
			am.ContextLength = &cl
		}
		if item.OutputTokenLimit > 0 {
			mot := item.OutputTokenLimit
			am.MaxOutputTokens = &mot
		}

		if geminiModelIsEmbedding(id, item.SupportedGenerationMethods) {
			am.Type = "embedding"
			am.InputModalities = []string{"text"}
			am.OutputModalities = []string{"embedding"}
		} else {
			am.InputModalities = []string{"text"}
			am.OutputModalities = []string{"text"}
		}
		models = append(models, am)
	}

	sortAvailableModels(models)
	return models, nil
}

func geminiModelIsEmbedding(id string, methods []string) bool {
	lowerID := strings.ToLower(strings.TrimSpace(id))
	if strings.Contains(lowerID, "embed") || strings.Contains(lowerID, "embedding") {
		return true
	}
	for _, method := range methods {
		lowerMethod := strings.ToLower(strings.TrimSpace(method))
		if lowerMethod == "embedcontent" || lowerMethod == "batchembedcontents" {
			return true
		}
	}
	return false
}

func upstreamNetworkError(err error) error {
	var denied sharedoutbound.DeniedError
	if errors.As(err, &denied) {
		return &UpstreamListModelsError{Kind: "request", Err: err}
	}
	return &UpstreamListModelsError{Kind: "network", Err: err}
}

func classifyCatalogStatus(status int, body []byte) error {
	if status == nethttp.StatusUnauthorized || status == nethttp.StatusForbidden {
		return &UpstreamListModelsError{Kind: "auth", StatusCode: status, Err: fmt.Errorf("status=%d", status)}
	}
	if status >= 200 && status < 300 {
		return nil
	}
	kind := "upstream"
	if status >= 400 && status < 500 {
		kind = "request"
	}
	return &UpstreamListModelsError{
		Kind:       kind,
		StatusCode: status,
		Err:        fmt.Errorf("status=%d body=%s", status, strings.TrimSpace(string(body))),
	}
}
