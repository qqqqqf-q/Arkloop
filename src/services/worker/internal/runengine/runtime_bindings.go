//go:build !desktop

package runengine

import (
	"context"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/environmentbindings"
	"github.com/jackc/pgx/v5/pgxpool"
)

func resolveAndPersistEnvironmentBindings(ctx context.Context, pool *pgxpool.Pool, run data.Run) (data.Run, error) {
	return environmentbindings.ResolveAndPersistRun(ctx, pool, run)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
