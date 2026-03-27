package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sharedoutbound "arkloop/services/shared/outboundurl"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type providerQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type ProviderSecretDecrypter func(ctx context.Context, encrypted string, keyVersion *int, providerName string) (*string, error)

type ProviderRuntimeState string

const (
	ProviderRuntimeStateReady         ProviderRuntimeState = "ready"
	ProviderRuntimeStateInactive      ProviderRuntimeState = "inactive"
	ProviderRuntimeStateMissingConfig ProviderRuntimeState = "missing_config"
	ProviderRuntimeStateDecryptFailed ProviderRuntimeState = "decrypt_failed"
	ProviderRuntimeStateInvalidConfig ProviderRuntimeState = "invalid_config"
)

type ProviderRuntimeStatus struct {
	OwnerKind     string
	GroupName     string
	ProviderName  string
	KeyPrefix     *string
	BaseURL       *string
	APIKeyValue   *string
	ConfigJSON    map[string]any
	RuntimeState  ProviderRuntimeState
	RuntimeReason string
}

func (s ProviderRuntimeStatus) Ready() bool {
	return s.RuntimeState == ProviderRuntimeStateReady
}

func (s ProviderRuntimeStatus) ProviderConfig() ProviderConfig {
	return ProviderConfig{
		GroupName:    strings.TrimSpace(s.GroupName),
		ProviderName: strings.TrimSpace(s.ProviderName),
		BaseURL:      s.BaseURL,
		APIKeyValue:  s.APIKeyValue,
		ConfigJSON:   copyJSONMap(s.ConfigJSON),
	}
}

func LoadPlatformProviders(ctx context.Context, pool providerQuerier, decrypt ProviderSecretDecrypter) ([]ProviderConfig, error) {
	statuses, err := LoadPlatformProviderStatuses(ctx, pool, decrypt)
	if err != nil {
		return nil, err
	}
	return ReadyProvidersFromStatuses(statuses), nil
}

func LoadUserProviders(ctx context.Context, pool providerQuerier, userID uuid.UUID, decrypt ProviderSecretDecrypter) ([]ProviderConfig, error) {
	statuses, err := LoadUserProviderStatuses(ctx, pool, userID, decrypt)
	if err != nil {
		return nil, err
	}
	return ReadyProvidersFromStatuses(statuses), nil
}

func ReadyProvidersFromStatuses(statuses []ProviderRuntimeStatus) []ProviderConfig {
	out := make([]ProviderConfig, 0, len(statuses))
	for _, status := range statuses {
		if !status.Ready() {
			continue
		}
		out = append(out, status.ProviderConfig())
	}
	return out
}

func LoadPlatformProviderStatuses(ctx context.Context, pool providerQuerier, decrypt ProviderSecretDecrypter) ([]ProviderRuntimeStatus, error) {
	return loadProviderStatuses(ctx, pool, `
		SELECT c.owner_kind, c.group_name, c.provider_name, c.key_prefix, c.base_url, c.config_json,
		       s.encrypted_value, s.key_version
		FROM tool_provider_configs c
		LEFT JOIN secrets s ON s.id = c.secret_id AND s.owner_kind = 'platform'
		WHERE c.owner_kind = 'platform' AND c.is_active = TRUE
		ORDER BY c.updated_at DESC
	`, decrypt)
}

func LoadUserProviderStatuses(ctx context.Context, pool providerQuerier, userID uuid.UUID, decrypt ProviderSecretDecrypter) ([]ProviderRuntimeStatus, error) {
	if userID == uuid.Nil {
		return nil, nil
	}
	return loadProviderStatuses(ctx, pool, `
		SELECT c.owner_kind, c.group_name, c.provider_name, c.key_prefix, c.base_url, c.config_json,
		       s.encrypted_value, s.key_version
		FROM tool_provider_configs c
		LEFT JOIN secrets s ON s.id = c.secret_id AND s.owner_kind = 'user'
		WHERE c.owner_kind = 'user' AND c.owner_user_id = $1 AND c.is_active = TRUE
		ORDER BY c.updated_at DESC
	`, decrypt, userID)
}

