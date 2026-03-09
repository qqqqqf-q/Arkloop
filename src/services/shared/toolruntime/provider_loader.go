package toolruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ProviderSecretDecrypter func(ctx context.Context, encrypted string, keyVersion *int, providerName string) (*string, error)

func LoadPlatformProviders(ctx context.Context, pool *pgxpool.Pool, decrypt ProviderSecretDecrypter) ([]ProviderConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return nil, nil
	}

	rows, err := pool.Query(ctx, `
		SELECT c.group_name, c.provider_name, c.base_url,
		       s.encrypted_value, s.key_version
		FROM tool_provider_configs c
		LEFT JOIN secrets s ON s.id = c.secret_id AND s.scope = 'platform'
		WHERE c.scope = 'platform' AND c.is_active = TRUE
		ORDER BY c.updated_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("tool_provider_configs query: %w", err)
	}
	defer rows.Close()

	providers := []ProviderConfig{}
	for rows.Next() {
		var (
			groupName    string
			providerName string
			baseURL      *string
			encrypted    *string
			keyVersion   *int
		)
		if err := rows.Scan(&groupName, &providerName, &baseURL, &encrypted, &keyVersion); err != nil {
			return nil, fmt.Errorf("tool_provider_configs scan: %w", err)
		}

		var apiKeyValue *string
		if decrypt != nil && encrypted != nil && strings.TrimSpace(*encrypted) != "" {
			apiKeyValue, err = decrypt(ctx, *encrypted, keyVersion, providerName)
			if err != nil {
				return nil, err
			}
		}

		providers = append(providers, ProviderConfig{
			GroupName:    strings.TrimSpace(groupName),
			ProviderName: strings.TrimSpace(providerName),
			BaseURL:      baseURL,
			APIKeyValue:  apiKeyValue,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tool_provider_configs rows: %w", err)
	}
	return providers, nil
}
