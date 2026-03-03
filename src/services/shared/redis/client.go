package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// NewClient 从 URL 创建 Redis 客户端并验证连通性。
// URL 格式：redis://:password@host:port/db
func NewClient(ctx context.Context, redisURL string) (*redis.Client, error) {
	return NewClientWithTimeout(ctx, redisURL, 0)
}

// NewClientWithTimeout 从 URL 创建 Redis 客户端并验证连通性。
// timeout 用于 ReadTimeout/WriteTimeout；0 表示使用默认值。
func NewClientWithTimeout(ctx context.Context, redisURL string, timeout time.Duration) (*redis.Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cleaned := strings.TrimSpace(redisURL)
	if cleaned == "" {
		return nil, fmt.Errorf("redis url must not be empty")
	}

	opts, err := redis.ParseURL(cleaned)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	// 让 context 的超时/取消生效（请求链路可用 context.WithTimeout 做 fail-open）。
	opts.ContextTimeoutEnabled = true

	if timeout > 0 {
		opts.ReadTimeout = timeout
		opts.WriteTimeout = timeout
	}

	client := redis.NewClient(opts)

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return client, nil
}
