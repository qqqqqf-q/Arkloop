package llmproviders

import (
	"errors"
	"strings"
)

const (
	anthropicAdvancedVersionKey      = "anthropic_version"
	anthropicAdvancedExtraHeadersKey = "extra_headers"
	anthropicBetaHeaderName          = "anthropic-beta"
	openVikingBackendKey             = "openviking_backend"
	openVikingExtraHeadersKey        = "openviking_extra_headers"
)

func ValidateAdvancedJSONForProvider(provider string, advancedJSON map[string]any) error {
	if advancedJSON != nil {
		if err := validateOpenVikingAdvancedJSON(advancedJSON); err != nil {
			return err
		}
	}
	if strings.TrimSpace(provider) != "anthropic" || advancedJSON == nil {
		return nil
	}
	return validateAnthropicAdvancedJSON(advancedJSON)
}

func validateOpenVikingAdvancedJSON(advancedJSON map[string]any) error {
	if advancedJSON == nil {
		return nil
	}
	if rawBackend, ok := advancedJSON[openVikingBackendKey]; ok {
		backend, ok := rawBackend.(string)
		if !ok || strings.TrimSpace(backend) == "" {
			return errors.New("advanced_json.openviking_backend must be a non-empty string")
		}
		if !IsValidOpenVikingBackend(backend) {
			return errors.New("advanced_json.openviking_backend must be one of openai, azure, volcengine, litellm")
		}
	}
	if rawHeaders, ok := advancedJSON[openVikingExtraHeadersKey]; ok {
		headers, ok := rawHeaders.(map[string]any)
		if !ok {
			return errors.New("advanced_json.openviking_extra_headers must be an object")
		}
		for key, value := range headers {
			if strings.TrimSpace(key) == "" {
				return errors.New("advanced_json.openviking_extra_headers keys must be non-empty strings")
			}
			headerValue, ok := value.(string)
			if !ok || strings.TrimSpace(headerValue) == "" {
				return errors.New("advanced_json.openviking_extra_headers values must be non-empty strings")
			}
		}
	}
	return nil
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

func OpenVikingBackendFromAdvancedJSON(advancedJSON map[string]any) string {
	if advancedJSON == nil {
		return ""
	}
	rawBackend, ok := advancedJSON[openVikingBackendKey]
	if !ok {
		return ""
	}
	backend, ok := rawBackend.(string)
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(backend))
}

func OpenVikingExtraHeadersFromAdvancedJSON(advancedJSON map[string]any) map[string]string {
	if advancedJSON == nil {
		return nil
	}
	rawHeaders, ok := advancedJSON[openVikingExtraHeadersKey]
	if !ok {
		rawHeaders = advancedJSON[anthropicAdvancedExtraHeadersKey]
	}
	headers, ok := rawHeaders.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		headerName := strings.TrimSpace(key)
		headerValue, ok := value.(string)
		if headerName == "" || !ok || strings.TrimSpace(headerValue) == "" {
			continue
		}
		out[headerName] = strings.TrimSpace(headerValue)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
