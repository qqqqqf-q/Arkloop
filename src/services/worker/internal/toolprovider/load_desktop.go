//go:build desktop

package toolprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/desktop"
	sharedencryption "arkloop/services/shared/encryption"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ActiveProviderConfig matches SaaS toolprovider for RunContext injection.
type ActiveProviderConfig struct {
	OwnerKind          string
	OwnerUserID        *uuid.UUID
	GroupName          string
	ProviderName       string
	APIKeyValue        *string
	OAuthValue         *string
	OAuthTokenSecretID *uuid.UUID
	OAuthClientID      *string
	OAuthScope         *string
	OAuthExpiresAt     *time.Time
	KeyPrefix          *string
	BaseURL            *string
	ConfigJSON         map[string]any
}

func ToRuntimeProviderConfig(cfg ActiveProviderConfig) sharedtoolruntime.ProviderConfig {
	return sharedtoolruntime.ProviderConfig{
		GroupName:    strings.TrimSpace(cfg.GroupName),
		ProviderName: strings.TrimSpace(cfg.ProviderName),
		BaseURL:      cfg.BaseURL,
		APIKeyValue:  cfg.APIKeyValue,
		OAuthValue:   cfg.OAuthValue,
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

func activeConfigsFromStatuses(statuses []sharedtoolruntime.ProviderRuntimeStatus) []ActiveProviderConfig {
	out := make([]ActiveProviderConfig, 0, len(statuses))
	for _, status := range statuses {
		if !status.Ready() {
			continue
		}
		out = append(out, ActiveProviderConfig{
			OwnerKind:          status.OwnerKind,
			OwnerUserID:        status.OwnerUserID,
			GroupName:          status.GroupName,
			ProviderName:       status.ProviderName,
			APIKeyValue:        status.APIKeyValue,
			OAuthValue:         status.OAuthValue,
			OAuthTokenSecretID: status.OAuthTokenSecretID,
			OAuthClientID:      status.OAuthClientID,
			OAuthScope:         status.OAuthScope,
			OAuthExpiresAt:     status.OAuthExpiresAt,
			KeyPrefix:          status.KeyPrefix,
			BaseURL:            status.BaseURL,
			ConfigJSON:         status.ConfigJSON,
		})
	}
	return out
}

// LoadDesktopActiveToolProviders returns active platform rows from SQLite.
func LoadDesktopActiveToolProviders(ctx context.Context, db data.DesktopDB) ([]ActiveProviderConfig, error) {
	if db == nil {
		return nil, nil
	}

	var keyRing *sharedencryption.KeyRing
	decrypt := func(_ context.Context, encrypted string, keyVersion *int, _ string) (*string, error) {
		if keyVersion == nil {
			return nil, fmt.Errorf("missing key version")
		}
		if keyRing == nil {
			ring, err := desktop.LoadEncryptionKeyRing(desktop.KeyRingOptions{})
			if err != nil {
				return nil, err
			}
			keyRing = ring
		}
		plain, err := keyRing.Decrypt(encrypted, *keyVersion)
		if err != nil {
			return nil, err
		}
		value := string(plain)
		return &value, nil
	}

	platformStatuses, err := sharedtoolruntime.LoadPlatformProviderStatuses(ctx, db, decrypt)
	if err != nil {
		return nil, err
	}
	refreshXAIProviderOAuthStatuses(ctx, db, platformStatuses, func(plaintext string) (string, int, error) {
		if keyRing == nil {
			ring, err := desktop.LoadEncryptionKeyRing(desktop.KeyRingOptions{})
			if err != nil {
				return "", 0, err
			}
			keyRing = ring
		}
		return keyRing.Encrypt([]byte(plaintext))
	})
	return activeConfigsFromStatuses(platformStatuses), nil
}

func refreshXAIProviderOAuthStatuses(
	ctx context.Context,
	db data.DesktopDB,
	statuses []sharedtoolruntime.ProviderRuntimeStatus,
	encrypt encryptProviderToken,
) {
	if db == nil {
		return
	}
	refreshXAIProviderOAuthStatusesCore(ctx, statuses, encrypt, func(ctx context.Context, status sharedtoolruntime.ProviderRuntimeStatus, encrypted string, keyVersion int, expiresAt *time.Time) error {
		if status.OAuthTokenSecretID == nil {
			return fmt.Errorf("oauth token secret id is missing")
		}
		if _, err := db.Exec(ctx, `
UPDATE secrets
   SET encrypted_value = $2, key_version = $3, updated_at = CURRENT_TIMESTAMP
 WHERE id = $1`,
			status.OAuthTokenSecretID.String(), encrypted, keyVersion,
		); err != nil {
			return err
		}
		_, err := db.Exec(ctx, `
UPDATE tool_provider_oauth_connections
   SET expires_at = $2, updated_at = CURRENT_TIMESTAMP
 WHERE token_secret_id = $1`,
			status.OAuthTokenSecretID.String(), expiresAt,
		)
		return err
	})
}
