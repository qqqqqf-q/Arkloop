package llmproviders

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"arkloop/services/api/internal/data"
)

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com"
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
	cfg, err := ResolveCatalogProtocolConfig(provider, apiKey)
	if err != nil {
		return nil, &UpstreamListModelsError{Kind: "unsupported_provider", Err: err}
	}
	return ListProtocolModels(ctx, cfg)
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
