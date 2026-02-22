package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Team struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	Name      string
	CreatedAt time.Time
}

type TeamMembership struct {
	TeamID    uuid.UUID
	UserID    uuid.UUID
	Role      string
	CreatedAt time.Time
}

type TeamRepository struct {
	db Querier
}

func NewTeamRepository(db Querier) (*TeamRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &TeamRepository{db: db}, nil
}

func (r *TeamRepository) Create(ctx context.Context, orgID uuid.UUID, name string) (Team, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return Team{}, fmt.Errorf("org_id must not be empty")
	}
	if name == "" {
		return Team{}, fmt.Errorf("name must not be empty")
	}

	var t Team
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO teams (org_id, name)
		 VALUES ($1, $2)
		 RETURNING id, org_id, name, created_at`,
		orgID, name,
	).Scan(&t.ID, &t.OrgID, &t.Name, &t.CreatedAt)
	if err != nil {
		return Team{}, err
	}
	return t, nil
}

func (r *TeamRepository) GetByID(ctx context.Context, teamID uuid.UUID) (*Team, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var t Team
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, name, created_at FROM teams WHERE id = $1`,
		teamID,
	).Scan(&t.ID, &t.OrgID, &t.Name, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

func (r *TeamRepository) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]Team, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return nil, fmt.Errorf("org_id must not be empty")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, name, created_at FROM teams WHERE org_id = $1 ORDER BY created_at ASC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	teams := []Team{}
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.OrgID, &t.Name, &t.CreatedAt); err != nil {
			return nil, err
		}
		teams = append(teams, t)
	}
	return teams, rows.Err()
}

// AddMember 将用户加入团队。若已是成员则返回 false。
func (r *TeamRepository) AddMember(ctx context.Context, teamID, userID uuid.UUID, role string) (TeamMembership, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if role == "" {
		role = "member"
	}

	var m TeamMembership
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO team_memberships (team_id, user_id, role)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (team_id, user_id) DO UPDATE SET role = EXCLUDED.role
		 RETURNING team_id, user_id, role, created_at`,
		teamID, userID, role,
	).Scan(&m.TeamID, &m.UserID, &m.Role, &m.CreatedAt)
	if err != nil {
		return TeamMembership{}, err
	}
	return m, nil
}

// IsMember 检查用户是否是团队成员。
func (r *TeamRepository) IsMember(ctx context.Context, teamID, userID uuid.UUID) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var count int
	err := r.db.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM team_memberships WHERE team_id = $1 AND user_id = $2`,
		teamID, userID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
