package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Subscription struct {
	ID                 uuid.UUID
	OrgID              uuid.UUID
	PlanID             uuid.UUID
	Status             string
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   time.Time
	CancelledAt        *time.Time
	CreatedAt          time.Time
}

type SubscriptionRepository struct {
	db Querier
}

func NewSubscriptionRepository(db Querier) (*SubscriptionRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &SubscriptionRepository{db: db}, nil
}

func (r *SubscriptionRepository) Create(
	ctx context.Context,
	orgID, planID uuid.UUID,
	periodStart, periodEnd time.Time,
) (Subscription, error) {
	if orgID == uuid.Nil {
		return Subscription{}, fmt.Errorf("subscriptions: org_id must not be empty")
	}
	if planID == uuid.Nil {
		return Subscription{}, fmt.Errorf("subscriptions: plan_id must not be empty")
	}
	if !periodEnd.After(periodStart) {
		return Subscription{}, fmt.Errorf("subscriptions: period_end must be after period_start")
	}

	var s Subscription
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO subscriptions (org_id, plan_id, current_period_start, current_period_end)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, org_id, plan_id, status, current_period_start, current_period_end, cancelled_at, created_at`,
		orgID, planID, periodStart, periodEnd,
	).Scan(
		&s.ID, &s.OrgID, &s.PlanID, &s.Status,
		&s.CurrentPeriodStart, &s.CurrentPeriodEnd, &s.CancelledAt, &s.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return Subscription{}, fmt.Errorf("subscriptions: org already has an active subscription")
		}
		return Subscription{}, fmt.Errorf("subscriptions.Create: %w", err)
	}
	return s, nil
}

func (r *SubscriptionRepository) GetByID(ctx context.Context, id uuid.UUID) (*Subscription, error) {
	var s Subscription
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, plan_id, status, current_period_start, current_period_end, cancelled_at, created_at
		 FROM subscriptions WHERE id = $1`,
		id,
	).Scan(
		&s.ID, &s.OrgID, &s.PlanID, &s.Status,
		&s.CurrentPeriodStart, &s.CurrentPeriodEnd, &s.CancelledAt, &s.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("subscriptions.GetByID: %w", err)
	}
	return &s, nil
}

// GetActiveByOrgID 返回 org 当前 active 的订阅。
func (r *SubscriptionRepository) GetActiveByOrgID(ctx context.Context, orgID uuid.UUID) (*Subscription, error) {
	var s Subscription
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, plan_id, status, current_period_start, current_period_end, cancelled_at, created_at
		 FROM subscriptions
		 WHERE org_id = $1 AND status = 'active'`,
		orgID,
	).Scan(
		&s.ID, &s.OrgID, &s.PlanID, &s.Status,
		&s.CurrentPeriodStart, &s.CurrentPeriodEnd, &s.CancelledAt, &s.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("subscriptions.GetActiveByOrgID: %w", err)
	}
	return &s, nil
}

func (r *SubscriptionRepository) Cancel(ctx context.Context, id uuid.UUID) (*Subscription, error) {
	var s Subscription
	err := r.db.QueryRow(
		ctx,
		`UPDATE subscriptions
		 SET status = 'cancelled', cancelled_at = now()
		 WHERE id = $1 AND status = 'active'
		 RETURNING id, org_id, plan_id, status, current_period_start, current_period_end, cancelled_at, created_at`,
		id,
	).Scan(
		&s.ID, &s.OrgID, &s.PlanID, &s.Status,
		&s.CurrentPeriodStart, &s.CurrentPeriodEnd, &s.CancelledAt, &s.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("subscriptions.Cancel: %w", err)
	}
	return &s, nil
}

func (r *SubscriptionRepository) List(ctx context.Context) ([]Subscription, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, plan_id, status, current_period_start, current_period_end, cancelled_at, created_at
		 FROM subscriptions
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("subscriptions.List: %w", err)
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(
			&s.ID, &s.OrgID, &s.PlanID, &s.Status,
			&s.CurrentPeriodStart, &s.CurrentPeriodEnd, &s.CancelledAt, &s.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("subscriptions.List scan: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}
