//go:build !desktop

package runlimit

import (
	"context"

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

// RedisConcurrencyLimiter 分布式并发限制器，Redis 不可用时回退到进程内计数。
type RedisConcurrencyLimiter struct {
	rdb      *redis.Client
	fallback *LocalConcurrencyLimiter
}

// NewRedisConcurrencyLimiter 创建 Redis 并发限制器。rdb 可以为 nil（fail-open 模式）。
func NewRedisConcurrencyLimiter(rdb *redis.Client) *RedisConcurrencyLimiter {
	return &RedisConcurrencyLimiter{
		rdb:      rdb,
		fallback: NewLocalConcurrencyLimiter(),
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
		return r.fallback.TryAcquire(ctx, key, maxRuns)
	}
	allowed, count, ok := parseTryAcquireResult(result)
	if !ok {
		return r.fallback.TryAcquire(ctx, key, maxRuns)
	}
	_ = r.fallback.Set(ctx, key, count)
	return allowed
}

// Release 为 org 原子地释放一个并发 run 槽，计数不低于 0。
func (r *RedisConcurrencyLimiter) Release(ctx context.Context, key string) {
	if r.rdb == nil {
		return
	}
	result, err := releaseScript.Run(ctx, r.rdb, []string{key}).Int64()
	if err != nil {
		r.fallback.Release(ctx, key)
		return
	}
	_ = r.fallback.Set(ctx, key, result)
}

// Set 直接设置 org 的活跃 run 计数（用于 SyncFromDB 修正漂移）。
func (r *RedisConcurrencyLimiter) Set(ctx context.Context, key string, count int64) error {
	_ = r.fallback.Set(ctx, key, count)
	if r.rdb == nil {
		return nil
	}
	return r.rdb.Set(ctx, key, count, defaultTTL).Err()
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
