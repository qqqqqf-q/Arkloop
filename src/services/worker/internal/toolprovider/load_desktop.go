//go:build desktop

package toolprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
	workerCrypto "arkloop/services/worker/internal/crypto"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ActiveProviderConfig matches SaaS toolprovider for RunContext injection.
type ActiveProviderConfig struct {
	OwnerKind    string
	GroupName    string
	ProviderName string
	APIKeyValue  *string
	KeyPrefix    *string
	BaseURL      *string
	ConfigJSON   map[string]any
}

func ToRuntimeProviderConfig(cfg ActiveProviderConfig) sharedtoolruntime.ProviderConfig {
	return sharedtoolruntime.ProviderConfig{
		GroupName:    strings.TrimSpace(cfg.GroupName),
		ProviderName: strings.TrimSpace(cfg.ProviderName),
		BaseURL:      cfg.BaseURL,
		APIKeyValue:  cfg.APIKeyValue,
		ConfigJSON:   copyJSONMapDesktop(cfg.ConfigJSON),
	}
}

func copyJSONMapDesktop(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func LoadActivePlatformProviders(ctx context.Context, pool *pgxpool.Pool) ([]ActiveProviderConfig, error) {
	_ = ctx
	_ = pool
	return nil, nil
}

func LoadActiveUserProviders(_ context.Context, _ *pgxpool.Pool, _ uuid.UUID) ([]ActiveProviderConfig, error) {
	return nil, nil
}

// LoadDesktopActiveToolProviders returns active rows from SQLite for platform then optional user scope.
func LoadDesktopActiveToolProviders(ctx context.Context, db data.DesktopDB, userID *uuid.UUID) (platform []ActiveProviderConfig, user []ActiveProviderConfig, err error) {
	if db == nil {
		return nil, nil, nil
	}
	platform, err = loadDesktopToolProviderRows(ctx, db, "platform", nil)
	if err != nil {
		return nil, nil, err
	}
	if userID != nil && *userID != uuid.Nil {
		user, err = loadDesktopToolProviderRows(ctx, db, "user", userID)
		if err != nil {
			return nil, nil, err
		}
	}
	return platform, user, nil
}

func loadDesktopToolProviderRows(ctx context.Context, db data.DesktopDB, ownerKind string, ownerUserID *uuid.UUID) ([]ActiveProviderConfig, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if ownerKind == "platform" {
		rows, err = db.Query(ctx, `
			SELECT c.group_name, c.provider_name, c.key_prefix, c.base_url, c.config_json,
			       s.encrypted_value, s.key_version
			FROM tool_provider_configs c
			LEFT JOIN secrets s ON s.id = c.secret_id
			WHERE c.owner_kind = 'platform' AND c.is_active = 1
			ORDER BY c.updated_at DESC`,
		)
	} else {
		if ownerUserID == nil {
			return nil, nil
		}
		rows, err = db.Query(ctx, `
			SELECT c.group_name, c.provider_name, c.key_prefix, c.base_url, c.config_json,
			       s.encrypted_value, s.key_version
			FROM tool_provider_configs c
			LEFT JOIN secrets s ON s.id = c.secret_id
			WHERE c.owner_kind = 'user' AND c.owner_user_id = $1 AND c.is_active = 1
			ORDER BY c.updated_at DESC`,
			ownerUserID.String(),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("tool_provider_configs query: %w", err)
	}
	defer rows.Close()

	out := []ActiveProviderConfig{}
	for rows.Next() {
		var (
			groupName, providerName string
			keyPrefix, baseURL      *string
			configJSONBytes         []byte
			encrypted               *string
			keyVersion              *int
		)
		if err := rows.Scan(&groupName, &providerName, &keyPrefix, &baseURL, &configJSONBytes, &encrypted, &keyVersion); err != nil {
			return nil, fmt.Errorf("tool_provider_configs scan: %w", err)
		}
		_ = keyVersion
		cfg := ActiveProviderConfig{
			OwnerKind:    ownerKind,
			GroupName:    strings.TrimSpace(groupName),
			ProviderName: strings.TrimSpace(providerName),
			KeyPrefix:    keyPrefix,
			BaseURL:      baseURL,
			ConfigJSON:   map[string]any{},
		}
		if len(configJSONBytes) > 0 && string(configJSONBytes) != "{}" {
			_ = json.Unmarshal(configJSONBytes, &cfg.ConfigJSON)
		}
		if encrypted != nil && strings.TrimSpace(*encrypted) != "" {
			plainBytes, decErr := workerCrypto.DecryptGCM(*encrypted)
			if decErr != nil {
				return nil, fmt.Errorf("tool_provider_configs decrypt: %w", decErr)
			}
			t := string(plainBytes)
			cfg.APIKeyValue = &t
		}
		out = append(out, cfg)
	}
	return out, rows.Err()
}
