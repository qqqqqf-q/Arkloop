package runlimit

import (
	"testing"
	"time"
)

func TestTryAcquireEnforcesLimit(t *testing.T) {
	setTestState(t, time.Minute)

	key := Key("org-a")

	if !TryAcquire(key, 2) {
		t.Fatal("first acquire should succeed")
	}
	if !TryAcquire(key, 2) {
		t.Fatal("second acquire should succeed")
	}
	if TryAcquire(key, 2) {
		t.Fatal("third acquire should be rejected")
	}

	Release(key)
	if !TryAcquire(key, 2) {
		t.Fatal("acquire after release should succeed")
	}
}

func TestTryAcquireNonPositiveLimitIsUnlimited(t *testing.T) {
	setTestState(t, time.Minute)

	key := Key("org-unlimited")

	for i := 0; i < 8; i++ {
		if !TryAcquire(key, 0) {
			t.Fatalf("acquire %d should succeed when maxRuns=0", i+1)
		}
	}
	if len(counters.entries) != 0 {
		t.Fatal("unlimited mode should not write counter state")
	}
}

func TestReleaseNeverGoesBelowZero(t *testing.T) {
	setTestState(t, time.Minute)

	key := Key("org-release")

	Release(key)
	Release(key)
	if !TryAcquire(key, 1) {
		t.Fatal("acquire should succeed after releases on empty counter")
	}
	if TryAcquire(key, 1) {
		t.Fatal("acquire should be rejected at limit")
	}
}

func TestSetUpdatesCounter(t *testing.T) {
	setTestState(t, time.Minute)

	key := Key("org-set")

	Set(key, 2)
	if TryAcquire(key, 2) {
		t.Fatal("acquire should be rejected after Set to limit")
	}

	Set(key, 0)
	if !TryAcquire(key, 2) {
		t.Fatal("acquire should succeed after Set to zero")
	}
}

func TestCounterExpires(t *testing.T) {
	clock := setTestState(t, time.Second)

	key := Key("org-expire")

	if !TryAcquire(key, 1) {
		t.Fatal("first acquire should succeed")
	}
	if TryAcquire(key, 1) {
		t.Fatal("acquire should be rejected before ttl expires")
	}

	*clock = clock.Add(2 * time.Second)
	if !TryAcquire(key, 1) {
		t.Fatal("acquire should succeed after ttl expires")
	}
}

func setTestState(t *testing.T, ttl time.Duration) *time.Time {
	t.Helper()

	current := time.Unix(1_700_000_000, 0).UTC()

	counters.mu.Lock()
	originalEntries := counters.entries
	originalNow := counters.now
	originalTTL := counters.defaultTTL
	counters.entries = make(map[string]localCounter)
	counters.now = func() time.Time { return current }
	counters.defaultTTL = ttl
	counters.mu.Unlock()

	t.Cleanup(func() {
		counters.mu.Lock()
		counters.entries = originalEntries
		counters.now = originalNow
		counters.defaultTTL = originalTTL
		counters.mu.Unlock()
	})

	return &current
}
