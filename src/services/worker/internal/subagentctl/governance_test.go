package subagentctl

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedSubAgent(t *testing.T, pool *pgxpool.Pool, orgID, parentRunID, parentThreadID, rootRunID, rootThreadID uuid.UUID, depth int, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO sub_agents (id, org_id, parent_run_id, parent_thread_id, root_run_id, root_thread_id, depth, source_type, context_mode, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		id, orgID, parentRunID, parentThreadID, rootRunID, rootThreadID, depth,
		"thread_spawn", "isolated", status,
	)
	if err != nil {
		t.Fatalf("seed sub_agent: %v", err)
	}
	return id
}

func seedPendingInput(t *testing.T, pool *pgxpool.Pool, subAgentID uuid.UUID, input string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO sub_agent_pending_inputs (sub_agent_id, input) VALUES ($1, $2)`,
		subAgentID, input,
	)
	if err != nil {
		t.Fatalf("seed pending input: %v", err)
	}
}

func setupGovernanceTest(t *testing.T, dbName string) (*pgxpool.Pool, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	db := testutil.SetupPostgresDatabase(t, dbName)
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	projectID := uuid.New()
	userID := uuid.New()
	seedThreadAndRun(t, pool, orgID, threadID, &projectID, &userID, runID)

	return pool, orgID, threadID, runID
}

func TestSpawnGovernorDepthLimit(t *testing.T) {
	pool, _, _, runID := setupGovernanceTest(t, "arkloop_gov_depth")

	governor := NewSpawnGovernor(SubAgentLimits{MaxDepth: 2})
	parentRun := data.Run{ID: runID}
	rootRunID := runID

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(context.Background())

	if err := governor.ValidateSpawn(context.Background(), tx, parentRun, rootRunID, 2); err != nil {
		t.Fatalf("depth=2 should pass: %v", err)
	}

	err = governor.ValidateSpawn(context.Background(), tx, parentRun, rootRunID, 3)
	if err == nil {
		t.Fatal("depth=3 should fail")
	}
	if !strings.Contains(err.Error(), "depth") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpawnGovernorActivePerRootRunLimit(t *testing.T) {
	pool, orgID, threadID, runID := setupGovernanceTest(t, "arkloop_gov_active_root")

	seedSubAgent(t, pool, orgID, runID, threadID, runID, threadID, 1, data.SubAgentStatusRunning)
	seedSubAgent(t, pool, orgID, runID, threadID, runID, threadID, 1, data.SubAgentStatusQueued)

	governor := NewSpawnGovernor(SubAgentLimits{MaxActivePerRootRun: 2})
	parentRun := data.Run{ID: runID, OrgID: orgID}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(context.Background())

	err = governor.ValidateSpawn(context.Background(), tx, parentRun, runID, 1)
	if err == nil {
		t.Fatal("should reject when active count reaches limit")
	}
	if !strings.Contains(err.Error(), "active") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpawnGovernorParallelChildrenLimit(t *testing.T) {
	pool, orgID, threadID, runID := setupGovernanceTest(t, "arkloop_gov_parallel")

	seedSubAgent(t, pool, orgID, runID, threadID, runID, threadID, 1, data.SubAgentStatusRunning)

	governor := NewSpawnGovernor(SubAgentLimits{MaxParallelChildren: 1})
	parentRun := data.Run{ID: runID, OrgID: orgID}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(context.Background())

	err = governor.ValidateSpawn(context.Background(), tx, parentRun, runID, 1)
	if err == nil {
		t.Fatal("should reject when parallel children reaches limit")
	}
	if !strings.Contains(err.Error(), "parallel") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpawnGovernorDescendantsPerRootRunLimit(t *testing.T) {
	pool, orgID, threadID, runID := setupGovernanceTest(t, "arkloop_gov_descendants")

	seedSubAgent(t, pool, orgID, runID, threadID, runID, threadID, 1, data.SubAgentStatusCompleted)
	seedSubAgent(t, pool, orgID, runID, threadID, runID, threadID, 1, data.SubAgentStatusFailed)
	seedSubAgent(t, pool, orgID, runID, threadID, runID, threadID, 1, data.SubAgentStatusRunning)

	governor := NewSpawnGovernor(SubAgentLimits{MaxDescendantsPerRootRun: 3})
	parentRun := data.Run{ID: runID, OrgID: orgID}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(context.Background())

	err = governor.ValidateSpawn(context.Background(), tx, parentRun, runID, 1)
	if err == nil {
		t.Fatal("should reject when descendant count reaches limit")
	}
	if !strings.Contains(err.Error(), "descendant") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpawnGovernorPendingInputLimit(t *testing.T) {
	pool, orgID, threadID, runID := setupGovernanceTest(t, "arkloop_gov_pending")

	subAgentID := seedSubAgent(t, pool, orgID, runID, threadID, runID, threadID, 1, data.SubAgentStatusRunning)
	seedPendingInput(t, pool, subAgentID, "input-1")
	seedPendingInput(t, pool, subAgentID, "input-2")

	governor := NewSpawnGovernor(SubAgentLimits{MaxPendingPerRootRun: 2})

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(context.Background())

	err = governor.ValidatePendingInput(context.Background(), tx, runID)
	if err == nil {
		t.Fatal("should reject when pending count reaches limit")
	}
	if !strings.Contains(err.Error(), "pending") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpawnGovernorZeroLimitMeansUnlimited(t *testing.T) {
	pool, orgID, threadID, runID := setupGovernanceTest(t, "arkloop_gov_unlimited")

	for i := 0; i < 10; i++ {
		subAgentID := seedSubAgent(t, pool, orgID, runID, threadID, runID, threadID, 1, data.SubAgentStatusRunning)
		seedPendingInput(t, pool, subAgentID, "queued-input")
	}

	governor := NewSpawnGovernor(SubAgentLimits{})
	parentRun := data.Run{ID: runID, OrgID: orgID}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(context.Background())

	if err := governor.ValidateSpawn(context.Background(), tx, parentRun, runID, 100); err != nil {
		t.Fatalf("zero limits should allow anything: %v", err)
	}
	if err := governor.ValidatePendingInput(context.Background(), tx, runID); err != nil {
		t.Fatalf("zero pending limit should allow anything: %v", err)
	}
}

func TestServiceSpawnRejectsOnParallelChildrenLimit(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_gov_svc_parallel")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	userID := uuid.New()
	seedThreadAndRun(t, pool, orgID, threadID, &projectID, &userID, runID)

	parentRun := data.Run{ID: runID, OrgID: orgID, ThreadID: threadID, ProjectID: &projectID, CreatedByUserID: &userID}
	service := NewService(pool, nil, &stubJobQueue{}, parentRun, "trace-gov", SubAgentLimits{MaxParallelChildren: 1})

	_, err = service.Spawn(context.Background(), isolatedSpawnRequest("first child"))
	if err != nil {
		t.Fatalf("first spawn should succeed: %v", err)
	}

	_, err = service.Spawn(context.Background(), isolatedSpawnRequest("second child"))
	if err == nil {
		t.Fatal("second spawn should be rejected")
	}
	if !strings.Contains(err.Error(), "parallel") {
		t.Fatalf("unexpected error: %v", err)
	}
}
