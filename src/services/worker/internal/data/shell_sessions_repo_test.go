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
	record := ShellSessionRecord{
		SessionRef:    "shref_test",
		OrgID:         orgID,
		ProfileRef:    "pref_test",
		WorkspaceRef:  "wsref_test",
		ThreadID:      &threadID,
		RunID:         &runID,
		ShareScope:    ShellShareScopeThread,
		State:         ShellSessionStateBusy,
		LiveSessionID: &liveSessionID,
		MetadataJSON:  map[string]any{"source": "test"},
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
}

func TestDefaultShellSessionBindingsRepository_UpsertAndGet(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_shell_session_bindings")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	orgID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	repo := DefaultShellSessionBindingsRepository{}
	if err := repo.Upsert(context.Background(), pool, orgID, "pref_test", ShellBindingScopeThread, "thread-1", "shref_a"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	ref, err := repo.Get(context.Background(), pool, orgID, "pref_test", ShellBindingScopeThread, "thread-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ref != "shref_a" {
		t.Fatalf("unexpected session_ref: %s", ref)
	}
}
