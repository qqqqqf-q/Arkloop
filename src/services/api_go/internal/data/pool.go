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
		return nil, fmt.Errorf("database dsn 不能为空")
	}

	pool, err := pgxpool.New(ctx, NormalizePostgresDSN(cleaned))
	if err != nil {
		return nil, err
	}
	return pool, nil
}
