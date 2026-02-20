package routing

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"

	"arkloop/services/worker/internal/stablejson"
)

const providerRoutingEnv = "ARKLOOP_PROVIDER_ROUTING_JSON"

var (
	idRegex      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
	envNameRegex = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

type ProviderKind string

const (
	ProviderKindStub      ProviderKind = "stub"
	ProviderKindOpenAI    ProviderKind = "openai"
	ProviderKindAnthropic ProviderKind = "anthropic"
)

type CredentialScope string

const (
	CredentialScopePlatform CredentialScope = "platform"
	CredentialScopeOrg      CredentialScope = "org"
)

type ProviderCredential struct {
	ID           string
	Scope        CredentialScope
	ProviderKind ProviderKind
	APIKeyEnv    *string
	BaseURL      *string
	OpenAIMode   *string
	AdvancedJSON map[string]any
}

func (c ProviderCredential) ToPublicJSON() map[string]any {
	payload := map[string]any{
		"credential_id": c.ID,
		"scope":         string(c.Scope),
		"provider_kind": string(c.ProviderKind),
	}
	if c.BaseURL != nil {
		payload["base_url"] = *c.BaseURL
	}
	if c.OpenAIMode != nil {
		payload["openai_api_mode"] = *c.OpenAIMode
	}
	if len(c.AdvancedJSON) > 0 {
		payload["advanced_json_sha256"] = stablejson.MustSha256(c.AdvancedJSON)
	}
	return payload
}

type ProviderRouteRule struct {
	ID           string
	Model        string
	CredentialID string
	When         map[string]any
}

func (r ProviderRouteRule) Matches(input map[string]any) bool {
	if len(r.When) == 0 {
		return true
	}
	for key, expected := range r.When {
		if !reflect.DeepEqual(input[key], expected) {
			return false
		}
	}
	return true
}

type ProviderRoutingConfig struct {
	DefaultRouteID string
	Credentials    []ProviderCredential
	Routes         []ProviderRouteRule
}

func DefaultRoutingConfig() ProviderRoutingConfig {
	credential := ProviderCredential{
		ID:           "stub_default",
		Scope:        CredentialScopePlatform,
		ProviderKind: ProviderKindStub,
		AdvancedJSON: map[string]any{},
	}
	route := ProviderRouteRule{
		ID:           "default",
		Model:        "stub",
		CredentialID: credential.ID,
		When:         map[string]any{},
	}
	return ProviderRoutingConfig{
		DefaultRouteID: route.ID,
		Credentials:    []ProviderCredential{credential},
		Routes:         []ProviderRouteRule{route},
	}
}

func LoadRoutingConfigFromEnv() (ProviderRoutingConfig, error) {
	raw := strings.TrimSpace(os.Getenv(providerRoutingEnv))
	if raw == "" {
		return DefaultRoutingConfig(), nil
	}

	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ProviderRoutingConfig{}, fmt.Errorf("%s is not valid JSON", providerRoutingEnv)
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return ProviderRoutingConfig{}, fmt.Errorf("%s must be a JSON object", providerRoutingEnv)
	}

	defaultRouteID, err := validateID(requiredString(root, "default_route_id"), "default_route_id")
	if err != nil {
		return ProviderRoutingConfig{}, err
	}

	credentialsRaw, ok := root["credentials"].([]any)
	if !ok || len(credentialsRaw) == 0 {
		return ProviderRoutingConfig{}, fmt.Errorf("credentials must be a non-empty array")
	}

	credentials := make([]ProviderCredential, 0, len(credentialsRaw))
	seenCredIDs := map[string]struct{}{}
	for idx, item := range credentialsRaw {
		obj, ok := item.(map[string]any)
		if !ok {
			return ProviderRoutingConfig{}, fmt.Errorf("credentials[%d] must be a JSON object", idx)
		}

		credID, err := validateID(requiredString(obj, "id"), "credential.id")
		if err != nil {
			return ProviderRoutingConfig{}, err
		}
		if _, exists := seenCredIDs[credID]; exists {
			return ProviderRoutingConfig{}, fmt.Errorf("credential.id duplicate: %s", credID)
		}
		seenCredIDs[credID] = struct{}{}

		scope, err := parseScope(requiredString(obj, "scope"))
		if err != nil {
			return ProviderRoutingConfig{}, fmt.Errorf("credential.scope: %w", err)
		}
		kind, err := parseProviderKind(requiredString(obj, "provider_kind"))
		if err != nil {
			return ProviderRoutingConfig{}, fmt.Errorf("credential.provider_kind: %w", err)
		}

		var apiKeyEnv *string
		if rawEnv, ok := obj["api_key_env"]; ok && rawEnv != nil {
			value, ok := rawEnv.(string)
			if !ok {
				return ProviderRoutingConfig{}, fmt.Errorf("credential.api_key_env must be a string")
			}
			cleaned := strings.TrimSpace(value)
			if cleaned == "" {
				return ProviderRoutingConfig{}, fmt.Errorf("credential.api_key_env must not be empty")
			}
			if !envNameRegex.MatchString(cleaned) {
				return ProviderRoutingConfig{}, fmt.Errorf("credential.api_key_env must be a valid env var name: %s", cleaned)
			}
			apiKeyEnv = &cleaned
		}

		var baseURL *string
		if rawBase, ok := obj["base_url"]; ok && rawBase != nil {
			value, ok := rawBase.(string)
			if !ok {
				return ProviderRoutingConfig{}, fmt.Errorf("credential.base_url must be a string")
			}
			cleaned := strings.TrimSpace(value)
			if cleaned == "" {
				return ProviderRoutingConfig{}, fmt.Errorf("credential.base_url must not be empty")
			}
			trimmed := strings.TrimRight(cleaned, "/")
			baseURL = &trimmed
		}

		var openaiMode *string
		if rawMode, ok := obj["openai_api_mode"]; ok && rawMode != nil {
			value, ok := rawMode.(string)
			if !ok {
				return ProviderRoutingConfig{}, fmt.Errorf("openai_api_mode must be a string")
			}
			cleaned := strings.TrimSpace(value)
			if cleaned == "" {
				return ProviderRoutingConfig{}, fmt.Errorf("openai_api_mode must not be empty")
			}
			if cleaned != "auto" && cleaned != "responses" && cleaned != "chat_completions" {
				return ProviderRoutingConfig{}, fmt.Errorf("openai_api_mode must be auto/responses/chat_completions")
			}
			openaiMode = &cleaned
		}

		advancedJSON := map[string]any{}
		if rawAdvanced, ok := obj["advanced_json"]; ok && rawAdvanced != nil {
			mapped, ok := rawAdvanced.(map[string]any)
			if !ok {
				return ProviderRoutingConfig{}, fmt.Errorf("credential.advanced_json must be a JSON object")
			}
			advancedJSON = mapped
		}

		if kind == ProviderKindOpenAI {
			if openaiMode == nil {
				return ProviderRoutingConfig{}, fmt.Errorf("OpenAI credential must specify openai_api_mode")
			}
		} else {
			if openaiMode != nil {
				return ProviderRoutingConfig{}, fmt.Errorf("only OpenAI credential may set openai_api_mode")
			}
		}

		if kind != ProviderKindStub && apiKeyEnv == nil {
			return ProviderRoutingConfig{}, fmt.Errorf("non-stub credential must provide api_key_env (stores env name only, not plaintext)")
		}

		credentials = append(credentials, ProviderCredential{
			ID:           credID,
			Scope:        scope,
			ProviderKind: kind,
			APIKeyEnv:    apiKeyEnv,
			BaseURL:      baseURL,
			OpenAIMode:   openaiMode,
			AdvancedJSON: advancedJSON,
		})
	}

	routesRaw, ok := root["routes"].([]any)
	if !ok || len(routesRaw) == 0 {
		return ProviderRoutingConfig{}, fmt.Errorf("routes must be a non-empty array")
	}

	routes := make([]ProviderRouteRule, 0, len(routesRaw))
	seenRouteIDs := map[string]struct{}{}
	for idx, item := range routesRaw {
		obj, ok := item.(map[string]any)
		if !ok {
			return ProviderRoutingConfig{}, fmt.Errorf("routes[%d] must be a JSON object", idx)
		}

		routeID, err := validateID(requiredString(obj, "id"), "route.id")
		if err != nil {
			return ProviderRoutingConfig{}, err
		}
		if _, exists := seenRouteIDs[routeID]; exists {
			return ProviderRoutingConfig{}, fmt.Errorf("route.id duplicate: %s", routeID)
		}
		seenRouteIDs[routeID] = struct{}{}

		model := strings.TrimSpace(requiredString(obj, "model"))
		if model == "" {
			return ProviderRoutingConfig{}, fmt.Errorf("routes[].model must not be empty")
		}
		credID, err := validateID(requiredString(obj, "credential_id"), "route.credential_id")
		if err != nil {
			return ProviderRoutingConfig{}, err
		}

		when := map[string]any{}
		if rawWhen, ok := obj["when"]; ok && rawWhen != nil {
			mapped, ok := rawWhen.(map[string]any)
			if !ok {
				return ProviderRoutingConfig{}, fmt.Errorf("route.when must be a JSON object")
			}
			when = mapped
		}

		routes = append(routes, ProviderRouteRule{
			ID:           routeID,
			Model:        model,
			CredentialID: credID,
			When:         when,
		})
	}

	credentialsByID := map[string]ProviderCredential{}
	for _, credential := range credentials {
		credentialsByID[credential.ID] = credential
	}

	for _, route := range routes {
		if _, exists := credentialsByID[route.CredentialID]; !exists {
			return ProviderRoutingConfig{}, fmt.Errorf("route.credential_id not found: %s", route.CredentialID)
		}
	}
	if _, exists := seenRouteIDs[defaultRouteID]; !exists {
		return ProviderRoutingConfig{}, fmt.Errorf("default_route_id not found: %s", defaultRouteID)
	}

	return ProviderRoutingConfig{
		DefaultRouteID: defaultRouteID,
		Credentials:    credentials,
		Routes:         routes,
	}, nil
}

