package routing

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ConfigLoader struct {
	pool     *pgxpool.Pool
	fallback ProviderRoutingConfig
}

func NewConfigLoader(pool *pgxpool.Pool, fallback ProviderRoutingConfig) *ConfigLoader {
	return &ConfigLoader{pool: pool, fallback: fallback}
}

func (l *ConfigLoader) Load(ctx context.Context, projectID *uuid.UUID, accountID *uuid.UUID) (ProviderRoutingConfig, error) {
	if l == nil {
		return ProviderRoutingConfig{}, nil
	}
	if l.pool != nil {
		loaded, err := LoadRoutingConfigFromDB(ctx, l.pool, projectID, accountID)
		if err != nil {
			slog.WarnContext(ctx, "routing: db load failed, using fallback", "err", err.Error())
		} else if len(loaded.Routes) > 0 {
			return loaded, nil
		} else {
			slog.WarnContext(ctx, "routing: db returned empty routes, using fallback")
		}
	}
	return l.fallback, nil
}
