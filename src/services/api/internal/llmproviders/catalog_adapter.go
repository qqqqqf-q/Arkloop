package llmproviders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"strconv"
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
	models, err := listOpenAIModels(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if isOpenRouterCatalogBaseURL(cfg.BaseURL) {
		embeddingModels, err := listOpenRouterEmbeddingModels(ctx, cfg)
		if err != nil {
			return nil, err
		}
		models = mergeAvailableModels(models, embeddingModels)
	}
	sortAvailableModels(models)
	return models, nil
}

func listOpenAIModels(ctx context.Context, cfg CatalogProtocolConfig) ([]AvailableModel, error) {
	body, status, err := fetchCatalogJSON(ctx, strings.TrimRight(cfg.BaseURL, "/")+"/models", func(req *nethttp.Request) {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		req.Header.Set("Accept", "application/json")
	})
	if err != nil {
		return nil, err
	}

	var payload struct {
		Data []openAIModelEntry `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &UpstreamListModelsError{Kind: "invalid_response", StatusCode: status, Err: err}
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
		am.ToolCalling, am.Reasoning, am.DefaultTemperature = inferModelCapabilities(id, am.Type, item.SupportedParameters, item.DefaultParameters)
		models = append(models, am)
	}
	return models, nil
}

func listOpenRouterEmbeddingModels(ctx context.Context, cfg CatalogProtocolConfig) ([]AvailableModel, error) {
	body, status, err := fetchCatalogJSON(ctx, strings.TrimRight(cfg.BaseURL, "/")+"/embeddings/models", func(req *nethttp.Request) {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		req.Header.Set("Accept", "application/json")
	})
	if err != nil {
		return nil, err
	}

	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Context int    `json:"context_length"`
			TopProv struct {
				MaxCompletionTokens int `json:"max_completion_tokens"`
			} `json:"top_provider"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &UpstreamListModelsError{Kind: "invalid_response", StatusCode: status, Err: err}
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
		model := AvailableModel{
			ID:               id,
			Name:             name,
			Type:             "embedding",
			InputModalities:  []string{"text"},
			OutputModalities: []string{"embedding"},
		}
		if item.Context > 0 {
			cl := item.Context
			model.ContextLength = &cl
		}
		if item.TopProv.MaxCompletionTokens > 0 {
			mot := item.TopProv.MaxCompletionTokens
			model.MaxOutputTokens = &mot
		}
		models = append(models, model)
	}
	return models, nil
}

func fetchCatalogJSON(ctx context.Context, url string, decorate func(*nethttp.Request)) ([]byte, int, error) {
	if err := sharedoutbound.DefaultPolicy().ValidateRequestURL(url); err != nil {
		return nil, 0, &UpstreamListModelsError{Kind: "request", Err: err}
	}

	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, url, nil)
	if err != nil {
		return nil, 0, &UpstreamListModelsError{Kind: "request", Err: err}
	}
	if decorate != nil {
		decorate(req)
	}

	resp, err := sharedoutbound.DefaultPolicy().NewHTTPClient(availableModelsTimeout).Do(req)
	if err != nil {
		return nil, 0, upstreamNetworkError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, availableModelsRespBytes))
	if err != nil {
		return nil, resp.StatusCode, &UpstreamListModelsError{Kind: "network", Err: err}
	}
	if err := classifyCatalogStatus(resp.StatusCode, body); err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func mergeAvailableModels(base []AvailableModel, extra []AvailableModel) []AvailableModel {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]int, len(base))
	for idx, model := range base {
		seen[strings.ToLower(model.ID)] = idx
	}
	for _, model := range extra {
		key := strings.ToLower(model.ID)
		if idx, ok := seen[key]; ok {
			if base[idx].Type == "" || base[idx].Type == "chat" {
				base[idx] = model
			}
			continue
		}
		seen[key] = len(base)
		base = append(base, model)
	}
	return base
}

