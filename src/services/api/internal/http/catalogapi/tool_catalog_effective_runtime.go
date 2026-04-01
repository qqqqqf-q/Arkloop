package catalogapi

import (
	"context"

	"arkloop/services/api/internal/data"

	sharedconfig "arkloop/services/shared/config"
	sharedtoolruntime "arkloop/services/shared/toolruntime"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
)

// *pgxpool.Pool 放进 data.DB 后，nil 指针仍是「非 nil 接口」，这里按真实连接判断。
func effectiveCatalogPoolReady(pool data.DB) bool {
	if pool == nil {
		return false
	}
	if p, ok := pool.(*pgxpool.Pool); ok {
		return p != nil
	}
	return true
}

func buildEffectiveBuiltinToolNameSet(
	ctx context.Context,
	pool data.DB,
	userID uuid.UUID,
	artifactStoreAvailable bool,
) map[string]struct{} {
	return buildEffectiveBuiltinToolNameSetForScope(ctx, pool, "user", &userID, artifactStoreAvailable)
}

func buildEffectiveBuiltinToolNameSetForScope(
	ctx context.Context,
	pool data.DB,
	ownerKind string,
	ownerUserID *uuid.UUID,
	artifactStoreAvailable bool,
) map[string]struct{} {
	snapshot, err := buildEffectiveRuntimeSnapshotForScope(ctx, pool, ownerKind, ownerUserID, artifactStoreAvailable)
	if err != nil {
		slog.WarnContext(ctx, "effective tool catalog: runtime snapshot build failed", "err", err.Error())
		return map[string]struct{}{}
	}
	return snapshot.BuiltinToolNameSet()
}

func buildEffectiveRuntimeSnapshotForScope(
	ctx context.Context,
	pool data.DB,
	ownerKind string,
	ownerUserID *uuid.UUID,
	artifactStoreAvailable bool,
) (sharedtoolruntime.RuntimeSnapshot, error) {
	var configStore sharedconfig.Store
	if effectiveCatalogPoolReady(pool) {
		configStore = sharedconfig.NewPGXStoreQuerier(pool)
	}
	resolver, _ := sharedconfig.NewResolver(
		sharedconfig.DefaultRegistry(),
		configStore,
		nil,
		0,
	)

	return sharedtoolruntime.BuildRuntimeSnapshot(ctx, sharedtoolruntime.SnapshotInput{
		ConfigResolver:         resolver,
		HasConversationSearch:  effectiveCatalogPoolReady(pool),
		ArtifactStoreAvailable: artifactStoreAvailable,
		LoadPlatformProviders: func(loadCtx context.Context) ([]sharedtoolruntime.ProviderConfig, error) {
			if pool == nil {
				return nil, nil
			}
			return loadEffectiveBuiltinProviders(loadCtx, pool, ownerKind, ownerUserID, toolProviderSecretDecrypter())
		},
	})
}
