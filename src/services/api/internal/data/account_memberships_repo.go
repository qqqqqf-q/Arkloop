package data

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AccountMembership struct {
	ID        uuid.UUID
	AccountID     uuid.UUID
	UserID    uuid.UUID
	Role      string
	RoleID    *uuid.UUID
	CreatedAt time.Time
}

type AccountMembershipRepository struct {
	db Querier
}

func NewAccountMembershipRepository(db Querier) (*AccountMembershipRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &AccountMembershipRepository{db: db}, nil
}

func (r *AccountMembershipRepository) Create(
	ctx context.Context,
	accountID uuid.UUID,
	userID uuid.UUID,
	role string,
) (AccountMembership, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cleanedRole := strings.TrimSpace(role)
	if cleanedRole == "" {
		cleanedRole = "member"
	}

	var membership AccountMembership
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO account_memberships (account_id, user_id, role)
		 VALUES ($1, $2, $3)
		 RETURNING id, account_id, user_id, role, role_id, created_at`,
		accountID,
		userID,
		cleanedRole,
	).Scan(&membership.ID, &membership.AccountID, &membership.UserID, &membership.Role, &membership.RoleID, &membership.CreatedAt)
	if err != nil {
		return AccountMembership{}, err
	}
	return membership, nil
}

func (r *AccountMembershipRepository) GetDefaultForUser(ctx context.Context, userID uuid.UUID) (*AccountMembership, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var membership AccountMembership
	err := r.db.QueryRow(
		ctx,
		`SELECT m.id, m.account_id, m.user_id, m.role, m.role_id, m.created_at
		 FROM account_memberships m
		 JOIN accounts o ON o.id = m.account_id
		 WHERE m.user_id = $1
		   AND o.type = 'personal'
		 LIMIT 1`,
		userID,
	).Scan(&membership.ID, &membership.AccountID, &membership.UserID, &membership.Role, &membership.RoleID, &membership.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &membership, nil
}

func (r *AccountMembershipRepository) GetByOrgAndUser(ctx context.Context, accountID, userID uuid.UUID) (*AccountMembership, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil || userID == uuid.Nil {
		return nil, nil
	}

	var membership AccountMembership
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, user_id, role, role_id, created_at
		 FROM account_memberships
		 WHERE account_id = $1 AND user_id = $2
		 LIMIT 1`,
		accountID,
		userID,
	).Scan(&membership.ID, &membership.AccountID, &membership.UserID, &membership.Role, &membership.RoleID, &membership.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &membership, nil
}

// SetRoleForUser 将用户的默认 membership（最早创建）的角色更新为 role。
func (r *AccountMembershipRepository) SetRoleForUser(ctx context.Context, userID uuid.UUID, role string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(
		ctx,
		`UPDATE account_memberships
		 SET role = $1
		 WHERE id = (
		     SELECT m.id
		     FROM account_memberships m
		     JOIN accounts o ON o.id = m.account_id
		     WHERE m.user_id = $2
		       AND o.type = 'personal'
		     LIMIT 1
		 )`,
		role, userID,
	)
	return err
}

func (r *AccountMembershipRepository) HasPlatformAdmin(ctx context.Context) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var exists bool
	err := r.db.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM account_memberships WHERE role = $1)`,
		"platform_admin",
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// ExistsForOrgAndUser 检查用户是否已是 account 成员，用于邀请接受前去重。
func (r *AccountMembershipRepository) ExistsForOrgAndUser(ctx context.Context, accountID, userID uuid.UUID) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var exists bool
	err := r.db.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM account_memberships WHERE account_id = $1 AND user_id = $2)`,
		accountID, userID,
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
