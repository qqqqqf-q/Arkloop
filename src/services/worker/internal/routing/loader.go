package routing

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const desktopRoutingConfigCacheTTL = time.Second

type ConfigLoader struct {
	pool            *pgxpool.Pool
	fallback        ProviderRoutingConfig
	desktopLoad     func(ctx context.Context) (ProviderRoutingConfig, error)
	desktopCacheTTL time.Duration
	mu              sync.Mutex
	desktopCached   ProviderRoutingConfig
	desktopCachedAt time.Time
}

func NewConfigLoader(pool *pgxpool.Pool, fallback ProviderRoutingConfig) *ConfigLoader {
	return &ConfigLoader{pool: pool, fallback: fallback}
}

// NewDesktopSQLiteRoutingLoader 用于 Desktop：无 pgx pool 时从 SQLite 拉取路由；accountID 在 Load 中忽略。
func NewDesktopSQLiteRoutingLoader(load func(ctx context.Context) (ProviderRoutingConfig, error), fallback ProviderRoutingConfig) *ConfigLoader {
	return &ConfigLoader{pool: nil, fallback: fallback, desktopLoad: load, desktopCacheTTL: desktopRoutingConfigCacheTTL}
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
		if cached, ok := l.loadDesktopCached(); ok {
			return cached, nil
		}
		loaded, err := l.desktopLoad(ctx)
		if err != nil {
			slog.WarnContext(ctx, "routing: desktop sqlite load failed, using fallback", "err", err.Error())
		} else if len(loaded.Routes) > 0 {
			l.storeDesktopCached(loaded)
			return cloneProviderRoutingConfig(loaded), nil
		} else {
			slog.WarnContext(ctx, "routing: desktop sqlite returned empty routes, using fallback")
		}
	}
	return l.fallback, nil
}

func (l *ConfigLoader) loadDesktopCached() (ProviderRoutingConfig, bool) {
	if l.desktopCacheTTL <= 0 {
		return ProviderRoutingConfig{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.desktopCached.Routes) == 0 || time.Since(l.desktopCachedAt) >= l.desktopCacheTTL {
		return ProviderRoutingConfig{}, false
	}
	return cloneProviderRoutingConfig(l.desktopCached), true
}

func (l *ConfigLoader) storeDesktopCached(config ProviderRoutingConfig) {
	if l.desktopCacheTTL <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.desktopCached = cloneProviderRoutingConfig(config)
	l.desktopCachedAt = time.Now()
}

func cloneProviderRoutingConfig(config ProviderRoutingConfig) ProviderRoutingConfig {
	out := ProviderRoutingConfig{DefaultRouteID: config.DefaultRouteID}
	out.Credentials = append([]ProviderCredential(nil), config.Credentials...)
	out.Routes = append([]ProviderRouteRule(nil), config.Routes...)
	return out
}
