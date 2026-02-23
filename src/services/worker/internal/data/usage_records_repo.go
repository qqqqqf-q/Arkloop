package data

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UsageRecordsRepository 在 Worker 侧写入 usage_records，与 RunsRepository 风格一致（零值可用）。
type UsageRecordsRepository struct{}

// Insert 在已有事务内写入 usage_records，ON CONFLICT (run_id) 时用最新值更新（幂等）。
func (UsageRecordsRepository) Insert(
	ctx context.Context,
	tx pgx.Tx,
	orgID, runID uuid.UUID,
	model string,
	inputTokens, outputTokens int64,
	costUSD float64,
) error {
	tag, err := tx.Exec(
		ctx,
		`INSERT INTO usage_records (org_id, run_id, model, input_tokens, output_tokens, cost_usd)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (run_id) DO UPDATE
		   SET model         = EXCLUDED.model,
		       input_tokens  = EXCLUDED.input_tokens,
		       output_tokens = EXCLUDED.output_tokens,
		       cost_usd      = EXCLUDED.cost_usd`,
		orgID, runID, model, inputTokens, outputTokens, costUSD,
	)
	if err != nil {
		return fmt.Errorf("usage_records.Insert: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("usage_records.Insert: no rows affected")
	}
	return nil
}
