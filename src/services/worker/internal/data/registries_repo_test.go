package data

import (
	"context"
	"errors"
	"testing"
	"time"

	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProfileRegistriesRepository_GetOrCreateAndTransitions(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_profile_registries")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	accountID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	ownerUserID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	repo := ProfileRegistriesRepository{}
	record, err := repo.GetOrCreate(context.Background(), pool, RegistryRecord{
		Ref:                 "pref_test",
		AccountID:               accountID,
		OwnerUserID:         &ownerUserID,
		DefaultWorkspaceRef: stringPtr("wsref_test"),
		MetadataJSON:        map[string]any{"source": "test"},
	})
	if err != nil {
		t.Fatalf("get or create: %v", err)
	}
	if record.Ref != "pref_test" || record.FlushState != FlushStateIdle {
		t.Fatalf("unexpected record: %#v", record)
	}
	if record.OwnerUserID == nil || *record.OwnerUserID != ownerUserID {
		t.Fatalf("unexpected owner_user_id: %#v", record.OwnerUserID)
	}
	if record.DefaultWorkspaceRef == nil || *record.DefaultWorkspaceRef != "wsref_test" {
		t.Fatalf("unexpected default_workspace_ref: %#v", record.DefaultWorkspaceRef)
	}

	record2, err := repo.GetOrCreate(context.Background(), pool, RegistryRecord{Ref: "pref_test", AccountID: accountID})
	if err != nil {
		t.Fatalf("get or create twice: %v", err)
	}
	if !record.CreatedAt.Equal(record2.CreatedAt) {
		t.Fatalf("expected idempotent create, got %v and %v", record.CreatedAt, record2.CreatedAt)
	}

	if err := repo.MarkFlushPending(context.Background(), pool, "pref_test"); err != nil {
		t.Fatalf("mark pending: %v", err)
	}
	if err := repo.MarkFlushRunning(context.Background(), pool, "pref_test"); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	failedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.MarkFlushFailed(context.Background(), pool, "pref_test", failedAt); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	stored, err := repo.Get(context.Background(), pool, "pref_test")
	if err != nil {
		t.Fatalf("get after fail: %v", err)
	}
	if stored.FlushState != FlushStateFailed || stored.FlushRetryCount != 1 || stored.LastFlushFailedAt == nil {
		t.Fatalf("unexpected failed record: %#v", stored)
	}

	succeededAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.MarkFlushSucceeded(context.Background(), pool, "pref_test", "rev-1", succeededAt); err != nil {
		t.Fatalf("mark succeeded: %v", err)
	}
	stored, err = repo.Get(context.Background(), pool, "pref_test")
	if err != nil {
		t.Fatalf("get after success: %v", err)
	}
	if stored.FlushState != FlushStateIdle || stored.FlushRetryCount != 0 {
		t.Fatalf("unexpected success state: %#v", stored)
	}
	if stored.LatestManifestRev == nil || *stored.LatestManifestRev != "rev-1" {
		t.Fatalf("unexpected latest manifest: %#v", stored.LatestManifestRev)
	}
	if stored.LastFlushSucceededAt == nil {
		t.Fatalf("expected success timestamp")
	}
}

func TestWorkspaceRegistriesRepository_FlushLeaseCAS(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_workspace_registries_lease")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	accountID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	repo := WorkspaceRegistriesRepository{}
	if _, err := repo.GetOrCreate(context.Background(), pool, RegistryRecord{Ref: "wsref_test", AccountID: accountID}); err != nil {
		t.Fatalf("get or create: %v", err)
	}
	if err := repo.MarkFlushPending(context.Background(), pool, "wsref_test"); err != nil {
		t.Fatalf("mark pending: %v", err)
	}
	leaseUntil := time.Now().UTC().Add(time.Minute)
	if err := repo.AcquireFlushLease(context.Background(), pool, "wsref_test", "holder-a", "", leaseUntil); err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	if err := repo.AcquireFlushLease(context.Background(), pool, "wsref_test", "holder-b", "", leaseUntil); !errors.Is(err, ErrFlushConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if err := repo.ReleaseFlushFailure(context.Background(), pool, "wsref_test", "holder-b", time.Now().UTC()); err != nil {
		t.Fatalf("release failure: %v", err)
	}
	stored, err := repo.Get(context.Background(), pool, "wsref_test")
	if err != nil {
		t.Fatalf("get after failed conflict: %v", err)
	}
	if stored.FlushState != FlushStateFailed || stored.FlushRetryCount != 1 {
		t.Fatalf("unexpected failed record: %#v", stored)
	}
	if err := repo.CommitFlushSuccess(context.Background(), pool, "wsref_test", "holder-a", "", "rev-1", time.Now().UTC()); err != nil {
		t.Fatalf("commit success: %v", err)
	}
	stored, err = repo.Get(context.Background(), pool, "wsref_test")
	if err != nil {
		t.Fatalf("get after success: %v", err)
	}
	if stored.LatestManifestRev == nil || *stored.LatestManifestRev != "rev-1" {
		t.Fatalf("unexpected latest revision: %#v", stored.LatestManifestRev)
	}
	if stored.LeaseHolderID != nil || stored.LeaseUntil != nil {
		t.Fatalf("expected cleared lease: %#v", stored)
	}
	if stored.FlushState != FlushStateIdle || stored.FlushRetryCount != 0 {
		t.Fatalf("unexpected success state: %#v", stored)
	}
	if err := repo.AcquireFlushLease(context.Background(), pool, "wsref_test", "holder-c", "", time.Now().UTC().Add(time.Minute)); !errors.Is(err, ErrFlushConflict) {
		t.Fatalf("expected base revision conflict, got %v", err)
	}
}

func TestWorkspaceRegistriesRepository_UpsertTouch(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_workspace_registries")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	accountID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	ownerUserID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	projectID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	repo := WorkspaceRegistriesRepository{}
	if err := repo.UpsertTouch(context.Background(), pool, RegistryRecord{
		Ref:                    "wsref_test",
		AccountID:                  accountID,
		OwnerUserID:            &ownerUserID,
		ProjectID:              &projectID,
		DefaultShellSessionRef: stringPtr("shref_test"),
		MetadataJSON:           map[string]any{"source": "test"},
	}); err != nil {
		t.Fatalf("upsert touch: %v", err)
	}
	record, err := repo.Get(context.Background(), pool, "wsref_test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if record.Ref != "wsref_test" || record.AccountID != accountID {
		t.Fatalf("unexpected record: %#v", record)
	}
	if record.OwnerUserID == nil || *record.OwnerUserID != ownerUserID {
		t.Fatalf("unexpected owner_user_id: %#v", record.OwnerUserID)
	}
	if record.ProjectID == nil || *record.ProjectID != projectID {
		t.Fatalf("unexpected project_id: %#v", record.ProjectID)
	}
	if record.DefaultShellSessionRef == nil || *record.DefaultShellSessionRef != "shref_test" {
		t.Fatalf("unexpected default_shell_session_ref: %#v", record.DefaultShellSessionRef)
	}
	if record.LastUsedAt.IsZero() {
		t.Fatalf("expected last_used_at to be set")
	}
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	copied := value
	return &copied
}
