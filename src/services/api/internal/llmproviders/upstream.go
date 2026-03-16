package llmproviders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"sort"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	sharedoutbound "arkloop/services/shared/outboundurl"
)

const (
	defaultOpenAIBaseURL        = "https://api.openai.com/v1"
	defaultAnthropicBaseURL     = "https://api.anthropic.com/v1"
	defaultAnthropicVersion     = "2023-06-01"
	availableModelsTimeout      = 15 * time.Second
	availableModelsResponseSize = 8 << 20
)

type UpstreamListModelsError struct {
	Kind       string
	StatusCode int
	Err        error
}

func (e *UpstreamListModelsError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return fmt.Sprintf("list upstream models failed: %s", e.Kind)
	}
	return fmt.Sprintf("list upstream models failed: %s: %v", e.Kind, e.Err)
}

func (e *UpstreamListModelsError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func listUpstreamModels(ctx context.Context, provider data.LlmCredential, apiKey string) ([]AvailableModel, error) {
	switch strings.ToLower(provider.Provider) {
	case "openai":
		return listOpenAIModels(ctx, provider, apiKey)
	case "anthropic":
		return listAnthropicModels(ctx, provider, apiKey)
	default:
		return nil, &UpstreamListModelsError{Kind: "unsupported_provider", Err: fmt.Errorf("unsupported provider: %s", provider.Provider)}
	}
}

func listOpenAIModels(ctx context.Context, provider data.LlmCredential, apiKey string) ([]AvailableModel, error) {
	baseURL := defaultOpenAIBaseURL
	if provider.BaseURL != nil && strings.TrimSpace(*provider.BaseURL) != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(*provider.BaseURL), "/")
	}
	modelsURL := baseURL + "/models"
	if err := sharedoutbound.DefaultPolicy().ValidateRequestURL(modelsURL); err != nil {
		return nil, &UpstreamListModelsError{Kind: "request", Err: err}
	}
	if baseURL != defaultOpenAIBaseURL {
		modelsURL += "?output_modalities=all"
	}
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, &UpstreamListModelsError{Kind: "request", Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := sharedoutbound.DefaultPolicy().NewHTTPClient(availableModelsTimeout).Do(req)
	if err != nil {
		var denied sharedoutbound.DeniedError
		if errors.As(err, &denied) {
			return nil, &UpstreamListModelsError{Kind: "request", Err: err}
		}
		return nil, &UpstreamListModelsError{Kind: "network", Err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, availableModelsResponseSize))
	if err != nil {
		return nil, &UpstreamListModelsError{Kind: "network", Err: err}
	}
	if resp.StatusCode == nethttp.StatusUnauthorized || resp.StatusCode == nethttp.StatusForbidden {
		return nil, &UpstreamListModelsError{Kind: "auth", StatusCode: resp.StatusCode, Err: fmt.Errorf("status=%d", resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		kind := "upstream"
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			kind = "request"
		}
		return nil, &UpstreamListModelsError{Kind: kind, StatusCode: resp.StatusCode, Err: fmt.Errorf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))}
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

type openAIModelEntry struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	ContextLength int                  `json:"context_length"`
	Architecture  openAIModelArchEntry `json:"architecture"`
	TopProvider   struct {
		MaxCompletionTokens int `json:"max_completion_tokens"`
	} `json:"top_provider"`
}

