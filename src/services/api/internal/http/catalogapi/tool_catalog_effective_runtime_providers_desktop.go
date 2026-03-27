//go:build desktop

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
	_ uuid.UUID,
	decrypt sharedtoolruntime.ProviderSecretDecrypter,
) ([]sharedtoolruntime.ProviderConfig, error) {
	platformStatuses, err := sharedtoolruntime.LoadPlatformProviderStatuses(ctx, pool, decrypt)
	if err != nil {
		return nil, err
	}
	return sharedtoolruntime.ReadyProvidersFromStatuses(platformStatuses), nil
}
