package pipeline_test

import (
	"context"
	"testing"

	sharedtoolruntime "arkloop/services/shared/toolruntime"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin"
	understandimage "arkloop/services/worker/internal/tools/builtin/understand_image"

	"github.com/google/uuid"
)

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

func TestToolBuildMiddleware_ResolveProviderAllowlistError(t *testing.T) {
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

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		t.Fatal("should not reach terminal")
		return nil
	})

	err := h(context.Background(), rc)
	if err == nil {
		t.Fatal("expected error from ambiguous providers")
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
	if err := registry.Register(understandimage.AgentSpec); err != nil {
		t.Fatalf("register understand_image: %v", err)
	}
	if err := registry.Register(understandimage.AgentSpecMiniMax); err != nil {
		t.Fatalf("register understand_image minimax: %v", err)
	}

	executors := map[string]tools.Executor{
		understandimage.AgentSpec.Name:        understandimage.NewToolExecutorWithProvider(&stubImageProvider{}),
		understandimage.AgentSpecMiniMax.Name: understandimage.NewToolExecutorWithProvider(&stubImageProvider{}),
	}

	rc := &pipeline.RunContext{
		Run: data.Run{
			ID: uuid.New(),
		},
		Emitter:                   events.NewEmitter("test"),
		ToolRegistry:              registry,
		ToolExecutors:             executors,
		AllowlistSet:              map[string]struct{}{"understand_image": {}},
		ActiveToolProviderByGroup: map[string]string{"image_understanding": "image_understanding.minimax"},
		ToolSpecs: []llm.ToolSpec{
			understandimage.LlmSpec,
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
		if rc.FinalSpecs[0].Name != "understand_image" {
			t.Fatalf("expected understand_image spec, got %s", rc.FinalSpecs[0].Name)
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

func TestToolBuildMiddleware_UnderstandImageSearchableWhenNotCore(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(understandimage.AgentSpec); err != nil {
		t.Fatalf("register understand_image: %v", err)
	}

	executors := map[string]tools.Executor{
		understandimage.AgentSpec.Name: understandimage.NewToolExecutorWithProvider(&stubImageProvider{}),
	}

	rc := &pipeline.RunContext{
		Run:                      data.Run{ID: uuid.New()},
		Emitter:                  events.NewEmitter("test"),
		ToolRegistry:             registry,
		ToolExecutors:            executors,
		AllowlistSet:             map[string]struct{}{"understand_image": {}},
		ToolSpecs:                []llm.ToolSpec{understandimage.LlmSpec},
		PersonaDefinition:        &personas.Definition{CoreTools: []string{"timeline_title"}},
		ActiveToolProviderByGroup: nil,
	}

	mw := pipeline.NewToolBuildMiddleware()
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if hasToolSpecName(rc.FinalSpecs, "understand_image") {
			t.Fatal("did not expect understand_image in final specs")
		}
		if !hasToolSpecName(rc.FinalSpecs, "search_tools") {
			t.Fatal("expected search_tools in final specs")
		}

		searchable := rc.ToolExecutor.SearchableSpecs()
		if _, ok := searchable["understand_image"]; !ok {
			t.Fatal("expected understand_image to be searchable")
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolBuildMiddleware_UnderstandImageUnavailableWhenNotConfigured(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(understandimage.AgentSpec); err != nil {
		t.Fatalf("register understand_image: %v", err)
	}

	executors := map[string]tools.Executor{
		understandimage.AgentSpec.Name: understandimage.NewToolExecutor(),
	}

	rc := &pipeline.RunContext{
		Run:                      data.Run{ID: uuid.New()},
		Emitter:                  events.NewEmitter("test"),
		ToolRegistry:             registry,
		ToolExecutors:            executors,
		AllowlistSet:             map[string]struct{}{"understand_image": {}},
		ToolSpecs:                []llm.ToolSpec{understandimage.LlmSpec},
		PersonaDefinition:        &personas.Definition{CoreTools: []string{"timeline_title"}},
		ActiveToolProviderByGroup: nil,
	}

	mw := pipeline.NewToolBuildMiddleware()
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if hasToolSpecName(rc.FinalSpecs, "understand_image") {
			t.Fatal("did not expect understand_image in final specs")
		}
		if !hasToolSpecName(rc.FinalSpecs, "search_tools") {
			t.Fatal("expected search_tools in final specs")
		}

		searchable := rc.ToolExecutor.SearchableSpecs()
		if _, ok := searchable["understand_image"]; ok {
			t.Fatal("did not expect understand_image to be searchable")
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

func (stubImageProvider) DescribeImage(_ context.Context, req understandimage.DescribeImageRequest) (understandimage.DescribeImageResponse, error) {
	return understandimage.DescribeImageResponse{
		Text:     "stub",
		Provider: "stub",
		Model:    "stub",
	}, nil
}

func (stubImageProvider) Name() string {
	return "stub"
}
