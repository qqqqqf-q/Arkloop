package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const DefaultProjectName = "Default"

type Project struct {
	ID          uuid.UUID
	AccountID   uuid.UUID
	TeamID      *uuid.UUID
	OwnerUserID *uuid.UUID
	Name        string
	Description *string
	Visibility  string
	IsDefault   bool
	DeletedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ProjectRepository struct {
	db Querier
}

func (r *ProjectRepository) WithTx(tx pgx.Tx) *ProjectRepository {
	return &ProjectRepository{db: tx}
}

func NewProjectRepository(db Querier) (*ProjectRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ProjectRepository{db: db}, nil
}

func (r *ProjectRepository) Create(
	ctx context.Context,
	accountID uuid.UUID,
	teamID *uuid.UUID,
	name string,
	description *string,
	visibility string,
) (Project, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return Project{}, fmt.Errorf("account_id must not be empty")
	}
	if name == "" {
		return Project{}, fmt.Errorf("name must not be empty")
	}
	if visibility == "" {
		visibility = "private"
	}
	return r.createWithOwner(ctx, accountID, teamID, nil, name, description, visibility, false)
}

func (r *ProjectRepository) CreateDefaultForOwner(
	ctx context.Context,
	accountID uuid.UUID,
	ownerUserID uuid.UUID,
) (Project, error) {
	if ownerUserID == uuid.Nil {
		return Project{}, fmt.Errorf("owner_user_id must not be empty")
	}
	return r.createWithOwner(ctx, accountID, nil, &ownerUserID, DefaultProjectName, nil, "private", true)
}

func (r *ProjectRepository) createWithOwner(
	ctx context.Context,
	accountID uuid.UUID,
	teamID *uuid.UUID,
	ownerUserID *uuid.UUID,
	name string,
	description *string,
	visibility string,
	isDefault bool,
) (Project, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return Project{}, fmt.Errorf("account_id must not be empty")
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
		`INSERT INTO projects (account_id, team_id, owner_user_id, name, description, visibility, is_default, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		 RETURNING id, account_id, team_id, owner_user_id, name, description, visibility, is_default, deleted_at, created_at, updated_at`,
		accountID, teamID, ownerUserID, name, description, visibility, isDefault,
	).Scan(&p.ID, &p.AccountID, &p.TeamID, &p.OwnerUserID, &p.Name, &p.Description, &p.Visibility, &p.IsDefault, &p.DeletedAt, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return Project{}, err
	}
	return p, nil
}

func (r *ProjectRepository) GetDefaultByOwner(ctx context.Context, accountID uuid.UUID, ownerUserID uuid.UUID) (*Project, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
	}
	if ownerUserID == uuid.Nil {
		return nil, fmt.Errorf("owner_user_id must not be empty")
	}

	var p Project
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, team_id, owner_user_id, name, description, visibility, is_default, deleted_at, created_at, updated_at
		 FROM projects
		 WHERE account_id = $1
		   AND owner_user_id = $2
		   AND is_default = true
		   AND deleted_at IS NULL
		 LIMIT 1`,
		accountID,
		ownerUserID,
	).Scan(&p.ID, &p.AccountID, &p.TeamID, &p.OwnerUserID, &p.Name, &p.Description, &p.Visibility, &p.IsDefault, &p.DeletedAt, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (r *ProjectRepository) GetOrCreateDefaultByOwner(ctx context.Context, accountID uuid.UUID, ownerUserID uuid.UUID) (Project, error) {
	existing, err := r.GetDefaultByOwner(ctx, accountID, ownerUserID)
	if err != nil {
		return Project{}, err
	}
	if existing != nil {
		return *existing, nil
	}

	created, err := r.CreateDefaultForOwner(ctx, accountID, ownerUserID)
	if err == nil {
		return created, nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		existing, getErr := r.GetDefaultByOwner(ctx, accountID, ownerUserID)
		if getErr != nil {
			return Project{}, getErr
		}
		if existing != nil {
			return *existing, nil
		}
	}

	return Project{}, err
}

func (r *ProjectRepository) GetByID(ctx context.Context, projectID uuid.UUID) (*Project, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var p Project
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, team_id, owner_user_id, name, description, visibility, is_default, deleted_at, created_at, updated_at
		 FROM projects
		 WHERE id = $1 AND deleted_at IS NULL`,
		projectID,
	).Scan(&p.ID, &p.AccountID, &p.TeamID, &p.OwnerUserID, &p.Name, &p.Description, &p.Visibility, &p.IsDefault, &p.DeletedAt, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (r *ProjectRepository) SoftDelete(ctx context.Context, projectID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if projectID == uuid.Nil {
		return fmt.Errorf("project_id must not be empty")
	}
	_, err := r.db.Exec(ctx,
		`UPDATE projects SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`,
		projectID,
	)
	return err
}

func (r *ProjectRepository) ListByAccount(ctx context.Context, accountID uuid.UUID) ([]Project, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, account_id, team_id, owner_user_id, name, description, visibility, is_default, deleted_at, created_at, updated_at
		 FROM projects
		 WHERE account_id = $1 AND deleted_at IS NULL
		 ORDER BY created_at ASC`,
		accountID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(
			&p.ID, &p.AccountID, &p.TeamID, &p.OwnerUserID, &p.Name, &p.Description, &p.Visibility, &p.IsDefault, &p.DeletedAt, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}
