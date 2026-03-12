//go:build !desktop

package runengine

import (
	"bytes"
	"context"
	"testing"

	sharedenvironmentref "arkloop/services/shared/environmentref"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/testutil"

	"arkloop/services/shared/database"
	"arkloop/services/shared/database/pgadapter"
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
	dbPool := pgadapter.New(pool)

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

	seedThreadAndRun(t, dbPool, orgID, threadA1, &projectID, &userA, runA1)
	seedThreadAndRun(t, dbPool, orgID, threadA2, &projectID, &userA, runA2)
	seedThreadAndRun(t, dbPool, orgID, threadB, &projectID, &userB, runB)

	first, err := resolveAndPersistEnvironmentBindings(context.Background(), dbPool, data.Run{
		ID:              runA1,
		OrgID:           orgID,
		ThreadID:        threadA1,
		ProjectID:       &projectID,
		CreatedByUserID: &userA,
	}, nil)
	if err != nil {
		t.Fatalf("resolve first run failed: %v", err)
	}
	second, err := resolveAndPersistEnvironmentBindings(context.Background(), dbPool, data.Run{
		ID:              runA2,
		OrgID:           orgID,
		ThreadID:        threadA2,
		ProjectID:       &projectID,
		CreatedByUserID: &userA,
	}, nil)
	if err != nil {
		t.Fatalf("resolve second run failed: %v", err)
	}
	third, err := resolveAndPersistEnvironmentBindings(context.Background(), dbPool, data.Run{
		ID:              runB,
		OrgID:           orgID,
		ThreadID:        threadB,
		ProjectID:       &projectID,
		CreatedByUserID: &userB,
	}, nil)
	if err != nil {
		t.Fatalf("resolve third run failed: %v", err)
	}

	expectedProfileA := sharedenvironmentref.BuildProfileRef(orgID, &userA)
	expectedProfileB := sharedenvironmentref.BuildProfileRef(orgID, &userB)
	expectedWorkspaceA := sharedenvironmentref.BuildWorkspaceRef(orgID, expectedProfileA, data.BindingScopeProject, projectID)
	expectedWorkspaceB := sharedenvironmentref.BuildWorkspaceRef(orgID, expectedProfileB, data.BindingScopeProject, projectID)

	if derefString(first.ProfileRef) != expectedProfileA || derefString(second.ProfileRef) != expectedProfileA {
		t.Fatalf("unexpected profile_ref for user A: %q / %q", derefString(first.ProfileRef), derefString(second.ProfileRef))
	}
	if derefString(first.WorkspaceRef) != expectedWorkspaceA || derefString(second.WorkspaceRef) != expectedWorkspaceA {
		t.Fatalf("unexpected workspace_ref for user A: %q / %q", derefString(first.WorkspaceRef), derefString(second.WorkspaceRef))
	}
	if derefString(third.ProfileRef) != expectedProfileB {
		t.Fatalf("unexpected profile_ref for user B: %q", derefString(third.ProfileRef))
	}
	if derefString(third.WorkspaceRef) != expectedWorkspaceB {
		t.Fatalf("unexpected workspace_ref for user B: %q", derefString(third.WorkspaceRef))
	}

	profileRepo := data.ProfileRegistriesRepository{}
	profileRecord, err := profileRepo.Get(context.Background(), dbPool, derefString(first.ProfileRef))
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
	workspaceRecord, err := workspaceRepo.Get(context.Background(), dbPool, derefString(first.WorkspaceRef))
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

func TestResolveAndPersistEnvironmentBindings_ThreadFallback(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_wg09_runtime_bindings_thread")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)
	dbPool := pgadapter.New(pool)

	orgID := uuid.New()
	userID := uuid.New()
	threadID := uuid.New()
	runID1 := uuid.New()
	runID2 := uuid.New()

	seedThreadAndRun(t, dbPool, orgID, threadID, nil, &userID, runID1)
	seedRunOnly(t, dbPool, orgID, threadID, &userID, runID2)

	first, err := resolveAndPersistEnvironmentBindings(context.Background(), dbPool, data.Run{
		ID:              runID1,
		OrgID:           orgID,
		ThreadID:        threadID,
		CreatedByUserID: &userID,
	}, nil)
	if err != nil {
		t.Fatalf("resolve first run failed: %v", err)
	}
	second, err := resolveAndPersistEnvironmentBindings(context.Background(), dbPool, data.Run{
		ID:              runID2,
		OrgID:           orgID,
		ThreadID:        threadID,
		CreatedByUserID: &userID,
	}, nil)
	if err != nil {
		t.Fatalf("resolve second run failed: %v", err)
	}

	expectedProfile := sharedenvironmentref.BuildProfileRef(orgID, &userID)
	expectedWorkspace := sharedenvironmentref.BuildWorkspaceRef(orgID, expectedProfile, data.BindingScopeThread, threadID)

	if derefString(first.ProfileRef) != expectedProfile || derefString(second.ProfileRef) != expectedProfile {
		t.Fatalf("unexpected thread fallback profile_ref: %q / %q", derefString(first.ProfileRef), derefString(second.ProfileRef))
	}
	if derefString(first.WorkspaceRef) != expectedWorkspace || derefString(second.WorkspaceRef) != expectedWorkspace {
		t.Fatalf("unexpected thread fallback workspace_ref: %q / %q", derefString(first.WorkspaceRef), derefString(second.WorkspaceRef))
	}
}

func TestResolveAndPersistEnvironmentBindings_NewThreadInheritsWorkspaceSkills(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_wg09_runtime_bindings_inherit_skills")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)
	dbPool := pgadapter.New(pool)

	orgID := uuid.New()
	userID := uuid.New()
	threadID1 := uuid.New()
	threadID2 := uuid.New()
	runID1 := uuid.New()
	runID2 := uuid.New()

	seedThreadAndRun(t, dbPool, orgID, threadID1, nil, &userID, runID1)
	seedThreadAndRun(t, dbPool, orgID, threadID2, nil, &userID, runID2)

	first, err := resolveAndPersistEnvironmentBindings(context.Background(), dbPool, data.Run{
		ID:              runID1,
		OrgID:           orgID,
		ThreadID:        threadID1,
		CreatedByUserID: &userID,
	}, nil)
	if err != nil {
		t.Fatalf("resolve first run failed: %v", err)
	}
	if _, err := dbPool.Exec(
		context.Background(),
		`INSERT INTO workspace_skill_enablements (workspace_ref, org_id, enabled_by_user_id, skill_key, version)
		 VALUES ($1, $2, $3, 'deep-research', '1.0.0')`,
		derefString(first.WorkspaceRef),
		orgID,
		userID,
	); err != nil {
		t.Fatalf("seed workspace skill enablement: %v", err)
	}

	second, err := resolveAndPersistEnvironmentBindings(context.Background(), dbPool, data.Run{
		ID:              runID2,
		OrgID:           orgID,
		ThreadID:        threadID2,
		CreatedByUserID: &userID,
	}, nil)
	if err != nil {
		t.Fatalf("resolve second run failed: %v", err)
	}

	expectedProfile := sharedenvironmentref.BuildProfileRef(orgID, &userID)
	expectedFirstWorkspace := sharedenvironmentref.BuildWorkspaceRef(orgID, expectedProfile, data.BindingScopeThread, threadID1)
	expectedSecondWorkspace := sharedenvironmentref.BuildWorkspaceRef(orgID, expectedProfile, data.BindingScopeThread, threadID2)

	if derefString(first.ProfileRef) != expectedProfile || derefString(second.ProfileRef) != expectedProfile {
		t.Fatalf("unexpected inherited-skill profile_ref: %q / %q", derefString(first.ProfileRef), derefString(second.ProfileRef))
	}
	if derefString(first.WorkspaceRef) != expectedFirstWorkspace {
		t.Fatalf("unexpected first workspace_ref: %q", derefString(first.WorkspaceRef))
	}
	if derefString(second.WorkspaceRef) != expectedSecondWorkspace {
		t.Fatalf("unexpected second workspace_ref: %q", derefString(second.WorkspaceRef))
	}

	var skillKey string
	var version string
	if err := dbPool.QueryRow(
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
	if err := dbPool.QueryRow(
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

func seedThreadAndRun(t *testing.T, pool database.DB, orgID, threadID uuid.UUID, projectID, userID *uuid.UUID, runID uuid.UUID) {
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

func seedRunOnly(t *testing.T, pool database.DB, orgID, threadID uuid.UUID, userID *uuid.UUID, runID uuid.UUID) {
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
