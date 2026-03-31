package data_test

import (
	"context"
	"testing"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
)

func TestUsageRepositoryWorksInDesktopSQLite(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, t.TempDir()+"/data.db")
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	repo, err := data.NewUsageRepository(pool)
	if err != nil {
		t.Fatalf("new usage repo: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO usage_records (
			account_id, feature_key, run_id, input_tokens, output_tokens, cache_creation_tokens,
			cache_read_tokens, cached_tokens, cost_usd, model, created_at
		) VALUES ($1, 'llm.tokens', $2, 100, 50, 20, 10, 5, 1.25, 'gpt-4o-mini', '2026-03-05 12:00:00')
	`, auth.DesktopAccountID, "run-1")
	if err != nil {
		t.Fatalf("seed usage record: %v", err)
	}

	summary, err := repo.GetMonthlyUsage(ctx, auth.DesktopAccountID, 2026, 3)
	if err != nil {
		t.Fatalf("get monthly usage: %v", err)
	}
	if summary.TotalInputTokens != 100 || summary.TotalOutputTokens != 50 || summary.RecordCount != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	daily, err := repo.GetDailyUsage(ctx, auth.DesktopAccountID, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("get daily usage: %v", err)
	}
	if len(daily) != 1 || daily[0].InputTokens != 100 {
		t.Fatalf("unexpected daily usage: %+v", daily)
	}

	byModel, err := repo.GetUsageByModel(ctx, auth.DesktopAccountID, 2026, 3)
	if err != nil {
		t.Fatalf("get usage by model: %v", err)
	}
	if len(byModel) != 1 || byModel[0].Model != "gpt-4o-mini" {
		t.Fatalf("unexpected usage by model: %+v", byModel)
	}
}
