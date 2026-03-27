package llmproviders

import "strings"

const (
	OpenVikingBackendOpenAI     = "openai"
	OpenVikingBackendAzure      = "azure"
	OpenVikingBackendVolcengine = "volcengine"
	OpenVikingBackendLiteLLM    = "litellm"
)

var validOpenVikingBackends = map[string]struct{}{
	OpenVikingBackendOpenAI:     {},
	OpenVikingBackendAzure:      {},
	OpenVikingBackendVolcengine: {},
	OpenVikingBackendLiteLLM:    {},
}

func IsValidOpenVikingBackend(raw string) bool {
	_, ok := validOpenVikingBackends[strings.ToLower(strings.TrimSpace(raw))]
	return ok
}

func ResolveOpenVikingBackend(provider string, advancedJSON map[string]any) string {
	if backend := OpenVikingBackendFromAdvancedJSON(advancedJSON); IsValidOpenVikingBackend(backend) {
		return backend
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		return OpenVikingBackendOpenAI
	case "anthropic", "gemini":
		return OpenVikingBackendLiteLLM
	default:
		return ""
	}
}

func IsSupportedOpenVikingEmbeddingBackend(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case OpenVikingBackendOpenAI, OpenVikingBackendVolcengine:
		return true
	default:
		return false
	}
}
