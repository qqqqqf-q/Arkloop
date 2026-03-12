package runlimit

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const KeyPrefix = "arkloop:account:active_runs:"

const defaultTTL = 24 * time.Hour

var now = time.Now

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

// TryAcquire 为 account 原子地获取一个并发 run 槽。
// Redis 运行时不可用时回退到进程内计数器。
func TryAcquire(ctx context.Context, rdb *redis.Client, key string, maxRuns int64) bool {
	if rdb == nil {
		return true
	}
	ttlSecs := int64(defaultTTL.Seconds())
	result, err := tryAcquireScript.Run(ctx, rdb, []string{key}, maxRuns, ttlSecs).Slice()
	if err != nil {
		return fallbackCounters.tryAcquire(key, maxRuns)
	}
	allowed, count, ok := parseTryAcquireResult(result)
	if !ok {
		return fallbackCounters.tryAcquire(key, maxRuns)
	}
	fallbackCounters.set(key, count, defaultTTL)
	return allowed
}

// Release 为 account 原子地释放一个并发 run 槽，计数不低于 0。
func Release(ctx context.Context, rdb *redis.Client, key string) {
	if rdb == nil {
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
