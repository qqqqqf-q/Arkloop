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

type AccountFeatureOverride struct {
	AccountID     uuid.UUID
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

func (r *FeatureFlagRepository) GetOrgOverride(
	ctx context.Context,
	accountID uuid.UUID,
	flagKey string,
) (*AccountFeatureOverride, error) {
	var o AccountFeatureOverride
	err := r.db.QueryRow(
		ctx,
		`SELECT account_id, flag_key, enabled, created_at
		 FROM account_feature_overrides
		 WHERE account_id = $1 AND flag_key = $2`,
		accountID, flagKey,
	).Scan(&o.AccountID, &o.FlagKey, &o.Enabled, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("feature_flags.GetOrgOverride: %w", err)
	}
	return &o, nil
}
