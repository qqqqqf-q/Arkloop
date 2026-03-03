package ratelimit

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter 是限流器的抽象接口，方便测试时替换。
type Limiter interface {
	Consume(ctx context.Context, key string) (ConsumeResult, error)
}

// ConsumeResult 是单次消费的结果。
type ConsumeResult struct {
	Allowed        bool
	Limit          int64
	Remaining      int64
	RetryAfterSecs int64
	ResetSecs      int64 // 桶完全恢复所需秒数
}

// tokenBucketScript 是原子 token bucket 的 Lua 实现。
// KEYS[1] = bucket key
// ARGV[1] = capacity（float）
// ARGV[2] = rate（tokens/second，float）
// ARGV[3] = now（Unix timestamp，float）
// 返回：{allowed(0/1), remaining_floor, retry_after_secs, capacity, reset_secs}
var tokenBucketScript = redis.NewScript(`
local key      = KEYS[1]
local capacity = tonumber(ARGV[1])
local rate     = tonumber(ARGV[2])
local now      = tonumber(ARGV[3])

local data   = redis.call("HMGET", key, "tokens", "ts")
local tokens = tonumber(data[1]) or capacity
local ts     = tonumber(data[2]) or now

local elapsed = math.max(0.0, now - ts)
tokens = math.min(capacity, tokens + elapsed * rate)

local allowed = tokens >= 1.0
if allowed then
    tokens = tokens - 1.0
end

-- TTL：填满桶所需时间的两倍，保证自动清理
local ttl = math.ceil(capacity / rate) * 2 + 1
redis.call("HSET", key, "tokens", tostring(tokens), "ts", tostring(now))
redis.call("EXPIRE", key, ttl)

local retry_after = 0
if not allowed then
    retry_after = math.ceil((1.0 - tokens) / rate)
end

local reset_secs = math.ceil((capacity - tokens) / rate)

return {allowed and 1 or 0, math.floor(tokens), retry_after, math.floor(capacity), reset_secs}
`)

// TokenBucket 是基于 Redis 的 token bucket 限流器。
type TokenBucket struct {
	rdb      *redis.Client
	capacity float64        // fallback capacity
	rate     float64        // fallback tokens per second
	provider func() Config  // 可选动态配置
	now      func() float64 // 返回 Unix 时间（秒，浮点），可注入以便测试
}

// NewTokenBucket 创建一个 token bucket。cfg.Capacity 和 cfg.RatePerSecond() 决定限流参数。
func NewTokenBucket(rdb *redis.Client, cfg Config) (*TokenBucket, error) {
	return NewTokenBucketWithProvider(rdb, cfg, nil)
}

// NewTokenBucketWithProvider 创建可选动态配置的 token bucket。
// provider 返回值非法时回退到 cfg。
func NewTokenBucketWithProvider(rdb *redis.Client, cfg Config, provider func() Config) (*TokenBucket, error) {
	if rdb == nil {
		return nil, fmt.Errorf("redis client must not be nil")
	}
	if cfg.Capacity <= 0 {
		return nil, fmt.Errorf("capacity must be positive")
	}
	if cfg.RatePerMinute <= 0 {
		return nil, fmt.Errorf("rate_per_minute must be positive")
	}
	return &TokenBucket{
		rdb:      rdb,
		capacity: cfg.Capacity,
		rate:     cfg.RatePerSecond(),
		provider: provider,
		now:      func() float64 { return float64(time.Now().UnixNano()) / 1e9 },
	}, nil
}

// Consume 从指定 key 的 bucket 消耗一个 token。
func (b *TokenBucket) Consume(ctx context.Context, key string) (ConsumeResult, error) {
	now := b.now()
	capacity := b.capacity
	rate := b.rate
	if b.provider != nil {
		cfg := b.provider()
		if cfg.Capacity > 0 {
			capacity = cfg.Capacity
		}
		if cfg.RatePerMinute > 0 {
			rate = cfg.RatePerSecond()
		}
	}

	result, err := tokenBucketScript.Run(ctx, b.rdb, []string{key},
		strconv.FormatFloat(capacity, 'g', -1, 64),
		strconv.FormatFloat(rate, 'g', -1, 64),
		strconv.FormatFloat(now, 'f', 6, 64),
	).Slice()
	if err != nil {
		return ConsumeResult{}, fmt.Errorf("token bucket script: %w", err)
	}
	if len(result) < 5 {
		return ConsumeResult{}, fmt.Errorf("unexpected script result length: %d", len(result))
	}

	allowedInt, _ := toInt64(result[0])
	remaining, _ := toInt64(result[1])
	retryAfter, _ := toInt64(result[2])
	limit, _ := toInt64(result[3])
	resetSecs, _ := toInt64(result[4])

	return ConsumeResult{
		Allowed:        allowedInt == 1,
		Limit:          limit,
		Remaining:      max64(remaining, 0),
		RetryAfterSecs: max64(retryAfter, 0),
		ResetSecs:      max64(resetSecs, 0),
	}, nil
}

func toInt64(v any) (int64, bool) {
	switch val := v.(type) {
	case int64:
		return val, true
	case float64:
		return int64(math.Round(val)), true
	}
	return 0, false
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
