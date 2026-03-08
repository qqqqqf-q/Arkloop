package runengine

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestResolveAndPersistEnvironmentBindings_ProjectScopedPerProfile(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_wg09_runtime_bindings_project")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	projectID := uuid.New()
	userA := uuid.New()
	userB := uuid.New()
	threadA1 := uuid.New()
	threadA2 := uuid.New()
	threadB := uuid.New()
	runA1 := uuid.New()
	runA2 := uuid.New()
	runB := uuid.New()

	seedThreadAndRun(t, pool, orgID, threadA1, &projectID, &userA, runA1)
	seedThreadAndRun(t, pool, orgID, threadA2, &projectID, &userA, runA2)
	seedThreadAndRun(t, pool, orgID, threadB, &projectID, &userB, runB)

	first, err := resolveAndPersistEnvironmentBindings(context.Background(), pool, data.Run{
		ID:              runA1,
		OrgID:           orgID,
		ThreadID:        threadA1,
		ProjectID:       &projectID,
		CreatedByUserID: &userA,
	})
	if err != nil {
		t.Fatalf("resolve first run failed: %v", err)
	}
	second, err := resolveAndPersistEnvironmentBindings(context.Background(), pool, data.Run{
		ID:              runA2,
		OrgID:           orgID,
		ThreadID:        threadA2,
		ProjectID:       &projectID,
		CreatedByUserID: &userA,
	})
	if err != nil {
		t.Fatalf("resolve second run failed: %v", err)
	}
	third, err := resolveAndPersistEnvironmentBindings(context.Background(), pool, data.Run{
		ID:              runB,
		OrgID:           orgID,
		ThreadID:        threadB,
		ProjectID:       &projectID,
		CreatedByUserID: &userB,
	})
	if err != nil {
		t.Fatalf("resolve third run failed: %v", err)
	}

	if derefString(first.ProfileRef) != derefString(second.ProfileRef) {
		t.Fatalf("expected same profile_ref for same user, got %q vs %q", derefString(first.ProfileRef), derefString(second.ProfileRef))
	}
	if derefString(first.WorkspaceRef) != derefString(second.WorkspaceRef) {
		t.Fatalf("expected same workspace_ref for same user+project, got %q vs %q", derefString(first.WorkspaceRef), derefString(second.WorkspaceRef))
	}
	if derefString(first.ProfileRef) == derefString(third.ProfileRef) {
		t.Fatalf("expected different profile_ref for different users, got %q", derefString(first.ProfileRef))
	}
	if derefString(first.WorkspaceRef) == derefString(third.WorkspaceRef) {
		t.Fatalf("expected different workspace_ref for different users in same project, got %q", derefString(first.WorkspaceRef))
	}
}

func TestResolveAndPersistEnvironmentBindings_ThreadFallback(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_wg09_runtime_bindings_thread")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	userID := uuid.New()
	threadID := uuid.New()
	runID1 := uuid.New()
	runID2 := uuid.New()

	seedThreadAndRun(t, pool, orgID, threadID, nil, &userID, runID1)
	seedRunOnly(t, pool, orgID, threadID, &userID, runID2)

	first, err := resolveAndPersistEnvironmentBindings(context.Background(), pool, data.Run{
		ID:              runID1,
		OrgID:           orgID,
		ThreadID:        threadID,
		CreatedByUserID: &userID,
	})
	if err != nil {
		t.Fatalf("resolve first run failed: %v", err)
	}
	second, err := resolveAndPersistEnvironmentBindings(context.Background(), pool, data.Run{
		ID:              runID2,
		OrgID:           orgID,
		ThreadID:        threadID,
		CreatedByUserID: &userID,
	})
	if err != nil {
		t.Fatalf("resolve second run failed: %v", err)
	}

	if derefString(first.WorkspaceRef) != derefString(second.WorkspaceRef) {
		t.Fatalf("expected thread fallback workspace_ref reused, got %q vs %q", derefString(first.WorkspaceRef), derefString(second.WorkspaceRef))
	}
}

func seedThreadAndRun(t *testing.T, pool *pgxpool.Pool, orgID, threadID uuid.UUID, projectID, userID *uuid.UUID, runID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(
		context.Background(),
		`INSERT INTO threads (id, org_id, created_by_user_id, project_id)
		 VALUES ($1, $2, $3, $4)`,
		threadID,
		orgID,
		userID,
		projectID,
	)
	if err != nil {
		t.Fatalf("insert thread failed: %v", err)
	}
	seedRunOnly(t, pool, orgID, threadID, userID, runID)
}

func seedRunOnly(t *testing.T, pool *pgxpool.Pool, orgID, threadID uuid.UUID, userID *uuid.UUID, runID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(
		context.Background(),
		`INSERT INTO runs (id, org_id, thread_id, created_by_user_id, status)
		 VALUES ($1, $2, $3, $4, 'running')`,
		runID,
		orgID,
		threadID,
		userID,
	)
	if err != nil {
		t.Fatalf("insert run failed: %v", err)
	}
}
