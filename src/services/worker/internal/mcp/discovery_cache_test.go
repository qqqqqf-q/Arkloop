package mcp

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestDiscoveryCache_CacheHit 验证缓存命中：预热后 10 个并发 Get（pool=nil）不触碰 DB。
func TestDiscoveryCache_CacheHit(t *testing.T) {
	cache := NewDiscoveryCache(60*time.Second, nil)
	accountID := uuid.New()
	profileRef := "profile-a"
	workspaceRef := "workspace-a"

	// 手动预热缓存（等效于首次 Get 后回填）
	cache.store(accountID, profileRef, workspaceRef, Registration{})

	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// pool=nil：若 cache miss 则 DiscoverFromDB 会因 pool=nil 报错；
			// cache hit 则直接返回，不碰 DB。
			_, err := cache.Get(context.Background(), nil, accountID, profileRef, workspaceRef)
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("call %d: unexpected error: %v", i, err)
		}
	}
}

// TestDiscoveryCache_Invalidate 验证 Invalidate 清除缓存条目后 Get 触发回源。
func TestDiscoveryCache_Invalidate(t *testing.T) {
	cache := NewDiscoveryCache(60*time.Second, nil)
	accountID := uuid.New()
	cache.store(accountID, "profile-a", "workspace-a", Registration{})

	cache.Invalidate(accountID)

	// 缓存已清除，Get 会调 DiscoverFromDB(ctx, nil, ...) → pool=nil → error
	_, err := cache.Get(context.Background(), nil, accountID, "profile-a", "workspace-a")
	if err == nil {
		t.Error("expected error after Invalidate (pool=nil forces DB call), got nil")
	}
}

// TestDiscoveryCache_TTLExpiry 验证 TTL 过期后 Get 触发回源。
func TestDiscoveryCache_TTLExpiry(t *testing.T) {
	cache := NewDiscoveryCache(50*time.Millisecond, nil)
	accountID := uuid.New()
	cache.store(accountID, "profile-a", "workspace-a", Registration{})

	time.Sleep(100 * time.Millisecond)

	// TTL 已过期，Get 会调 DiscoverFromDB(ctx, nil, ...) → pool=nil → error
	_, err := cache.Get(context.Background(), nil, accountID, "profile-a", "workspace-a")
	if err == nil {
		t.Error("expected error after TTL expiry (pool=nil forces DB call), got nil")
	}
}

// TestDiscoveryCache_ZeroTTL 验证 TTL=0 时每次都回源（不缓存）。
func TestDiscoveryCache_ZeroTTL(t *testing.T) {
	cache := NewDiscoveryCache(0, nil)
	accountID := uuid.New()
	cache.store(accountID, "profile-a", "workspace-a", Registration{})

	// TTL=0 不启用缓存，每次都直接回源
	_, err := cache.Get(context.Background(), nil, accountID, "profile-a", "workspace-a")
	if err == nil {
		t.Error("expected error with TTL=0 (no cache, pool=nil forces DB call), got nil")
	}
}
