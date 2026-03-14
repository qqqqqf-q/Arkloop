package pipeline

import (
	"context"
	"fmt"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/testutil"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSubAgentContextMiddlewareRestoresRouteAndNarrowsAllowlist(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "pipeline_subagent_context")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	projectID := uuid.New()
	parentThreadID := uuid.New()
	childThreadID := uuid.New()
	parentRunID := uuid.New()
	childRunID := uuid.New()
	subAgentID := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3), ($4, $2, $3)`, parentThreadID, orgID, projectID, childThreadID); err != nil {
		t.Fatalf("insert threads: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running'), ($4, $2, $5, 'running')`, parentRunID, orgID, parentThreadID, childRunID, childThreadID); err != nil {
		t.Fatalf("insert runs: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO sub_agents (id, org_id, parent_run_id, parent_thread_id, root_run_id, root_thread_id, depth, source_type, context_mode, status, current_run_id) VALUES ($1, $2, $3, $4, $3, $4, 1, $5, $6, $7, $8)`, subAgentID, orgID, parentRunID, parentThreadID, data.SubAgentSourceTypeThreadSpawn, data.SubAgentContextModeForkRecent, data.SubAgentStatusQueued, childRunID); err != nil {
		t.Fatalf("insert sub_agent: %v", err)
	}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	storage := subagentctl.NewSnapshotStorage()
	if err := storage.Save(context.Background(), tx, subAgentID, subagentctl.ContextSnapshot{
		ContextMode: data.SubAgentContextModeForkRecent,
		Runtime: subagentctl.ContextSnapshotRuntime{
			ToolAllowlist: []string{"echo"},
			ToolDenylist:  []string{"browser"},
			RouteID:       "route_parent",
		},
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit snapshot: %v", err)
	}

	registry := tools.NewRegistry()
	if err := registry.Register(tools.AgentToolSpec{Name: "echo", Version: "1", Description: "echo", RiskLevel: tools.RiskLevelLow}); err != nil {
		t.Fatalf("register echo: %v", err)
	}
	if err := registry.Register(tools.AgentToolSpec{Name: "noop", Version: "1", Description: "noop", RiskLevel: tools.RiskLevelLow}); err != nil {
		t.Fatalf("register noop: %v", err)
	}
	if err := registry.Register(tools.AgentToolSpec{Name: "browser", Version: "1", Description: "browser", RiskLevel: tools.RiskLevelHigh}); err != nil {
		t.Fatalf("register browser: %v", err)
	}

	rc := &RunContext{
		Run:          data.Run{ID: childRunID, AccountID: orgID, ThreadID: childThreadID, ParentRunID: &parentRunID},
		Pool:         pool,
		InputJSON:    map[string]any{},
		AllowlistSet: map[string]struct{}{"echo": {}, "noop": {}, "browser": {}},
		ToolRegistry: registry,
	}
	mw := NewSubAgentContextMiddleware(storage)
	if err := mw(context.Background(), rc, func(_ context.Context, rc *RunContext) error {
		if got := rc.InputJSON["route_id"]; got != "route_parent" {
			return fmt.Errorf("unexpected route_id: %#v", got)
		}
		if _, ok := rc.AllowlistSet["echo"]; !ok {
			return fmt.Errorf("echo missing: %#v", rc.AllowlistSet)
		}
		if _, ok := rc.AllowlistSet["noop"]; ok {
			return fmt.Errorf("noop should be removed: %#v", rc.AllowlistSet)
		}
		if _, ok := rc.AllowlistSet["browser"]; ok {
			return fmt.Errorf("browser should be denied: %#v", rc.AllowlistSet)
		}
		if len(rc.ToolDenylist) != 1 || rc.ToolDenylist[0] != "browser" {
			return fmt.Errorf("unexpected denylist: %#v", rc.ToolDenylist)
		}
		return nil
	}); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}
}

