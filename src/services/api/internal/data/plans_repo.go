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

type Plan struct {
	ID          uuid.UUID
	Name        string
	DisplayName string
	CreatedAt   time.Time
}

type PlanRepository struct {
	db Querier
}

func NewPlanRepository(db Querier) (*PlanRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &PlanRepository{db: db}, nil
}

func (r *PlanRepository) Create(ctx context.Context, name, displayName string) (Plan, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Plan{}, fmt.Errorf("plans: name must not be empty")
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return Plan{}, fmt.Errorf("plans: display_name must not be empty")
	}

	var p Plan
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO plans (name, display_name)
		 VALUES ($1, $2)
		 RETURNING id, name, display_name, created_at`,
		name, displayName,
	).Scan(&p.ID, &p.Name, &p.DisplayName, &p.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Plan{}, fmt.Errorf("plans: name %q already exists", name)
		}
		return Plan{}, fmt.Errorf("plans.Create: %w", err)
	}
	return p, nil
}

func (r *PlanRepository) GetByID(ctx context.Context, id uuid.UUID) (*Plan, error) {
	var p Plan
	err := r.db.QueryRow(
		ctx,
		`SELECT id, name, display_name, created_at FROM plans WHERE id = $1`,
		id,
	).Scan(&p.ID, &p.Name, &p.DisplayName, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("plans.GetByID: %w", err)
	}
	return &p, nil
}

func (r *PlanRepository) GetByName(ctx context.Context, name string) (*Plan, error) {
	var p Plan
	err := r.db.QueryRow(
		ctx,
		`SELECT id, name, display_name, created_at FROM plans WHERE name = $1`,
		name,
	).Scan(&p.ID, &p.Name, &p.DisplayName, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("plans.GetByName: %w", err)
	}
	return &p, nil
}

func (r *PlanRepository) List(ctx context.Context) ([]Plan, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT id, name, display_name, created_at FROM plans ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("plans.List: %w", err)
	}
	defer rows.Close()

	var plans []Plan
	for rows.Next() {
		var p Plan
		if err := rows.Scan(&p.ID, &p.Name, &p.DisplayName, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("plans.List scan: %w", err)
		}
		plans = append(plans, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plans.List rows: %w", err)
	}
	return plans, nil
}
