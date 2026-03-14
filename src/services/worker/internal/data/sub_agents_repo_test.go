package data

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSubAgentRepository_CreateAndTransitions(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_sub_agents_repo_transitions")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	parentThreadID := uuid.New()
	childThreadID := uuid.New()
	parentRunID := uuid.New()
	childRunID := uuid.New()
	seedThread(t, pool, orgID, parentThreadID, uuid.New(), nil)
	seedRun(t, pool, orgID, parentThreadID, parentRunID, nil)
	seedThread(t, pool, orgID, childThreadID, uuid.New(), nil)
	seedRun(t, pool, orgID, childThreadID, childRunID, &parentRunID)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(context.Background())

	repo := SubAgentRepository{}
	record, err := repo.Create(context.Background(), tx, SubAgentCreateParams{
		OrgID:          orgID,
		ParentRunID:    parentRunID,
		ParentThreadID: parentThreadID,
		RootRunID:      parentRunID,
		RootThreadID:   parentThreadID,
		Depth:          1,
		PersonaID:      ptr("researcher@1"),
		SourceType:     SubAgentSourceTypeThreadSpawn,
		ContextMode:    SubAgentContextModeIsolated,
	})
	if err != nil {
		t.Fatalf("create sub_agent: %v", err)
	}
	if record.Status != SubAgentStatusCreated {
		t.Fatalf("unexpected initial status: %s", record.Status)
	}
	if err := repo.TransitionToQueued(context.Background(), tx, record.ID, childRunID); err != nil {
		t.Fatalf("transition queued: %v", err)
	}
	if err := repo.TransitionToRunning(context.Background(), tx, childRunID); err != nil {
		t.Fatalf("transition running: %v", err)
	}
	if err := repo.TransitionToTerminal(context.Background(), tx, childRunID, SubAgentStatusCompleted, nil); err != nil {
		t.Fatalf("transition terminal: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit: %v", err)
	}

	readTx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer readTx.Rollback(context.Background())

	got, err := repo.Get(context.Background(), readTx, record.ID)
	if err != nil {
		t.Fatalf("get sub_agent: %v", err)
	}
	if got == nil {
		t.Fatal("expected sub_agent record")
	}
	if got.Status != SubAgentStatusCompleted {
		t.Fatalf("unexpected terminal status: %s", got.Status)
	}
	if got.CurrentRunID != nil {
		t.Fatalf("expected current_run_id cleared, got %#v", got.CurrentRunID)
	}
	if got.LastCompletedRunID == nil || *got.LastCompletedRunID != childRunID {
		t.Fatalf("unexpected last_completed_run_id: %#v", got.LastCompletedRunID)
	}
	if got.CompletedAt == nil {
		t.Fatal("expected completed_at to be set")
	}
}

func TestSubAgentRepository_RejectsIllegalTransitions(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_sub_agents_repo_illegal")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	seedThread(t, pool, orgID, threadID, uuid.New(), nil)
	seedRun(t, pool, orgID, threadID, runID, nil)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(context.Background())

	repo := SubAgentRepository{}
	record, err := repo.Create(context.Background(), tx, SubAgentCreateParams{
		OrgID:          orgID,
		ParentRunID:    runID,
		ParentThreadID: threadID,
		RootRunID:      runID,
		RootThreadID:   threadID,
		Depth:          1,
		SourceType:     SubAgentSourceTypeThreadSpawn,
		ContextMode:    SubAgentContextModeIsolated,
	})
	if err != nil {
		t.Fatalf("create sub_agent: %v", err)
	}
	if err := validateSubAgentStatusTransition(SubAgentStatusCreated, SubAgentStatusCompleted); err == nil {
		t.Fatal("expected created -> completed transition to fail")
	}
	if err := repo.TransitionToQueued(context.Background(), tx, record.ID, uuid.New()); err != nil {
		t.Fatalf("transition queued: %v", err)
	}
	if err := validateSubAgentStatusTransition(SubAgentStatusClosed, SubAgentStatusRunning); err == nil {
		t.Fatal("expected closed -> running transition to fail")
	}
}

func TestRunsRepository_GetLineage(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_runs_lineage")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	rootThreadID := uuid.New()
	childThreadID := uuid.New()
	grandThreadID := uuid.New()
	rootRunID := uuid.New()
	childRunID := uuid.New()
	grandRunID := uuid.New()

	seedThread(t, pool, orgID, rootThreadID, uuid.New(), nil)
	seedRun(t, pool, orgID, rootThreadID, rootRunID, nil)
	seedThread(t, pool, orgID, childThreadID, uuid.New(), nil)
	seedRun(t, pool, orgID, childThreadID, childRunID, &rootRunID)
	seedThread(t, pool, orgID, grandThreadID, uuid.New(), nil)
	seedRun(t, pool, orgID, grandThreadID, grandRunID, &childRunID)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(context.Background())

	repo := RunsRepository{}
	rootLineage, err := repo.GetLineage(context.Background(), tx, rootRunID)
	if err != nil {
		t.Fatalf("root lineage: %v", err)
	}
	if rootLineage.RootRunID != rootRunID || rootLineage.RootThreadID != rootThreadID || rootLineage.Depth != 0 {
		t.Fatalf("unexpected root lineage: %#v", rootLineage)
	}
	grandLineage, err := repo.GetLineage(context.Background(), tx, grandRunID)
	if err != nil {
		t.Fatalf("grand lineage: %v", err)
	}
	if grandLineage.RootRunID != rootRunID || grandLineage.RootThreadID != rootThreadID || grandLineage.Depth != 2 {
		t.Fatalf("unexpected grand lineage: %#v", grandLineage)
	}
}

func seedThread(t *testing.T, pool *pgxpool.Pool, orgID, threadID, projectID uuid.UUID, userID *uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO threads (id, account_id, created_by_user_id, project_id) VALUES ($1, $2, $3, $4)`,
		threadID, orgID, userID, projectID,
	)
	if err != nil {
		t.Fatalf("insert thread: %v", err)
	}
}

func seedRun(t *testing.T, pool *pgxpool.Pool, orgID, threadID, runID uuid.UUID, parentRunID *uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO runs (id, account_id, thread_id, parent_run_id, status) VALUES ($1, $2, $3, $4, 'running')`,
		runID, orgID, threadID, parentRunID,
	)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
}

func ptr(value string) *string { return &value }
