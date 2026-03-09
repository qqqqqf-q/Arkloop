package http

import (
	"context"
	"fmt"
	"os"
	"strings"

	apicrypto "arkloop/services/api/internal/crypto"
	sharedtoolruntime "arkloop/services/shared/toolruntime"

	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
)

func buildEffectiveBuiltinToolNameSet(
	ctx context.Context,
	pool *pgxpool.Pool,
	artifactStoreAvailable bool,
) map[string]struct{} {
	providers, err := loadEffectivePlatformProviders(ctx, pool)
	if err != nil {
		slog.WarnContext(ctx, "effective tool catalog: platform provider load failed", "err", err.Error())
	}
	browserEnabled := resolveEffectiveBrowserEnabled(ctx, pool)
	resolved := sharedtoolruntime.ResolveBuiltin(sharedtoolruntime.ResolveInput{
		HasConversationSearch:  pool != nil,
		ArtifactStoreAvailable: artifactStoreAvailable,
		BrowserEnabled:         browserEnabled,
		Env: sharedtoolruntime.EnvConfig{
			SandboxBaseURL:   strings.TrimSpace(os.Getenv("ARKLOOP_SANDBOX_BASE_URL")),
			MemoryBaseURL:    strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_BASE_URL")),
			MemoryRootAPIKey: strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_ROOT_API_KEY")),
		},
		PlatformProviders: providers,
	})
	return resolved.ToolNameSet()
}

func resolveEffectiveBrowserEnabled(ctx context.Context, pool *pgxpool.Pool) bool {
	if raw, ok := os.LookupEnv("ARKLOOP_BROWSER_ENABLED"); ok {
		switch strings.TrimSpace(strings.ToLower(raw)) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	}
	if pool == nil {
		return false
	}
	var value string
	err := pool.QueryRow(ctx, `SELECT value FROM platform_settings WHERE key = $1`, "browser.enabled").Scan(&value)
	if err != nil {
		return false
	}
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func loadEffectivePlatformProviders(ctx context.Context, pool *pgxpool.Pool) ([]sharedtoolruntime.ProviderConfig, error) {
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

	var keyRing *apicrypto.KeyRing
	providers := []sharedtoolruntime.ProviderConfig{}
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
		if encrypted != nil && strings.TrimSpace(*encrypted) != "" {
			if keyVersion == nil {
				return nil, fmt.Errorf("tool_provider_configs decrypt: missing key version for %s", providerName)
			}
			if keyRing == nil {
				keyRing, err = apicrypto.NewKeyRingFromEnv()
				if err != nil {
					return nil, fmt.Errorf("tool_provider_configs decrypt: %w", err)
				}
			}
			plaintext, err := keyRing.Decrypt(*encrypted, *keyVersion)
			if err != nil {
				return nil, fmt.Errorf("tool_provider_configs decrypt: %w", err)
			}
			value := string(plaintext)
			apiKeyValue = &value
		}

		providers = append(providers, sharedtoolruntime.ProviderConfig{
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