func isOpenRouterCatalogBaseURL(baseURL string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(baseURL)), "openrouter.ai")
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
			ID             string `json:"id"`
			DisplayName    string `json:"display_name"`
			Name           string `json:"name"`
			Type           string `json:"type"`
			MaxInputTokens int    `json:"max_input_tokens"`
			MaxTokens      int    `json:"max_tokens"`
			Capabilities   *struct {
				Thinking struct {
					Supported bool `json:"supported"`
				} `json:"thinking"`
				ImageInput struct {
					Supported bool `json:"supported"`
				} `json:"image_input"`
			} `json:"capabilities"`
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
		am := AvailableModel{
			ID:               id,
			Name:             name,
			Type:             "chat",
			InputModalities:  []string{"text"},
			OutputModalities: []string{"text"},
		}
		if item.MaxInputTokens > 0 {
			cl := item.MaxInputTokens
			am.ContextLength = &cl
		}
		if item.MaxTokens > 0 {
			mot := item.MaxTokens
			am.MaxOutputTokens = &mot
		}
		if strings.Contains(strings.ToLower(id), "embed") {
			am.Type = "embedding"
			am.InputModalities = []string{"text"}
			am.OutputModalities = []string{"embedding"}
		} else {
			toolCalling := true
			am.ToolCalling = &toolCalling
			if item.Capabilities != nil {
				if item.Capabilities.Thinking.Supported || modelLooksReasoningCapable(id) {
					reasoning := true
					am.Reasoning = &reasoning
				}
				if item.Capabilities.ImageInput.Supported {
					am.InputModalities = []string{"text", "image"}
				}
			} else if modelLooksReasoningCapable(id) {
				reasoning := true
				am.Reasoning = &reasoning
			}
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
			toolCalling := true
			am.ToolCalling = &toolCalling
			am.InputModalities = []string{"text"}
			am.OutputModalities = []string{"text"}
			if modelLooksReasoningCapable(id) {
				reasoning := true
				am.Reasoning = &reasoning
			}
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

var reasoningPrefixes = []string{
	"o1",
	"o3",
	"o4",
	"gpt-5",
	"claude-3.7",
	"claude-3-7",
	"claude-sonnet-4",
	"claude-opus-4",
	"gemini-2.5",
}

func inferModelCapabilities(modelID string, modelType string, supportedParameters []string, defaultParameters map[string]any) (*bool, *bool, *float64) {
	lowerID := strings.ToLower(strings.TrimSpace(modelID))
	if lowerID == "" {
		return nil, nil, nil
	}

	paramSet := make(map[string]struct{}, len(supportedParameters))
	for _, param := range supportedParameters {
		cleaned := strings.ToLower(strings.TrimSpace(param))
		if cleaned == "" {
			continue
		}
		paramSet[cleaned] = struct{}{}
	}

	var toolCalling *bool
	if strings.EqualFold(strings.TrimSpace(modelType), "chat") {
		if _, ok := paramSet["tools"]; ok {
			value := true
			toolCalling = &value
		}
		if _, ok := paramSet["tool_choice"]; ok {
			value := true
			toolCalling = &value
		}
	}

	var reasoning *bool
	if _, ok := paramSet["reasoning"]; ok {
		value := true
		reasoning = &value
	}
	if _, ok := paramSet["reasoning_effort"]; ok {
		value := true
		reasoning = &value
	}
	if _, ok := paramSet["include_reasoning"]; ok {
		value := true
		reasoning = &value
	}

	if reasoning == nil && modelLooksReasoningCapable(modelID) {
		value := true
		reasoning = &value
	}

	var defaultTemperature *float64
	if defaultParameters != nil {
		if value, ok := normalizedCatalogFloat(defaultParameters["temperature"]); ok {
			defaultTemperature = &value
		}
	}

	return toolCalling, reasoning, defaultTemperature
}

func modelLooksReasoningCapable(modelID string) bool {
	lowerID := strings.ToLower(strings.TrimSpace(modelID))
	if lowerID == "" {
		return false
	}
	for _, prefix := range reasoningPrefixes {
		if strings.HasPrefix(lowerID, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

func normalizedCatalogFloat(raw any) (float64, bool) {
	switch value := raw.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		if err == nil {
			return parsed, true
		}
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
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
