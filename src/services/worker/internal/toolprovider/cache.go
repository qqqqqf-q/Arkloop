package toolprovider

import (
	"context"
	"log/slog"
	"strings"
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

func (c *Cache) Get(ctx context.Context, pool *pgxpool.Pool, projectID uuid.UUID) ([]ActiveProviderConfig, error) {
	return c.GetUser(ctx, pool, projectID)
}

func (c *Cache) GetUser(ctx context.Context, pool *pgxpool.Pool, projectID uuid.UUID) ([]ActiveProviderConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil {
		return LoadActiveUserProviders(ctx, pool, projectID)
	}

	if c.ttl > 0 {
		if raw, ok := c.entries.Load(projectID.String()); ok {
			entry := raw.(cacheEntry)
			if time.Since(entry.cachedAt) < c.ttl {
				return entry.providers, nil
			}
		}
	}

	providers, err := LoadActiveUserProviders(ctx, pool, projectID)
	if err != nil {
		return nil, err
	}

	if c.ttl > 0 {
		c.entries.Store(projectID.String(), cacheEntry{
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

func (c *Cache) Invalidate(projectID uuid.UUID) {
	c.InvalidateUser(projectID)
}

func (c *Cache) InvalidateUser(projectID uuid.UUID) {
	if c == nil {
		return
	}
	c.entries.Delete(projectID.String())
}

func (c *Cache) InvalidatePlatform() {
	if c == nil {
		return
	}
	c.entries.Delete(platformCacheKey)
}

func (c *Cache) StartInvalidationListener(ctx context.Context, directPool *pgxpool.Pool) {
	if c == nil || directPool == nil || c.ttl <= 0 {
		return
	}
	go c.runInvalidationListener(ctx, directPool)
}

func (c *Cache) runInvalidationListener(ctx context.Context, directPool *pgxpool.Pool) {
	const (
		baseDelay = 1 * time.Second
		maxDelay  = 30 * time.Second
	)
	delay := baseDelay

	for {
		if ctx.Err() != nil {
			return
		}

		err := c.listenOnce(ctx, directPool)
		if ctx.Err() != nil {
			return
		}

		slog.WarnContext(ctx, "tool provider cache: LISTEN connection lost, retrying", "err", err, "delay", delay)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}

func (c *Cache) listenOnce(ctx context.Context, directPool *pgxpool.Pool) error {
	conn, err := directPool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN tool_provider_config_changed"); err != nil {
		return err
	}

	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}

		payload := strings.TrimSpace(n.Payload)
		if payload == "" {
			continue
		}
		if payload == platformCacheKey {
			c.InvalidatePlatform()
			continue
		}

		projectID, err := uuid.Parse(payload)
		if err != nil {
			continue
		}
		c.InvalidateUser(projectID)
	}
}
