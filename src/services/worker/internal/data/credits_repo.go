package data

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// InsufficientCreditsError 余额不足时返回。
type InsufficientCreditsError struct {
	Required  int64
	Available int64
}

func (e *InsufficientCreditsError) Error() string {
	return fmt.Sprintf("insufficient credits: required %d, available %d", e.Required, e.Available)
}

// CreditsRepository 在 Worker 侧扣减积分，与 UsageRecordsRepository 风格一致（零值可用）。
type CreditsRepository struct{}

// DeductStandalone 自管理事务的积分扣减，用于工具调用等需要立即扣减的场景。
// metadata 可选，非 nil 时写入 credit_transactions.metadata（计算明细）。
func (CreditsRepository) DeductStandalone(
	ctx context.Context,
	pool interface {
		Begin(context.Context) (pgx.Tx, error)
	},
	accountID uuid.UUID,
	amount int64,
	runID uuid.UUID,
	refType string,
	metadata map[string]any,
) error {
	if amount <= 0 {
		return nil
	}
	if pool == nil {
		return fmt.Errorf("credits.DeductStandalone: pool is nil")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("credits.DeductStandalone: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := deductBalance(ctx, tx, accountID, amount); err != nil {
		return fmt.Errorf("credits.DeductStandalone: %w", err)
	}

	if err := insertTransaction(ctx, tx, accountID, -amount, refType, runID, metadata); err != nil {
		return fmt.Errorf("credits.DeductStandalone: %w", err)
	}
	return tx.Commit(ctx)
}

// Deduct 在已有事务内原子扣减积分并写交易流水。
// metadata 可选，非 nil 时写入 credit_transactions.metadata（计算明细）。
func (CreditsRepository) Deduct(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	amount int64,
	runID uuid.UUID,
	metadata map[string]any,
) error {
	if amount <= 0 {
		return nil
	}

	if err := deductBalance(ctx, tx, accountID, amount); err != nil {
		return fmt.Errorf("credits.Deduct: %w", err)
	}

	if err := insertTransaction(ctx, tx, accountID, -amount, "run", runID, metadata); err != nil {
		return fmt.Errorf("credits.Deduct: %w", err)
	}
	return nil
}

func deductBalance(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, amount int64) error {
	tag, err := tx.Exec(ctx,
		`UPDATE credits SET balance = balance - $1, updated_at = now()
		 WHERE account_id = $2 AND balance >= $1`,
		amount, accountID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var balance int64
		_ = tx.QueryRow(ctx,
			`SELECT COALESCE(balance, 0) FROM credits WHERE account_id = $1`,
			accountID,
		).Scan(&balance)
		return &InsufficientCreditsError{Required: amount, Available: balance}
	}
	return nil
}

func insertTransaction(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, amount int64, refType string, refID uuid.UUID, metadata map[string]any) error {
	var metaJSON []byte
	if metadata != nil {
		var err error
		metaJSON, err = json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("marshal metadata: %w", err)
		}
	}

	_, err := tx.Exec(ctx,
		`INSERT INTO credit_transactions (account_id, amount, type, reference_type, reference_id, metadata)
		 VALUES ($1, $2, 'consumption', $3, $4, $5)`,
		accountID, amount, refType, refID, metaJSON,
	)
	return err
}
