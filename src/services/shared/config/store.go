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

type dbQueryRow interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type PGXStore struct {
	q dbQueryRow // 仅 *pgxpool.Pool 时用 NewPGXStore，避免 nil 指针装箱进 interface
}

// NewPGXStore 包装 *pgxpool.Pool。pool 为 nil 时返回空 Store（不查库）。
func NewPGXStore(pool *pgxpool.Pool) *PGXStore {
	if pool == nil {
		return &PGXStore{}
	}
	return &PGXStore{q: pool}
}

// NewPGXStoreQuerier 包装任意带 QueryRow 的实现（如 api data.DB、desktop DesktopDB）。
// q 为 nil interface 时不查库。
func NewPGXStoreQuerier(q dbQueryRow) *PGXStore {
	if q == nil {
		return &PGXStore{}
	}
	return &PGXStore{q: q}
}

func (s *PGXStore) GetPlatformSetting(ctx context.Context, key string) (string, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.q == nil {
		return "", false, nil
	}

	var value string
	err := s.q.QueryRow(ctx, `SELECT value FROM platform_settings WHERE key = $1 LIMIT 1`, key).Scan(&value)
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
	if s == nil || s.q == nil {
		return "", false, nil
	}
	if projectID == uuid.Nil {
		return "", false, fmt.Errorf("project_id must not be empty")
	}

	var value string
	err := s.q.QueryRow(ctx, `SELECT value FROM project_settings WHERE project_id = $1 AND key = $2 LIMIT 1`, projectID, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get project setting %q: %w", key, err)
	}
	return value, true, nil
}
