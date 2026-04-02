//go:build !desktop

package runengine_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/executor"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/runengine"
	"arkloop/services/worker/internal/testutil"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin"
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

func TestEngineV1InjectsPersonaSystemPromptAndBudgets(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_wg09_persona")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()

	if err := seedRunStartedWithPersona(t, pool, accountID, threadID, runID, "normal@1"); err != nil {
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

	personasRoot, err := personas.BuiltinPersonasRoot()
	if err != nil {
		t.Fatalf("BuiltinPersonasRoot failed: %v", err)
	}
	personaRegistry, err := personas.LoadRegistry(personasRoot)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}

	engine, err := runengine.NewEngineV1(runengine.EngineV1Deps{
		Router:                 router,
		AuxGateway:             gateway,
		EmitDebugEvents:        false,
		ToolRegistry:           toolRegistry,
		ToolExecutors:          builtin.Executors(nil, nil, nil, nil),
		AllLlmToolSpecs:        builtin.LlmSpecs(),
		BaseToolAllowlistNames: []string{"echo"},
		PersonaRegistryGetter:  func() *personas.Registry { return personaRegistry },
		ExecutorRegistry:       executor.DefaultExecutorRegistry(),
	})
	if err != nil {
		t.Fatalf("NewEngineV1 failed: %v", err)
	}

	run := data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}
	if err := engine.Execute(context.Background(), pool, run, runengine.ExecuteInput{TraceID: "trace"}); err != nil {
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
	if len(gateway.request.Messages[0].Content) == 0 || gateway.request.Messages[0].Content[0].Text == "" {
		t.Fatalf("expected non-empty system prompt from normal persona")
	}
	if gateway.request.MaxOutputTokens == nil || *gateway.request.MaxOutputTokens != 20480 {
		t.Fatalf("expected max_output_tokens 20480, got %v", gateway.request.MaxOutputTokens)
	}
}

func TestEngineV1AppliesClawPersonaPromptAndToolAllowlist(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_wg09_claw_persona")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()

	if err := seedRunStartedWithPersona(t, pool, orgID, threadID, runID, "claw@1"); err != nil {
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

	personasRoot, err := personas.BuiltinPersonasRoot()
	if err != nil {
		t.Fatalf("BuiltinPersonasRoot failed: %v", err)
	}
	personaRegistry, err := personas.LoadRegistry(personasRoot)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}

	engine, err := runengine.NewEngineV1(runengine.EngineV1Deps{
		Router:                 router,
		AuxGateway:             gateway,
		EmitDebugEvents:        false,
		ToolRegistry:           toolRegistry,
		ToolExecutors:          builtin.Executors(nil, nil, nil, nil),
		AllLlmToolSpecs:        builtin.LlmSpecs(),
		BaseToolAllowlistNames: toolRegistry.ListNames(),
		PersonaRegistryGetter:  func() *personas.Registry { return personaRegistry },
		ExecutorRegistry:       executor.DefaultExecutorRegistry(),
	})
	if err != nil {
		t.Fatalf("NewEngineV1 failed: %v", err)
	}

	run := data.Run{ID: runID, AccountID: orgID, ThreadID: threadID}
	if err := engine.Execute(context.Background(), pool, run, runengine.ExecuteInput{TraceID: "trace"}); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !gateway.called {
		t.Fatalf("expected gateway called")
	}
	if len(gateway.request.Messages) == 0 || len(gateway.request.Messages[0].Content) == 0 {
		t.Fatalf("expected system prompt injected")
	}
	systemPrompt := gateway.request.Messages[0].Content[0].Text
	if !strings.Contains(systemPrompt, "Claw 模式") {
		t.Fatalf("expected claw system prompt, got %q", systemPrompt)
	}
	if gateway.request.MaxOutputTokens == nil || *gateway.request.MaxOutputTokens != 12288 {
		t.Fatalf("expected max_output_tokens 12288, got %v", gateway.request.MaxOutputTokens)
	}
	if gateway.request.Temperature == nil || *gateway.request.Temperature != 0.4 {
		t.Fatalf("expected temperature 0.4, got %v", gateway.request.Temperature)
	}
	toolNames := map[string]struct{}{}
	for _, spec := range gateway.request.Tools {
		toolNames[spec.Name] = struct{}{}
	}
	for _, required := range []string{"web_search", "exec_command", "browser", "timeline_title"} {
		if _, ok := toolNames[required]; !ok {
			t.Fatalf("expected tool %s in request, got %#v", required, toolNames)
		}
	}
	for _, denied := range []string{"memory_write", "memory_edit", "memory_forget"} {
		if _, ok := toolNames[denied]; ok {
			t.Fatalf("unexpected tool %s in request", denied)
		}
	}
}

func seedRunStartedWithPersona(
	t *testing.T,
	pool *pgxpool.Pool,
	accountID uuid.UUID,
	threadID uuid.UUID,
	runID uuid.UUID,
	personaRef string,
) error {
	t.Helper()

	startedData := map[string]any{
		"route_id":   "default",
		"persona_id": personaRef,
	}
	encoded, err := json.Marshal(startedData)
	if err != nil {
		return err
	}

	_, err = pool.Exec(
		context.Background(),
		`INSERT INTO runs (id, account_id, thread_id, created_by_user_id, status)
		 VALUES ($1, $2, $3, NULL, 'running')`,
		runID,
		accountID,
		threadID,
	)
	if err != nil {
		return err
	}

	_, err = pool.Exec(
		context.Background(),
		`INSERT INTO run_events (run_id, type, data_json)
		 VALUES ($1, 'run.started', $2::jsonb)`,
		runID,
		string(encoded),
	)
	return err
}
