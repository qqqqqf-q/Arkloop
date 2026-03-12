//go:build !desktop

package runlimit

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

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

// RedisConcurrencyLimiter 分布式并发限制器，Redis 不可用时回退到进程内计数。
type RedisConcurrencyLimiter struct {
	rdb      *redis.Client
	fallback *localCounterStore
}

// NewRedisConcurrencyLimiter 创建 Redis 并发限制器。rdb 可以为 nil（fail-open 模式）。
func NewRedisConcurrencyLimiter(rdb *redis.Client) *RedisConcurrencyLimiter {
	return &RedisConcurrencyLimiter{
		rdb: rdb,
		fallback: &localCounterStore{
			entries:    make(map[string]localCounter),
			now:        time.Now,
			defaultTTL: defaultTTL,
		},
	}
}

// TryAcquire 为 org 原子地获取一个并发 run 槽。
// Redis 运行时不可用时回退到进程内计数器。
func (r *RedisConcurrencyLimiter) TryAcquire(ctx context.Context, key string, maxRuns int64) bool {
	if r.rdb == nil {
		return true
	}
	ttlSecs := int64(defaultTTL.Seconds())
	result, err := tryAcquireScript.Run(ctx, r.rdb, []string{key}, maxRuns, ttlSecs).Slice()
	if err != nil {
		return r.fallback.tryAcquire(key, maxRuns)
	}
	allowed, count, ok := parseTryAcquireResult(result)
	if !ok {
		return r.fallback.tryAcquire(key, maxRuns)
	}
	r.fallback.set(key, count, defaultTTL)
	return allowed
}

// Release 为 org 原子地释放一个并发 run 槽，计数不低于 0。
func (r *RedisConcurrencyLimiter) Release(ctx context.Context, key string) {
	if r.rdb == nil {
		return
	}
	result, err := releaseScript.Run(ctx, r.rdb, []string{key}).Int64()
	if err != nil {
		r.fallback.release(key)
		return
	}
	r.fallback.set(key, result, defaultTTL)
}

// Set 直接设置 org 的活跃 run 计数（用于 SyncFromDB 修正漂移）。
func (r *RedisConcurrencyLimiter) Set(ctx context.Context, key string, count int64) error {
	r.fallback.set(key, count, defaultTTL)
	if r.rdb == nil {
		return nil
	}
	return r.rdb.Set(ctx, key, count, defaultTTL).Err()
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
