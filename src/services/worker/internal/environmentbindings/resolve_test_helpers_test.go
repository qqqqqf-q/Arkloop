package environmentbindings

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/data"
	"github.com/google/uuid"
)

func seedThreadAndRun(t *testing.T, pool data.DB, accountID, threadID uuid.UUID, projectID, userID *uuid.UUID, runID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(
		context.Background(),
		`INSERT INTO threads (id, account_id, created_by_user_id, project_id)
		 VALUES ($1, $2, $3, $4)`,
		threadID,
		accountID,
		userID,
		projectID,
	)
	if err != nil {
		t.Fatalf("insert thread failed: %v", err)
	}
	seedRunOnly(t, pool, accountID, threadID, userID, runID)
}

func seedRunOnly(t *testing.T, pool data.DB, accountID, threadID uuid.UUID, userID *uuid.UUID, runID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(
		context.Background(),
		`INSERT INTO runs (id, account_id, thread_id, created_by_user_id, status)
		 VALUES ($1, $2, $3, $4, 'running')`,
		runID,
		accountID,
		threadID,
		userID,
	)
	if err != nil {
		t.Fatalf("insert run failed: %v", err)
	}
}
