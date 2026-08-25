package toolprovider

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const platformCacheKey = "platform"

type cacheEntry struct {
	providers []ActiveProviderConfig
	cachedAt  time.Time
}

type Cache struct {
	entries sync.Map
	ttl     time.Duration
}

func NewCache(ttl time.Duration) *Cache {
	return &Cache{ttl: ttl}
}

func (c *Cache) Get(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) ([]ActiveProviderConfig, error) {
	return c.GetUser(ctx, pool, userID)
}

func (c *Cache) GetUser(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) ([]ActiveProviderConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil {
		return LoadActiveUserProviders(ctx, pool, userID)
	}

	if c.ttl > 0 {
		if raw, ok := c.entries.Load(userID.String()); ok {
			entry := raw.(cacheEntry)
			if time.Since(entry.cachedAt) < c.ttl {
				return entry.providers, nil
			}
		}
	}

	providers, err := LoadActiveUserProviders(ctx, pool, userID)
	if err != nil {
		return nil, err
	}

	if c.ttl > 0 {
		c.entries.Store(userID.String(), cacheEntry{
			providers: providers,
			cachedAt:  time.Now(),
		})
	}

	return providers, nil
}

func (c *Cache) GetPlatform(ctx context.Context, pool *pgxpool.Pool) ([]ActiveProviderConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil {
		return LoadActivePlatformProviders(ctx, pool)
	}

	if c.ttl > 0 {
		if raw, ok := c.entries.Load(platformCacheKey); ok {
			entry := raw.(cacheEntry)
			if time.Since(entry.cachedAt) < c.ttl {
				return entry.providers, nil
			}
		}
	}

	providers, err := LoadActivePlatformProviders(ctx, pool)
	if err != nil {
		return nil, err
	}

	if c.ttl > 0 {
		c.entries.Store(platformCacheKey, cacheEntry{
			providers: providers,
			cachedAt:  time.Now(),
		})
	}

	return providers, nil
}

func (c *Cache) Invalidate(userID uuid.UUID) {
	c.InvalidateUser(userID)
}

func (c *Cache) InvalidateUser(userID uuid.UUID) {
	if c == nil {
		return
	}
	c.entries.Delete(userID.String())
}

func (c *Cache) InvalidatePlatform() {
	if c == nil {
		return
	}
	c.entries.Delete(platformCacheKey)
}
