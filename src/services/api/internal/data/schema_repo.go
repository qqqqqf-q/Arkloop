package data

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SchemaRepository struct {
	pool *pgxpool.Pool
}

func NewSchemaRepository(pool *pgxpool.Pool) (*SchemaRepository, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	return &SchemaRepository{pool: pool}, nil
}

func (r *SchemaRepository) CurrentSchemaVersion(ctx context.Context) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var version int64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied = true`,
	).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("query goose_db_version: %w", err)
	}
	return version, nil
}
