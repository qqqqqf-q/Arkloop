package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Referral struct {
	ID             uuid.UUID
	InviterUserID  uuid.UUID
	InviteeUserID  uuid.UUID
	InviteCodeID   uuid.UUID
	Credited       bool
	CreatedAt      time.Time
}

// ReferralWithUsers 列表时附带邀请人/被邀请人信息。
type ReferralWithUsers struct {
	Referral
	InviterDisplayName string
	InviteeDisplayName string
}

// ReferralTreeNode 递归推广树的节点。
type ReferralTreeNode struct {
	UserID      uuid.UUID
	DisplayName string
	InviterID   *uuid.UUID
	Depth       int
	CreatedAt   time.Time
}

type ReferralRepository struct {
	db Querier
}

func NewReferralRepository(db Querier) (*ReferralRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ReferralRepository{db: db}, nil
}

func (r *ReferralRepository) Create(
	ctx context.Context,
	inviterUserID, inviteeUserID, inviteCodeID uuid.UUID,
) (*Referral, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var ref Referral
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO referrals (inviter_user_id, invitee_user_id, invite_code_id)
		 VALUES ($1, $2, $3)
		 RETURNING id, inviter_user_id, invitee_user_id, invite_code_id, credited, created_at`,
		inviterUserID, inviteeUserID, inviteCodeID,
	).Scan(&ref.ID, &ref.InviterUserID, &ref.InviteeUserID, &ref.InviteCodeID, &ref.Credited, &ref.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("referrals.Create: %w", err)
	}
	return &ref, nil
}

func (r *ReferralRepository) GetByInviteeUserID(ctx context.Context, inviteeUserID uuid.UUID) (*Referral, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var ref Referral
	err := r.db.QueryRow(
		ctx,
		`SELECT id, inviter_user_id, invitee_user_id, invite_code_id, credited, created_at
		 FROM referrals WHERE invitee_user_id = $1`,
		inviteeUserID,
	).Scan(&ref.ID, &ref.InviterUserID, &ref.InviteeUserID, &ref.InviteCodeID, &ref.Credited, &ref.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("referrals.GetByInviteeUserID: %w", err)
	}
	return &ref, nil
}

func (r *ReferralRepository) ListByInviterUserID(
	ctx context.Context,
	inviterUserID uuid.UUID,
	limit int,
	beforeCreatedAt *time.Time,
	beforeID *uuid.UUID,
) ([]ReferralWithUsers, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		return nil, fmt.Errorf("referrals.ListByInviterUserID: limit must be positive")
	}
	if (beforeCreatedAt == nil) != (beforeID == nil) {
		return nil, fmt.Errorf("referrals.ListByInviterUserID: before_created_at and before_id must be provided together")
	}

	sql := `SELECT r.id, r.inviter_user_id, r.invitee_user_id, r.invite_code_id, r.credited, r.created_at,
	               u_inviter.display_name, u_invitee.display_name
	        FROM referrals r
	        JOIN users u_inviter ON u_inviter.id = r.inviter_user_id
	        JOIN users u_invitee ON u_invitee.id = r.invitee_user_id
	        WHERE r.inviter_user_id = $1`
	args := []any{inviterUserID}
	argIdx := 2

	if beforeCreatedAt != nil && beforeID != nil {
		sql += fmt.Sprintf(" AND (r.created_at < $%d OR (r.created_at = $%d AND r.id < $%d))", argIdx, argIdx, argIdx+1)
		args = append(args, beforeCreatedAt.UTC(), *beforeID)
		argIdx += 2
	}

	sql += fmt.Sprintf(" ORDER BY r.created_at DESC, r.id DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("referrals.ListByInviterUserID: %w", err)
	}
	defer rows.Close()

	var items []ReferralWithUsers
	for rows.Next() {
		var item ReferralWithUsers
		if err := rows.Scan(
			&item.ID, &item.InviterUserID, &item.InviteeUserID, &item.InviteCodeID,
			&item.Credited, &item.CreatedAt,
			&item.InviterDisplayName, &item.InviteeDisplayName,
		); err != nil {
			return nil, fmt.Errorf("referrals.ListByInviterUserID scan: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetReferralTree 递归查询推广树（以 userID 为根，向下展开被邀请人），限深度 maxDepth。
func (r *ReferralRepository) GetReferralTree(ctx context.Context, userID uuid.UUID, maxDepth int) ([]ReferralTreeNode, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxDepth <= 0 || maxDepth > 3 {
		maxDepth = 3
	}

	rows, err := r.db.Query(
		ctx,
		`WITH RECURSIVE tree AS (
		    SELECT u.id AS user_id, u.display_name, NULL::uuid AS inviter_id, 0 AS depth, u.created_at
		    FROM users u WHERE u.id = $1
		    UNION ALL
		    SELECT u.id, u.display_name, r.inviter_user_id, tree.depth + 1, r.created_at
		    FROM referrals r
		    JOIN users u ON u.id = r.invitee_user_id
		    JOIN tree ON tree.user_id = r.inviter_user_id
		    WHERE tree.depth < $2
		 )
		 SELECT user_id, display_name, inviter_id, depth, created_at
		 FROM tree
		 ORDER BY depth ASC, created_at ASC`,
		userID, maxDepth,
	)
	if err != nil {
		return nil, fmt.Errorf("referrals.GetReferralTree: %w", err)
	}
	defer rows.Close()

	var nodes []ReferralTreeNode
	for rows.Next() {
		var n ReferralTreeNode
		if err := rows.Scan(&n.UserID, &n.DisplayName, &n.InviterID, &n.Depth, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("referrals.GetReferralTree scan: %w", err)
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func (r *ReferralRepository) MarkCredited(ctx context.Context, id uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(ctx, `UPDATE referrals SET credited = true WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("referrals.MarkCredited: %w", err)
	}
	return nil
}
