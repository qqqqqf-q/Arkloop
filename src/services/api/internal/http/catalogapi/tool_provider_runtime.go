package catalogapi

import (
	"context"

	"arkloop/services/api/internal/data"
	sharedtoolruntime "arkloop/services/shared/toolruntime"

	"github.com/google/uuid"
)

func loadToolProviderRuntimeStatusMap(
	ctx context.Context,
	pool data.DB,
	ownerKind string,
	ownerUserID *uuid.UUID,
) (map[string]sharedtoolruntime.ProviderRuntimeStatus, error) {
	out := map[string]sharedtoolruntime.ProviderRuntimeStatus{}
	if pool == nil {
		return out, nil
	}

	var (
		statuses []sharedtoolruntime.ProviderRuntimeStatus
		err      error
	)
	switch ownerKind {
	case "platform":
		statuses, err = sharedtoolruntime.LoadPlatformProviderStatuses(ctx, pool, toolProviderSecretDecrypter())
	case "user":
		if ownerUserID == nil {
			return out, nil
		}
		statuses, err = sharedtoolruntime.LoadUserProviderStatuses(ctx, pool, *ownerUserID, toolProviderSecretDecrypter())
	default:
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, status := range statuses {
		out[status.ProviderName] = status
	}
	return out, nil
}