type openAIModelArchEntry struct {
	Modality         string   `json:"modality"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

func classifyOpenAIModel(modelID string, arch openAIModelArchEntry) (modelType string, inputMods []string, outputMods []string) {
	lower := strings.ToLower(modelID)

	if len(arch.OutputModalities) > 0 {
		inputMods = arch.InputModalities
		outputMods = arch.OutputModalities
		for _, om := range outputMods {
			omLower := strings.ToLower(om)
			if omLower == "embedding" || omLower == "embeddings" {
				return "embedding", inputMods, outputMods
			}
		}
		if containsModality(outputMods, "image") && !containsModality(outputMods, "text") {
			return "image", inputMods, outputMods
		}
		if containsModality(outputMods, "audio") && !containsModality(outputMods, "text") {
			return "audio", inputMods, outputMods
		}
		return "chat", inputMods, outputMods
	}

	if arch.Modality != "" {
		parts := strings.SplitN(arch.Modality, "->", 2)
		if len(parts) == 2 {
			inputMods = parseModalities(parts[0])
			outputMods = parseModalities(parts[1])
			for _, om := range outputMods {
				if om == "embedding" || om == "embeddings" {
					return "embedding", inputMods, outputMods
				}
			}
			if containsModality(outputMods, "image") && !containsModality(outputMods, "text") {
				return "image", inputMods, outputMods
			}
			if containsModality(outputMods, "audio") && !containsModality(outputMods, "text") {
				return "audio", inputMods, outputMods
			}
			return "chat", inputMods, outputMods
		}
	}

	if strings.Contains(lower, "embedding") || strings.Contains(lower, "embed") {
		return "embedding", []string{"text"}, []string{"embedding"}
	}
	if strings.Contains(lower, "moderation") {
		return "moderation", []string{"text"}, []string{"text"}
	}
	if strings.Contains(lower, "dall-e") || strings.Contains(lower, "image-") {
		return "image", []string{"text"}, []string{"image"}
	}
	if strings.Contains(lower, "tts") || strings.Contains(lower, "whisper") {
		return "audio", []string{"text"}, []string{"audio"}
	}

	return "chat", nil, nil
}

func parseModalities(s string) []string {
	parts := strings.Split(s, "+")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			result = append(result, t)
		}
	}
	return result
}

func containsModality(mods []string, target string) bool {
	for _, m := range mods {
		if m == target {
			return true
		}
	}
	return false
}

func listAnthropicModels(ctx context.Context, provider data.LlmCredential, apiKey string) ([]AvailableModel, error) {
	baseURL := defaultAnthropicBaseURL
	if provider.BaseURL != nil && strings.TrimSpace(*provider.BaseURL) != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(*provider.BaseURL), "/")
	}
	if err := sharedoutbound.DefaultPolicy().ValidateRequestURL(baseURL + "/models"); err != nil {
		return nil, &UpstreamListModelsError{Kind: "request", Err: err}
	}
	version, extraHeaders := parseAnthropicAdvanced(provider.AdvancedJSON)
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, &UpstreamListModelsError{Kind: "request", Err: err}
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", version)
	req.Header.Set("Accept", "application/json")
	for key, value := range extraHeaders {
		req.Header.Set(key, value)
	}

	resp, err := sharedoutbound.DefaultPolicy().NewHTTPClient(availableModelsTimeout).Do(req)
	if err != nil {
		var denied sharedoutbound.DeniedError
		if errors.As(err, &denied) {
			return nil, &UpstreamListModelsError{Kind: "request", Err: err}
		}
		return nil, &UpstreamListModelsError{Kind: "network", Err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, availableModelsResponseSize))
	if err != nil {
		return nil, &UpstreamListModelsError{Kind: "network", Err: err}
	}
	if resp.StatusCode == nethttp.StatusUnauthorized || resp.StatusCode == nethttp.StatusForbidden {
		return nil, &UpstreamListModelsError{Kind: "auth", StatusCode: resp.StatusCode, Err: fmt.Errorf("status=%d", resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		kind := "upstream"
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			kind = "request"
		}
		return nil, &UpstreamListModelsError{Kind: kind, StatusCode: resp.StatusCode, Err: fmt.Errorf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))}
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

func parseAnthropicAdvanced(advanced map[string]any) (string, map[string]string) {
	version := defaultAnthropicVersion
	extraHeaders := map[string]string{}
	if advanced == nil {
		return version, extraHeaders
	}
	if rawVersion, ok := advanced["anthropic_version"]; ok {
		if value, ok := rawVersion.(string); ok && strings.TrimSpace(value) != "" {
			version = strings.TrimSpace(value)
		}
	}
	if rawHeaders, ok := advanced["extra_headers"]; ok {
		if headers, ok := rawHeaders.(map[string]any); ok {
			for key, value := range headers {
				text, ok := value.(string)
				if !ok || strings.TrimSpace(text) == "" {
					continue
				}
				extraHeaders[key] = strings.TrimSpace(text)
			}
		}
	}
	return version, extraHeaders
}

func sortAvailableModels(models []AvailableModel) {
	sort.Slice(models, func(i, j int) bool {
		left := strings.ToLower(models[i].Name)
		right := strings.ToLower(models[j].Name)
		if left == right {
			return strings.ToLower(models[i].ID) < strings.ToLower(models[j].ID)
		}
		return left < right
	})
}
