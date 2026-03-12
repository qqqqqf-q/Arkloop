package data

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type RBACRole struct {
	ID          uuid.UUID
	AccountID       *uuid.UUID
	Name        string
	Permissions []string
	IsSystem    bool
	CreatedAt   time.Time
}

type RBACRolesRepository struct {
	db Querier
}

func NewRBACRolesRepository(db Querier) (*RBACRolesRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &RBACRolesRepository{db: db}, nil
}

// GetSystemRole 按名称查询系统角色（account_id IS NULL）。
func (r *RBACRolesRepository) GetSystemRole(ctx context.Context, name string) (*RBACRole, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var role RBACRole
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, name, permissions, is_system, created_at
		 FROM rbac_roles
		 WHERE name = $1 AND account_id IS NULL`,
		name,
	).Scan(&role.ID, &role.AccountID, &role.Name, &role.Permissions, &role.IsSystem, &role.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &role, nil
}

// ListSystemRoles 返回所有系统内置角色。
func (r *RBACRolesRepository) ListSystemRoles(ctx context.Context) ([]RBACRole, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, account_id, name, permissions, is_system, created_at
		 FROM rbac_roles
		 WHERE account_id IS NULL AND is_system = TRUE
		 ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []RBACRole
	for rows.Next() {
		var role RBACRole
		if err := rows.Scan(&role.ID, &role.AccountID, &role.Name, &role.Permissions, &role.IsSystem, &role.CreatedAt); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}
