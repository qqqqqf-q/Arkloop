package data

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PoolLimits struct {
	MaxConns int32
	MinConns int32
}

func (l PoolLimits) Validate() error {
	if l.MaxConns <= 0 {
		return fmt.Errorf("pool max_conns must be greater than 0")
	}
	if l.MinConns < 0 {
		return fmt.Errorf("pool min_conns must not be negative")
	}
	if l.MinConns > l.MaxConns {
		return fmt.Errorf("pool min_conns must not exceed max_conns")
	}
	return nil
}

func NewPool(ctx context.Context, dsn string, limits PoolLimits) (*pgxpool.Pool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cleaned := strings.TrimSpace(dsn)
	if cleaned == "" {
		return nil, fmt.Errorf("database dsn must not be empty")
	}

	if err := limits.Validate(); err != nil {
		return nil, err
	}

	cfg, err := pgxpool.ParseConfig(NormalizePostgresDSN(cleaned))
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = limits.MaxConns
	cfg.MinConns = limits.MinConns

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return pool, nil
}
