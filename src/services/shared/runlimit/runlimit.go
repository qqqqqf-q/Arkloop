package runlimit

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const KeyPrefix = "arkloop:account:active_runs:"

const defaultTTL = 24 * time.Hour

// degradedMultiplier scales the limit when using in-memory fallback,
// compensating for per-process (not global) counting.
const degradedMultiplier int64 = 3

var now = time.Now

// rate-limited warning for Redis unavailability
var warnState struct {
	mu   sync.Mutex
	last time.Time
}

func warnDegraded() {
	const interval = time.Minute
	warnState.mu.Lock()
	defer warnState.mu.Unlock()
	if time.Since(warnState.last) < interval {
		return
	}
	warnState.last = time.Now()
	slog.Warn("runlimit: Redis unavailable, using in-memory fallback")
}

type localCounter struct {
	count     int64
	expiresAt time.Time
}

type localCounterStore struct {
	mu         sync.Mutex
	entries    map[string]localCounter
	now        func() time.Time
	defaultTTL time.Duration
}

var fallbackCounters = localCounterStore{
	entries:    make(map[string]localCounter),
	now:        now,
	defaultTTL: defaultTTL,
}

// tryAcquireScript 原子 check-and-increment。
// KEYS[1] = active runs key
// ARGV[1] = max concurrent runs
// ARGV[2] = TTL seconds
// 返回 {allowed, current_count}。
var tryAcquireScript = redis.NewScript(`
local key = KEYS[1]
local max = tonumber(ARGV[1])
local ttl = tonumber(ARGV[2])
local cur = tonumber(redis.call("GET", key) or "0")
if cur >= max then
    return {0, cur}
end
cur = redis.call("INCR", key)
if ttl > 0 then
    redis.call("EXPIRE", key, ttl)
end
return {1, cur}
`)

// releaseScript 原子 decrement，不低于 0。
// KEYS[1] = active runs key
// 返回 decrement 后的值。
var releaseScript = redis.NewScript(`
local key = KEYS[1]
local cur = tonumber(redis.call("GET", key) or "0")
if cur <= 0 then
    return 0
end
return redis.call("DECR", key)
`)

// TryAcquire atomically acquires a concurrent run slot for an account.
// Falls back to in-memory counter with relaxed limit when Redis is unavailable.
func TryAcquire(ctx context.Context, rdb *redis.Client, key string, maxRuns int64) bool {
	degradedMax := maxRuns * degradedMultiplier
	if rdb == nil {
		warnDegraded()
		return fallbackCounters.tryAcquire(key, degradedMax)
	}
	ttlSecs := int64(defaultTTL.Seconds())
	result, err := tryAcquireScript.Run(ctx, rdb, []string{key}, maxRuns, ttlSecs).Slice()
	if err != nil {
		warnDegraded()
		return fallbackCounters.tryAcquire(key, degradedMax)
	}
	allowed, count, ok := parseTryAcquireResult(result)
	if !ok {
		warnDegraded()
		return fallbackCounters.tryAcquire(key, degradedMax)
	}
	fallbackCounters.set(key, count, defaultTTL)
	return allowed
}

// Release atomically releases a concurrent run slot, count never goes below 0.
func Release(ctx context.Context, rdb *redis.Client, key string) {
	if rdb == nil {
		fallbackCounters.release(key)
		return
	}
	result, err := releaseScript.Run(ctx, rdb, []string{key}).Int64()
	if err != nil {
		fallbackCounters.release(key)
		return
	}
	fallbackCounters.set(key, result, defaultTTL)
}

// Key 根据 accountID 字符串构建 Redis key。
func Key(accountID string) string {
	return KeyPrefix + accountID
}

// Set 直接设置 account 的活跃 run 计数（用于 SyncFromDB 修正漂移）。
func Set(ctx context.Context, rdb *redis.Client, key string, count int64) error {
	fallbackCounters.set(key, count, defaultTTL)
	if rdb == nil {
		return nil
	}
	return rdb.Set(ctx, key, count, defaultTTL).Err()
}

func (s *localCounterStore) tryAcquire(key string, maxRuns int64) bool {
	if maxRuns <= 0 {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry := s.current(key)
	if entry.count >= maxRuns {
		return false
	}
	entry.count++
	entry.expiresAt = s.now().Add(s.defaultTTL)
	s.entries[key] = entry
	return true
}

func (s *localCounterStore) release(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := s.current(key)
	if entry.count <= 1 {
		delete(s.entries, key)
		return
	}
	entry.count--
	entry.expiresAt = s.now().Add(s.defaultTTL)
	s.entries[key] = entry
}

func (s *localCounterStore) set(key string, count int64, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if count <= 0 {
		delete(s.entries, key)
		return
	}
	entry := localCounter{count: count, expiresAt: s.now().Add(ttl)}
	s.entries[key] = entry
}

func (s *localCounterStore) current(key string) localCounter {
	entry, ok := s.entries[key]
	if !ok {
		return localCounter{}
	}
	if !entry.expiresAt.IsZero() && !entry.expiresAt.After(s.now()) {
		delete(s.entries, key)
		return localCounter{}
	}
	return entry
}

func parseTryAcquireResult(result []any) (bool, int64, bool) {
	if len(result) != 2 {
		return false, 0, false
	}
	allowed, ok := result[0].(int64)
	if !ok {
		return false, 0, false
	}
	count, ok := result[1].(int64)
	if !ok {
		return false, 0, false
	}
	return allowed == 1, count, true
}
