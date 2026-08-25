package data

import (
	"context"
	"fmt"

	"arkloop/services/shared/runlimit"

	"github.com/google/uuid"
)

type RunLimiter struct {
	maxRuns int64
}

func NewRunLimiter(maxRuns int64) (*RunLimiter, error) {
	if maxRuns <= 0 {
		return nil, fmt.Errorf("max_runs must be positive")
	}
	return &RunLimiter{maxRuns: maxRuns}, nil
}

// TryAcquire 为 account 获取一个并发 run 槽，达到上限时拒绝。
func (l *RunLimiter) TryAcquire(ctx context.Context, accountID uuid.UUID) bool {
	key := runlimit.Key(accountID.String())
	return runlimit.TryAcquire(key, l.maxRuns)
}

// Release 释放 account 的一个并发 run 槽，计数不低于 0。
func (l *RunLimiter) Release(ctx context.Context, accountID uuid.UUID) {
	key := runlimit.Key(accountID.String())
	runlimit.Release(key)
}

// SyncFromDB 从数据库查询 account 实际活跃 run 数量并重置计数器。
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
	runlimit.Set(key, count)
	return nil
}
