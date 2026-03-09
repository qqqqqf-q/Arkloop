package routing

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ConfigLoader struct {
	pool     *pgxpool.Pool
	fallback ProviderRoutingConfig
}

func NewConfigLoader(pool *pgxpool.Pool, fallback ProviderRoutingConfig) *ConfigLoader {
	return &ConfigLoader{pool: pool, fallback: fallback}
}

func (l *ConfigLoader) Load(ctx context.Context) (ProviderRoutingConfig, error) {
	if l == nil {
		return ProviderRoutingConfig{}, nil
	}
	if l.pool != nil {
		loaded, err := LoadRoutingConfigFromDB(ctx, l.pool)
		if err != nil {
			slog.WarnContext(ctx, "routing: db load failed, using fallback", "err", err.Error())
		} else if len(loaded.Routes) > 0 {
			return loaded, nil
		}
	}
	return l.fallback, nil
}
