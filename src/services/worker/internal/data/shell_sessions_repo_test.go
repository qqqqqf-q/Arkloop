package data

import (
	"context"
	"testing"
	"time"

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

	accountID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	threadID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	runID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	repo := ShellSessionsRepository{}
	liveSessionID := "live-1"
	restoreRev := "restore-1"
	bindingKey := "thread:" + threadID.String()
	record := ShellSessionRecord{
		SessionRef:        "shref_test",
		AccountID:             accountID,
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

	stored, err := repo.GetBySessionRef(context.Background(), pool, accountID, "shref_test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.SessionRef != record.SessionRef || stored.WorkspaceRef != record.WorkspaceRef {
		t.Fatalf("unexpected stored record: %#v", stored)
	}
	if stored.LiveSessionID == nil || *stored.LiveSessionID != liveSessionID {
		t.Fatalf("unexpected live_session_id: %#v", stored.LiveSessionID)
	}
	if stored.SessionType != ShellSessionTypeShell {
		t.Fatalf("expected shell session type, got %q", stored.SessionType)
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

	accountID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	repo := ShellSessionsRepository{}
	if err := repo.Upsert(context.Background(), pool, ShellSessionRecord{
		SessionRef:   "shref_test",
		AccountID:        accountID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
		ShareScope:   ShellShareScopeThread,
		State:        ShellSessionStateReady,
		MetadataJSON: map[string]any{},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.UpdateRestoreRevision(context.Background(), pool, accountID, "shref_test", "restore-2"); err != nil {
		t.Fatalf("update restore revision: %v", err)
	}

	stored, err := repo.GetBySessionRef(context.Background(), pool, accountID, "shref_test")
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

	accountID := uuid.New()
	runID := uuid.New()
	threadID := uuid.New()
	repo := ShellSessionsRepository{}
	bindingKey := "thread:" + threadID.String()
	older := ShellSessionRecord{
		SessionRef:        "shref_old",
		AccountID:             accountID,
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

	latest, err := repo.GetLatestByRun(context.Background(), pool, accountID, runID)
	if err != nil {
		t.Fatalf("get latest by run: %v", err)
	}
	if latest.SessionRef != newer.SessionRef {
		t.Fatalf("expected latest run session %q, got %q", newer.SessionRef, latest.SessionRef)
	}

	bound, err := repo.GetByDefaultBindingKey(context.Background(), pool, accountID, "pref_test", bindingKey)
	if err != nil {
		t.Fatalf("get by default binding key: %v", err)
	}
	if bound.SessionRef != newer.SessionRef {
		t.Fatalf("expected authoritative bound session %q, got %q", newer.SessionRef, bound.SessionRef)
	}

	olderStored, err := repo.GetBySessionRef(context.Background(), pool, accountID, older.SessionRef)
	if err != nil {
		t.Fatalf("get older session: %v", err)
	}
	if olderStored.DefaultBindingKey != nil {
		t.Fatalf("expected older competing binding cleared, got %#v", olderStored.DefaultBindingKey)
	}
}

func TestShellSessionsRepository_IsolatesSessionTypes(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_shell_sessions_types")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	repo := ShellSessionsRepository{}
	accountID := uuid.New()
	runID := uuid.New()
	threadID := uuid.New()
	bindingKey := "thread:" + threadID.String()
	shellRecord := ShellSessionRecord{
		SessionRef:        "shref_shell",
		SessionType:       ShellSessionTypeShell,
		AccountID:             accountID,
		ProfileRef:        "pref_test",
		WorkspaceRef:      "wsref_test",
		ThreadID:          &threadID,
		RunID:             &runID,
		ShareScope:        ShellShareScopeThread,
		State:             ShellSessionStateReady,
		DefaultBindingKey: &bindingKey,
		MetadataJSON:      map[string]any{},
	}
	browserRecord := shellRecord
	browserRecord.SessionRef = "brref_browser"
	browserRecord.SessionType = ShellSessionTypeBrowser
	if err := repo.Upsert(context.Background(), pool, shellRecord); err != nil {
		t.Fatalf("upsert shell: %v", err)
	}
	if err := repo.Upsert(context.Background(), pool, browserRecord); err != nil {
		t.Fatalf("upsert browser: %v", err)
	}

	latestShell, err := repo.GetLatestByRunAndType(context.Background(), pool, accountID, runID, ShellSessionTypeShell)
	if err != nil {
		t.Fatalf("get latest shell: %v", err)
	}
	if latestShell.SessionRef != shellRecord.SessionRef {
		t.Fatalf("expected shell session %q, got %q", shellRecord.SessionRef, latestShell.SessionRef)
	}

	latestBrowser, err := repo.GetLatestByRunAndType(context.Background(), pool, accountID, runID, ShellSessionTypeBrowser)
	if err != nil {
		t.Fatalf("get latest browser: %v", err)
	}
	if latestBrowser.SessionRef != browserRecord.SessionRef {
		t.Fatalf("expected browser session %q, got %q", browserRecord.SessionRef, latestBrowser.SessionRef)
	}

	boundShell, err := repo.GetByDefaultBindingKeyAndType(context.Background(), pool, accountID, "pref_test", bindingKey, ShellSessionTypeShell)
	if err != nil {
		t.Fatalf("get shell binding: %v", err)
	}
	if boundShell.SessionRef != shellRecord.SessionRef {
		t.Fatalf("expected shell binding %q, got %q", shellRecord.SessionRef, boundShell.SessionRef)
	}

	boundBrowser, err := repo.GetByDefaultBindingKeyAndType(context.Background(), pool, accountID, "pref_test", bindingKey, ShellSessionTypeBrowser)
	if err != nil {
		t.Fatalf("get browser binding: %v", err)
	}
	if boundBrowser.SessionRef != browserRecord.SessionRef {
		t.Fatalf("expected browser binding %q, got %q", browserRecord.SessionRef, boundBrowser.SessionRef)
	}

	if _, err := repo.GetBySessionRefAndType(context.Background(), pool, accountID, browserRecord.SessionRef, ShellSessionTypeShell); !IsShellSessionNotFound(err) {
		t.Fatalf("expected typed lookup miss, got %v", err)
	}
}

func TestShellSessionsRepository_ClearLiveSession(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_shell_sessions_clear_live")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	accountID := uuid.New()
	liveSessionID := "live-1"
	repo := ShellSessionsRepository{}
	if err := repo.Upsert(context.Background(), pool, ShellSessionRecord{
		SessionRef:    "shref_test",
		AccountID:         accountID,
		ProfileRef:    "pref_test",
		WorkspaceRef:  "wsref_test",
		ShareScope:    ShellShareScopeWorkspace,
		State:         ShellSessionStateBusy,
		LiveSessionID: &liveSessionID,
		MetadataJSON:  map[string]any{},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.ClearLiveSession(context.Background(), pool, accountID, "shref_test"); err != nil {
		t.Fatalf("clear live session: %v", err)
	}
	stored, err := repo.GetBySessionRef(context.Background(), pool, accountID, "shref_test")
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

func TestShellSessionsRepository_WriterLeaseLifecycle(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_shell_sessions_writer_lease")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	repo := ShellSessionsRepository{}
	accountID := uuid.New()
	if err := repo.Upsert(context.Background(), pool, ShellSessionRecord{
		SessionRef:   "shref_test",
		AccountID:        accountID,
		ProfileRef:   "pref_test",
		WorkspaceRef: "wsref_test",
		ShareScope:   ShellShareScopeWorkspace,
		State:        ShellSessionStateReady,
		MetadataJSON: map[string]any{},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	firstUntil := time.Now().UTC().Add(2 * time.Minute)
	first, err := repo.AcquireWriterLease(context.Background(), pool, accountID, "shref_test", "run:first", firstUntil)
	if err != nil {
		t.Fatalf("acquire first lease: %v", err)
	}
	if first.LeaseOwnerID == nil || *first.LeaseOwnerID != "run:first" {
		t.Fatalf("unexpected lease owner: %#v", first.LeaseOwnerID)
	}
	if first.LeaseEpoch != 0 {
		t.Fatalf("unexpected initial lease epoch: %d", first.LeaseEpoch)
	}

	renewedUntil := time.Now().UTC().Add(3 * time.Minute)
	renewed, err := repo.RenewWriterLease(context.Background(), pool, accountID, "shref_test", "run:first", renewedUntil)
	if err != nil {
		t.Fatalf("renew lease: %v", err)
	}
	if renewed.LeaseEpoch != 0 {
		t.Fatalf("renew should keep epoch, got %d", renewed.LeaseEpoch)
	}
	if renewed.LeaseUntil == nil || !renewed.LeaseUntil.After(firstUntil) {
		t.Fatalf("expected renewed lease_until after first acquire, got %#v", renewed.LeaseUntil)
	}

	_, err = repo.AcquireWriterLease(context.Background(), pool, accountID, "shref_test", "run:second", time.Now().UTC().Add(2*time.Minute))
	if !IsShellSessionLeaseConflict(err) {
		t.Fatalf("expected lease conflict, got %v", err)
	}

	if err := repo.ReleaseWriterLease(context.Background(), pool, accountID, "shref_test", "run:second"); err != nil {
		t.Fatalf("release with wrong owner should be ignored, got %v", err)
	}
	stillHeld, err := repo.GetBySessionRef(context.Background(), pool, accountID, "shref_test")
	if err != nil {
		t.Fatalf("get after wrong release: %v", err)
	}
	if stillHeld.LeaseOwnerID == nil || *stillHeld.LeaseOwnerID != "run:first" {
		t.Fatalf("expected first owner to remain, got %#v", stillHeld.LeaseOwnerID)
	}

	staleUntil := time.Now().UTC().Add(-time.Minute)
	if _, err := pool.Exec(context.Background(), `UPDATE shell_sessions SET lease_until = $3 WHERE account_id = $1 AND session_ref = $2`, accountID, "shref_test", staleUntil); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	second, err := repo.AcquireWriterLease(context.Background(), pool, accountID, "shref_test", "run:second", time.Now().UTC().Add(2*time.Minute))
	if err != nil {
		t.Fatalf("acquire second lease after expiry: %v", err)
	}
	if second.LeaseOwnerID == nil || *second.LeaseOwnerID != "run:second" {
		t.Fatalf("unexpected second lease owner: %#v", second.LeaseOwnerID)
	}
	if second.LeaseEpoch != 1 {
		t.Fatalf("expected epoch increment after owner switch, got %d", second.LeaseEpoch)
	}

	if err := repo.ClearFinishedWriterLease(context.Background(), pool, accountID, "shref_test"); err != nil {
		t.Fatalf("clear finished lease: %v", err)
	}
	cleared, err := repo.GetBySessionRef(context.Background(), pool, accountID, "shref_test")
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if cleared.LeaseOwnerID != nil || cleared.LeaseUntil != nil {
		t.Fatalf("expected cleared lease, got owner=%#v until=%#v", cleared.LeaseOwnerID, cleared.LeaseUntil)
	}
	if cleared.State != ShellSessionStateReady {
		t.Fatalf("expected ready after clear, got %s", cleared.State)
	}
}
