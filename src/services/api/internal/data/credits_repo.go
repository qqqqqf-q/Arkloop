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

type Credit struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	Balance   int64
	UpdatedAt time.Time
}

type CreditTransaction struct {
	ID            uuid.UUID
	OrgID         uuid.UUID
	Amount        int64
	Type          string
	ReferenceType *string
	ReferenceID   *uuid.UUID
	Note          *string
	CreatedAt     time.Time
}

type CreditTransactionDetail struct {
	CreditTransaction
	ThreadTitle *string
}

type InsufficientCreditsError struct {
	Required  int64
	Available int64
}

func (e InsufficientCreditsError) Error() string {
	return fmt.Sprintf("insufficient credits: required %d, available %d", e.Required, e.Available)
}

type CreditsRepository struct {
	db Querier
}

func NewCreditsRepository(db Querier) (*CreditsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &CreditsRepository{db: db}, nil
}

func (r *CreditsRepository) WithTx(tx pgx.Tx) *CreditsRepository {
	return &CreditsRepository{db: tx}
}

// GetBalance 查询 org 的积分余额，不存在返回 nil。
func (r *CreditsRepository) GetBalance(ctx context.Context, orgID uuid.UUID) (*Credit, error) {
	var c Credit
	err := r.db.QueryRow(ctx,
		`SELECT id, org_id, balance, updated_at FROM credits WHERE org_id = $1`,
		orgID,
	).Scan(&c.ID, &c.OrgID, &c.Balance, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("credits.GetBalance: %w", err)
	}
	return &c, nil
}

// InitBalance 为新 org 初始化积分余额并写入 initial_grant 交易。
func (r *CreditsRepository) InitBalance(ctx context.Context, orgID uuid.UUID, amount int64) (Credit, error) {
	var c Credit
	err := r.db.QueryRow(ctx,
		`INSERT INTO credits (org_id, balance) VALUES ($1, $2)
		 RETURNING id, org_id, balance, updated_at`,
		orgID, amount,
	).Scan(&c.ID, &c.OrgID, &c.Balance, &c.UpdatedAt)
	if err != nil {
		return Credit{}, fmt.Errorf("credits.InitBalance: %w", err)
	}

	_, err = r.db.Exec(ctx,
		`INSERT INTO credit_transactions (org_id, amount, type) VALUES ($1, $2, 'initial_grant')`,
		orgID, amount,
	)
	if err != nil {
		return Credit{}, fmt.Errorf("credits.InitBalance tx: %w", err)
	}
	return c, nil
}

// Deduct 原子扣减积分，余额不足时返回 InsufficientCreditsError。
func (r *CreditsRepository) Deduct(
	ctx context.Context,
	orgID uuid.UUID,
	amount int64,
	txType string,
	referenceType *string,
	referenceID *uuid.UUID,
	note *string,
) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE credits SET balance = balance - $1, updated_at = now()
		 WHERE org_id = $2 AND balance >= $1`,
		amount, orgID,
	)
	if err != nil {
		return fmt.Errorf("credits.Deduct: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// 查询实际余额用于错误信息
		var balance int64
		_ = r.db.QueryRow(ctx, `SELECT COALESCE(balance, 0) FROM credits WHERE org_id = $1`, orgID).Scan(&balance)
		return InsufficientCreditsError{Required: amount, Available: balance}
	}

	_, err = r.db.Exec(ctx,
		`INSERT INTO credit_transactions (org_id, amount, type, reference_type, reference_id, note)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		orgID, -amount, txType, referenceType, referenceID, note,
	)
	if err != nil {
		return fmt.Errorf("credits.Deduct tx: %w", err)
	}
	return nil
}

// Add 原子增加积分余额。
func (r *CreditsRepository) Add(
	ctx context.Context,
	orgID uuid.UUID,
	amount int64,
	txType string,
	referenceType *string,
	referenceID *uuid.UUID,
	note *string,
) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE credits SET balance = balance + $1, updated_at = now() WHERE org_id = $2`,
		amount, orgID,
	)
	if err != nil {
		return fmt.Errorf("credits.Add: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// org 尚无积分记录，创建
		_, err = r.db.Exec(ctx,
			`INSERT INTO credits (org_id, balance) VALUES ($1, $2)`,
			orgID, amount,
		)
		if err != nil {
			return fmt.Errorf("credits.Add init: %w", err)
		}
	}

	_, err = r.db.Exec(ctx,
		`INSERT INTO credit_transactions (org_id, amount, type, reference_type, reference_id, note)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		orgID, amount, txType, referenceType, referenceID, note,
	)
	if err != nil {
		return fmt.Errorf("credits.Add tx: %w", err)
	}
	return nil
}

