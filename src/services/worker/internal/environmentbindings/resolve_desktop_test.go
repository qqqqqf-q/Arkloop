//go:build desktop

package environmentbindings

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/worker/internal/data"
	"github.com/google/uuid"
)

func TestResolveAndPersistRun_ProjectScopedPerProfileDesktop(t *testing.T) {
	db := openDesktopBindingsDB(t)
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

	seedDesktopBindingsAccount(t, db, accountID, userA, userB)
	seedDesktopBindingsProject(t, db, accountID, projectID)
	seedThreadAndRun(t, db, accountID, threadA1, &projectID, &userA, runA1)
	seedThreadAndRun(t, db, accountID, threadA2, &projectID, &userA, runA2)
	seedThreadAndRun(t, db, accountID, threadB, &projectID, &userB, runB)

	first, err := ResolveAndPersistRun(context.Background(), db, data.Run{
		ID:              runA1,
		AccountID:       accountID,
		ThreadID:        threadA1,
		ProjectID:       &projectID,
		CreatedByUserID: &userA,
	})
	if err != nil {
		t.Fatalf("resolve first run failed: %v", err)
	}
	second, err := ResolveAndPersistRun(context.Background(), db, data.Run{
		ID:              runA2,
		AccountID:       accountID,
		ThreadID:        threadA2,
		ProjectID:       &projectID,
		CreatedByUserID: &userA,
	})
	if err != nil {
		t.Fatalf("resolve second run failed: %v", err)
	}
	third, err := ResolveAndPersistRun(context.Background(), db, data.Run{
		ID:              runB,
		AccountID:       accountID,
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
	profileRecord, err := profileRepo.Get(context.Background(), db, derefString(first.ProfileRef))
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
	workspaceRecord, err := workspaceRepo.Get(context.Background(), db, derefString(first.WorkspaceRef))
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

func TestResolveAndPersistRun_ThreadFallbackDesktop(t *testing.T) {
	db := openDesktopBindingsDB(t)
	accountID := uuid.New()
	userID := uuid.New()
	threadID := uuid.New()
	runID1 := uuid.New()
	runID2 := uuid.New()

	seedDesktopBindingsAccount(t, db, accountID, userID)
	seedThreadAndRun(t, db, accountID, threadID, nil, &userID, runID1)
	seedRunOnly(t, db, accountID, threadID, &userID, runID2)

	first, err := ResolveAndPersistRun(context.Background(), db, data.Run{
		ID:              runID1,
		AccountID:       accountID,
		ThreadID:        threadID,
		CreatedByUserID: &userID,
	})
	if err != nil {
		t.Fatalf("resolve first run failed: %v", err)
	}
	second, err := ResolveAndPersistRun(context.Background(), db, data.Run{
		ID:              runID2,
		AccountID:       accountID,
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

func TestResolveAndPersistRun_NewThreadInheritsWorkspaceSkillsDesktop(t *testing.T) {
	db := openDesktopBindingsDB(t)
	accountID := uuid.New()
	userID := uuid.New()
	threadID1 := uuid.New()
	threadID2 := uuid.New()
	runID1 := uuid.New()
	runID2 := uuid.New()

	seedDesktopBindingsAccount(t, db, accountID, userID)
	seedThreadAndRun(t, db, accountID, threadID1, nil, &userID, runID1)
	seedThreadAndRun(t, db, accountID, threadID2, nil, &userID, runID2)

	first, err := ResolveAndPersistRun(context.Background(), db, data.Run{
		ID:              runID1,
		AccountID:       accountID,
		ThreadID:        threadID1,
		CreatedByUserID: &userID,
	})
	if err != nil {
		t.Fatalf("resolve first run failed: %v", err)
	}
	if _, err := db.Exec(
		context.Background(),
		`INSERT INTO workspace_skill_enablements (workspace_ref, account_id, enabled_by_user_id, skill_key, version)
		 VALUES ($1, $2, $3, 'deep-research', '1.0.0')`,
		derefString(first.WorkspaceRef),
		accountID,
		userID,
	); err != nil {
		t.Fatalf("seed workspace skill enablement: %v", err)
	}

	second, err := ResolveAndPersistRun(context.Background(), db, data.Run{
		ID:              runID2,
		AccountID:       accountID,
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
	if err := db.QueryRow(
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
	if err := db.QueryRow(
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

func openDesktopBindingsDB(t *testing.T) *sqlitepgx.Pool {
	t.Helper()
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlitePool.Close(); err != nil {
			t.Fatalf("close sqlite pool: %v", err)
		}
	})
	return sqlitepgx.New(sqlitePool.Unwrap())
}

func seedDesktopBindingsAccount(t *testing.T, db data.DB, accountID uuid.UUID, userIDs ...uuid.UUID) {
	t.Helper()
	for idx, userID := range userIDs {
		if _, err := db.Exec(
			context.Background(),
			`INSERT INTO users (id, username, email, status)
			 VALUES ($1, $2, $3, 'active')`,
			userID,
			"desktop-user-"+userID.String(),
			"desktop-user-"+userID.String()+"@test.local",
		); err != nil {
			t.Fatalf("seed desktop user %d: %v", idx, err)
		}
	}
	ownerUserID := userIDs[0]
	if _, err := db.Exec(
		context.Background(),
		`INSERT INTO accounts (id, slug, name, type, status, owner_user_id)
		 VALUES ($1, $2, $3, 'personal', 'active', $4)`,
		accountID,
		"desktop-account-"+accountID.String(),
		"Desktop Bindings",
		ownerUserID,
	); err != nil {
		t.Fatalf("seed desktop account: %v", err)
	}
}

func seedDesktopBindingsProject(t *testing.T, db data.DB, accountID, projectID uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(
		context.Background(),
		`INSERT INTO projects (id, account_id, name, visibility)
		 VALUES ($1, $2, $3, 'private')`,
		projectID,
		accountID,
		"Desktop Bindings Project",
	); err != nil {
		t.Fatalf("seed desktop project: %v", err)
	}
}
