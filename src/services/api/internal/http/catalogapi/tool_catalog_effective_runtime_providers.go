//go:build !desktop

package catalogapi

import (
	"context"

	"arkloop/services/api/internal/data"
	sharedtoolruntime "arkloop/services/shared/toolruntime"

	"github.com/google/uuid"
)

func loadEffectiveBuiltinProviders(
	ctx context.Context,
	pool data.DB,
	userID uuid.UUID,
	decrypt sharedtoolruntime.ProviderSecretDecrypter,
) ([]sharedtoolruntime.ProviderConfig, error) {
	platformStatuses, err := sharedtoolruntime.LoadPlatformProviderStatuses(ctx, pool, decrypt)
	if err != nil {
		return nil, err
	}
	userStatuses, err := sharedtoolruntime.LoadUserProviderStatuses(ctx, pool, userID, decrypt)
	if err != nil {
		return nil, err
	}
	providers := sharedtoolruntime.ReadyProvidersFromStatuses(platformStatuses)
	providers = append(providers, sharedtoolruntime.ReadyProvidersFromStatuses(userStatuses)...)
	return providers, nil
}
