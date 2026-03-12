package toolprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
	workerCrypto "arkloop/services/worker/internal/crypto"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ActiveProviderConfig struct {
	OwnerKind    string
	GroupName    string
	ProviderName string
	APIKeyValue  *string
	KeyPrefix    *string
	BaseURL      *string
	ConfigJSON   map[string]any
}

func LoadActiveUserProviders(ctx context.Context, pool *pgxpool.Pool, projectID uuid.UUID) ([]ActiveProviderConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return nil, nil
	}
	if projectID == uuid.Nil {
		return nil, fmt.Errorf("project_id must not be empty")
	}

	rows, err := pool.Query(ctx, `
		SELECT c.owner_kind, c.group_name, c.provider_name, c.key_prefix, c.base_url, c.config_json,
		       s.encrypted_value, s.key_version
		FROM tool_provider_configs c
		LEFT JOIN secrets s ON s.id = c.secret_id
		WHERE c.owner_kind = 'user' AND c.project_id = $1 AND c.is_active = TRUE
		ORDER BY c.updated_at DESC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("tool_provider_configs query: %w", err)
	}
	defer rows.Close()

	out := []ActiveProviderConfig{}
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
		_ = keyVersion

		cfg := ActiveProviderConfig{
			OwnerKind:    strings.TrimSpace(ownerKind),
			GroupName:    strings.TrimSpace(groupName),
			ProviderName: strings.TrimSpace(providerName),
			KeyPrefix:    keyPrefix,
			BaseURL:      baseURL,
			ConfigJSON:   map[string]any{},
		}

		if len(configJSON) > 0 {
			_ = json.Unmarshal(configJSON, &cfg.ConfigJSON)
		}

		if encrypted != nil && strings.TrimSpace(*encrypted) != "" {
			plainBytes, err := workerCrypto.DecryptGCM(*encrypted)
			if err != nil {
				return nil, fmt.Errorf("tool_provider_configs decrypt: %w", err)
			}
			plaintext := string(plainBytes)
			cfg.APIKeyValue = &plaintext
		}

		out = append(out, cfg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tool_provider_configs rows: %w", err)
	}

	return out, nil
}

func LoadActivePlatformProviders(ctx context.Context, pool *pgxpool.Pool) ([]ActiveProviderConfig, error) {
	providers, err := sharedtoolruntime.LoadPlatformProviders(ctx, pool, decryptPlatformProviderSecret)
	if err != nil {
		return nil, err
	}
	out := make([]ActiveProviderConfig, 0, len(providers))
	for _, provider := range providers {
		out = append(out, ActiveProviderConfig{
			OwnerKind:    "platform",
			GroupName:    provider.GroupName,
			ProviderName: provider.ProviderName,
			APIKeyValue:  provider.APIKeyValue,
			BaseURL:      provider.BaseURL,
			ConfigJSON:   map[string]any{},
		})
	}
	return out, nil
}

func decryptPlatformProviderSecret(ctx context.Context, encrypted string, keyVersion *int, providerName string) (*string, error) {
	_ = ctx
	_ = keyVersion
	_ = providerName
	plainBytes, err := workerCrypto.DecryptGCM(encrypted)
	if err != nil {
		return nil, fmt.Errorf("tool_provider_configs decrypt: %w", err)
	}
	plaintext := string(plainBytes)
	return &plaintext, nil
}
