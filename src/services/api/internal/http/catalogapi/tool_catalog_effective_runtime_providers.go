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
	ownerKind string,
	ownerUserID *uuid.UUID,
	decrypt sharedtoolruntime.ProviderSecretDecrypter,
) ([]sharedtoolruntime.ProviderConfig, error) {
	platformStatuses, err := sharedtoolruntime.LoadPlatformProviderStatuses(ctx, pool, decrypt)
	if err != nil {
		return nil, err
	}
	providers := sharedtoolruntime.ReadyProvidersFromStatuses(platformStatuses)
	if ownerKind == "user" && ownerUserID != nil {
		userStatuses, err := sharedtoolruntime.LoadUserProviderStatuses(ctx, pool, *ownerUserID, decrypt)
		if err != nil {
			return nil, err
		}
		providers = append(providers, sharedtoolruntime.ReadyProvidersFromStatuses(userStatuses)...)
	}
	return providers, nil
}