func TestSubAgentContextMiddlewareNarrowsRoleExpandedAllowlist(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "pipeline_subagent_context_role")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	projectID := uuid.New()
	parentThreadID := uuid.New()
	childThreadID := uuid.New()
	parentRunID := uuid.New()
	childRunID := uuid.New()
	subAgentID := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3), ($4, $2, $3)`, parentThreadID, orgID, projectID, childThreadID); err != nil {
		t.Fatalf("insert threads: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running'), ($4, $2, $5, 'running')`, parentRunID, orgID, parentThreadID, childRunID, childThreadID); err != nil {
		t.Fatalf("insert runs: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO sub_agents (id, org_id, parent_run_id, parent_thread_id, root_run_id, root_thread_id, depth, source_type, context_mode, status, current_run_id) VALUES ($1, $2, $3, $4, $3, $4, 1, $5, $6, $7, $8)`, subAgentID, orgID, parentRunID, parentThreadID, data.SubAgentSourceTypeThreadSpawn, data.SubAgentContextModeForkRecent, data.SubAgentStatusQueued, childRunID); err != nil {
		t.Fatalf("insert sub_agent: %v", err)
	}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	storage := subagentctl.NewSnapshotStorage()
	if err := storage.Save(context.Background(), tx, subAgentID, subagentctl.ContextSnapshot{
		ContextMode: data.SubAgentContextModeForkRecent,
		Runtime: subagentctl.ContextSnapshotRuntime{
			ToolAllowlist: []string{"tool_b", "tool_c"},
			ToolDenylist:  []string{"tool_c"},
		},
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit snapshot: %v", err)
	}

	toolRegistry := tools.NewRegistry()
	for _, spec := range []tools.AgentToolSpec{
		{Name: "tool_a", Version: "1", Description: "a", RiskLevel: tools.RiskLevelLow},
		{Name: "tool_b", Version: "1", Description: "b", RiskLevel: tools.RiskLevelLow},
		{Name: "tool_c", Version: "1", Description: "c", RiskLevel: tools.RiskLevelLow},
	} {
		if err := toolRegistry.Register(spec); err != nil {
			t.Fatalf("register tool: %v", err)
		}
	}
	personaRegistry := personas.NewRegistry()
	if err := personaRegistry.Register(personas.Definition{
		ID:             "p1",
		Version:        "1",
		Title:          "Test",
		PromptMD:       "base prompt",
		ExecutorType:   "agent.simple",
		ExecutorConfig: map[string]any{},
		ToolAllowlist:  []string{"tool_a"},
		Roles: map[string]personas.RoleOverride{
			"worker": {
				HasToolAllowlist: true,
				ToolAllowlist:    []string{"tool_b", "tool_c"},
			},
		},
	}); err != nil {
		t.Fatalf("register persona: %v", err)
	}

	rc := &RunContext{
		Run:          data.Run{ID: childRunID, AccountID: orgID, ThreadID: childThreadID, ParentRunID: &parentRunID},
		Pool:         pool,
		InputJSON:    map[string]any{"persona_id": "p1", "role": "worker"},
		AllowlistSet: map[string]struct{}{"tool_a": {}, "tool_b": {}, "tool_c": {}},
		ToolRegistry: toolRegistry,
	}
	mwPersona := NewPersonaResolutionMiddleware(
		func() *personas.Registry { return personaRegistry },
		nil,
		data.RunsRepository{},
		data.RunEventsRepository{},
		nil,
	)
	mwSubAgent := NewSubAgentContextMiddleware(storage)
	h := Build([]RunMiddleware{mwPersona, mwSubAgent}, func(_ context.Context, rc *RunContext) error {
		if _, ok := rc.AllowlistSet["tool_b"]; !ok {
			return fmt.Errorf("tool_b missing: %#v", rc.AllowlistSet)
		}
		if _, ok := rc.AllowlistSet["tool_a"]; ok {
			return fmt.Errorf("tool_a should be removed: %#v", rc.AllowlistSet)
		}
		if _, ok := rc.AllowlistSet["tool_c"]; ok {
			return fmt.Errorf("tool_c should be denied: %#v", rc.AllowlistSet)
		}
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}
}
