package routing

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ConfigLoader struct {
	pool        *pgxpool.Pool
	fallback    ProviderRoutingConfig
	desktopLoad func(ctx context.Context) (ProviderRoutingConfig, error)
}

func NewConfigLoader(pool *pgxpool.Pool, fallback ProviderRoutingConfig) *ConfigLoader {
	return &ConfigLoader{pool: pool, fallback: fallback}
}

// NewDesktopSQLiteRoutingLoader 用于 Desktop：无 pgx pool 时从 SQLite 拉取路由；accountID 在 Load 中忽略。
func NewDesktopSQLiteRoutingLoader(load func(ctx context.Context) (ProviderRoutingConfig, error), fallback ProviderRoutingConfig) *ConfigLoader {
	return &ConfigLoader{pool: nil, fallback: fallback, desktopLoad: load}
}

func (l *ConfigLoader) Load(ctx context.Context, accountID *uuid.UUID) (ProviderRoutingConfig, error) {
	if l == nil {
		return ProviderRoutingConfig{}, nil
	}
	if l.pool != nil {
		loaded, err := LoadRoutingConfigFromDB(ctx, l.pool, accountID)
		if err != nil {
			slog.WarnContext(ctx, "routing: db load failed, using fallback", "err", err.Error())
		} else if len(loaded.Routes) > 0 {
			return loaded, nil
		} else {
			slog.WarnContext(ctx, "routing: db returned empty routes, using fallback")
		}
	} else if l.desktopLoad != nil {
		loaded, err := l.desktopLoad(ctx)
		if err != nil {
			slog.WarnContext(ctx, "routing: desktop sqlite load failed, using fallback", "err", err.Error())
		} else if len(loaded.Routes) > 0 {
			return loaded, nil
		} else {
			slog.WarnContext(ctx, "routing: desktop sqlite returned empty routes, using fallback")
		}
	}
	return l.fallback, nil
}
