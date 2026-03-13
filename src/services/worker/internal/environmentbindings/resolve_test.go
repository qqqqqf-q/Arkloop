package environmentbindings

import (
	"bytes"
	"context"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestResolveAndPersistRun_ProjectScopedPerProfile(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_wg09_runtime_bindings_project")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	userA := uuid.New()
	userB := uuid.New()
	threadA1 := uuid.New()
	threadA2 := uuid.New()
	threadB := uuid.New()
	runA1 := uuid.New()
	runA2 := uuid.New()
	runB := uuid.New()

	seedThreadAndRun(t, pool, accountID, threadA1, &projectID, &userA, runA1)
	seedThreadAndRun(t, pool, accountID, threadA2, &projectID, &userA, runA2)
	seedThreadAndRun(t, pool, accountID, threadB, &projectID, &userB, runB)

	first, err := ResolveAndPersistRun(context.Background(), pool, data.Run{
		ID:              runA1,
		AccountID:           accountID,
		ThreadID:        threadA1,
		ProjectID:       &projectID,
		CreatedByUserID: &userA,
	})
	if err != nil {
		t.Fatalf("resolve first run failed: %v", err)
	}
	second, err := ResolveAndPersistRun(context.Background(), pool, data.Run{
		ID:              runA2,
		AccountID:           accountID,
		ThreadID:        threadA2,
		ProjectID:       &projectID,
		CreatedByUserID: &userA,
	})
	if err != nil {
		t.Fatalf("resolve second run failed: %v", err)
	}
	third, err := ResolveAndPersistRun(context.Background(), pool, data.Run{
		ID:              runB,
		AccountID:           accountID,
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

	profileRepo := data.ProfileRegistriesRepository{}
	profileRecord, err := profileRepo.Get(context.Background(), pool, derefString(first.ProfileRef))
	if err != nil {
		t.Fatalf("get profile registry: %v", err)
	}
	if profileRecord.DefaultWorkspaceRef == nil || *profileRecord.DefaultWorkspaceRef != derefString(first.WorkspaceRef) {
		t.Fatalf("unexpected default_workspace_ref: %#v", profileRecord.DefaultWorkspaceRef)
	}
	if profileRecord.OwnerUserID == nil || *profileRecord.OwnerUserID != userA {
		t.Fatalf("unexpected profile owner_user_id: %#v", profileRecord.OwnerUserID)
	}

	workspaceRepo := data.WorkspaceRegistriesRepository{}
	workspaceRecord, err := workspaceRepo.Get(context.Background(), pool, derefString(first.WorkspaceRef))
	if err != nil {
		t.Fatalf("get workspace registry: %v", err)
	}
	if workspaceRecord.ProjectID == nil || *workspaceRecord.ProjectID != projectID {
		t.Fatalf("unexpected workspace project_id: %#v", workspaceRecord.ProjectID)
	}
	if workspaceRecord.OwnerUserID == nil || *workspaceRecord.OwnerUserID != userA {
		t.Fatalf("unexpected workspace owner_user_id: %#v", workspaceRecord.OwnerUserID)
	}
}

func TestResolveAndPersistRun_ThreadFallback(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_wg09_runtime_bindings_thread")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	userID := uuid.New()
	threadID := uuid.New()
	runID1 := uuid.New()
	runID2 := uuid.New()

	seedThreadAndRun(t, pool, accountID, threadID, nil, &userID, runID1)
	seedRunOnly(t, pool, accountID, threadID, &userID, runID2)

	first, err := ResolveAndPersistRun(context.Background(), pool, data.Run{
		ID:              runID1,
		AccountID:           accountID,
		ThreadID:        threadID,
		CreatedByUserID: &userID,
	})
	if err != nil {
		t.Fatalf("resolve first run failed: %v", err)
	}
	second, err := ResolveAndPersistRun(context.Background(), pool, data.Run{
		ID:              runID2,
		AccountID:           accountID,
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

func TestResolveAndPersistRun_NewThreadInheritsWorkspaceSkills(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_wg09_runtime_bindings_inherit_skills")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	userID := uuid.New()
	threadID1 := uuid.New()
	threadID2 := uuid.New()
	runID1 := uuid.New()
	runID2 := uuid.New()

	seedThreadAndRun(t, pool, accountID, threadID1, nil, &userID, runID1)
	seedThreadAndRun(t, pool, accountID, threadID2, nil, &userID, runID2)

	first, err := ResolveAndPersistRun(context.Background(), pool, data.Run{
		ID:              runID1,
		AccountID:           accountID,
		ThreadID:        threadID1,
		CreatedByUserID: &userID,
	})
	if err != nil {
		t.Fatalf("resolve first run failed: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`INSERT INTO workspace_skill_enablements (workspace_ref, account_id, enabled_by_user_id, skill_key, version)
		 VALUES ($1, $2, $3, 'deep-research', '1.0.0')`,
		derefString(first.WorkspaceRef),
		accountID,
		userID,
	); err != nil {
		t.Fatalf("seed workspace skill enablement: %v", err)
	}

	second, err := ResolveAndPersistRun(context.Background(), pool, data.Run{
		ID:              runID2,
		AccountID:           accountID,
		ThreadID:        threadID2,
		CreatedByUserID: &userID,
	})
	if err != nil {
		t.Fatalf("resolve second run failed: %v", err)
	}
	if derefString(first.WorkspaceRef) == derefString(second.WorkspaceRef) {
		t.Fatalf("expected new thread workspace_ref, got %q", derefString(second.WorkspaceRef))
	}

	var skillKey string
	var version string
	if err := pool.QueryRow(
		context.Background(),
		`SELECT skill_key, version FROM workspace_skill_enablements WHERE workspace_ref = $1`,
		derefString(second.WorkspaceRef),
	).Scan(&skillKey, &version); err != nil {
		t.Fatalf("load inherited workspace skill: %v", err)
	}
	if skillKey != "deep-research" || version != "1.0.0" {
		t.Fatalf("unexpected inherited skill: %s@%s", skillKey, version)
	}

	var metadata []byte
	if err := pool.QueryRow(
		context.Background(),
		`SELECT metadata_json FROM workspace_registries WHERE workspace_ref = $1`,
		derefString(second.WorkspaceRef),
	).Scan(&metadata); err != nil {
		t.Fatalf("load workspace metadata: %v", err)
	}
	if !bytes.Contains(metadata, []byte("deep-research@1.0.0")) {
		t.Fatalf("expected inherited skill ref in workspace metadata, got %s", string(metadata))
	}
}

func seedThreadAndRun(t *testing.T, pool *pgxpool.Pool, accountID, threadID uuid.UUID, projectID, userID *uuid.UUID, runID uuid.UUID) {
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

func seedRunOnly(t *testing.T, pool *pgxpool.Pool, accountID, threadID uuid.UUID, userID *uuid.UUID, runID uuid.UUID) {
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
