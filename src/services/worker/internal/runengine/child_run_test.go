//go:build !desktop

package runengine

import (
	"context"
	"strings"
	"testing"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type childRunTestQueue struct {
	enqueuedRunIDs []uuid.UUID
}

func (q *childRunTestQueue) EnqueueRun(
	_ context.Context,
	_ uuid.UUID,
	runID uuid.UUID,
	_ string,
	_ string,
	_ map[string]any,
	_ *time.Time,
) (uuid.UUID, error) {
	q.enqueuedRunIDs = append(q.enqueuedRunIDs, runID)
	return uuid.New(), nil
}

func (*childRunTestQueue) Lease(context.Context, int, []string) (*queue.JobLease, error) {
	panic("unexpected Lease call")
}

func (*childRunTestQueue) Heartbeat(context.Context, queue.JobLease, int) error {
	panic("unexpected Heartbeat call")
}

func (*childRunTestQueue) Ack(context.Context, queue.JobLease) error {
	panic("unexpected Ack call")
}

func (*childRunTestQueue) Nack(context.Context, queue.JobLease, *int) error {
	panic("unexpected Nack call")
}

func TestCreateAndEnqueueChildRunInheritsProjectID(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_wg09_child_run_project")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(context.Background(), `ALTER TABLE threads ALTER COLUMN project_id SET NOT NULL`); err != nil {
		t.Fatalf("enforce project_id not null: %v", err)
	}

	accountID := uuid.New()
	projectID := uuid.New()
	userID := uuid.New()
	parentThreadID := uuid.New()
	parentRunID := uuid.New()
	childRunID := uuid.New()
	profileRef := "pref_parent"
	workspaceRef := "wsref_parent"

	seedChildRunParent(t, pool, accountID, projectID, userID, parentThreadID, parentRunID)

	jobQueue := &childRunTestQueue{}
	err = createAndEnqueueChildRun(context.Background(), pool, nil, jobQueue, childRunID, data.Run{
		ID:              parentRunID,
		AccountID:           accountID,
		ThreadID:        parentThreadID,
		ProjectID:       &projectID,
		CreatedByUserID: &userID,
		ProfileRef:      stringPtr(profileRef),
		WorkspaceRef:    stringPtr(workspaceRef),
	}, "trace-child", "extended-search", "hello child")
	if err != nil {
		t.Fatalf("createAndEnqueueChildRun failed: %v", err)
	}

	if len(jobQueue.enqueuedRunIDs) != 1 || jobQueue.enqueuedRunIDs[0] != childRunID {
		t.Fatalf("unexpected enqueued runs: %#v", jobQueue.enqueuedRunIDs)
	}

	var storedProjectID uuid.UUID
	var storedParentRunID uuid.UUID
	var storedInput string
	var storedProfileRef *string
	var storedWorkspaceRef *string
	err = pool.QueryRow(
		context.Background(),
		`SELECT t.project_id, r.parent_run_id, m.content, r.profile_ref, r.workspace_ref
		   FROM runs r
		   JOIN threads t ON t.id = r.thread_id
		   JOIN messages m ON m.thread_id = t.id
		  WHERE r.id = $1
		  LIMIT 1`,
		childRunID,
	).Scan(&storedProjectID, &storedParentRunID, &storedInput, &storedProfileRef, &storedWorkspaceRef)
	if err != nil {
		t.Fatalf("load child run failed: %v", err)
	}
	if storedProjectID != projectID {
		t.Fatalf("unexpected child thread project_id: %s", storedProjectID)
	}
	if storedParentRunID != parentRunID {
		t.Fatalf("unexpected parent_run_id: %s", storedParentRunID)
	}
	if storedInput != "hello child" {
		t.Fatalf("unexpected child input: %q", storedInput)
	}
	if storedProfileRef == nil || *storedProfileRef != profileRef {
		t.Fatalf("unexpected child profile_ref: %#v", storedProfileRef)
	}
	if storedWorkspaceRef == nil || *storedWorkspaceRef != workspaceRef {
		t.Fatalf("unexpected child workspace_ref: %#v", storedWorkspaceRef)
	}
}

func TestCreateAndEnqueueChildRunRejectsMissingProjectID(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_wg09_child_run_missing_project")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	childRunID := uuid.New()
	accountID := uuid.New()
	userID := uuid.New()
	jobQueue := &childRunTestQueue{}

	err = createAndEnqueueChildRun(context.Background(), pool, nil, jobQueue, childRunID, data.Run{
		ID:              uuid.New(),
		AccountID:           accountID,
		ProjectID:       nil,
		CreatedByUserID: &userID,
	}, "trace-child", "extended-search", "hello child")
	if err == nil {
		t.Fatal("expected missing project_id error, got nil")
	}
	if !strings.Contains(err.Error(), "parent run project_id must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobQueue.enqueuedRunIDs) != 0 {
		t.Fatalf("expected no enqueue, got %#v", jobQueue.enqueuedRunIDs)
	}
}

func seedChildRunParent(
	t *testing.T,
	pool *pgxpool.Pool,
	accountID uuid.UUID,
	projectID uuid.UUID,
	userID uuid.UUID,
	threadID uuid.UUID,
	runID uuid.UUID,
) {
	t.Helper()

	if _, err := pool.Exec(
		context.Background(),
		`INSERT INTO threads (id, account_id, created_by_user_id, project_id)
		 VALUES ($1, $2, $3, $4)`,
		threadID,
		accountID,
		userID,
		projectID,
	); err != nil {
		t.Fatalf("insert parent thread failed: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`INSERT INTO runs (id, account_id, thread_id, created_by_user_id, status)
		 VALUES ($1, $2, $3, $4, 'running')`,
		runID,
		accountID,
		threadID,
		userID,
	); err != nil {
		t.Fatalf("insert parent run failed: %v", err)
	}
}

func stringPtr(value string) *string {
	return &value
}

func TestParseChildRunResult_IncludesFailureMessage(t *testing.T) {
	_, err := parseChildRunResult("failed\nroute stream failed")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "route stream failed") {
		t.Fatalf("expected failure message in error, got %v", err)
	}
}
