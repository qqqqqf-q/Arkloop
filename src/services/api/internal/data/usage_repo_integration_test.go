package data

import (
	"context"
	"testing"
	"time"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
)

func setupUsageTestRepo(t *testing.T) (*UsageRepository, *OrgRepository, context.Context) {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "api_go_usage")
	ctx := context.Background()

	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	usageRepo, err := NewUsageRepository(pool)
	if err != nil {
		t.Fatalf("new usage repo: %v", err)
	}

	orgRepo, err := NewOrgRepository(pool)
	if err != nil {
		t.Fatalf("new org repo: %v", err)
	}

	return usageRepo, orgRepo, ctx
}

func insertUsageAt(t *testing.T, repo *UsageRepository, ctx context.Context, orgID uuid.UUID, model string, input, output int64, costUSD float64, at time.Time) {
	t.Helper()

	threadID := uuid.New()
	_, err := repo.db.Exec(ctx,
		`INSERT INTO threads (id, org_id, title)
		 VALUES ($1, $2, $3)`,
		threadID, orgID, "usage-test",
	)
	if err != nil {
		t.Fatalf("insert thread: %v", err)
	}

	runID := uuid.New()
	_, err = repo.db.Exec(ctx,
		`INSERT INTO runs (id, org_id, thread_id)
		 VALUES ($1, $2, $3)`,
		runID, orgID, threadID,
	)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}

	_, err = repo.db.Exec(ctx,
		`INSERT INTO usage_records (org_id, run_id, model, input_tokens, output_tokens, cost_usd, recorded_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		orgID, runID, model, input, output, costUSD, at,
	)
	if err != nil {
		t.Fatalf("insert usage: %v", err)
	}
}

func TestGetDailyUsage(t *testing.T) {
	repo, orgRepo, ctx := setupUsageTestRepo(t)

	org, err := orgRepo.Create(ctx, "daily-test", "Daily Test Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	day1 := time.Date(2025, 6, 10, 8, 0, 0, 0, time.UTC)
	day2 := time.Date(2025, 6, 11, 14, 0, 0, 0, time.UTC)
	day2b := time.Date(2025, 6, 11, 20, 0, 0, 0, time.UTC)

	insertUsageAt(t, repo, ctx, org.ID, "gpt-4", 100, 50, 0.01, day1)
	insertUsageAt(t, repo, ctx, org.ID, "gpt-4", 200, 100, 0.02, day2)
	insertUsageAt(t, repo, ctx, org.ID, "claude-3", 300, 150, 0.03, day2b)

	start := time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 12, 0, 0, 0, 0, time.UTC)

	rows, err := repo.GetDailyUsage(ctx, org.ID, start, end)
	if err != nil {
		t.Fatalf("GetDailyUsage: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 days, got %d", len(rows))
	}

	if rows[0].InputTokens != 100 || rows[0].OutputTokens != 50 || rows[0].RecordCount != 1 {
		t.Errorf("day1 mismatch: %+v", rows[0])
	}
	if rows[1].InputTokens != 500 || rows[1].OutputTokens != 250 || rows[1].RecordCount != 2 {
		t.Errorf("day2 mismatch: %+v", rows[1])
	}
}

func TestGetDailyUsageEmpty(t *testing.T) {
	repo, orgRepo, ctx := setupUsageTestRepo(t)

	org, err := orgRepo.Create(ctx, "empty-daily", "Empty Daily Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	rows, err := repo.GetDailyUsage(ctx, org.ID, start, end)
	if err != nil {
		t.Fatalf("GetDailyUsage: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty, got %d rows", len(rows))
	}
}

func TestGetUsageByModel(t *testing.T) {
	repo, orgRepo, ctx := setupUsageTestRepo(t)

	org, err := orgRepo.Create(ctx, "model-test", "Model Test Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	base := time.Date(2025, 7, 5, 12, 0, 0, 0, time.UTC)
	insertUsageAt(t, repo, ctx, org.ID, "gpt-4", 1000, 500, 0.10, base)
	insertUsageAt(t, repo, ctx, org.ID, "gpt-4", 2000, 1000, 0.20, base.Add(time.Hour))
	insertUsageAt(t, repo, ctx, org.ID, "claude-3", 500, 250, 0.05, base.Add(2*time.Hour))

	rows, err := repo.GetUsageByModel(ctx, org.ID, 2025, 7)
	if err != nil {
		t.Fatalf("GetUsageByModel: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 models, got %d", len(rows))
	}

	// 按 token 总量降序，gpt-4 应排首位
	if rows[0].Model != "gpt-4" {
		t.Errorf("expected first model gpt-4, got %s", rows[0].Model)
	}
	if rows[0].InputTokens != 3000 || rows[0].OutputTokens != 1500 || rows[0].RecordCount != 2 {
		t.Errorf("gpt-4 mismatch: %+v", rows[0])
	}
	if rows[1].Model != "claude-3" || rows[1].InputTokens != 500 {
		t.Errorf("claude-3 mismatch: %+v", rows[1])
	}
}

func TestGetUsageByModelEmpty(t *testing.T) {
	repo, orgRepo, ctx := setupUsageTestRepo(t)

	org, err := orgRepo.Create(ctx, "model-empty", "Model Empty Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	rows, err := repo.GetUsageByModel(ctx, org.ID, 2025, 1)
	if err != nil {
		t.Fatalf("GetUsageByModel: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty, got %d rows", len(rows))
	}
}

func TestGetGlobalDailyUsage(t *testing.T) {
	repo, orgRepo, ctx := setupUsageTestRepo(t)

	org1, err := orgRepo.Create(ctx, "global-org1", "Global Org 1", "personal")
	if err != nil {
		t.Fatalf("create org1: %v", err)
	}
	org2, err := orgRepo.Create(ctx, "global-org2", "Global Org 2", "personal")
	if err != nil {
		t.Fatalf("create org2: %v", err)
	}

	day := time.Date(2025, 8, 1, 10, 0, 0, 0, time.UTC)
	insertUsageAt(t, repo, ctx, org1.ID, "gpt-4", 100, 50, 0.01, day)
	insertUsageAt(t, repo, ctx, org2.ID, "claude-3", 200, 100, 0.02, day)

	start := time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 8, 2, 0, 0, 0, 0, time.UTC)

	rows, err := repo.GetGlobalDailyUsage(ctx, start, end)
	if err != nil {
		t.Fatalf("GetGlobalDailyUsage: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 day, got %d", len(rows))
	}
	if rows[0].InputTokens != 300 || rows[0].OutputTokens != 150 || rows[0].RecordCount != 2 {
		t.Errorf("global daily mismatch: %+v", rows[0])
	}
}
