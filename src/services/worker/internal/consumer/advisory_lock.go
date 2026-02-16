package consumer

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UnlockFunc func(ctx context.Context) error

type RunLocker interface {
	TryAcquire(ctx context.Context, runID uuid.UUID) (UnlockFunc, bool, error)
}

type PgAdvisoryLocker struct {
	pool *pgxpool.Pool
}

func NewPgAdvisoryLocker(pool *pgxpool.Pool) (*PgAdvisoryLocker, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool 不能为空")
	}
	return &PgAdvisoryLocker{pool: pool}, nil
}

func (l *PgAdvisoryLocker) TryAcquire(ctx context.Context, runID uuid.UUID) (UnlockFunc, bool, error) {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}

	lockKey := advisoryLockKey(runID)
	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockKey).Scan(&acquired); err != nil {
		conn.Release()
		return nil, false, err
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}

	unlock := func(ctx context.Context) error {
		defer conn.Release()
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockKey); err != nil {
			return err
		}
		return nil
	}
	return unlock, true, nil
}

func advisoryLockKey(runID uuid.UUID) int64 {
	var value uint64
	for _, item := range runID[8:] {
		value = (value << 8) | uint64(item)
	}
	return int64(value)
}