func (c ProviderRoutingConfig) GetCredential(credentialID string) (ProviderCredential, bool) {
	for _, credential := range c.Credentials {
		if credential.ID == credentialID {
			return credential, true
		}
	}
	return ProviderCredential{}, false
}

func (c ProviderRoutingConfig) GetRoute(routeID string) (ProviderRouteRule, bool) {
	for _, route := range c.Routes {
		if route.ID == routeID {
			return route, true
		}
	}
	return ProviderRouteRule{}, false
}

func requiredString(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func validateID(value string, label string) (string, error) {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return "", fmt.Errorf("%s must not be empty", label)
	}
	if !idRegex.MatchString(cleaned) {
		return "", fmt.Errorf("%s invalid: %s", label, cleaned)
	}
	return cleaned, nil
}

func parseProviderKind(value string) (ProviderKind, error) {
	cleaned := strings.ToLower(strings.TrimSpace(value))
	switch cleaned {
	case "stub":
		return ProviderKindStub, nil
	case "openai":
		return ProviderKindOpenAI, nil
	case "anthropic":
		return ProviderKindAnthropic, nil
	default:
		return "", fmt.Errorf("must be stub/openai/anthropic")
	}
}

func parseScope(value string) (CredentialScope, error) {
	cleaned := strings.ToLower(strings.TrimSpace(value))
	switch cleaned {
	case "platform":
		return CredentialScopePlatform, nil
	case "org":
		return CredentialScopeOrg, nil
	default:
		return "", fmt.Errorf("must be platform/org")
	}
}
