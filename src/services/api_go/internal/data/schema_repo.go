package data

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SchemaRepository struct {
	pool *pgxpool.Pool
}

func NewSchemaRepository(pool *pgxpool.Pool) (*SchemaRepository, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool 不能为空")
	}
	return &SchemaRepository{pool: pool}, nil
}

func (r *SchemaRepository) CurrentAlembicVersion(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var version string
	if err := r.pool.QueryRow(ctx, `SELECT version_num FROM alembic_version LIMIT 1`).Scan(&version); err != nil {
		return "", err
	}

	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("alembic_version 为空")
	}
	return version, nil
}
