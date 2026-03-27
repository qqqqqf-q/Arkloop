package catalogapi

import (
	"context"
	"fmt"

	"arkloop/services/api/internal/data"

	apicrypto "arkloop/services/api/internal/crypto"
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

	snapshot, err := sharedtoolruntime.BuildRuntimeSnapshot(ctx, sharedtoolruntime.SnapshotInput{
		ConfigResolver:         resolver,
		HasConversationSearch:  effectiveCatalogPoolReady(pool),
		ArtifactStoreAvailable: artifactStoreAvailable,
		LoadPlatformProviders: func(loadCtx context.Context) ([]sharedtoolruntime.ProviderConfig, error) {
			if pool == nil {
				return nil, nil
			}
			platformProviders, err := sharedtoolruntime.LoadPlatformProviders(loadCtx, pool, decryptPlatformProviderSecret)
			if err != nil {
				return nil, err
			}
			userProviders, err := sharedtoolruntime.LoadUserProviders(loadCtx, pool, userID, decryptPlatformProviderSecret)
			if err != nil {
				return nil, err
			}
			return append(platformProviders, userProviders...), nil
		},
	})
	if err != nil {
		slog.WarnContext(ctx, "effective tool catalog: runtime snapshot build failed", "err", err.Error())
		return map[string]struct{}{}
	}
	return snapshot.BuiltinToolNameSet()
}

func decryptPlatformProviderSecret(ctx context.Context, encrypted string, keyVersion *int, providerName string) (*string, error) {
	_ = ctx
	if keyVersion == nil {
		return nil, fmt.Errorf("tool_provider_configs decrypt: missing key version for %s", providerName)
	}
	keyRing, err := apicrypto.NewKeyRingFromEnv()
	if err != nil {
		return nil, fmt.Errorf("tool_provider_configs decrypt: %w", err)
	}
	plaintext, err := keyRing.Decrypt(encrypted, *keyVersion)
	if err != nil {
		return nil, fmt.Errorf("tool_provider_configs decrypt: %w", err)
	}
	value := string(plaintext)
	return &value, nil
}
