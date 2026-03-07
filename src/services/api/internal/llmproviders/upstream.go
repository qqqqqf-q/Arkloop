package llmproviders

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"sort"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
)

const (
	defaultOpenAIBaseURL        = "https://api.openai.com/v1"
	defaultAnthropicBaseURL     = "https://api.anthropic.com/v1"
	defaultAnthropicVersion     = "2023-06-01"
	availableModelsTimeout      = 15 * time.Second
	availableModelsResponseSize = 2 << 20
)

var availableModelsHTTPClient = &nethttp.Client{Timeout: availableModelsTimeout}

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
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, &UpstreamListModelsError{Kind: "request", Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := availableModelsHTTPClient.Do(req)
	if err != nil {
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
			ID string `json:"id"`
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
		models = append(models, AvailableModel{ID: id, Name: id})
	}
	sortAvailableModels(models)
	return models, nil
}

func listAnthropicModels(ctx context.Context, provider data.LlmCredential, apiKey string) ([]AvailableModel, error) {
	baseURL := defaultAnthropicBaseURL
	if provider.BaseURL != nil && strings.TrimSpace(*provider.BaseURL) != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(*provider.BaseURL), "/")
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

	resp, err := availableModelsHTTPClient.Do(req)
	if err != nil {
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
		models = append(models, AvailableModel{ID: id, Name: name})
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
