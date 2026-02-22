package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Project struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	TeamID      *uuid.UUID
	Name        string
	Description *string
	Visibility  string
	DeletedAt   *time.Time
	CreatedAt   time.Time
}

type ProjectRepository struct {
	db Querier
}

func NewProjectRepository(db Querier) (*ProjectRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ProjectRepository{db: db}, nil
}

func (r *ProjectRepository) Create(
	ctx context.Context,
	orgID uuid.UUID,
	teamID *uuid.UUID,
	name string,
	description *string,
	visibility string,
) (Project, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return Project{}, fmt.Errorf("org_id must not be empty")
	}
	if name == "" {
		return Project{}, fmt.Errorf("name must not be empty")
	}
	if visibility == "" {
		visibility = "private"
	}

	var p Project
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO projects (org_id, team_id, name, description, visibility)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, org_id, team_id, name, description, visibility, deleted_at, created_at`,
		orgID, teamID, name, description, visibility,
	).Scan(&p.ID, &p.OrgID, &p.TeamID, &p.Name, &p.Description, &p.Visibility, &p.DeletedAt, &p.CreatedAt)
	if err != nil {
		return Project{}, err
	}
	return p, nil
}

func (r *ProjectRepository) GetByID(ctx context.Context, projectID uuid.UUID) (*Project, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var p Project
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, team_id, name, description, visibility, deleted_at, created_at
		 FROM projects
		 WHERE id = $1 AND deleted_at IS NULL`,
		projectID,
	).Scan(&p.ID, &p.OrgID, &p.TeamID, &p.Name, &p.Description, &p.Visibility, &p.DeletedAt, &p.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (r *ProjectRepository) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]Project, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return nil, fmt.Errorf("org_id must not be empty")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, team_id, name, description, visibility, deleted_at, created_at
		 FROM projects
		 WHERE org_id = $1 AND deleted_at IS NULL
		 ORDER BY created_at ASC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(
			&p.ID, &p.OrgID, &p.TeamID, &p.Name, &p.Description, &p.Visibility, &p.DeletedAt, &p.CreatedAt,
		); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}
