//go:build !desktop

package runlimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisTryAcquireEnforcesLimit(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	lim := NewRedisConcurrencyLimiter(rdb)
	ctx := context.Background()
	key := Key("org-redis")

	if !lim.TryAcquire(ctx, key, 2) {
		t.Fatal("first acquire should succeed")
	}
	if !lim.TryAcquire(ctx, key, 2) {
		t.Fatal("second acquire should succeed")
	}
	if lim.TryAcquire(ctx, key, 2) {
		t.Fatal("third acquire should be rejected")
	}

	lim.Release(ctx, key)
	if !lim.TryAcquire(ctx, key, 2) {
		t.Fatal("acquire after release should succeed")
	}
}

func TestRedisTryAcquireFallsBackWhenUnavailable(t *testing.T) {
	rdb := newBrokenRedisClient(t)
	lim := newTestRedisLimiter(t, rdb, time.Minute)
	ctx := context.Background()
	key := Key("org-fallback")

	if !lim.TryAcquire(ctx, key, 2) {
		t.Fatal("first fallback acquire should succeed")
	}
	if !lim.TryAcquire(ctx, key, 2) {
		t.Fatal("second fallback acquire should succeed")
	}
	if lim.TryAcquire(ctx, key, 2) {
		t.Fatal("fallback acquire should reject after reaching limit")
	}

	lim.Release(ctx, key)
	if !lim.TryAcquire(ctx, key, 2) {
		t.Fatal("fallback acquire after release should succeed")
	}
}

func TestRedisUsesWarmStateAfterFailure(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	lim := NewRedisConcurrencyLimiter(rdb)
	ctx := context.Background()
	key := Key("org-warm")

	if !lim.TryAcquire(ctx, key, 2) {
		t.Fatal("warm acquire should succeed")
	}

	mr.Close()

	if !lim.TryAcquire(ctx, key, 2) {
		t.Fatal("fallback should allow one more slot from warm state")
	}
	if lim.TryAcquire(ctx, key, 2) {
		t.Fatal("fallback should reject after warm state reaches limit")
	}
}

func TestRedisSetUpdatesFallbackCounter(t *testing.T) {
	rdb := newBrokenRedisClient(t)
	lim := newTestRedisLimiter(t, rdb, time.Minute)
	ctx := context.Background()
	key := Key("org-set")

	if err := lim.Set(ctx, key, 2); err == nil {
		t.Fatal("Set should return redis error when redis is unavailable")
	}
	if lim.TryAcquire(ctx, key, 2) {
		t.Fatal("fallback state should reflect Set count")
	}

	// Use nil-rdb limiter to Set to 0 on the same fallback store.
	lim.rdb = nil
	if err := lim.Set(ctx, key, 0); err != nil {
		t.Fatalf("Set with nil redis: %v", err)
	}
	lim.rdb = rdb
	if !lim.TryAcquire(ctx, key, 2) {
		t.Fatal("fallback state should be reset after Set to zero")
	}
}

func TestRedisFallbackCounterExpires(t *testing.T) {
	current := time.Unix(1_700_000_000, 0).UTC()
	rdb := newBrokenRedisClient(t)
	lim := newTestRedisLimiterWithClock(t, rdb, time.Second, func() time.Time { return current })
	ctx := context.Background()
	key := Key("org-expire")

	if !lim.TryAcquire(ctx, key, 1) {
		t.Fatal("initial fallback acquire should succeed")
	}
	if lim.TryAcquire(ctx, key, 1) {
		t.Fatal("fallback acquire should reject before ttl expires")
	}

	current = current.Add(2 * time.Second)
	if !lim.TryAcquire(ctx, key, 1) {
		t.Fatal("fallback acquire should succeed after ttl expires")
	}
}

func TestRedisNilClientFailOpen(t *testing.T) {
	lim := NewRedisConcurrencyLimiter(nil)
	ctx := context.Background()
	key := Key("org-nil")

	if !lim.TryAcquire(ctx, key, 1) {
		t.Fatal("nil rdb should fail-open")
	}
	// Should not panic.
	lim.Release(ctx, key)

	if err := lim.Set(ctx, key, 5); err != nil {
		t.Fatalf("Set with nil rdb: %v", err)
	}
}

// --- helpers ---

func newBrokenRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  20 * time.Millisecond,
		ReadTimeout:  20 * time.Millisecond,
		WriteTimeout: 20 * time.Millisecond,
		MaxRetries:   0,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func newTestRedisLimiter(t *testing.T, rdb *redis.Client, ttl time.Duration) *RedisConcurrencyLimiter {
	t.Helper()
	return newTestRedisLimiterWithClock(t, rdb, ttl, time.Now)
}

func newTestRedisLimiterWithClock(t *testing.T, rdb *redis.Client, ttl time.Duration, clock func() time.Time) *RedisConcurrencyLimiter {
	t.Helper()
	return &RedisConcurrencyLimiter{
		rdb: rdb,
		fallback: &localCounterStore{
			entries:    make(map[string]localCounter),
			now:        clock,
			defaultTTL: ttl,
		},
	}
}
