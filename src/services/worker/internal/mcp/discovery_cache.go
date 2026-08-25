package mcp

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type cacheEntry struct {
	registration Registration
	cachedAt     time.Time
}

type CacheResult struct {
	Hit bool
	TTL time.Duration
}

func discoveryCacheKey(accountID uuid.UUID, profileRef string, workspaceRef string) string {
	return accountID.String() + "|" + strings.TrimSpace(profileRef) + "|" + strings.TrimSpace(workspaceRef)
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
func (c *DiscoveryCache) Get(ctx context.Context, pool DiscoveryQueryer, accountID uuid.UUID, profileRef string, workspaceRef string) (Registration, error) {
	reg, _, _, err := c.GetWithMeta(ctx, pool, accountID, profileRef, workspaceRef)
	return reg, err
}

func (c *DiscoveryCache) GetWithMeta(ctx context.Context, pool DiscoveryQueryer, accountID uuid.UUID, profileRef string, workspaceRef string) (Registration, CacheResult, DiscoverDiagnostics, error) {
	cacheKey := discoveryCacheKey(accountID, profileRef, workspaceRef)
	meta := CacheResult{TTL: c.ttl}
	if c.ttl > 0 {
		if raw, ok := c.entries.Load(cacheKey); ok {
			entry := raw.(cacheEntry)
			if time.Since(entry.cachedAt) < c.ttl {
				meta.Hit = true
				return entry.registration, meta, DiscoverDiagnostics{}, nil
			}
		}
	}

	reg, diag, err := DiscoverFromDBWithDiagnostics(ctx, pool, accountID, profileRef, workspaceRef, c.mcpPool)
	if err != nil {
		return Registration{}, meta, diag, err
	}

	if c.ttl > 0 {
		c.entries.Store(cacheKey, cacheEntry{
			registration: reg,
			cachedAt:     time.Now(),
		})
	}

	return reg, meta, diag, nil
}

// Invalidate 删除指定 account 的缓存条目。
func (c *DiscoveryCache) Invalidate(accountID uuid.UUID) {
	prefix := accountID.String() + "|"
	c.entries.Range(func(key, _ any) bool {
		text, ok := key.(string)
		if ok && strings.HasPrefix(text, prefix) {
			c.entries.Delete(key)
		}
		return true
	})
}

func (c *DiscoveryCache) MCPPool() *Pool {
	if c == nil {
		return nil
	}
	return c.mcpPool
}

// store 预填缓存条目，仅供测试使用。
func (c *DiscoveryCache) store(accountID uuid.UUID, profileRef string, workspaceRef string, reg Registration) {
	c.entries.Store(discoveryCacheKey(accountID, profileRef, workspaceRef), cacheEntry{
		registration: reg,
		cachedAt:     time.Now(),
	})
}
