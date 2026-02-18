package data

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type OrgMembership struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	UserID    uuid.UUID
	Role      string
	CreatedAt time.Time
}

type OrgMembershipRepository struct {
	db Querier
}

func NewOrgMembershipRepository(db Querier) (*OrgMembershipRepository, error) {
	if db == nil {
		return nil, errors.New("db 不能为空")
	}
	return &OrgMembershipRepository{db: db}, nil
}

func (r *OrgMembershipRepository) Create(
	ctx context.Context,
	orgID uuid.UUID,
	userID uuid.UUID,
	role string,
) (OrgMembership, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cleanedRole := strings.TrimSpace(role)
	if cleanedRole == "" {
		cleanedRole = "member"
	}

	var membership OrgMembership
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO org_memberships (org_id, user_id, role)
		 VALUES ($1, $2, $3)
		 RETURNING id, org_id, user_id, role, created_at`,
		orgID,
		userID,
		cleanedRole,
	).Scan(&membership.ID, &membership.OrgID, &membership.UserID, &membership.Role, &membership.CreatedAt)
	if err != nil {
		return OrgMembership{}, err
	}
	return membership, nil
}

func (r *OrgMembershipRepository) GetDefaultForUser(ctx context.Context, userID uuid.UUID) (*OrgMembership, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var membership OrgMembership
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, user_id, role, created_at
		 FROM org_memberships
		 WHERE user_id = $1
		 ORDER BY created_at ASC
		 LIMIT 1`,
		userID,
	).Scan(&membership.ID, &membership.OrgID, &membership.UserID, &membership.Role, &membership.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &membership, nil
}
