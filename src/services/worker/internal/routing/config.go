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
	"github.com/jackc/pgx/v5"
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
	CredentialScopeProject  CredentialScope = "project"
)

type ProviderCredential struct {
	ID           string
	Name         string // llm_credentials.name，用于 AgentConfig.Model 匹配
	Scope        CredentialScope
	ProviderKind ProviderKind
	APIKeyEnv    *string // 环境变量名（env var 加载路径使用）
	APIKeyValue  *string // 明文 API Key（DB 加载路径使用，优先于 APIKeyEnv）
	BaseURL      *string
	OpenAIMode   *string
	AdvancedJSON map[string]any
}

func (c ProviderCredential) ToPublicJSON() map[string]any {
	payload := map[string]any{
		"credential_id":   c.ID,
		"credential_name": c.Name,
		"scope":           string(c.Scope),
		"provider_kind":   string(c.ProviderKind),
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
	ID                  string
	Model               string
	CredentialID        string
	When                map[string]any
	AdvancedJSON        map[string]any
	Multiplier          float64
	CostPer1kInput      *float64
	CostPer1kOutput     *float64
	CostPer1kCacheWrite *float64
	CostPer1kCacheRead  *float64
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
		AdvancedJSON: map[string]any{},
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

		advancedJSON := map[string]any{}
		if rawAdvanced, ok := obj["advanced_json"]; ok && rawAdvanced != nil {
			mapped, ok := rawAdvanced.(map[string]any)
			if !ok {
				return ProviderRoutingConfig{}, fmt.Errorf("route.advanced_json must be a JSON object")
			}
			advancedJSON = mapped
		}

		routes = append(routes, ProviderRouteRule{
			ID:           routeID,
			Model:        model,
			CredentialID: credID,
			When:         when,
			AdvancedJSON: advancedJSON,
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

// GetHighestPriorityRouteByCredentialName 按凭证显示名称找到最优路由。
// 优先匹配有 When 条件且命中 inputJSON 的路由，其次取该凭证首条路由。
// routes 已按 priority DESC 排好序，直接遍历取首个匹配项即可。
func (c ProviderRoutingConfig) GetHighestPriorityRouteByCredentialName(name string, inputJSON map[string]any) (ProviderRouteRule, ProviderCredential, bool) {
	if strings.TrimSpace(name) == "" {
		return ProviderRouteRule{}, ProviderCredential{}, false
	}
	credIDByName := c.findCredentialIDByName(name)
	if credIDByName == "" {
		return ProviderRouteRule{}, ProviderCredential{}, false
	}
	return c.pickBestRoute(func(route ProviderRouteRule) bool {
		return route.CredentialID == credIDByName
	}, inputJSON)
}

func (c ProviderRoutingConfig) GetHighestPriorityRouteByCredentialAndModel(name string, model string, inputJSON map[string]any) (ProviderRouteRule, ProviderCredential, bool) {
	credIDByName := c.findCredentialIDByName(name)
	if credIDByName == "" || strings.TrimSpace(model) == "" {
		return ProviderRouteRule{}, ProviderCredential{}, false
	}
	return c.pickBestRoute(func(route ProviderRouteRule) bool {
		return route.CredentialID == credIDByName && strings.EqualFold(route.Model, model)
	}, inputJSON)
}

func (c ProviderRoutingConfig) GetHighestPriorityRouteByModel(model string, inputJSON map[string]any) (ProviderRouteRule, ProviderCredential, bool) {
	if strings.TrimSpace(model) == "" {
		return ProviderRouteRule{}, ProviderCredential{}, false
	}
	return c.pickBestRoute(func(route ProviderRouteRule) bool {
		return strings.EqualFold(route.Model, model)
	}, inputJSON)
}

func (c ProviderRoutingConfig) findCredentialIDByName(name string) string {
	for _, cred := range c.Credentials {
		if strings.EqualFold(cred.Name, name) {
			return cred.ID
		}
	}
	return ""
}

func (c ProviderRoutingConfig) pickBestRoute(match func(ProviderRouteRule) bool, inputJSON map[string]any) (ProviderRouteRule, ProviderCredential, bool) {
	for _, route := range c.Routes {
		if match(route) && len(route.When) > 0 && route.Matches(inputJSON) {
			cred, _ := c.GetCredential(route.CredentialID)
			return route, cred, true
		}
	}
	for _, route := range c.Routes {
		if match(route) && len(route.When) == 0 {
			cred, _ := c.GetCredential(route.CredentialID)
			return route, cred, true
		}
	}
	for _, route := range c.Routes {
		if match(route) {
			cred, _ := c.GetCredential(route.CredentialID)
			return route, cred, true
		}
	}
	return ProviderRouteRule{}, ProviderCredential{}, false
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
	case "project":
		return CredentialScopeProject, nil
	default:
		return "", fmt.Errorf("must be platform/project")
	}
}

// LoadRoutingConfigFromDB 从数据库加载路由配置。
// projectID 为 nil 时只加载 platform 路由；非 nil 时加载 platform + 该 project 的路由（project 优先）。
// 若数据库中无路由配置（len(Routes)==0），调用方应回退到环境变量。
func LoadRoutingConfigFromDB(ctx context.Context, pool *pgxpool.Pool, projectID *uuid.UUID) (ProviderRoutingConfig, error) {
	if pool == nil {
		return ProviderRoutingConfig{}, fmt.Errorf("pool must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// 一次 JOIN 拿到所有需要的字段，包含 secrets 的加密值
	var (
		rows pgx.Rows
		err  error
	)
	if projectID == nil {
		rows, err = pool.Query(ctx, `
		SELECT r.id, r.credential_id, r.model, r.when_json, r.is_default,
		       r.advanced_json, r.multiplier, r.cost_per_1k_input, r.cost_per_1k_output,
		       r.cost_per_1k_cache_write, r.cost_per_1k_cache_read,
		       c.id, c.scope, c.name, c.provider, c.base_url, c.openai_api_mode, c.advanced_json,
		       s.encrypted_value, s.key_version
		FROM llm_routes r
		JOIN llm_credentials c ON c.id = r.credential_id
		LEFT JOIN secrets s ON s.id = c.secret_id
		WHERE c.revoked_at IS NULL
		  AND r.project_id IS NULL
		ORDER BY r.is_default DESC,
		         r.priority DESC,
		         r.created_at ASC,
		         r.id ASC
	`)
	} else {
		rows, err = pool.Query(ctx, `
		SELECT r.id, r.credential_id, r.model, r.when_json, r.is_default,
		       r.advanced_json, r.multiplier, r.cost_per_1k_input, r.cost_per_1k_output,
		       r.cost_per_1k_cache_write, r.cost_per_1k_cache_read,
		       c.id, c.scope, c.name, c.provider, c.base_url, c.openai_api_mode, c.advanced_json,
		       s.encrypted_value, s.key_version
		FROM llm_routes r
		JOIN llm_credentials c ON c.id = r.credential_id
		LEFT JOIN secrets s ON s.id = c.secret_id
		WHERE c.revoked_at IS NULL
		  AND (
			r.project_id IS NULL
			OR r.project_id = $1
		  )
		ORDER BY CASE WHEN r.project_id IS NOT NULL THEN 0 ELSE 1 END ASC,
		         r.is_default DESC,
		         r.priority DESC,
		         r.created_at ASC,
		         r.id ASC
	`, *projectID)
	}
	if err != nil {
		return ProviderRoutingConfig{}, fmt.Errorf("routing: db query: %w", err)
	}
	defer rows.Close()

		type rowData struct {
		routeID             uuid.UUID
		credentialID        uuid.UUID
		model               string
		whenJSON            []byte
		isDefault           bool
		routeAdvancedJSON   []byte
		multiplier          float64
		costPer1kInput      *float64
		costPer1kOutput     *float64
			costPer1kCacheWrite *float64
			costPer1kCacheRead  *float64
			credID              uuid.UUID
			scope               string
			credName            string
		provider            string
		baseURL             *string
		openaiAPIMode       *string
		advancedJSON        []byte
		encryptedValue      *string
		keyVersion          *int
	}

	var allRows []rowData
	for rows.Next() {
		var rd rowData
		if err := rows.Scan(
			&rd.routeID, &rd.credentialID, &rd.model, &rd.whenJSON, &rd.isDefault,
			&rd.routeAdvancedJSON, &rd.multiplier, &rd.costPer1kInput, &rd.costPer1kOutput,
			&rd.costPer1kCacheWrite, &rd.costPer1kCacheRead,
			&rd.credID, &rd.scope, &rd.credName, &rd.provider, &rd.baseURL, &rd.openaiAPIMode, &rd.advancedJSON,
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
	credentialOrder := make([]string, 0, len(allRows))
	defaultRouteID := ""
	for _, rd := range allRows {
		credIDStr := rd.credID.String()
		if _, exists := credentialsByID[credIDStr]; exists {
			if rd.isDefault && defaultRouteID == "" {
				defaultRouteID = rd.routeID.String()
			}
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

		scope, err := parseScope(rd.scope)
		if err != nil {
			slog.WarnContext(ctx, "routing: skipping credential with unsupported scope",
				"credential_id", credIDStr,
				"scope", rd.scope,
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

		advancedJSON := map[string]any{}
		if len(rd.advancedJSON) > 0 {
			_ = json.Unmarshal(rd.advancedJSON, &advancedJSON)
		}

		cred := ProviderCredential{
			ID:           credIDStr,
			Name:         rd.credName,
			Scope:        scope,
			ProviderKind: kind,
			APIKeyValue:  apiKeyValue,
			BaseURL:      rd.baseURL,
			OpenAIMode:   rd.openaiAPIMode,
			AdvancedJSON: advancedJSON,
		}
		credentialsByID[credIDStr] = cred
		credentialOrder = append(credentialOrder, credIDStr)
		if rd.isDefault && defaultRouteID == "" {
			defaultRouteID = rd.routeID.String()
		}
	}

	var routes []ProviderRouteRule

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

		routeAdvancedJSON := map[string]any{}
		if len(rd.routeAdvancedJSON) > 0 {
			_ = json.Unmarshal(rd.routeAdvancedJSON, &routeAdvancedJSON)
		}

		multiplier := rd.multiplier
		if multiplier <= 0 {
			multiplier = 1.0
		}

		route := ProviderRouteRule{
			ID:                  routeIDStr,
			Model:               rd.model,
			CredentialID:        credIDStr,
			When:                when,
			AdvancedJSON:        routeAdvancedJSON,
			Multiplier:          multiplier,
			CostPer1kInput:      rd.costPer1kInput,
			CostPer1kOutput:     rd.costPer1kOutput,
			CostPer1kCacheWrite: rd.costPer1kCacheWrite,
			CostPer1kCacheRead:  rd.costPer1kCacheRead,
		}
		routes = append(routes, route)
	}

	if len(routes) == 0 {
		return ProviderRoutingConfig{}, nil
	}

	if defaultRouteID == "" && len(routes) > 0 {
		defaultRouteID = routes[0].ID
	}

	credentials := make([]ProviderCredential, 0, len(credentialOrder))
	for _, credID := range credentialOrder {
		cred, ok := credentialsByID[credID]
		if !ok {
			continue
		}
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
