//go:build desktop

package catalogapi

import (
	"context"

	"arkloop/services/api/internal/data"

	"github.com/jackc/pgx/v5/pgxpool"
)

func notifyToolProviderChanged(_ context.Context, _ *pgxpool.Pool, _ data.DB, _ string) {
}
