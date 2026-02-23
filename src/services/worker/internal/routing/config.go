package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"regexp"
	"strings"

	workerCrypto "arkloop/services/worker/internal/crypto"
	"arkloop/services/worker/internal/stablejson"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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
	APIKeyEnv    *string  // 环境变量名（env var 加载路径使用）
	APIKeyValue  *string  // 明文 API Key（DB 加载路径使用，优先于 APIKeyEnv）
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
	ID              string
	Model           string
	CredentialID    string
	When            map[string]any
	Multiplier      float64
	CostPer1kInput  *float64
	CostPer1kOutput *float64
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

// LoadRoutingConfigFromDB 从数据库加载路由配置。
// 查询所有未吊销凭证的路由，解密 API Key 后构建 ProviderRoutingConfig。
// 若数据库中无路由配置（len(Routes)==0），调用方应回退到环境变量。
func LoadRoutingConfigFromDB(ctx context.Context, pool *pgxpool.Pool) (ProviderRoutingConfig, error) {
	if pool == nil {
		return ProviderRoutingConfig{}, fmt.Errorf("pool must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// 一次 JOIN 拿到所有需要的字段，包含 secrets 的加密值
	rows, err := pool.Query(ctx, `
		SELECT r.id, r.credential_id, r.model, r.when_json, r.is_default,
		       r.multiplier, r.cost_per_1k_input, r.cost_per_1k_output,
		       c.id, c.provider, c.base_url, c.openai_api_mode,
		       s.encrypted_value, s.key_version
		FROM llm_routes r
		JOIN llm_credentials c ON c.id = r.credential_id
		LEFT JOIN secrets s ON s.id = c.secret_id
		WHERE c.revoked_at IS NULL
		ORDER BY r.priority DESC, r.is_default DESC
	`)
	if err != nil {
		return ProviderRoutingConfig{}, fmt.Errorf("routing: db query: %w", err)
	}
	defer rows.Close()

	type rowData struct {
		routeID         uuid.UUID
		credentialID    uuid.UUID
		model           string
		whenJSON        []byte
		isDefault       bool
		multiplier      float64
		costPer1kInput  *float64
		costPer1kOutput *float64
		credID          uuid.UUID
		provider        string
		baseURL         *string
		openaiAPIMode   *string
		encryptedValue  *string
		keyVersion      *int
	}

	var allRows []rowData
	for rows.Next() {
		var rd rowData
		if err := rows.Scan(
			&rd.routeID, &rd.credentialID, &rd.model, &rd.whenJSON, &rd.isDefault,
			&rd.multiplier, &rd.costPer1kInput, &rd.costPer1kOutput,
			&rd.credID, &rd.provider, &rd.baseURL, &rd.openaiAPIMode,
			&rd.encryptedValue, &rd.keyVersion,
		); err != nil {
			return ProviderRoutingConfig{}, fmt.Errorf("routing: scan: %w", err)
		}
		allRows = append(allRows, rd)
	}
	if err := rows.Err(); err != nil {
		return ProviderRoutingConfig{}, fmt.Errorf("routing: rows: %w", err)
	}

	if len(allRows) == 0 {
		return ProviderRoutingConfig{}, nil
	}

	credentialsByID := map[string]ProviderCredential{}
	for _, rd := range allRows {
		credIDStr := rd.credID.String()
		if _, exists := credentialsByID[credIDStr]; exists {
			continue
		}

		kind, err := dbProviderToKind(rd.provider)
		if err != nil {
			slog.WarnContext(ctx, "routing: skipping credential with unsupported provider",
				"credential_id", credIDStr,
				"provider", rd.provider,
			)
			continue
		}

		var apiKeyValue *string
		if rd.encryptedValue != nil && rd.keyVersion != nil {
			plainBytes, err := workerCrypto.DecryptGCM(*rd.encryptedValue)
			if err != nil {
				return ProviderRoutingConfig{}, fmt.Errorf("routing: decrypt credential %s: %w", credIDStr, err)
			}
			plaintext := string(plainBytes)
			apiKeyValue = &plaintext
		}

		cred := ProviderCredential{
			ID:           credIDStr,
			Scope:        CredentialScopeOrg,
			ProviderKind: kind,
			APIKeyValue:  apiKeyValue,
			BaseURL:      rd.baseURL,
			OpenAIMode:   rd.openaiAPIMode,
			AdvancedJSON: map[string]any{},
		}
		credentialsByID[credIDStr] = cred
	}

	var (
		routes         []ProviderRouteRule
		defaultRouteID string
	)

	seen := map[string]struct{}{}
	for _, rd := range allRows {
		routeIDStr := rd.routeID.String()
		credIDStr := rd.credentialID.String()

		if _, exists := seen[routeIDStr]; exists {
			continue
		}
		if _, credExists := credentialsByID[credIDStr]; !credExists {
			continue
		}
		seen[routeIDStr] = struct{}{}

		when := map[string]any{}
		if len(rd.whenJSON) > 0 {
			_ = json.Unmarshal(rd.whenJSON, &when)
		}

		multiplier := rd.multiplier
		if multiplier <= 0 {
			multiplier = 1.0
		}

		route := ProviderRouteRule{
			ID:              routeIDStr,
			Model:           rd.model,
			CredentialID:    credIDStr,
			When:            when,
			Multiplier:      multiplier,
			CostPer1kInput:  rd.costPer1kInput,
			CostPer1kOutput: rd.costPer1kOutput,
		}
		routes = append(routes, route)

		if rd.isDefault && defaultRouteID == "" {
			defaultRouteID = routeIDStr
		}
	}

	if len(routes) == 0 {
		return ProviderRoutingConfig{}, nil
	}
	if defaultRouteID == "" {
		defaultRouteID = routes[0].ID
	}

	credentials := make([]ProviderCredential, 0, len(credentialsByID))
	for _, cred := range credentialsByID {
		credentials = append(credentials, cred)
	}

	return ProviderRoutingConfig{
		DefaultRouteID: defaultRouteID,
		Credentials:    credentials,
		Routes:         routes,
	}, nil
}

// dbProviderToKind 将数据库中的 provider 字符串映射到 ProviderKind。
func dbProviderToKind(provider string) (ProviderKind, error) {
	switch strings.ToLower(provider) {
	case "openai":
		return ProviderKindOpenAI, nil
	case "anthropic":
		return ProviderKindAnthropic, nil
	default:
		return "", fmt.Errorf("unsupported provider: %s", provider)
	}
}




