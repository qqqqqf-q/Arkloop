//go:build !desktop

package toolprovider

import (
	"context"
	"fmt"
	"time"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
	workerCrypto "arkloop/services/worker/internal/crypto"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

func LoadActiveUserProviders(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) ([]ActiveProviderConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return nil, nil
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user_id must not be empty")
	}
	statuses, err := sharedtoolruntime.LoadUserProviderStatuses(ctx, pool, userID, decryptPlatformProviderSecret)
	if err != nil {
		return nil, err
	}
	refreshXAIProviderOAuthStatuses(ctx, pool, statuses, encryptProviderSecret)
	return activeConfigsFromStatuses(statuses), nil
}

func LoadActivePlatformProviders(ctx context.Context, pool *pgxpool.Pool) ([]ActiveProviderConfig, error) {
	statuses, err := sharedtoolruntime.LoadPlatformProviderStatuses(ctx, pool, decryptPlatformProviderSecret)
	if err != nil {
		return nil, err
	}
	refreshXAIProviderOAuthStatuses(ctx, pool, statuses, encryptProviderSecret)
	return activeConfigsFromStatuses(statuses), nil
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

func decryptPlatformProviderSecret(ctx context.Context, encrypted string, keyVersion *int, providerName string) (*string, error) {
	_ = ctx
	if keyVersion == nil {
		return nil, fmt.Errorf("tool_provider_configs decrypt: missing key version for %s", providerName)
	}
	plainBytes, err := workerCrypto.DecryptGCM(encrypted)
	if err != nil {
		return nil, fmt.Errorf("tool_provider_configs decrypt: %w", err)
	}
	plaintext := string(plainBytes)
	return &plaintext, nil
}

func encryptProviderSecret(plaintext string) (string, int, error) {
	return workerCrypto.EncryptWithCurrentKey([]byte(plaintext))
}

func refreshXAIProviderOAuthStatuses(
	ctx context.Context,
	pool *pgxpool.Pool,
	statuses []sharedtoolruntime.ProviderRuntimeStatus,
	encrypt encryptProviderToken,
) {
	if pool == nil {
		return
	}
	refreshXAIProviderOAuthStatusesCore(ctx, statuses, encrypt, func(ctx context.Context, status sharedtoolruntime.ProviderRuntimeStatus, encrypted string, keyVersion int, expiresAt *time.Time) error {
		if status.OAuthTokenSecretID == nil {
			return fmt.Errorf("oauth token secret id is missing")
		}
		_, err := pool.Exec(ctx, `
UPDATE secrets
   SET encrypted_value = $2, key_version = $3, updated_at = now()
 WHERE id = $1`,
			*status.OAuthTokenSecretID, encrypted, keyVersion,
		)
		if err != nil {
			return err
		}
		_, err = pool.Exec(ctx, `
UPDATE tool_provider_oauth_connections
   SET expires_at = $2, updated_at = now()
 WHERE token_secret_id = $1`,
			*status.OAuthTokenSecretID, expiresAt,
		)
		return err
	})
}
