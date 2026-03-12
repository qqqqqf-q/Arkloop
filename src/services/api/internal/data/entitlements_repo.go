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

// PlanEntitlement 表示 plan 下的某个权益配置项。
type PlanEntitlement struct {
	ID        uuid.UUID
	PlanID    uuid.UUID
	Key       string
	Value     string
	ValueType string
}

// AccountEntitlementOverride 表示对单个 account 的权益覆盖。
type AccountEntitlementOverride struct {
	ID              uuid.UUID
	AccountID           uuid.UUID
	Key             string
	Value           string
	ValueType       string
	Reason          *string
	ExpiresAt       *time.Time
	CreatedByUserID *uuid.UUID
	CreatedAt       time.Time
}

type EntitlementsRepository struct {
	db Querier
}

func NewEntitlementsRepository(db Querier) (*EntitlementsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &EntitlementsRepository{db: db}, nil
}

var validValueTypes = map[string]struct{}{
	"int":    {},
	"bool":   {},
	"string": {},
}

// SetForPlan upsert plan_entitlement，同 key 下重复则更新 value/value_type。
func (r *EntitlementsRepository) SetForPlan(
	ctx context.Context,
	planID uuid.UUID,
	key, value, valueType string,
) (PlanEntitlement, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return PlanEntitlement{}, fmt.Errorf("entitlements: key must not be empty")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return PlanEntitlement{}, fmt.Errorf("entitlements: value must not be empty")
	}
	valueType = strings.TrimSpace(valueType)
	if _, ok := validValueTypes[valueType]; !ok {
		return PlanEntitlement{}, fmt.Errorf("entitlements: value_type must be one of int, bool, string")
	}

	var pe PlanEntitlement
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO plan_entitlements (plan_id, key, value, value_type)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (plan_id, key)
		 DO UPDATE SET value = EXCLUDED.value, value_type = EXCLUDED.value_type
		 RETURNING id, plan_id, key, value, value_type`,
		planID, key, value, valueType,
	).Scan(&pe.ID, &pe.PlanID, &pe.Key, &pe.Value, &pe.ValueType)
	if err != nil {
		return PlanEntitlement{}, fmt.Errorf("entitlements.SetForPlan: %w", err)
	}
	return pe, nil
}

func (r *EntitlementsRepository) ListByPlan(ctx context.Context, planID uuid.UUID) ([]PlanEntitlement, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT id, plan_id, key, value, value_type
		 FROM plan_entitlements
		 WHERE plan_id = $1
		 ORDER BY key ASC`,
		planID,
	)
	if err != nil {
		return nil, fmt.Errorf("entitlements.ListByPlan: %w", err)
	}
	defer rows.Close()

	var items []PlanEntitlement
	for rows.Next() {
		var pe PlanEntitlement
		if err := rows.Scan(&pe.ID, &pe.PlanID, &pe.Key, &pe.Value, &pe.ValueType); err != nil {
			return nil, fmt.Errorf("entitlements.ListByPlan scan: %w", err)
		}
		items = append(items, pe)
	}
	return items, rows.Err()
}

// GetPlanEntitlement 查询 plan 下指定 key 的权益。
func (r *EntitlementsRepository) GetPlanEntitlement(ctx context.Context, planID uuid.UUID, key string) (*PlanEntitlement, error) {
	var pe PlanEntitlement
	err := r.db.QueryRow(
		ctx,
		`SELECT id, plan_id, key, value, value_type
		 FROM plan_entitlements
		 WHERE plan_id = $1 AND key = $2`,
		planID, key,
	).Scan(&pe.ID, &pe.PlanID, &pe.Key, &pe.Value, &pe.ValueType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("entitlements.GetPlanEntitlement: %w", err)
	}
	return &pe, nil
}

