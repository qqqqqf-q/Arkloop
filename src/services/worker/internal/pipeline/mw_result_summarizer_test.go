package pipeline_test

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestResultSummarizerMiddleware_PassThroughWithoutConfig(t *testing.T) {
	mw := pipeline.NewResultSummarizerMiddleware(nil, nil, false, 0, nil)
	rc := &pipeline.RunContext{}

	var reached bool
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		reached = true
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reached {
		t.Fatal("terminal handler was not called")
	}
}

func TestResultSummarizerMiddleware_InjectsSummarizer(t *testing.T) {
	stubCfg, _ := llm.AuxGatewayConfigFromEnv()
	stubCfg.Enabled = true
	stubCfg.DeltaCount = 1
	stubCfg.DeltaInterval = 0
	auxGateway := llm.NewAuxGateway(stubCfg)

	registry := tools.NewRegistry()
	if err := registry.Register(tools.AgentToolSpec{Name: "echo", Version: "1", Description: "echo tool", RiskLevel: tools.RiskLevelLow}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	dispatch := tools.NewDispatchingExecutor(registry, tools.NewPolicyEnforcer(registry, tools.AllowlistFromNames([]string{"echo"})))
	if err := dispatch.Bind("echo", resultSummarizerTestExecutor{}); err != nil {
		t.Fatalf("bind tool failed: %v", err)
	}
	db := testutil.SetupPostgresDatabase(t, "arkloop_result_summarizer_mw")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	mw := pipeline.NewResultSummarizerMiddleware(pool, auxGateway, false, 0, nil)
	rc := &pipeline.RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			ThreadID:  uuid.New(),
			AccountID: uuid.New(),
		},
		Gateway: auxGateway,
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{Model: "stub-model"},
		},
		ToolExecutor: dispatch,
		ResultSummarizer: &personas.ResultSummarizerConfig{
			Prompt:         "compress",
			MaxTokens:      123,
			ThresholdBytes: 456,
		},
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		res := rc.ToolExecutor.Execute(context.Background(), "echo", nil, tools.ExecutionContext{
			Emitter: events.NewEmitter("test"),
		}, "call-1")
		if res.ResultJSON["_summarized"] != true {
			t.Fatalf("expected summarized result, got %#v", res.ResultJSON)
		}
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

type resultSummarizerTestExecutor struct{}

func (resultSummarizerTestExecutor) Execute(_ context.Context, _ string, _ map[string]any, _ tools.ExecutionContext, _ string) tools.ExecutionResult {
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"output": strings.Repeat("x", 70000)},
	}
}
