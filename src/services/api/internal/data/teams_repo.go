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

	var exists bool
	err := r.db.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM team_memberships WHERE team_id = $1 AND user_id = $2)`,
		teamID, userID,
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// WithTx 返回一个使用给定事务的 TeamRepository 副本。
func (r *TeamRepository) WithTx(tx pgx.Tx) *TeamRepository {
	return &TeamRepository{db: tx}
}

// CountMembers 统计团队当前成员数。
func (r *TeamRepository) CountMembers(ctx context.Context, teamID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM team_memberships WHERE team_id = $1`,
		teamID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("teams.CountMembers: %w", err)
	}
	return count, nil
}

// TeamWithCount 是携带成员数量的 Team 视图。
type TeamWithCount struct {
	Team
	MembersCount int64
}

// ListByOrgWithCounts 返回 org 下所有 team，每行含当前成员数。
func (r *TeamRepository) ListByOrgWithCounts(ctx context.Context, orgID uuid.UUID) ([]TeamWithCount, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return nil, fmt.Errorf("org_id must not be empty")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT t.id, t.org_id, t.name, t.created_at, COUNT(m.user_id)
		 FROM teams t
		 LEFT JOIN team_memberships m ON m.team_id = t.id
		 WHERE t.org_id = $1
		 GROUP BY t.id
		 ORDER BY t.created_at ASC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []TeamWithCount{}
	for rows.Next() {
		var twc TeamWithCount
		if err := rows.Scan(&twc.ID, &twc.OrgID, &twc.Name, &twc.CreatedAt, &twc.MembersCount); err != nil {
			return nil, err
		}
		result = append(result, twc)
	}
	return result, rows.Err()
}

// ListMembers 返回团队的所有成员。
func (r *TeamRepository) ListMembers(ctx context.Context, teamID uuid.UUID) ([]TeamMembership, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT team_id, user_id, role, created_at FROM team_memberships WHERE team_id = $1 ORDER BY created_at ASC`,
		teamID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := []TeamMembership{}
	for rows.Next() {
		var m TeamMembership
		if err := rows.Scan(&m.TeamID, &m.UserID, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// RemoveMember 将用户从团队中移除。
func (r *TeamRepository) RemoveMember(ctx context.Context, teamID, userID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := r.db.Exec(
		ctx,
		`DELETE FROM team_memberships WHERE team_id = $1 AND user_id = $2`,
		teamID, userID,
	)
	return err
}

// Delete 删除团队，org_id 用于防止跨 org 误删。
func (r *TeamRepository) Delete(ctx context.Context, orgID, teamID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := r.db.Exec(
		ctx,
		`DELETE FROM teams WHERE id = $1 AND org_id = $2`,
		teamID, orgID,
	)
	return err
}
