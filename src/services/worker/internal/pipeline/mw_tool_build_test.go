package pipeline_test

import (
	"context"
	"sync"
	"testing"

	sharedtoolruntime "arkloop/services/shared/toolruntime"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin"
	readtool "arkloop/services/worker/internal/tools/builtin/read"

	"github.com/google/uuid"
)

type recordingExecutor struct {
	mu         sync.Mutex
	calledWith string
}

func (e *recordingExecutor) Execute(
	_ context.Context,
	toolName string,
	_ map[string]any,
	_ tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	e.mu.Lock()
	e.calledWith = toolName
	e.mu.Unlock()
	return tools.ExecutionResult{ResultJSON: map[string]any{"ok": true}}
}

func (e *recordingExecutor) CalledWith() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calledWith
}

func TestToolBuildMiddleware_BuildsExecutorAndFiltersSpecs(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.EchoAgentSpec); err != nil {
		t.Fatalf("register echo: %v", err)
	}
	if err := registry.Register(builtin.NoopAgentSpec); err != nil {
		t.Fatalf("register noop: %v", err)
	}

	executors := map[string]tools.Executor{
		"echo": builtin.EchoExecutor{},
		"noop": builtin.NoopExecutor{},
	}

	rc := &pipeline.RunContext{
		Run: data.Run{
			ID: uuid.New(),
		},
		Emitter:                   events.NewEmitter("test"),
		ToolRegistry:              registry,
		ToolExecutors:             executors,
		AllowlistSet:              map[string]struct{}{"echo": {}, "noop": {}},
		ActiveToolProviderByGroup: nil,
		ToolSpecs: []llm.ToolSpec{
			builtin.EchoLlmSpec,
			builtin.NoopLlmSpec,
		},
	}

	mw := pipeline.NewToolBuildMiddleware()

	var reached bool
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		reached = true
		if rc.ToolExecutor == nil {
			t.Fatal("ToolExecutor not set")
		}
		if len(rc.FinalSpecs) != 2 {
			t.Fatalf("expected 2 final specs, got %d", len(rc.FinalSpecs))
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reached {
		t.Fatal("terminal handler not reached")
	}
}

func TestToolBuildMiddleware_DropsUnboundExecutors(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.EchoAgentSpec); err != nil {
		t.Fatalf("register echo: %v", err)
	}
	if err := registry.Register(builtin.NoopAgentSpec); err != nil {
		t.Fatalf("register noop: %v", err)
	}

	executors := map[string]tools.Executor{
		"echo": builtin.EchoExecutor{},
		// noop not bound
	}

	rc := &pipeline.RunContext{
		Run: data.Run{
			ID: uuid.New(),
		},
		Emitter:                   events.NewEmitter("test"),
		ToolRegistry:              registry,
		ToolExecutors:             executors,
		AllowlistSet:              map[string]struct{}{"echo": {}, "noop": {}},
		ActiveToolProviderByGroup: nil,
		ToolSpecs: []llm.ToolSpec{
			builtin.EchoLlmSpec,
			builtin.NoopLlmSpec,
		},
	}

	mw := pipeline.NewToolBuildMiddleware()

	var reached bool
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		reached = true
		if rc.ToolExecutor == nil {
			t.Fatal("ToolExecutor not set")
		}
		if len(rc.FinalSpecs) != 1 {
			t.Fatalf("expected 1 final spec, got %d", len(rc.FinalSpecs))
		}
		if rc.FinalSpecs[0].Name != "echo" {
			t.Fatalf("expected echo spec, got %s", rc.FinalSpecs[0].Name)
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reached {
		t.Fatal("terminal handler not reached")
	}
}

func TestToolBuildMiddleware_EmptyAllowlist(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.EchoAgentSpec); err != nil {
		t.Fatalf("register echo: %v", err)
	}

	executors := map[string]tools.Executor{
		"echo": builtin.EchoExecutor{},
	}

	rc := &pipeline.RunContext{
		Run: data.Run{
			ID: uuid.New(),
		},
		Emitter:                   events.NewEmitter("test"),
		ToolRegistry:              registry,
		ToolExecutors:             executors,
		AllowlistSet:              map[string]struct{}{}, // empty
		ActiveToolProviderByGroup: nil,
		ToolSpecs: []llm.ToolSpec{
			builtin.EchoLlmSpec,
		},
	}

	mw := pipeline.NewToolBuildMiddleware()

	var reached bool
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		reached = true
		if rc.ToolExecutor == nil {
			t.Fatal("ToolExecutor not set")
		}
		if len(rc.FinalSpecs) != 0 {
			t.Fatalf("expected 0 final specs, got %d", len(rc.FinalSpecs))
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reached {
		t.Fatal("terminal handler not reached")
	}
}

