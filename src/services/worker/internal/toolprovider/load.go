//go:build !desktop

package toolprovider

import (
	"context"
	"fmt"

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
	return activeConfigsFromStatuses(statuses), nil
}

func LoadActivePlatformProviders(ctx context.Context, pool *pgxpool.Pool) ([]ActiveProviderConfig, error) {
	statuses, err := sharedtoolruntime.LoadPlatformProviderStatuses(ctx, pool, decryptPlatformProviderSecret)
	if err != nil {
		return nil, err
	}
	return activeConfigsFromStatuses(statuses), nil
}

func activeConfigsFromStatuses(statuses []sharedtoolruntime.ProviderRuntimeStatus) []ActiveProviderConfig {
	out := make([]ActiveProviderConfig, 0, len(statuses))
	for _, status := range statuses {
		if !status.Ready() {
			continue
		}
		out = append(out, ActiveProviderConfig{
			OwnerKind:    status.OwnerKind,
			GroupName:    status.GroupName,
			ProviderName: status.ProviderName,
			APIKeyValue:  status.APIKeyValue,
			KeyPrefix:    status.KeyPrefix,
			BaseURL:      status.BaseURL,
			ConfigJSON:   status.ConfigJSON,
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
