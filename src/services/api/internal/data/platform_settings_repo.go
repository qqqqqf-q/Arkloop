package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type PlatformSetting struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}

type PlatformSettingsRepository struct {
	db Querier
}

func NewPlatformSettingsRepository(db Querier) (*PlatformSettingsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &PlatformSettingsRepository{db: db}, nil
}

func (r *PlatformSettingsRepository) Get(ctx context.Context, key string) (*PlatformSetting, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var s PlatformSetting
	err := r.db.QueryRow(
		ctx,
		`SELECT key, value, updated_at FROM platform_settings WHERE key = $1`,
		key,
	).Scan(&s.Key, &s.Value, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("platform_settings.Get: %w", err)
	}
	return &s, nil
}

func (r *PlatformSettingsRepository) Set(ctx context.Context, key, value string) (*PlatformSetting, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("platform_settings.Set: key must not be empty")
	}
	var s PlatformSetting
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO platform_settings (key, value, updated_at)
		 VALUES ($1, $2, now())
		 ON CONFLICT (key)
		 DO UPDATE SET value = EXCLUDED.value, updated_at = now()
		 RETURNING key, value, updated_at`,
		key, value,
	).Scan(&s.Key, &s.Value, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("platform_settings.Set: %w", err)
	}
	return &s, nil
}

func (r *PlatformSettingsRepository) List(ctx context.Context) ([]PlatformSetting, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(ctx, `SELECT key, value, updated_at FROM platform_settings ORDER BY key ASC`)
	if err != nil {
		return nil, fmt.Errorf("platform_settings.List: %w", err)
	}
	defer rows.Close()

	var items []PlatformSetting
	for rows.Next() {
		var s PlatformSetting
		if err := rows.Scan(&s.Key, &s.Value, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("platform_settings.List scan: %w", err)
		}
		items = append(items, s)
	}
	return items, rows.Err()
}
