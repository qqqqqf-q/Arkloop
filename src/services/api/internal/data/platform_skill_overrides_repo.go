package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type PlatformSkillOverride struct {
	ProfileRef string
	SkillKey   string
	Version    string
	Status     string // "manual" | "removed"
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type PlatformSkillOverridesRepository struct {
	db Querier
}

func NewPlatformSkillOverridesRepository(db Querier) (*PlatformSkillOverridesRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &PlatformSkillOverridesRepository{db: db}, nil
}

func (r *PlatformSkillOverridesRepository) SetOverride(ctx context.Context, profileRef, skillKey, version, status string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	profileRef = strings.TrimSpace(profileRef)
	skillKey = strings.TrimSpace(skillKey)
	version = strings.TrimSpace(version)
	status = strings.TrimSpace(status)
	if profileRef == "" || skillKey == "" || version == "" || status == "" {
		return fmt.Errorf("override params must not be empty")
	}
	_, err := r.db.Exec(
		ctx,
		`INSERT INTO profile_platform_skill_overrides (profile_ref, skill_key, version, status)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (profile_ref, skill_key, version)
		 DO UPDATE SET status = $4, updated_at = now()`,
		profileRef,
		skillKey,
		version,
		status,
	)
	return err
}

func (r *PlatformSkillOverridesRepository) DeleteOverride(ctx context.Context, profileRef, skillKey, version string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(
		ctx,
		`DELETE FROM profile_platform_skill_overrides WHERE profile_ref = $1 AND skill_key = $2 AND version = $3`,
		strings.TrimSpace(profileRef),
		strings.TrimSpace(skillKey),
		strings.TrimSpace(version),
	)
	return err
}

func (r *PlatformSkillOverridesRepository) ListByProfile(ctx context.Context, profileRef string) ([]PlatformSkillOverride, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT profile_ref, skill_key, version, status, created_at, updated_at
		   FROM profile_platform_skill_overrides
		  WHERE profile_ref = $1
		  ORDER BY skill_key, version`,
		strings.TrimSpace(profileRef),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PlatformSkillOverride, 0)
	for rows.Next() {
		var item PlatformSkillOverride
		if err := rows.Scan(&item.ProfileRef, &item.SkillKey, &item.Version, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *PlatformSkillOverridesRepository) GetOverride(ctx context.Context, profileRef, skillKey, version string) (*PlatformSkillOverride, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var item PlatformSkillOverride
	err := r.db.QueryRow(
		ctx,
		`SELECT profile_ref, skill_key, version, status, created_at, updated_at
		   FROM profile_platform_skill_overrides
		  WHERE profile_ref = $1 AND skill_key = $2 AND version = $3`,
		strings.TrimSpace(profileRef),
		strings.TrimSpace(skillKey),
		strings.TrimSpace(version),
	).Scan(&item.ProfileRef, &item.SkillKey, &item.Version, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}