func TestToolBuildMiddleware_SkipsProviderManagedGroupWithoutActiveProvider(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(tools.AgentToolSpec{
		Name:        "web_search.tavily",
		LlmName:     "web_search",
		Version:     "1",
		Description: "search",
		RiskLevel:   tools.RiskLevelLow,
	}); err != nil {
		t.Fatalf("register tavily: %v", err)
	}
	if err := registry.Register(tools.AgentToolSpec{
		Name:        "web_search.searxng",
		LlmName:     "web_search",
		Version:     "1",
		Description: "search",
		RiskLevel:   tools.RiskLevelLow,
	}); err != nil {
		t.Fatalf("register searxng: %v", err)
	}

	executors := map[string]tools.Executor{}

	rc := &pipeline.RunContext{
		Run: data.Run{
			ID: uuid.New(),
		},
		Emitter:                   events.NewEmitter("test"),
		ToolRegistry:              registry,
		ToolExecutors:             executors,
		AllowlistSet:              map[string]struct{}{"web_search.tavily": {}, "web_search.searxng": {}},
		ActiveToolProviderByGroup: nil,
		ToolSpecs:                 []llm.ToolSpec{},
	}

	mw := pipeline.NewToolBuildMiddleware()

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if len(rc.FinalSpecs) != 0 {
			t.Fatalf("expected no final specs, got %d", len(rc.FinalSpecs))
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolBuildMiddleware_FiltersUnavailableRuntimeManagedTools(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(tools.AgentToolSpec{Name: "browser", Version: "1", Description: "browser", RiskLevel: tools.RiskLevelHigh}); err != nil {
		t.Fatalf("register browser: %v", err)
	}
	executors := map[string]tools.Executor{"browser": builtin.NoopExecutor{}}
	runtimeSnapshot := sharedtoolruntime.RuntimeSnapshot{}
	rc := &pipeline.RunContext{
		Run:           data.Run{ID: uuid.New()},
		Emitter:       events.NewEmitter("test"),
		ToolRegistry:  registry,
		ToolExecutors: executors,
		AllowlistSet:  map[string]struct{}{"browser": {}},
		ToolSpecs:     []llm.ToolSpec{{Name: "browser"}},
		Runtime:       &runtimeSnapshot,
	}
	mw := pipeline.NewToolBuildMiddleware()
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if len(rc.FinalSpecs) != 0 {
			t.Fatalf("expected browser spec to be filtered, got %d", len(rc.FinalSpecs))
		}
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolBuildMiddleware_KeepsUserProviderTool(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(readtool.AgentSpec); err != nil {
		t.Fatalf("register read: %v", err)
	}
	if err := registry.Register(readtool.AgentSpecMiniMax); err != nil {
		t.Fatalf("register read minimax: %v", err)
	}

	executors := map[string]tools.Executor{
		readtool.AgentSpec.Name:        readtool.NewToolExecutorWithProvider(&stubImageProvider{}),
		readtool.AgentSpecMiniMax.Name: readtool.NewToolExecutorWithProvider(&stubImageProvider{}),
	}

	rc := &pipeline.RunContext{
		Run: data.Run{
			ID: uuid.New(),
		},
		Emitter:                   events.NewEmitter("test"),
		ToolRegistry:              registry,
		ToolExecutors:             executors,
		AllowlistSet:              map[string]struct{}{"read": {}},
		ActiveToolProviderByGroup: map[string]string{"read": "read.minimax"},
		ToolSpecs: []llm.ToolSpec{
			readtool.LlmSpec,
		},
		Runtime: &sharedtoolruntime.RuntimeSnapshot{},
	}

	resolved, err := pipeline.ResolveProviderAllowlist(rc.AllowlistSet, rc.ToolRegistry, rc.ActiveToolProviderByGroup)
	if err != nil {
		t.Fatalf("resolve provider allowlist: %v", err)
	}
	filtered := pipeline.FilterAllowlistByRuntime(resolved, rc.Runtime, rc.ToolRegistry, rc.ActiveToolProviderByGroup)
	if len(filtered) == 0 {
		t.Fatalf("allowlist filtered to empty: %v", filtered)
	}

	mw := pipeline.NewToolBuildMiddleware()
	var reached bool
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		reached = true
		if rc.ToolExecutor == nil {
			t.Fatal("ToolExecutor not set")
		}
		if len(rc.FinalSpecs) != 1 {
			t.Fatalf("expected 1 final spec, got %d", len(rc.FinalSpecs))
		}
		if rc.FinalSpecs[0].Name != "read" {
			t.Fatalf("expected read spec, got %s", rc.FinalSpecs[0].Name)
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reached {
		t.Fatal("terminal handler not reached")
	}
}

func TestToolBuildMiddleware_BindsDuckduckgoProvider(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(tools.AgentToolSpec{
		Name:        "web_search.duckduckgo",
		LlmName:     "web_search",
		Version:     "1",
		Description: "search",
		RiskLevel:   tools.RiskLevelLow,
	}); err != nil {
		t.Fatalf("register duckduckgo: %v", err)
	}

	exec := &recordingExecutor{}
	rc := &pipeline.RunContext{
		Run:          data.Run{ID: uuid.New()},
		Emitter:      events.NewEmitter("test"),
		ToolRegistry: registry,
		ToolExecutors: map[string]tools.Executor{
			"web_search.duckduckgo": exec,
		},
		AllowlistSet: map[string]struct{}{
			"web_search": {},
		},
		ActiveToolProviderByGroup: map[string]string{
			"web_search": "web_search.duckduckgo",
		},
		ToolSpecs: []llm.ToolSpec{
			{Name: "web_search"},
		},
	}

	mw := pipeline.NewToolBuildMiddleware()
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if rc.ToolExecutor == nil {
			t.Fatal("ToolExecutor not set")
		}
		if len(rc.FinalSpecs) != 1 || rc.FinalSpecs[0].Name != "web_search" {
			t.Fatalf("unexpected final specs: %+v", rc.FinalSpecs)
		}
		result := rc.ToolExecutor.Execute(
			context.Background(),
			"web_search",
			map[string]any{"query": "x"},
			tools.ExecutionContext{Emitter: events.NewEmitter("trace")},
			"call1",
		)
		if result.Error != nil {
			t.Fatalf("unexpected error: %+v", result.Error)
		}
		if got := exec.CalledWith(); got != "web_search.duckduckgo" {
			t.Fatalf("expected web_search.duckduckgo, got %q", got)
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolBuildMiddleware_ReadSearchableWhenNotCore(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(readtool.AgentSpec); err != nil {
		t.Fatalf("register read: %v", err)
	}

	executors := map[string]tools.Executor{
		readtool.AgentSpec.Name: readtool.NewToolExecutorWithProvider(&stubImageProvider{}),
	}

	rc := &pipeline.RunContext{
		Run:                       data.Run{ID: uuid.New()},
		Emitter:                   events.NewEmitter("test"),
		ToolRegistry:              registry,
		ToolExecutors:             executors,
		AllowlistSet:              map[string]struct{}{"read": {}},
		ToolSpecs:                 []llm.ToolSpec{readtool.LlmSpec},
		PersonaDefinition:         &personas.Definition{CoreTools: []string{"timeline_title"}},
		ActiveToolProviderByGroup: nil,
	}

	mw := pipeline.NewToolBuildMiddleware()
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if hasToolSpecName(rc.FinalSpecs, "read") {
			t.Fatal("did not expect read in final specs")
		}
		if !hasToolSpecName(rc.FinalSpecs, "search_tools") {
			t.Fatal("expected search_tools in final specs")
		}

		searchable := rc.ToolExecutor.SearchableSpecs()
		if _, ok := searchable["read"]; !ok {
			t.Fatal("expected read to be searchable")
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolBuildMiddleware_ReadSearchableWithoutProviderConfig(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(readtool.AgentSpec); err != nil {
		t.Fatalf("register read: %v", err)
	}

	executors := map[string]tools.Executor{
		readtool.AgentSpec.Name: readtool.NewToolExecutor(),
	}

	rc := &pipeline.RunContext{
		Run:                       data.Run{ID: uuid.New()},
		Emitter:                   events.NewEmitter("test"),
		ToolRegistry:              registry,
		ToolExecutors:             executors,
		AllowlistSet:              map[string]struct{}{"read": {}},
		ToolSpecs:                 []llm.ToolSpec{readtool.LlmSpec},
		PersonaDefinition:         &personas.Definition{CoreTools: []string{"timeline_title"}},
		ActiveToolProviderByGroup: nil,
	}

	mw := pipeline.NewToolBuildMiddleware()
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if hasToolSpecName(rc.FinalSpecs, "read") {
			t.Fatal("did not expect read in final specs")
		}
		if !hasToolSpecName(rc.FinalSpecs, "search_tools") {
			t.Fatal("expected search_tools in final specs")
		}

		searchable := rc.ToolExecutor.SearchableSpecs()
		if _, ok := searchable["read"]; !ok {
			t.Fatal("expected read to remain searchable for file reads")
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func hasToolSpecName(specs []llm.ToolSpec, name string) bool {
	for _, spec := range specs {
		if spec.Name == name {
			return true
		}
	}
	return false
}

type stubImageProvider struct{}

func (stubImageProvider) DescribeImage(_ context.Context, req readtool.DescribeImageRequest) (readtool.DescribeImageResponse, error) {
	return readtool.DescribeImageResponse{
		Text:     "stub",
		Provider: "stub",
		Model:    "stub",
	}, nil
}

func (stubImageProvider) Name() string {
	return "stub"
}
