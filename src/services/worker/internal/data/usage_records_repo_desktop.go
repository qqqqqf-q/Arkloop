//go:build desktop

package data

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UsageRecordsRepository 在 Worker 侧写入 usage_records，与 RunsRepository 风格一致（零值可用）。
type UsageRecordsRepository struct{}

// Insert 在已有事务内写入 usage_records，ON CONFLICT (run_id, usage_type) 时用最新值更新（幂等）。
func (UsageRecordsRepository) Insert(
	ctx context.Context,
	tx pgx.Tx,
	accountID, runID uuid.UUID,
	model string,
	inputTokens, outputTokens int64,
	cacheCreationTokens, cacheReadTokens, cachedTokens int64,
	costUSD float64,
) error {
	tag, err := tx.Exec(
		ctx,
		`INSERT INTO usage_records (account_id, run_id, model, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens, cached_tokens, cost_usd, usage_type, feature_key, quantity)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'llm', 'worker.llm', 1)
		 ON CONFLICT (run_id, usage_type) DO UPDATE
		   SET model                = EXCLUDED.model,
		       input_tokens         = EXCLUDED.input_tokens,
		       output_tokens        = EXCLUDED.output_tokens,
		       cache_creation_tokens = EXCLUDED.cache_creation_tokens,
		       cache_read_tokens    = EXCLUDED.cache_read_tokens,
		       cached_tokens        = EXCLUDED.cached_tokens,
		       cost_usd             = EXCLUDED.cost_usd`,
		accountID, runID, model, inputTokens, outputTokens,
		cacheCreationTokens, cacheReadTokens, cachedTokens, costUSD,
	)
	if err != nil {
		return fmt.Errorf("usage_records.Insert: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("usage_records.Insert: no rows affected")
	}
	return nil
}

// InsertMemoryUsage 通过连接池（非事务）写入 memory 类型的 usage record。
// costUSD <= 0 时跳过写入。
func (UsageRecordsRepository) InsertMemoryUsage(
	ctx context.Context,
	pool DesktopDB,
	accountID, runID uuid.UUID,
	costUSD float64,
) error {
	if costUSD <= 0 {
		return nil
	}
	tag, err := pool.Exec(
		ctx,
		`INSERT INTO usage_records (account_id, run_id, model, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens, cached_tokens, cost_usd, usage_type, feature_key, quantity)
		 VALUES ($1, $2, 'memory/openviking', 0, 0, 0, 0, 0, $3, 'memory', 'worker.memory', 1)
		 ON CONFLICT (run_id, usage_type) DO UPDATE
		   SET cost_usd = EXCLUDED.cost_usd`,
		accountID, runID, costUSD,
	)
	if err != nil {
		return fmt.Errorf("usage_records.InsertMemoryUsage: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("usage_records.InsertMemoryUsage: no rows affected")
	}
	return nil
}
