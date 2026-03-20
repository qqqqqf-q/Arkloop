//go:build !desktop

package catalogapi

import (
	"context"

	"arkloop/services/api/internal/data"

	"github.com/jackc/pgx/v5/pgxpool"
)

func notifyToolProviderChanged(ctx context.Context, directPool *pgxpool.Pool, pool data.DB, payload string) {
	if directPool != nil {
		_, _ = directPool.Exec(ctx, "SELECT pg_notify('tool_provider_config_changed', $1)", payload)
		return
	}
	if pool != nil {
		_, _ = pool.Exec(ctx, "SELECT pg_notify('tool_provider_config_changed', $1)", payload)
	}
}