// ListTransactions 查询 org 的积分交易流水。
func (r *CreditsRepository) ListTransactions(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]CreditTransaction, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := r.db.Query(ctx,
		`SELECT id, org_id, amount, type, reference_type, reference_id, note, created_at
		 FROM credit_transactions
		 WHERE org_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`,
		orgID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("credits.ListTransactions: %w", err)
	}
	defer rows.Close()

	txns := []CreditTransaction{}
	for rows.Next() {
		var t CreditTransaction
		if err := rows.Scan(&t.ID, &t.OrgID, &t.Amount, &t.Type, &t.ReferenceType, &t.ReferenceID, &t.Note, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("credits.ListTransactions scan: %w", err)
		}
		txns = append(txns, t)
	}
	return txns, rows.Err()
}

// ListTransactionsWithDetails 查询积分流水，LEFT JOIN runs+threads 获取对话标题，支持可选日期过滤。
func (r *CreditsRepository) ListTransactionsWithDetails(
	ctx context.Context,
	orgID uuid.UUID,
	limit, offset int,
	fromDate, toDate *time.Time,
) ([]CreditTransactionDetail, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	args := []any{orgID}
	where := "ct.org_id = $1"
	if fromDate != nil {
		args = append(args, *fromDate)
		where += fmt.Sprintf(" AND ct.created_at >= $%d", len(args))
	}
	if toDate != nil {
		args = append(args, *toDate)
		where += fmt.Sprintf(" AND ct.created_at < $%d", len(args))
	}
	args = append(args, limit, offset)

	query := fmt.Sprintf(`
		SELECT ct.id, ct.org_id, ct.amount, ct.type, ct.reference_type, ct.reference_id, ct.note, ct.created_at,
		       t.title
		FROM credit_transactions ct
		LEFT JOIN runs r ON ct.reference_type = 'run' AND r.id = ct.reference_id
		LEFT JOIN threads t ON t.id = r.thread_id
		WHERE %s
		ORDER BY ct.created_at DESC
		LIMIT $%d OFFSET $%d`,
		where, len(args)-1, len(args),
	)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("credits.ListTransactionsWithDetails: %w", err)
	}
	defer rows.Close()

	txns := []CreditTransactionDetail{}
	for rows.Next() {
		var d CreditTransactionDetail
		if err := rows.Scan(
			&d.ID, &d.OrgID, &d.Amount, &d.Type, &d.ReferenceType, &d.ReferenceID, &d.Note, &d.CreatedAt,
			&d.ThreadTitle,
		); err != nil {
			return nil, fmt.Errorf("credits.ListTransactionsWithDetails scan: %w", err)
		}
		txns = append(txns, d)
	}
	return txns, rows.Err()
}

// BulkAdjust 对所有 org 统一调整积分，正数为增加，负数为扣减（余额不低于 0）。
// 返回实际影响的行数。
func (r *CreditsRepository) BulkAdjust(ctx context.Context, amount int64, note string) (int64, error) {
if amount == 0 {
return 0, fmt.Errorf("credits.BulkAdjust: amount must not be zero")
}
var tag pgconn.CommandTag
var err error
if amount > 0 {
tag, err = r.db.Exec(ctx,
`UPDATE credits SET balance = balance + $1, updated_at = now()`,
amount,
)
} else {
tag, err = r.db.Exec(ctx,
`UPDATE credits SET balance = GREATEST(0, balance + $1), updated_at = now()`,
amount,
)
}
if err != nil {
return 0, fmt.Errorf("credits.BulkAdjust update: %w", err)
}
affected := tag.RowsAffected()
if affected == 0 {
return 0, nil
}
_, err = r.db.Exec(ctx,
`INSERT INTO credit_transactions (org_id, amount, type, note)
 SELECT org_id, $1, 'admin_bulk_adjustment', $2 FROM credits`,
amount, note,
)
if err != nil {
return affected, fmt.Errorf("credits.BulkAdjust tx: %w", err)
}
return affected, nil
}

// ResetAll 将所有 org 的积分余额归零，并记录交易流水。
func (r *CreditsRepository) ResetAll(ctx context.Context, note string) (int64, error) {
_, err := r.db.Exec(ctx,
`INSERT INTO credit_transactions (org_id, amount, type, note)
 SELECT org_id, -balance, 'admin_reset', $1 FROM credits WHERE balance != 0`,
note,
)
if err != nil {
return 0, fmt.Errorf("credits.ResetAll tx: %w", err)
}
tag, err := r.db.Exec(ctx,
`UPDATE credits SET balance = 0, updated_at = now() WHERE balance != 0`,
)
if err != nil {
return 0, fmt.Errorf("credits.ResetAll update: %w", err)
}
return tag.RowsAffected(), nil
}
