package runlimit

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLocalBasicAcquireAndRelease(t *testing.T) {
	lim := NewLocalConcurrencyLimiter()
	ctx := context.Background()
	key := "test:basic"

	if !lim.TryAcquire(ctx, key, 2) {
		t.Fatal("first acquire should succeed")
	}
	if !lim.TryAcquire(ctx, key, 2) {
		t.Fatal("second acquire should succeed")
	}

	lim.Release(ctx, key)
	lim.Release(ctx, key)
}

func TestLocalEnforceLimit(t *testing.T) {
	lim := NewLocalConcurrencyLimiter()
	ctx := context.Background()
	key := "test:limit"

	if !lim.TryAcquire(ctx, key, 1) {
		t.Fatal("first acquire should succeed")
	}
	if lim.TryAcquire(ctx, key, 1) {
		t.Fatal("second acquire should be rejected (limit=1)")
	}
}

func TestLocalReleaseThenReacquire(t *testing.T) {
	lim := NewLocalConcurrencyLimiter()
	ctx := context.Background()
	key := "test:reacquire"

	if !lim.TryAcquire(ctx, key, 1) {
		t.Fatal("acquire should succeed")
	}
	if lim.TryAcquire(ctx, key, 1) {
		t.Fatal("should reject at limit")
	}

	lim.Release(ctx, key)

	if !lim.TryAcquire(ctx, key, 1) {
		t.Fatal("acquire after release should succeed")
	}
}

func TestLocalSetOverridesCount(t *testing.T) {
	lim := NewLocalConcurrencyLimiter()
	ctx := context.Background()
	key := "test:set"

	if err := lim.Set(ctx, key, 5); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if lim.TryAcquire(ctx, key, 5) {
		t.Fatal("should reject when Set fills all slots")
	}

	if err := lim.Set(ctx, key, 3); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !lim.TryAcquire(ctx, key, 5) {
		t.Fatal("should allow acquire after Set lowers count")
	}
}

func TestLocalSetToZeroClears(t *testing.T) {
	lim := NewLocalConcurrencyLimiter()
	ctx := context.Background()
	key := "test:setzero"

	if err := lim.Set(ctx, key, 5); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := lim.Set(ctx, key, 0); err != nil {
		t.Fatalf("Set to 0: %v", err)
	}
	if !lim.TryAcquire(ctx, key, 1) {
		t.Fatal("should succeed after Set to 0")
	}
}

func TestLocalTTLExpiration(t *testing.T) {
	current := time.Unix(1_700_000_000, 0).UTC()
	clock := func() time.Time { return current }
	lim := NewLocalConcurrencyLimiterWithTTL(time.Second, clock)
	ctx := context.Background()
	key := "test:ttl"

	if !lim.TryAcquire(ctx, key, 1) {
		t.Fatal("acquire should succeed")
	}
	if lim.TryAcquire(ctx, key, 1) {
		t.Fatal("should reject before TTL expires")
	}

	current = current.Add(2 * time.Second)

	if !lim.TryAcquire(ctx, key, 1) {
		t.Fatal("should succeed after TTL expires")
	}
}

func TestLocalConcurrentAccess(t *testing.T) {
	lim := NewLocalConcurrencyLimiter()
	ctx := context.Background()
	key := "test:concurrent"
	const maxRuns int64 = 5
	const goroutines = 50

	var wg sync.WaitGroup
	acquired := make(chan bool, goroutines)

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acquired <- lim.TryAcquire(ctx, key, maxRuns)
		}()
	}
	wg.Wait()
	close(acquired)

	var count int
	for ok := range acquired {
		if ok {
			count++
		}
	}
	if int64(count) != maxRuns {
		t.Fatalf("expected exactly %d acquires, got %d", maxRuns, count)
	}
}

func TestLocalDoubleReleaseNeverNegative(t *testing.T) {
	lim := NewLocalConcurrencyLimiter()
	ctx := context.Background()
	key := "test:double-release"

	if !lim.TryAcquire(ctx, key, 2) {
		t.Fatal("first acquire should succeed")
	}

	// Release once (legitimate), then release again (extra).
	lim.Release(ctx, key)
	lim.Release(ctx, key)

	// After double-release the count must not be negative.
	// Acquiring with maxRuns=1 must succeed (count is 0, not -1).
	if !lim.TryAcquire(ctx, key, 1) {
		t.Fatal("acquire after double-release should succeed (count should be 0, not negative)")
	}
	// Exactly at limit now.
	if lim.TryAcquire(ctx, key, 1) {
		t.Fatal("should reject when at limit")
	}
}

func TestLocalMaxRunsZeroOrNegative(t *testing.T) {
	lim := NewLocalConcurrencyLimiter()
	ctx := context.Background()
	key := "test:negative"

	if lim.TryAcquire(ctx, key, 0) {
		t.Fatal("maxRuns=0 should return false")
	}
	if lim.TryAcquire(ctx, key, -1) {
		t.Fatal("maxRuns=-1 should return false")
	}
}
