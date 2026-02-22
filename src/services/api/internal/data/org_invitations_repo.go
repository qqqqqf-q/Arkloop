package data

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type OrgInvitation struct {
	ID               uuid.UUID
	OrgID            uuid.UUID
	InvitedByUserID  uuid.UUID
	Email            string
	Role             string
	Token            string
	ExpiresAt        time.Time
	AcceptedAt       *time.Time
	AcceptedByUserID *uuid.UUID
	CreatedAt        time.Time
}

type OrgInvitationsRepository struct {
	db Querier
}

func NewOrgInvitationsRepository(db Querier) (*OrgInvitationsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &OrgInvitationsRepository{db: db}, nil
}

func (r *OrgInvitationsRepository) Create(
	ctx context.Context,
	orgID uuid.UUID,
	invitedByUserID uuid.UUID,
	email string,
	role string,
) (OrgInvitation, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return OrgInvitation{}, fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(raw)
	expiresAt := time.Now().UTC().Add(7 * 24 * time.Hour)

	var inv OrgInvitation
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO org_invitations (org_id, invited_by_user_id, email, role, token, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, org_id, invited_by_user_id, email, role, token,
		           expires_at, accepted_at, accepted_by_user_id, created_at`,
		orgID, invitedByUserID, email, role, token, expiresAt,
	).Scan(
		&inv.ID, &inv.OrgID, &inv.InvitedByUserID, &inv.Email, &inv.Role, &inv.Token,
		&inv.ExpiresAt, &inv.AcceptedAt, &inv.AcceptedByUserID, &inv.CreatedAt,
	)
	if err != nil {
		return OrgInvitation{}, err
	}
	return inv, nil
}

func (r *OrgInvitationsRepository) GetByToken(ctx context.Context, token string) (*OrgInvitation, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var inv OrgInvitation
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, invited_by_user_id, email, role, token,
		        expires_at, accepted_at, accepted_by_user_id, created_at
		 FROM org_invitations
		 WHERE token = $1`,
		token,
	).Scan(
		&inv.ID, &inv.OrgID, &inv.InvitedByUserID, &inv.Email, &inv.Role, &inv.Token,
		&inv.ExpiresAt, &inv.AcceptedAt, &inv.AcceptedByUserID, &inv.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &inv, nil
}

// ListActiveByOrg 返回未接受且未过期的邀请。
func (r *OrgInvitationsRepository) ListActiveByOrg(ctx context.Context, orgID uuid.UUID) ([]OrgInvitation, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, invited_by_user_id, email, role, token,
		        expires_at, accepted_at, accepted_by_user_id, created_at
		 FROM org_invitations
		 WHERE org_id = $1 AND accepted_at IS NULL AND expires_at > now()
		 ORDER BY created_at DESC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	invitations := []OrgInvitation{}
	for rows.Next() {
		var inv OrgInvitation
		if err := rows.Scan(
			&inv.ID, &inv.OrgID, &inv.InvitedByUserID, &inv.Email, &inv.Role, &inv.Token,
			&inv.ExpiresAt, &inv.AcceptedAt, &inv.AcceptedByUserID, &inv.CreatedAt,
		); err != nil {
			return nil, err
		}
		invitations = append(invitations, inv)
	}
	return invitations, rows.Err()
}

// MarkAccepted 在事务内调用，标记邀请已被接受。返回 false 表示邀请不存在或已被并发操作。
func (r *OrgInvitationsRepository) MarkAccepted(ctx context.Context, invitationID, acceptedByUserID uuid.UUID) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	tag, err := r.db.Exec(
		ctx,
		`UPDATE org_invitations
		 SET accepted_at = now(), accepted_by_user_id = $2
		 WHERE id = $1 AND accepted_at IS NULL AND expires_at > now()`,
		invitationID, acceptedByUserID,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// Delete 硬删除未接受的邀请，需同时满足 org_id 以防越权。返回 bool 表示是否命中。
func (r *OrgInvitationsRepository) Delete(ctx context.Context, invitationID, orgID uuid.UUID) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	tag, err := r.db.Exec(
		ctx,
		`DELETE FROM org_invitations WHERE id = $1 AND org_id = $2 AND accepted_at IS NULL`,
		invitationID, orgID,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
