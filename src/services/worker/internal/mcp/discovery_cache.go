package mcp

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type cacheEntry struct {
	registration Registration
	cachedAt     time.Time
}

// DiscoveryCache 按 accountID 缓存 DiscoverFromDB 的结果。
// 缓存在 Worker 进程内全局有效，TTL 到期后下次访问触发回源。
type DiscoveryCache struct {
	entries sync.Map
	ttl     time.Duration
	mcpPool *Pool
}

// NewDiscoveryCache 创建缓存实例。ttl 为 0 时退化为不缓存（每次回源）。
func NewDiscoveryCache(ttl time.Duration, mcpPool *Pool) *DiscoveryCache {
	return &DiscoveryCache{
		ttl:     ttl,
		mcpPool: mcpPool,
	}
}

// Get 返回 accountID 对应的 MCP Registration。
// 缓存命中且未过期时直接返回，否则调 DiscoverFromDB 并回填缓存。
func (c *DiscoveryCache) Get(ctx context.Context, pool *pgxpool.Pool, accountID uuid.UUID) (Registration, error) {
	if c.ttl > 0 {
		if raw, ok := c.entries.Load(accountID.String()); ok {
			entry := raw.(cacheEntry)
			if time.Since(entry.cachedAt) < c.ttl {
				return entry.registration, nil
			}
		}
	}

	reg, err := DiscoverFromDB(ctx, pool, accountID, c.mcpPool)
	if err != nil {
		return Registration{}, err
	}

	if c.ttl > 0 {
		c.entries.Store(accountID.String(), cacheEntry{
			registration: reg,
			cachedAt:     time.Now(),
		})
	}

	return reg, nil
}

// Invalidate 删除指定 account 的缓存条目。
func (c *DiscoveryCache) Invalidate(accountID uuid.UUID) {
	c.entries.Delete(accountID.String())
}

// StartInvalidationListener 启动后台 goroutine，LISTEN mcp_config_changed，
// 收到通知后按 payload（accountID 字符串）主动失效对应缓存条目。
// directPool 必须是直连（不经 PgBouncer），否则 LISTEN 将失效。
// 连接断开时自动重试，ctx 取消时退出。
func (c *DiscoveryCache) StartInvalidationListener(ctx context.Context, directPool *pgxpool.Pool) {
	if c == nil || directPool == nil || c.ttl <= 0 {
		return
	}
	go c.runInvalidationListener(ctx, directPool)
}

func (c *DiscoveryCache) runInvalidationListener(ctx context.Context, directPool *pgxpool.Pool) {
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
			// 正常关闭，不打印错误
			return
		}

		slog.WarnContext(ctx, "mcp cache: LISTEN connection lost, retrying", "err", err, "delay", delay)

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

// listenOnce 持有一条连接执行 LISTEN，直到 ctx 取消或连接断开。
// 返回 nil 仅当 ctx 取消；连接断开时返回非 nil error。
func (c *DiscoveryCache) listenOnce(ctx context.Context, directPool *pgxpool.Pool) error {
	conn, err := directPool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN mcp_config_changed"); err != nil {
		return err
	}

	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		accountID, err := uuid.Parse(n.Payload)
		if err != nil {
			continue
		}
		c.Invalidate(accountID)
	}
}

// store 预填缓存条目，仅供测试使用。
func (c *DiscoveryCache) store(accountID uuid.UUID, reg Registration) {
	c.entries.Store(accountID.String(), cacheEntry{
		registration: reg,
		cachedAt:     time.Now(),
	})
}
