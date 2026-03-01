package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type OrgSetting struct {
	OrgID     string
	Key       string
	Value     string
	UpdatedAt time.Time
}

type OrgSettingsRepository struct {
	db Querier
}

func NewOrgSettingsRepository(db Querier) (*OrgSettingsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &OrgSettingsRepository{db: db}, nil
}

func (r *OrgSettingsRepository) Get(ctx context.Context, orgID, key string) (*OrgSetting, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var s OrgSetting
	err := r.db.QueryRow(
		ctx,
		`SELECT org_id, key, value, updated_at FROM org_settings WHERE org_id = $1 AND key = $2`,
		orgID, key,
	).Scan(&s.OrgID, &s.Key, &s.Value, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("org_settings.Get: %w", err)
	}
	return &s, nil
}

func (r *OrgSettingsRepository) Set(ctx context.Context, orgID, key, value string) (*OrgSetting, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("org_settings.Set: key must not be empty")
	}
	var s OrgSetting
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO org_settings (org_id, key, value, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (org_id, key)
		 DO UPDATE SET value = EXCLUDED.value, updated_at = now()
		 RETURNING org_id, key, value, updated_at`,
		orgID, key, value,
	).Scan(&s.OrgID, &s.Key, &s.Value, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("org_settings.Set: %w", err)
	}
	return &s, nil
}

func (r *OrgSettingsRepository) Delete(ctx context.Context, orgID, key string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(ctx, `DELETE FROM org_settings WHERE org_id = $1 AND key = $2`, orgID, key)
	if err != nil {
		return fmt.Errorf("org_settings.Delete: %w", err)
	}
	return nil
}

func (r *OrgSettingsRepository) List(ctx context.Context, orgID string) ([]OrgSetting, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT org_id, key, value, updated_at FROM org_settings WHERE org_id = $1 ORDER BY key ASC`,
		orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("org_settings.List: %w", err)
	}
	defer rows.Close()

	var items []OrgSetting
	for rows.Next() {
		var s OrgSetting
		if err := rows.Scan(&s.OrgID, &s.Key, &s.Value, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("org_settings.List scan: %w", err)
		}
		items = append(items, s)
	}
	return items, rows.Err()
}
