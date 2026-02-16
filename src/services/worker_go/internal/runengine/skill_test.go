package runengine

import (
	"context"
	"encoding/json"
	"testing"

	"arkloop/services/worker_go/internal/data"
	"arkloop/services/worker_go/internal/llm"
	"arkloop/services/worker_go/internal/routing"
	"arkloop/services/worker_go/internal/skills"
	"arkloop/services/worker_go/internal/testutil"
	"arkloop/services/worker_go/internal/tools"
	"arkloop/services/worker_go/internal/tools/builtin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type recordingGateway struct {
	called  bool
	request llm.Request
}

func (g *recordingGateway) Stream(ctx context.Context, request llm.Request, yield func(llm.StreamEvent) error) error {
	g.called = true
	g.request = request
	if err := yield(llm.StreamMessageDelta{ContentDelta: "ok", Role: "assistant"}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{})
}

func TestEngineV1InjectsSkillSystemPromptAndBudgets(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_wg09_skill")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()

	if err := seedRunStartedWithSkill(t, pool, orgID, threadID, runID, "demo_no_tools@1"); err != nil {
		t.Fatalf("seed run failed: %v", err)
	}

	router := routing.NewProviderRouter(routing.DefaultRoutingConfig())
	gateway := &recordingGateway{}

	toolRegistry := tools.NewRegistry()
	for _, spec := range builtin.AgentSpecs() {
		if err := toolRegistry.Register(spec); err != nil {
			t.Fatalf("register tool spec failed: %v", err)
		}
	}

	skillsRoot, err := skills.BuiltinSkillsRoot()
	if err != nil {
		t.Fatalf("BuiltinSkillsRoot failed: %v", err)
	}
	skillRegistry, err := skills.LoadRegistry(skillsRoot)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}

	engine, err := NewEngineV1(EngineV1Deps{
		Router:                 router,
		StubGateway:            gateway,
		EmitDebugEvents:        false,
		ToolRegistry:           toolRegistry,
		ToolExecutors:          builtin.Executors(),
		AllLlmToolSpecs:        builtin.LlmSpecs(),
		BaseToolAllowlistNames: []string{"echo"},
		SkillRegistry:          skillRegistry,
	})
	if err != nil {
		t.Fatalf("NewEngineV1 failed: %v", err)
	}

	run := data.Run{ID: runID, OrgID: orgID, ThreadID: threadID}
	if err := engine.Execute(context.Background(), pool, run, ExecuteInput{TraceID: "trace"}); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !gateway.called {
		t.Fatalf("expected gateway called")
	}
	if len(gateway.request.Messages) == 0 {
		t.Fatalf("expected request messages")
	}
	if gateway.request.Messages[0].Role != "system" {
		t.Fatalf("expected system prompt injected, got role=%s", gateway.request.Messages[0].Role)
	}
	if len(gateway.request.Messages[0].Content) == 0 || gateway.request.Messages[0].Content[0].Text != "NO_TOOLS_PROMPT_SENTINEL: DO NOT CALL ANY TOOLS." {
		t.Fatalf("unexpected system prompt: %#v", gateway.request.Messages[0].Content)
	}
	if gateway.request.MaxOutputTokens == nil || *gateway.request.MaxOutputTokens != 64 {
		t.Fatalf("unexpected max_output_tokens: %#v", gateway.request.MaxOutputTokens)
	}
	if len(gateway.request.Tools) != 0 {
		t.Fatalf("expected no tools exposed for demo_no_tools skill, got %d", len(gateway.request.Tools))
	}
}

func seedRunStartedWithSkill(
	t *testing.T,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	threadID uuid.UUID,
	runID uuid.UUID,
	skillRef string,
) error {
	t.Helper()

	startedData := map[string]any{
		"route_id": "default",
		"skill_id": skillRef,
	}
	encoded, err := json.Marshal(startedData)
	if err != nil {
		return err
	}

	_, err = pool.Exec(
		context.Background(),
		`INSERT INTO runs (id, org_id, thread_id, created_by_user_id, status, next_event_seq)
		 VALUES ($1, $2, $3, NULL, 'running', 2)`,
		runID,
		orgID,
		threadID,
	)
	if err != nil {
		return err
	}

	_, err = pool.Exec(
		context.Background(),
		`INSERT INTO run_events (run_id, seq, type, data_json)
		 VALUES ($1, 1, 'run.started', $2::jsonb)`,
		runID,
		string(encoded),
	)
	return err
}

