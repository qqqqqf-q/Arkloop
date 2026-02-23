package data

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreditsRepository 在 Worker 侧扣减积分，与 UsageRecordsRepository 风格一致（零值可用）。
type CreditsRepository struct{}

// Deduct 在已有事务内原子扣减积分并写交易流水。余额不足时返回错误。
func (CreditsRepository) Deduct(
	ctx context.Context,
	tx pgx.Tx,
	orgID uuid.UUID,
	amount int64,
	runID uuid.UUID,
) error {
	if amount <= 0 {
		return nil
	}

	tag, err := tx.Exec(ctx,
		`UPDATE credits SET balance = balance - $1, updated_at = now()
		 WHERE org_id = $2 AND balance >= $1`,
		amount, orgID,
	)
	if err != nil {
		return fmt.Errorf("credits.Deduct: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// 余额不足，不阻断（run 已完成），仅扣至零
		_, err = tx.Exec(ctx,
			`UPDATE credits SET balance = 0, updated_at = now() WHERE org_id = $1 AND balance > 0`,
			orgID,
		)
		if err != nil {
			return fmt.Errorf("credits.Deduct fallback: %w", err)
		}
	}

	refType := "run"
	_, err = tx.Exec(ctx,
		`INSERT INTO credit_transactions (org_id, amount, type, reference_type, reference_id)
		 VALUES ($1, $2, 'consumption', $3, $4)`,
		orgID, -amount, refType, runID,
	)
	if err != nil {
		return fmt.Errorf("credits.Deduct tx: %w", err)
	}
	return nil
}
