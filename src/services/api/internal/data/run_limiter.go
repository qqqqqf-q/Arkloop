package data

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const activeRunsKeyPrefix = "arkloop:org:active_runs:"

type RunLimiter struct {
	rdb     *redis.Client
	maxRuns int64
}

func NewRunLimiter(rdb *redis.Client, maxRuns int64) (*RunLimiter, error) {
	if rdb == nil {
		return nil, fmt.Errorf("redis client must not be nil")
	}
	if maxRuns <= 0 {
		return nil, fmt.Errorf("max_runs must be positive")
	}
	return &RunLimiter{rdb: rdb, maxRuns: maxRuns}, nil
}

// TryAcquire 为 org 获取一个并发 run 槽。
// INCR 后超限则 DECR 回滚并返回 false；Redis 不可用时 fail-open 返回 true。
func (l *RunLimiter) TryAcquire(ctx context.Context, orgID uuid.UUID) bool {
	key := activeRunsKeyPrefix + orgID.String()
	count, err := l.rdb.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if count > l.maxRuns {
		_ = l.rdb.Decr(ctx, key)
		return false
	}
	return true
}

// Release 释放 org 的一个并发 run 槽，计数不低于 0。
func (l *RunLimiter) Release(ctx context.Context, orgID uuid.UUID) {
	key := activeRunsKeyPrefix + orgID.String()
	result := l.rdb.Decr(ctx, key)
	if result.Err() == nil && result.Val() < 0 {
		_ = l.rdb.Set(ctx, key, 0, 0)
	}
}

// SyncFromDB 从数据库查询 org 实际活跃 run 数量并重置 Redis 计数器。
// 在 Worker 崩溃后手动调用以修正计数漂移。
func (l *RunLimiter) SyncFromDB(ctx context.Context, q Querier, orgID uuid.UUID) error {
	var count int64
	err := q.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM runs WHERE org_id = $1 AND status = 'running'`,
		orgID,
	).Scan(&count)
	if err != nil {
		return err
	}
	key := activeRunsKeyPrefix + orgID.String()
	return l.rdb.Set(ctx, key, count, 0).Err()
}
