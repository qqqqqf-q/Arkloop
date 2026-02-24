package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type FeatureFlag struct {
	ID           uuid.UUID
	Key          string
	Description  *string
	DefaultValue bool
	CreatedAt    time.Time
}

type OrgFeatureOverride struct {
	OrgID     uuid.UUID
	FlagKey   string
	Enabled   bool
	CreatedAt time.Time
}

type FeatureFlagRepository struct {
	db Querier
}

func NewFeatureFlagRepository(db Querier) (*FeatureFlagRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &FeatureFlagRepository{db: db}, nil
}

func (r *FeatureFlagRepository) CreateFlag(
	ctx context.Context,
	key string,
	description *string,
	defaultValue bool,
) (FeatureFlag, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return FeatureFlag{}, fmt.Errorf("feature_flags: key must not be empty")
	}

	var f FeatureFlag
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO feature_flags (key, description, default_value)
		 VALUES ($1, $2, $3)
		 RETURNING id, key, description, default_value, created_at`,
		key, description, defaultValue,
	).Scan(&f.ID, &f.Key, &f.Description, &f.DefaultValue, &f.CreatedAt)
	if err != nil {
		return FeatureFlag{}, fmt.Errorf("feature_flags.CreateFlag: %w", err)
	}
	return f, nil
}

func (r *FeatureFlagRepository) GetFlag(ctx context.Context, key string) (*FeatureFlag, error) {
	var f FeatureFlag
	err := r.db.QueryRow(
		ctx,
		`SELECT id, key, description, default_value, created_at
		 FROM feature_flags WHERE key = $1`,
		key,
	).Scan(&f.ID, &f.Key, &f.Description, &f.DefaultValue, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("feature_flags.GetFlag: %w", err)
	}
	return &f, nil
}

func (r *FeatureFlagRepository) ListFlags(ctx context.Context) ([]FeatureFlag, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT id, key, description, default_value, created_at
		 FROM feature_flags ORDER BY key ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("feature_flags.ListFlags: %w", err)
	}
	defer rows.Close()

	var items []FeatureFlag
	for rows.Next() {
		var f FeatureFlag
		if err := rows.Scan(&f.ID, &f.Key, &f.Description, &f.DefaultValue, &f.CreatedAt); err != nil {
			return nil, fmt.Errorf("feature_flags.ListFlags scan: %w", err)
		}
		items = append(items, f)
	}
	return items, rows.Err()
}

// UpdateFlagDefaultValue 更新 flag 的全局默认值。
func (r *FeatureFlagRepository) UpdateFlagDefaultValue(
	ctx context.Context,
	key string,
	defaultValue bool,
) (*FeatureFlag, error) {
	var f FeatureFlag
	err := r.db.QueryRow(
		ctx,
		`UPDATE feature_flags SET default_value = $1 WHERE key = $2
		 RETURNING id, key, description, default_value, created_at`,
		defaultValue, key,
	).Scan(&f.ID, &f.Key, &f.Description, &f.DefaultValue, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("feature_flags.UpdateFlagDefaultValue: %w", err)
	}
	return &f, nil
}

func (r *FeatureFlagRepository) DeleteFlag(ctx context.Context, key string) error {
	_, err := r.db.Exec(
		ctx,
		`DELETE FROM feature_flags WHERE key = $1`,
		key,
	)
	if err != nil {
		return fmt.Errorf("feature_flags.DeleteFlag: %w", err)
	}
	return nil
}

// SetOrgOverride upsert org 级 override，同 (org_id, flag_key) 重复则更新 enabled。
func (r *FeatureFlagRepository) SetOrgOverride(
	ctx context.Context,
	orgID uuid.UUID,
	flagKey string,
	enabled bool,
) (OrgFeatureOverride, error) {
	flagKey = strings.TrimSpace(flagKey)
	if flagKey == "" {
		return OrgFeatureOverride{}, fmt.Errorf("feature_flags: flag_key must not be empty")
	}
	if orgID == uuid.Nil {
		return OrgFeatureOverride{}, fmt.Errorf("feature_flags: org_id must not be empty")
	}

	var o OrgFeatureOverride
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO org_feature_overrides (org_id, flag_key, enabled)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (org_id, flag_key)
		 DO UPDATE SET enabled = EXCLUDED.enabled
		 RETURNING org_id, flag_key, enabled, created_at`,
		orgID, flagKey, enabled,
	).Scan(&o.OrgID, &o.FlagKey, &o.Enabled, &o.CreatedAt)
	if err != nil {
		return OrgFeatureOverride{}, fmt.Errorf("feature_flags.SetOrgOverride: %w", err)
	}
	return o, nil
}

func (r *FeatureFlagRepository) GetOrgOverride(
	ctx context.Context,
	orgID uuid.UUID,
	flagKey string,
) (*OrgFeatureOverride, error) {
	var o OrgFeatureOverride
	err := r.db.QueryRow(
		ctx,
		`SELECT org_id, flag_key, enabled, created_at
		 FROM org_feature_overrides
		 WHERE org_id = $1 AND flag_key = $2`,
		orgID, flagKey,
	).Scan(&o.OrgID, &o.FlagKey, &o.Enabled, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("feature_flags.GetOrgOverride: %w", err)
	}
	return &o, nil
}

func (r *FeatureFlagRepository) ListOrgOverrides(
	ctx context.Context,
	orgID uuid.UUID,
) ([]OrgFeatureOverride, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT org_id, flag_key, enabled, created_at
		 FROM org_feature_overrides
		 WHERE org_id = $1 ORDER BY flag_key ASC`,
		orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("feature_flags.ListOrgOverrides: %w", err)
	}
	defer rows.Close()

	var items []OrgFeatureOverride
	for rows.Next() {
		var o OrgFeatureOverride
		if err := rows.Scan(&o.OrgID, &o.FlagKey, &o.Enabled, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("feature_flags.ListOrgOverrides scan: %w", err)
		}
		items = append(items, o)
	}
	return items, rows.Err()
}

// ListOverridesByFlag lists all org overrides for a given flag key.
func (r *FeatureFlagRepository) ListOverridesByFlag(
	ctx context.Context,
	flagKey string,
) ([]OrgFeatureOverride, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT org_id, flag_key, enabled, created_at
		 FROM org_feature_overrides
		 WHERE flag_key = $1 ORDER BY org_id ASC`,
		flagKey,
	)
	if err != nil {
		return nil, fmt.Errorf("feature_flags.ListOverridesByFlag: %w", err)
	}
	defer rows.Close()

	var items []OrgFeatureOverride
	for rows.Next() {
		var o OrgFeatureOverride
		if err := rows.Scan(&o.OrgID, &o.FlagKey, &o.Enabled, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("feature_flags.ListOverridesByFlag scan: %w", err)
		}
		items = append(items, o)
	}
	return items, rows.Err()
}

func (r *FeatureFlagRepository) DeleteOrgOverride(
	ctx context.Context,
	orgID uuid.UUID,
	flagKey string,
) error {
	_, err := r.db.Exec(
		ctx,
		`DELETE FROM org_feature_overrides WHERE org_id = $1 AND flag_key = $2`,
		orgID, flagKey,
	)
	if err != nil {
		return fmt.Errorf("feature_flags.DeleteOrgOverride: %w", err)
	}
	return nil
}