func loadProviderStatuses(ctx context.Context, pool providerQuerier, sql string, decrypt ProviderSecretDecrypter, args ...any) ([]ProviderRuntimeStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return nil, nil
	}

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("tool_provider_configs query: %w", err)
	}
	defer rows.Close()

	statuses := make([]ProviderRuntimeStatus, 0)
	for rows.Next() {
		var (
			ownerKind    string
			groupName    string
			providerName string
			keyPrefix    *string
			baseURL      *string
			configJSON   []byte
			encrypted    *string
			keyVersion   *int
		)
		if err := rows.Scan(&ownerKind, &groupName, &providerName, &keyPrefix, &baseURL, &configJSON, &encrypted, &keyVersion); err != nil {
			return nil, fmt.Errorf("tool_provider_configs scan: %w", err)
		}

		status := ProviderRuntimeStatus{
			OwnerKind:    strings.TrimSpace(ownerKind),
			GroupName:    strings.TrimSpace(groupName),
			ProviderName: strings.TrimSpace(providerName),
			KeyPrefix:    keyPrefix,
			BaseURL:      baseURL,
			ConfigJSON:   map[string]any{},
			RuntimeState: ProviderRuntimeStateReady,
		}
		if len(configJSON) > 0 {
			_ = json.Unmarshal(configJSON, &status.ConfigJSON)
		}

		if encrypted != nil && strings.TrimSpace(*encrypted) != "" {
			if decrypt == nil {
				status.RuntimeState = ProviderRuntimeStateDecryptFailed
				status.RuntimeReason = "secret_decrypt_unavailable"
			} else {
				plain, decErr := decrypt(ctx, *encrypted, keyVersion, status.ProviderName)
				if decErr != nil {
					status.RuntimeState = ProviderRuntimeStateDecryptFailed
					status.RuntimeReason = "secret_decrypt_failed"
				} else {
					status.APIKeyValue = plain
				}
			}
		}

		if status.RuntimeState == ProviderRuntimeStateReady {
			status.RuntimeState, status.RuntimeReason = evaluateProviderRuntimeStatus(status)
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tool_provider_configs rows: %w", err)
	}
	return statuses, nil
}

func evaluateProviderRuntimeStatus(status ProviderRuntimeStatus) (ProviderRuntimeState, string) {
	switch strings.TrimSpace(status.ProviderName) {
	case "web_search.tavily":
		if blankPtr(status.APIKeyValue) {
			return ProviderRuntimeStateMissingConfig, "missing_api_key"
		}
		return ProviderRuntimeStateReady, ""
	case "web_search.searxng":
		return validateInternalBaseURL(status.BaseURL)
	case "web_fetch.jina":
		if blankPtr(status.APIKeyValue) {
			return ProviderRuntimeStateMissingConfig, "missing_api_key"
		}
		return ProviderRuntimeStateReady, ""
	case "web_fetch.firecrawl":
		return validateInternalBaseURL(status.BaseURL)
	case "web_fetch.basic":
		return ProviderRuntimeStateReady, ""
	case "image_understanding.minimax":
		if blankPtr(status.APIKeyValue) {
			return ProviderRuntimeStateMissingConfig, "missing_api_key"
		}
		return validateOptionalBaseURL(status.BaseURL)
	case "sandbox.docker", "sandbox.firecracker":
		return validateInternalBaseURL(status.BaseURL)
	case "memory.openviking":
		if blankPtr(status.APIKeyValue) {
			return ProviderRuntimeStateMissingConfig, "missing_api_key"
		}
		return validateInternalBaseURL(status.BaseURL)
	default:
		if strings.TrimSpace(status.GroupName) == "image_understanding" && blankPtr(status.APIKeyValue) {
			return ProviderRuntimeStateMissingConfig, "missing_api_key"
		}
		return ProviderRuntimeStateReady, ""
	}
}

func validateOptionalBaseURL(baseURL *string) (ProviderRuntimeState, string) {
	if blankPtr(baseURL) {
		return ProviderRuntimeStateReady, ""
	}
	if _, err := sharedoutbound.DefaultPolicy().NormalizeBaseURL(strings.TrimSpace(*baseURL)); err != nil {
		return ProviderRuntimeStateInvalidConfig, "invalid_base_url"
	}
	return ProviderRuntimeStateReady, ""
}

func validateInternalBaseURL(baseURL *string) (ProviderRuntimeState, string) {
	if blankPtr(baseURL) {
		return ProviderRuntimeStateMissingConfig, "missing_base_url"
	}
	if _, err := sharedoutbound.DefaultPolicy().NormalizeInternalBaseURL(strings.TrimSpace(*baseURL)); err != nil {
		return ProviderRuntimeStateInvalidConfig, "invalid_base_url"
	}
	return ProviderRuntimeStateReady, ""
}

func blankPtr(value *string) bool {
	return value == nil || strings.TrimSpace(*value) == ""
}
