package data

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cleaned := strings.TrimSpace(dsn)
	if cleaned == "" {
		return nil, fmt.Errorf("database dsn must not be empty")
	}

	pool, err := pgxpool.New(ctx, NormalizePostgresDSN(cleaned))
	if err != nil {
		return nil, err
	}
	return pool, nil
}

// NewDirectPool 创建专用于 LISTEN/NOTIFY 的小连接池（不经过 PgBouncer）。
// MaxConns 较小，因为只有 SSE handler 使用。
func NewDirectPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cleaned := strings.TrimSpace(dsn)
	if cleaned == "" {
		return nil, fmt.Errorf("direct database dsn must not be empty")
	}

	cfg, err := pgxpool.ParseConfig(NormalizePostgresDSN(cleaned))
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 10

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return pool, nil
}
