package data

import (
	"context"
	"fmt"

	"arkloop/services/shared/runlimit"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

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

// TryAcquire 为 account 原子地获取一个并发 run 槽。
// Redis 不可用时 fail-open 返回 true。
func (l *RunLimiter) TryAcquire(ctx context.Context, accountID uuid.UUID) bool {
	key := runlimit.Key(accountID.String())
	return runlimit.TryAcquire(ctx, l.rdb, key, l.maxRuns)
}

// Release 原子地释放 account 的一个并发 run 槽，计数不低于 0。
func (l *RunLimiter) Release(ctx context.Context, accountID uuid.UUID) {
	key := runlimit.Key(accountID.String())
	runlimit.Release(ctx, l.rdb, key)
}

// SyncFromDB 从数据库查询 account 实际活跃 run 数量并重置 Redis 计数器。
func (l *RunLimiter) SyncFromDB(ctx context.Context, q Querier, accountID uuid.UUID) error {
	var count int64
	err := q.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM runs WHERE account_id = $1 AND status = 'running'`,
		accountID,
	).Scan(&count)
	if err != nil {
		return err
	}
	key := runlimit.Key(accountID.String())
	return runlimit.Set(ctx, l.rdb, key, count)
}
