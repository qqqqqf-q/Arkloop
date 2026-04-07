//go:build !desktop

package data

import (
	"context"
	"errors"
	"testing"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
)

func TestJobRepositoryRejectsDuplicateRunExecute(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_jobs_repo")
	ctx := context.Background()

	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 8, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	repo, err := NewJobRepository(pool)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
	}

	accountID := uuid.New()
	runID := uuid.New()

	jobID1, err := repo.EnqueueRun(ctx, accountID, runID, "0123456789abcdef0123456789abcdef", RunExecuteJobType, map[string]any{"source": "first"}, nil)
	if err != nil {
		t.Fatalf("first enqueue failed: %v", err)
	}
	if jobID1 == uuid.Nil {
		t.Fatal("expected non-nil first job id")
	}
	_, err = repo.EnqueueRun(ctx, accountID, runID, "fedcba9876543210fedcba9876543210", RunExecuteJobType, map[string]any{"source": "second"}, nil)
	if !errors.Is(err, ErrRunExecuteAlreadyQueued) {
		t.Fatalf("expected ErrRunExecuteAlreadyQueued, got %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM jobs
		  WHERE job_type = $1
		    AND payload_json->>'run_id' = $2
		    AND status IN ($3, $4)`,
		RunExecuteJobType,
		runID.String(),
		JobStatusQueued,
		JobStatusLeased,
	).Scan(&count); err != nil {
		t.Fatalf("count jobs failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one active job, got %d", count)
	}
}
