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
	AccountID          uuid.UUID
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
	accountID, planID uuid.UUID,
	periodStart, periodEnd time.Time,
) (Subscription, error) {
	if accountID == uuid.Nil {
		return Subscription{}, fmt.Errorf("subscriptions: account_id must not be empty")
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
		`INSERT INTO subscriptions (account_id, plan_id, current_period_start, current_period_end)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, account_id, plan_id, status, current_period_start, current_period_end, cancelled_at, created_at`,
		accountID, planID, periodStart, periodEnd,
	).Scan(
		&s.ID, &s.AccountID, &s.PlanID, &s.Status,
		&s.CurrentPeriodStart, &s.CurrentPeriodEnd, &s.CancelledAt, &s.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return Subscription{}, fmt.Errorf("subscriptions: account already has an active subscription")
		}
		return Subscription{}, fmt.Errorf("subscriptions.Create: %w", err)
	}
	return s, nil
}

func (r *SubscriptionRepository) GetByID(ctx context.Context, id uuid.UUID) (*Subscription, error) {
	var s Subscription
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, plan_id, status, current_period_start, current_period_end, cancelled_at, created_at
		 FROM subscriptions WHERE id = $1`,
		id,
	).Scan(
		&s.ID, &s.AccountID, &s.PlanID, &s.Status,
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

// GetActiveByAccountID 返回 account 当前 active 的订阅。
func (r *SubscriptionRepository) GetActiveByAccountID(ctx context.Context, accountID uuid.UUID) (*Subscription, error) {
	var s Subscription
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, plan_id, status, current_period_start, current_period_end, cancelled_at, created_at
		 FROM subscriptions
		 WHERE account_id = $1 AND status = 'active'`,
		accountID,
	).Scan(
		&s.ID, &s.AccountID, &s.PlanID, &s.Status,
		&s.CurrentPeriodStart, &s.CurrentPeriodEnd, &s.CancelledAt, &s.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("subscriptions.GetActiveByAccountID: %w", err)
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
		 RETURNING id, account_id, plan_id, status, current_period_start, current_period_end, cancelled_at, created_at`,
		id,
	).Scan(
		&s.ID, &s.AccountID, &s.PlanID, &s.Status,
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
		`SELECT id, account_id, plan_id, status, current_period_start, current_period_end, cancelled_at, created_at
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
			&s.ID, &s.AccountID, &s.PlanID, &s.Status,
			&s.CurrentPeriodStart, &s.CurrentPeriodEnd, &s.CancelledAt, &s.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("subscriptions.List scan: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}
