package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type UsageRecord struct {
	ID           uuid.UUID
	AccountID    uuid.UUID
	RunID        uuid.UUID
	Model        string
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
	RecordedAt   time.Time
}

type UsageSummary struct {
	AccountID         uuid.UUID
	Year              int
	Month             int
	TotalInputTokens  int64
	TotalOutputTokens int64
	TotalCacheCreationTokens int64
	TotalCacheReadTokens     int64
	TotalCachedTokens        int64
	TotalCostUSD      float64
	RecordCount       int64
}

type UsageRepository struct {
	db Querier
}

func NewUsageRepository(db Querier) (*UsageRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &UsageRepository{db: db}, nil
}

func (r *UsageRepository) Insert(
	ctx context.Context,
	accountID, runID uuid.UUID,
	model string,
	inputTokens, outputTokens int64,
	costUSD float64,
) (UsageRecord, error) {
	if accountID == uuid.Nil {
		return UsageRecord{}, fmt.Errorf("usage_records: account_id must not be empty")
	}
	if runID == uuid.Nil {
		return UsageRecord{}, fmt.Errorf("usage_records: run_id must not be empty")
	}

	var rec UsageRecord
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO usage_records (account_id, run_id, model, input_tokens, output_tokens, cost_usd)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, account_id, run_id, model, input_tokens, output_tokens, cost_usd, recorded_at`,
		accountID, runID, model, inputTokens, outputTokens, costUSD,
	).Scan(
		&rec.ID, &rec.AccountID, &rec.RunID, &rec.Model,
		&rec.InputTokens, &rec.OutputTokens, &rec.CostUSD, &rec.RecordedAt,
	)
	if err != nil {
		return UsageRecord{}, fmt.Errorf("usage_records.Insert: %w", err)
	}
	return rec, nil
}

// GetMonthlyUsage 汇总指定 account 在某月的 token 用量和成本。
// 使用时间范围查询，确保 account 维度索引可被命中。
func (r *UsageRepository) GetMonthlyUsage(
	ctx context.Context,
	accountID uuid.UUID,
	year, month int,
) (*UsageSummary, error) {
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("usage_records: account_id must not be empty")
	}

	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	summary := &UsageSummary{AccountID: accountID, Year: year, Month: month}
	err := r.db.QueryRow(
		ctx,
		`SELECT
		     COALESCE(SUM(input_tokens),  0),
		     COALESCE(SUM(output_tokens), 0),
		     COALESCE(SUM(cache_creation_tokens), 0),
		     COALESCE(SUM(cache_read_tokens), 0),
		     COALESCE(SUM(cached_tokens), 0),
		     COALESCE(SUM(cost_usd),      0),
		     COUNT(*)
		 FROM usage_records
		 WHERE account_id = $1 AND recorded_at >= $2 AND recorded_at < $3`,
		accountID, start, end,
	).Scan(
		&summary.TotalInputTokens,
		&summary.TotalOutputTokens,
		&summary.TotalCacheCreationTokens,
		&summary.TotalCacheReadTokens,
		&summary.TotalCachedTokens,
		&summary.TotalCostUSD,
		&summary.RecordCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return summary, nil
	}
	if err != nil {
		return nil, fmt.Errorf("usage_records.GetMonthlyUsage: %w", err)
	}
	return summary, nil
}

type DailyUsage struct {
	Date         time.Time
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
	RecordCount  int64
}

type ModelUsage struct {
	Model        string
	InputTokens  int64
	OutputTokens int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	CachedTokens        int64
	CostUSD      float64
	RecordCount  int64
}

// GetDailyUsage 按日聚合指定 account 在 [startDate, endDate) 内的用量。
func (r *UsageRepository) GetDailyUsage(
	ctx context.Context,
	accountID uuid.UUID,
	startDate, endDate time.Time,
) ([]DailyUsage, error) {
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("usage_records: account_id must not be empty")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT
		     DATE_TRUNC('day', recorded_at) AS day,
		     COALESCE(SUM(input_tokens),  0),
		     COALESCE(SUM(output_tokens), 0),
		     COALESCE(SUM(cost_usd),      0),
		     COUNT(*)
		 FROM usage_records
		 WHERE account_id = $1 AND recorded_at >= $2 AND recorded_at < $3
		 GROUP BY day
		 ORDER BY day`,
		accountID, startDate, endDate,
	)
	if err != nil {
		return nil, fmt.Errorf("usage_records.GetDailyUsage: %w", err)
	}
	defer rows.Close()

	var result []DailyUsage
	for rows.Next() {
		var d DailyUsage
		if err := rows.Scan(&d.Date, &d.InputTokens, &d.OutputTokens, &d.CostUSD, &d.RecordCount); err != nil {
			return nil, fmt.Errorf("usage_records.GetDailyUsage scan: %w", err)
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

// GetUsageByModel 按模型分组聚合指定 account 在某月的用量。
func (r *UsageRepository) GetUsageByModel(
	ctx context.Context,
	accountID uuid.UUID,
	year, month int,
) ([]ModelUsage, error) {
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("usage_records: account_id must not be empty")
	}

	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	rows, err := r.db.Query(
		ctx,
		`SELECT
		     model,
		     COALESCE(SUM(input_tokens),  0),
		     COALESCE(SUM(output_tokens), 0),
		     COALESCE(SUM(cache_creation_tokens), 0),
		     COALESCE(SUM(cache_read_tokens), 0),
		     COALESCE(SUM(cached_tokens), 0),
		     COALESCE(SUM(cost_usd),      0),
		     COUNT(*)
		 FROM usage_records
		 WHERE account_id = $1 AND recorded_at >= $2 AND recorded_at < $3
		 GROUP BY model
		 ORDER BY SUM(input_tokens) + SUM(output_tokens) DESC`,
		accountID, start, end,
	)
	if err != nil {
		return nil, fmt.Errorf("usage_records.GetUsageByModel: %w", err)
	}
	defer rows.Close()

	var result []ModelUsage
	for rows.Next() {
		var m ModelUsage
		if err := rows.Scan(
			&m.Model,
			&m.InputTokens,
			&m.OutputTokens,
			&m.CacheCreationTokens,
			&m.CacheReadTokens,
			&m.CachedTokens,
			&m.CostUSD,
			&m.RecordCount,
		); err != nil {
			return nil, fmt.Errorf("usage_records.GetUsageByModel scan: %w", err)
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// GetGlobalDailyUsage 按日聚合全平台在 [startDate, endDate) 内的用量（admin 用）。
func (r *UsageRepository) GetGlobalDailyUsage(
	ctx context.Context,
	startDate, endDate time.Time,
) ([]DailyUsage, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT
		     DATE_TRUNC('day', recorded_at) AS day,
		     COALESCE(SUM(input_tokens),  0),
		     COALESCE(SUM(output_tokens), 0),
		     COALESCE(SUM(cost_usd),      0),
		     COUNT(*)
		 FROM usage_records
		 WHERE recorded_at >= $1 AND recorded_at < $2
		 GROUP BY day
		 ORDER BY day`,
		startDate, endDate,
	)
	if err != nil {
		return nil, fmt.Errorf("usage_records.GetGlobalDailyUsage: %w", err)
	}
	defer rows.Close()

	var result []DailyUsage
	for rows.Next() {
		var d DailyUsage
		if err := rows.Scan(&d.Date, &d.InputTokens, &d.OutputTokens, &d.CostUSD, &d.RecordCount); err != nil {
			return nil, fmt.Errorf("usage_records.GetGlobalDailyUsage scan: %w", err)
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

// GetGlobalUsageByModel 按模型分组聚合全平台在某月的用量（admin 用）。
func (r *UsageRepository) GetGlobalUsageByModel(
	ctx context.Context,
	year, month int,
) ([]ModelUsage, error) {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	rows, err := r.db.Query(
		ctx,
		`SELECT
		     model,
		     COALESCE(SUM(input_tokens),  0),
		     COALESCE(SUM(output_tokens), 0),
		     COALESCE(SUM(cost_usd),      0),
		     COUNT(*)
		 FROM usage_records
		 WHERE recorded_at >= $1 AND recorded_at < $2
		 GROUP BY model
		 ORDER BY SUM(input_tokens) + SUM(output_tokens) DESC`,
		start, end,
	)
	if err != nil {
		return nil, fmt.Errorf("usage_records.GetGlobalUsageByModel: %w", err)
	}
	defer rows.Close()

	var result []ModelUsage
	for rows.Next() {
		var m ModelUsage
		if err := rows.Scan(&m.Model, &m.InputTokens, &m.OutputTokens, &m.CostUSD, &m.RecordCount); err != nil {
			return nil, fmt.Errorf("usage_records.GetGlobalUsageByModel scan: %w", err)
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// GetGlobalMonthlyUsage 汇总全平台在某月的 token 用量和成本。
func (r *UsageRepository) GetGlobalMonthlyUsage(
	ctx context.Context,
	year, month int,
) (*UsageSummary, error) {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	summary := &UsageSummary{Year: year, Month: month}
	err := r.db.QueryRow(
		ctx,
		`SELECT
		     COALESCE(SUM(input_tokens),  0),
		     COALESCE(SUM(output_tokens), 0),
		     COALESCE(SUM(cost_usd),      0),
		     COUNT(*)
		 FROM usage_records
		 WHERE recorded_at >= $1 AND recorded_at < $2`,
		start, end,
	).Scan(
		&summary.TotalInputTokens,
		&summary.TotalOutputTokens,
		&summary.TotalCostUSD,
		&summary.RecordCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return summary, nil
	}
	if err != nil {
		return nil, fmt.Errorf("usage_records.GetGlobalMonthlyUsage: %w", err)
	}
	return summary, nil
}

// GlobalUsageSummary 全局用量聚合（跨所有 org）。
type GlobalUsageSummary struct {
	TotalInputTokens  int64
	TotalOutputTokens int64
	TotalCostUSD      float64
}

func (r *UsageRepository) GetGlobalSummary(ctx context.Context) (*GlobalUsageSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	summary := &GlobalUsageSummary{}
	err := r.db.QueryRow(
		ctx,
		`SELECT
		     COALESCE(SUM(input_tokens),  0),
		     COALESCE(SUM(output_tokens), 0),
		     COALESCE(SUM(cost_usd),      0)
		 FROM usage_records`,
	).Scan(
		&summary.TotalInputTokens,
		&summary.TotalOutputTokens,
		&summary.TotalCostUSD,
	)
	if err != nil {
		return nil, fmt.Errorf("usage_records.GetGlobalSummary: %w", err)
	}
	return summary, nil
}
