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
	OrgID        uuid.UUID
	RunID        uuid.UUID
	Model        string
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
	RecordedAt   time.Time
}

type UsageSummary struct {
	OrgID             uuid.UUID
	Year              int
	Month             int
	TotalInputTokens  int64
	TotalOutputTokens int64
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
	orgID, runID uuid.UUID,
	model string,
	inputTokens, outputTokens int64,
	costUSD float64,
) (UsageRecord, error) {
	if orgID == uuid.Nil {
		return UsageRecord{}, fmt.Errorf("usage_records: org_id must not be empty")
	}
	if runID == uuid.Nil {
		return UsageRecord{}, fmt.Errorf("usage_records: run_id must not be empty")
	}

	var rec UsageRecord
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO usage_records (org_id, run_id, model, input_tokens, output_tokens, cost_usd)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, org_id, run_id, model, input_tokens, output_tokens, cost_usd, recorded_at`,
		orgID, runID, model, inputTokens, outputTokens, costUSD,
	).Scan(
		&rec.ID, &rec.OrgID, &rec.RunID, &rec.Model,
		&rec.InputTokens, &rec.OutputTokens, &rec.CostUSD, &rec.RecordedAt,
	)
	if err != nil {
		return UsageRecord{}, fmt.Errorf("usage_records.Insert: %w", err)
	}
	return rec, nil
}

// GetMonthlyUsage 汇总指定 org 在某月的 token 用量和成本。
// 使用时间范围查询，确保索引 idx_usage_records_org_recorded 可被命中。
func (r *UsageRepository) GetMonthlyUsage(
	ctx context.Context,
	orgID uuid.UUID,
	year, month int,
) (*UsageSummary, error) {
	if orgID == uuid.Nil {
		return nil, fmt.Errorf("usage_records: org_id must not be empty")
	}

	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	summary := &UsageSummary{OrgID: orgID, Year: year, Month: month}
	err := r.db.QueryRow(
		ctx,
		`SELECT
		     COALESCE(SUM(input_tokens),  0),
		     COALESCE(SUM(output_tokens), 0),
		     COALESCE(SUM(cost_usd),      0),
		     COUNT(*)
		 FROM usage_records
		 WHERE org_id = $1 AND recorded_at >= $2 AND recorded_at < $3`,
		orgID, start, end,
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
		return nil, fmt.Errorf("usage_records.GetMonthlyUsage: %w", err)
	}
	return summary, nil
}
