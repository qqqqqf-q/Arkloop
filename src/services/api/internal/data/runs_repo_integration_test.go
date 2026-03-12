package data

import (
	"context"
	"testing"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
)

func setupRunsTestRepo(t *testing.T) (*RunEventRepository, *AccountRepository, context.Context) {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "api_go_runs")
	ctx := context.Background()

	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	runRepo, err := NewRunEventRepository(pool)
	if err != nil {
		t.Fatalf("new run repo: %v", err)
	}

	orgRepo, err := NewAccountRepository(pool)
	if err != nil {
		t.Fatalf("new org repo: %v", err)
	}

	return runRepo, orgRepo, ctx
}

func TestListRunsAggregatesJoinedUsageAndCredits(t *testing.T) {
	repo, orgRepo, ctx := setupRunsTestRepo(t)

	org, err := orgRepo.Create(ctx, "runs-join-test", "Runs Join Test Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	threadID := uuid.New()
	_, err = repo.db.Exec(ctx,
		`INSERT INTO threads (id, account_id, title)
		 VALUES ($1, $2, $3)`,
		threadID, org.ID, "runs-join-test",
	)
	if err != nil {
		t.Fatalf("insert thread: %v", err)
	}

	runID := uuid.New()
	_, err = repo.db.Exec(ctx,
		`INSERT INTO runs (id, account_id, thread_id, total_input_tokens, total_output_tokens, total_cost_usd)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		runID, org.ID, threadID, 120, 60, 0.12,
	)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}

	_, err = repo.db.Exec(ctx,
		`INSERT INTO usage_records (account_id, run_id, usage_type, cache_read_tokens, cache_creation_tokens, cached_tokens)
		 VALUES ($1, $2, 'llm', $3, $4, $5),
		        ($1, $2, 'embedding', $6, $7, $8)`,
		org.ID, runID, 10, 20, 30, 1, 2, 3,
	)
	if err != nil {
		t.Fatalf("insert usage records: %v", err)
	}

	_, err = repo.db.Exec(ctx,
		`INSERT INTO credit_transactions (account_id, amount, type, reference_type, reference_id)
		 VALUES ($1, $2, 'consumption', 'run', $3),
		        ($1, $4, 'consumption', 'run', $3)`,
		org.ID, -5, runID, -7,
	)
	if err != nil {
		t.Fatalf("insert credit transactions: %v", err)
	}

	runs, total, err := repo.ListRuns(ctx, ListRunsParams{RunID: &runID, Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}

	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run row, got %d", len(runs))
	}

	row := runs[0]
	if row.CacheReadTokens == nil || *row.CacheReadTokens != 11 {
		t.Fatalf("expected cache_read_tokens 11, got %+v", row.CacheReadTokens)
	}
	if row.CacheCreationTokens == nil || *row.CacheCreationTokens != 22 {
		t.Fatalf("expected cache_creation_tokens 22, got %+v", row.CacheCreationTokens)
	}
	if row.CachedTokens == nil || *row.CachedTokens != 33 {
		t.Fatalf("expected cached_tokens 33, got %+v", row.CachedTokens)
	}
	if row.CreditsUsed == nil || *row.CreditsUsed != 12 {
		t.Fatalf("expected credits_used 12, got %+v", row.CreditsUsed)
	}
}
