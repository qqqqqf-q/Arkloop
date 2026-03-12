package config

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	GetPlatformSetting(ctx context.Context, key string) (string, bool, error)
	GetProjectSetting(ctx context.Context, projectID uuid.UUID, key string) (string, bool, error)
}

type PGXStore struct {
	pool *pgxpool.Pool
}

func NewPGXStore(pool *pgxpool.Pool) *PGXStore {
	return &PGXStore{pool: pool}
}

func (s *PGXStore) GetPlatformSetting(ctx context.Context, key string) (string, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.pool == nil {
		return "", false, nil
	}

	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM platform_settings WHERE key = $1 LIMIT 1`, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get platform setting %q: %w", key, err)
	}
	return value, true, nil
}

func (s *PGXStore) GetProjectSetting(ctx context.Context, projectID uuid.UUID, key string) (string, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.pool == nil {
		return "", false, nil
	}
	if projectID == uuid.Nil {
		return "", false, fmt.Errorf("project_id must not be empty")
	}

	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM project_settings WHERE project_id = $1 AND key = $2 LIMIT 1`, projectID, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get project setting %q: %w", key, err)
	}
	return value, true, nil
}