func (r *EntitlementsRepository) CreateOverride(
	ctx context.Context,
	accountID uuid.UUID,
	key, value, valueType string,
	reason *string,
	expiresAt *time.Time,
	createdByUserID uuid.UUID,
) (AccountEntitlementOverride, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return AccountEntitlementOverride{}, fmt.Errorf("entitlements: key must not be empty")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return AccountEntitlementOverride{}, fmt.Errorf("entitlements: value must not be empty")
	}
	valueType = strings.TrimSpace(valueType)
	if _, ok := validValueTypes[valueType]; !ok {
		return AccountEntitlementOverride{}, fmt.Errorf("entitlements: value_type must be one of int, bool, string")
	}
	if accountID == uuid.Nil {
		return AccountEntitlementOverride{}, fmt.Errorf("entitlements: account_id must not be empty")
	}

	var o AccountEntitlementOverride
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO account_entitlement_overrides (account_id, key, value, value_type, reason, expires_at, created_by_user_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (account_id, key)
		 DO UPDATE SET value = EXCLUDED.value, value_type = EXCLUDED.value_type,
		              reason = EXCLUDED.reason, expires_at = EXCLUDED.expires_at,
		              created_by_user_id = EXCLUDED.created_by_user_id
		 RETURNING id, account_id, key, value, value_type, reason, expires_at, created_by_user_id, created_at`,
		accountID, key, value, valueType, reason, expiresAt, createdByUserID,
	).Scan(
		&o.ID, &o.AccountID, &o.Key, &o.Value, &o.ValueType,
		&o.Reason, &o.ExpiresAt, &o.CreatedByUserID, &o.CreatedAt,
	)
	if err != nil {
		return AccountEntitlementOverride{}, fmt.Errorf("entitlements.CreateOverride: %w", err)
	}
	return o, nil
}

func (r *EntitlementsRepository) ListOverridesByOrg(ctx context.Context, accountID uuid.UUID) ([]AccountEntitlementOverride, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT id, account_id, key, value, value_type, reason, expires_at, created_by_user_id, created_at
		 FROM account_entitlement_overrides
		 WHERE account_id = $1
		 ORDER BY key ASC`,
		accountID,
	)
	if err != nil {
		return nil, fmt.Errorf("entitlements.ListOverridesByOrg: %w", err)
	}
	defer rows.Close()

	var items []AccountEntitlementOverride
	for rows.Next() {
		var o AccountEntitlementOverride
		if err := rows.Scan(
			&o.ID, &o.AccountID, &o.Key, &o.Value, &o.ValueType,
			&o.Reason, &o.ExpiresAt, &o.CreatedByUserID, &o.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("entitlements.ListOverridesByOrg scan: %w", err)
		}
		items = append(items, o)
	}
	return items, rows.Err()
}

func (r *EntitlementsRepository) GetOverrideByOrgAndKey(
	ctx context.Context,
	accountID uuid.UUID,
	key string,
) (*AccountEntitlementOverride, error) {
	var o AccountEntitlementOverride
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, key, value, value_type, reason, expires_at, created_by_user_id, created_at
		 FROM account_entitlement_overrides
		 WHERE account_id = $1 AND key = $2`,
		accountID, key,
	).Scan(
		&o.ID, &o.AccountID, &o.Key, &o.Value, &o.ValueType,
		&o.Reason, &o.ExpiresAt, &o.CreatedByUserID, &o.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("entitlements.GetOverrideByOrgAndKey: %w", err)
	}
	return &o, nil
}

func (r *EntitlementsRepository) GetOverrideByID(
	ctx context.Context,
	id uuid.UUID,
	accountID uuid.UUID,
) (*AccountEntitlementOverride, error) {
	var o AccountEntitlementOverride
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, key, value, value_type, reason, expires_at, created_by_user_id, created_at
		 FROM account_entitlement_overrides
		 WHERE id = $1 AND account_id = $2`,
		id, accountID,
	).Scan(
		&o.ID, &o.AccountID, &o.Key, &o.Value, &o.ValueType,
		&o.Reason, &o.ExpiresAt, &o.CreatedByUserID, &o.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("entitlements.GetOverrideByID: %w", err)
	}
	return &o, nil
}

// GetOverride 查询 account 下指定 key 的未过期 override。
func (r *EntitlementsRepository) GetOverride(ctx context.Context, accountID uuid.UUID, key string) (*AccountEntitlementOverride, error) {
	var o AccountEntitlementOverride
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, key, value, value_type, reason, expires_at, created_by_user_id, created_at
		 FROM account_entitlement_overrides
		 WHERE account_id = $1 AND key = $2
		   AND (expires_at IS NULL OR expires_at > now())`,
		accountID, key,
	).Scan(
		&o.ID, &o.AccountID, &o.Key, &o.Value, &o.ValueType,
		&o.Reason, &o.ExpiresAt, &o.CreatedByUserID, &o.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("entitlements.GetOverride: %w", err)
	}
	return &o, nil
}

func (r *EntitlementsRepository) DeleteOverride(ctx context.Context, id, accountID uuid.UUID) error {
	tag, err := r.db.Exec(
		ctx,
		`DELETE FROM account_entitlement_overrides WHERE id = $1 AND account_id = $2`,
		id, accountID,
	)
	if err != nil {
		return fmt.Errorf("entitlements.DeleteOverride: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return nil
}
