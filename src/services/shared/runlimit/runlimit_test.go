package runlimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestTryAcquireNilRdbUsesFallback(t *testing.T) {
	setFallbackTestState(t, time.Minute)

	ctx := context.Background()
	key := Key("org-nil")

	// nil rdb uses degraded mode: maxRuns=2, limit=6
	for i := 0; i < 6; i++ {
		if !TryAcquire(ctx, nil, key, 2) {
			t.Fatalf("nil-rdb acquire %d should succeed (degraded limit=6)", i+1)
		}
	}
	if TryAcquire(ctx, nil, key, 2) {
		t.Fatal("nil-rdb should reject after reaching degraded limit")
	}

	Release(ctx, nil, key)
	if !TryAcquire(ctx, nil, key, 2) {
		t.Fatal("nil-rdb acquire after release should succeed")
	}
}

func TestTryAcquireWithRedisEnforcesLimit(t *testing.T) {
	setFallbackTestState(t, time.Minute)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	ctx := context.Background()
	key := Key("org-redis")

	if !TryAcquire(ctx, rdb, key, 2) {
		t.Fatal("first acquire should succeed")
	}
	if !TryAcquire(ctx, rdb, key, 2) {
		t.Fatal("second acquire should succeed")
	}
	if TryAcquire(ctx, rdb, key, 2) {
		t.Fatal("third acquire should be rejected")
	}

	Release(ctx, rdb, key)
	if !TryAcquire(ctx, rdb, key, 2) {
		t.Fatal("acquire after release should succeed")
	}
}

func TestTryAcquireFallsBackWhenRedisUnavailable(t *testing.T) {
	setFallbackTestState(t, time.Minute)

	rdb := newBrokenRedisClient(t)
	ctx := context.Background()
	key := Key("org-fallback")

	// degraded mode: maxRuns=2, in-memory limit = 2*3 = 6
	for i := 0; i < 6; i++ {
		if !TryAcquire(ctx, rdb, key, 2) {
			t.Fatalf("fallback acquire %d should succeed (degraded limit=6)", i+1)
		}
	}
	if TryAcquire(ctx, rdb, key, 2) {
		t.Fatal("fallback acquire should reject after reaching degraded limit")
	}

	Release(ctx, rdb, key)
	if !TryAcquire(ctx, rdb, key, 2) {
		t.Fatal("fallback acquire after release should succeed")
	}
}

func TestTryAcquireUsesWarmStateAfterRedisFailure(t *testing.T) {
	setFallbackTestState(t, time.Minute)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	ctx := context.Background()
	key := Key("org-warm")

	if !TryAcquire(ctx, rdb, key, 2) {
		t.Fatal("warm acquire should succeed")
	}

	mr.Close()

	// warm state: count=1, degraded limit=6, so 5 more slots available
	for i := 0; i < 5; i++ {
		if !TryAcquire(ctx, rdb, key, 2) {
			t.Fatalf("fallback should allow slot %d from warm state (degraded limit=6)", i+2)
		}
	}
	if TryAcquire(ctx, rdb, key, 2) {
		t.Fatal("fallback should reject after warm state reaches degraded limit")
	}
}

func TestSetUpdatesFallbackCounter(t *testing.T) {
	setFallbackTestState(t, time.Minute)

	rdb := newBrokenRedisClient(t)
	ctx := context.Background()
	key := Key("org-set")

	// set count to degraded limit (2*3=6) so TryAcquire is rejected
	if err := Set(ctx, rdb, key, 6); err == nil {
		t.Fatal("Set should return redis error when redis is unavailable")
	}
	if TryAcquire(ctx, rdb, key, 2) {
		t.Fatal("fallback state should reflect Set count")
	}

	if err := Set(ctx, nil, key, 0); err != nil {
		t.Fatalf("Set with nil redis: %v", err)
	}
	if !TryAcquire(ctx, rdb, key, 2) {
		t.Fatal("fallback state should be reset after Set to zero")
	}
}

func TestFallbackCounterExpires(t *testing.T) {
	clock := setFallbackTestState(t, time.Second)

	rdb := newBrokenRedisClient(t)
	ctx := context.Background()
	key := Key("org-expire")

	// degraded mode: maxRuns=1, limit=3
	for i := 0; i < 3; i++ {
		if !TryAcquire(ctx, rdb, key, 1) {
			t.Fatalf("fallback acquire %d should succeed (degraded limit=3)", i+1)
		}
	}
	if TryAcquire(ctx, rdb, key, 1) {
		t.Fatal("fallback acquire should reject before ttl expires")
	}

	*clock = clock.Add(2 * time.Second)
	if !TryAcquire(ctx, rdb, key, 1) {
		t.Fatal("fallback acquire should succeed after ttl expires")
	}
}

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

func setFallbackTestState(t *testing.T, ttl time.Duration) *time.Time {
	t.Helper()

	current := time.Unix(1_700_000_000, 0).UTC()

	fallbackCounters.mu.Lock()
	originalEntries := fallbackCounters.entries
	originalNow := fallbackCounters.now
	originalTTL := fallbackCounters.defaultTTL
	fallbackCounters.entries = make(map[string]localCounter)
	fallbackCounters.now = func() time.Time { return current }
	fallbackCounters.defaultTTL = ttl
	fallbackCounters.mu.Unlock()

	t.Cleanup(func() {
		fallbackCounters.mu.Lock()
		fallbackCounters.entries = originalEntries
		fallbackCounters.now = originalNow
		fallbackCounters.defaultTTL = originalTTL
		fallbackCounters.mu.Unlock()
	})

	return &current
}
