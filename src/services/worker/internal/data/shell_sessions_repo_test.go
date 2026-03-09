package data

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestShellSessionsRepository_UpsertAndGet(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_shell_sessions")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	threadID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	runID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	repo := ShellSessionsRepository{}
	liveSessionID := "live-1"
	restoreRev := "restore-1"
	bindingKey := "thread:" + threadID.String()
	record := ShellSessionRecord{
		SessionRef:        "shref_test",
		OrgID:             orgID,
		ProfileRef:        "pref_test",
		WorkspaceRef:      "wsref_test",
		ThreadID:          &threadID,
		RunID:             &runID,
		ShareScope:        ShellShareScopeThread,
		State:             ShellSessionStateBusy,
		LiveSessionID:     &liveSessionID,
		LatestRestoreRev:  &restoreRev,
		DefaultBindingKey: &bindingKey,
		MetadataJSON:      map[string]any{"source": "test"},
	}
	if err := repo.Upsert(context.Background(), pool, record); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	stored, err := repo.GetBySessionRef(context.Background(), pool, orgID, "shref_test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.SessionRef != record.SessionRef || stored.WorkspaceRef != record.WorkspaceRef {
		t.Fatalf("unexpected stored record: %#v", stored)
	}
	if stored.LiveSessionID == nil || *stored.LiveSessionID != liveSessionID {
		t.Fatalf("unexpected live_session_id: %#v", stored.LiveSessionID)
	}
	if stored.State != ShellSessionStateBusy {
		t.Fatalf("unexpected state: %s", stored.State)
	}
	if stored.LatestRestoreRev == nil || *stored.LatestRestoreRev != restoreRev {
		t.Fatalf("unexpected latest_restore_rev: %#v", stored.LatestRestoreRev)
	}
	if stored.DefaultBindingKey == nil || *stored.DefaultBindingKey != bindingKey {
		t.Fatalf("unexpected default_binding_key: %#v", stored.DefaultBindingKey)
	}
}

func TestShellSessionsRepository_UpdateRestoreRevision(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_shell_sessions_restore")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	repo := ShellSessionsRepository{}
	if err := repo.Upsert(context.Background(), pool, ShellSessionRecord{
		SessionRef:   "shref_test",
		OrgID:        orgID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
		ShareScope:   ShellShareScopeThread,
		State:        ShellSessionStateReady,
		MetadataJSON: map[string]any{},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.UpdateRestoreRevision(context.Background(), pool, orgID, "shref_test", "restore-2"); err != nil {
		t.Fatalf("update restore revision: %v", err)
	}

	stored, err := repo.GetBySessionRef(context.Background(), pool, orgID, "shref_test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.LatestRestoreRev == nil || *stored.LatestRestoreRev != "restore-2" {
		t.Fatalf("unexpected latest_restore_rev: %#v", stored.LatestRestoreRev)
	}
}

func TestShellSessionsRepository_GetLatestByRunAndDefaultBinding(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_shell_sessions_lookup")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	runID := uuid.New()
	threadID := uuid.New()
	repo := ShellSessionsRepository{}
	bindingKey := "thread:" + threadID.String()
	older := ShellSessionRecord{
		SessionRef:        "shref_old",
		OrgID:             orgID,
		ProfileRef:        "pref_test",
		WorkspaceRef:      "wsref_test",
		ThreadID:          &threadID,
		RunID:             &runID,
		ShareScope:        ShellShareScopeThread,
		State:             ShellSessionStateReady,
		DefaultBindingKey: &bindingKey,
		MetadataJSON:      map[string]any{},
	}
	newer := older
	newer.SessionRef = "shref_new"
	if err := repo.Upsert(context.Background(), pool, older); err != nil {
		t.Fatalf("upsert older: %v", err)
	}
	if err := repo.Upsert(context.Background(), pool, newer); err != nil {
		t.Fatalf("upsert newer: %v", err)
	}

	latest, err := repo.GetLatestByRun(context.Background(), pool, orgID, runID)
	if err != nil {
		t.Fatalf("get latest by run: %v", err)
	}
	if latest.SessionRef != newer.SessionRef {
		t.Fatalf("expected latest run session %q, got %q", newer.SessionRef, latest.SessionRef)
	}

	bound, err := repo.GetByDefaultBindingKey(context.Background(), pool, orgID, "pref_test", bindingKey)
	if err != nil {
		t.Fatalf("get by default binding key: %v", err)
	}
	if bound.SessionRef != newer.SessionRef {
		t.Fatalf("expected latest bound session %q, got %q", newer.SessionRef, bound.SessionRef)
	}
}

func TestShellSessionsRepository_ClearLiveSession(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_shell_sessions_clear_live")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.New()
	liveSessionID := "live-1"
	repo := ShellSessionsRepository{}
	if err := repo.Upsert(context.Background(), pool, ShellSessionRecord{
		SessionRef:    "shref_test",
		OrgID:         orgID,
		ProfileRef:    "pref_test",
		WorkspaceRef:  "wsref_test",
		ShareScope:    ShellShareScopeWorkspace,
		State:         ShellSessionStateBusy,
		LiveSessionID: &liveSessionID,
		MetadataJSON:  map[string]any{},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.ClearLiveSession(context.Background(), pool, orgID, "shref_test"); err != nil {
		t.Fatalf("clear live session: %v", err)
	}
	stored, err := repo.GetBySessionRef(context.Background(), pool, orgID, "shref_test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.LiveSessionID != nil {
		t.Fatalf("expected live_session_id cleared, got %#v", stored.LiveSessionID)
	}
	if stored.State != ShellSessionStateReady {
		t.Fatalf("expected state ready after clear, got %s", stored.State)
	}
}
