package tools

import (
	"context"
	"strings"
	"sync"
	"testing"

	"arkloop/services/worker/internal/events"
)

type recordingExecutor struct {
	mu         sync.Mutex
	calledWith string
}

type fixedResultExecutor struct {
	mu      sync.Mutex
	context ExecutionContext
	result  ExecutionResult
}

func (e *recordingExecutor) Execute(
	_ context.Context,
	toolName string,
	_ map[string]any,
	_ ExecutionContext,
	_ string,
) ExecutionResult {
	e.mu.Lock()
	e.calledWith = toolName
	e.mu.Unlock()
	return ExecutionResult{ResultJSON: map[string]any{"ok": true}}
}

func (e *recordingExecutor) CalledWith() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calledWith
}

func (e *fixedResultExecutor) Execute(
	_ context.Context,
	_ string,
	_ map[string]any,
	ctx ExecutionContext,
	_ string,
) ExecutionResult {
	e.mu.Lock()
	e.context = ctx
	e.mu.Unlock()
	return e.result
}

func (e *fixedResultExecutor) Context() ExecutionContext {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.context
}

func TestDispatchingExecutorResolvesLlmNameToProvider(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(AgentToolSpec{
		Name:        "web_search.tavily",
		LlmName:     "web_search",
		Version:     "1",
		Description: "x",
		RiskLevel:   RiskLevelLow,
	}); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	allowlist := AllowlistFromNames([]string{"web_search.tavily"})
	policy := NewPolicyEnforcer(registry, allowlist)
	dispatch := NewDispatchingExecutor(registry, policy)

	exec := &recordingExecutor{}
	if err := dispatch.Bind("web_search.tavily", exec); err != nil {
		t.Fatalf("bind failed: %v", err)
	}

	ctx := context.Background()
	emit := events.NewEmitter("trace")
	result := dispatch.Execute(ctx, "web_search", map[string]any{"query": "x"}, ExecutionContext{Emitter: emit}, "call1")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if got := exec.CalledWith(); got != "web_search.tavily" {
		t.Fatalf("expected web_search.tavily, got %q", got)
	}
}

func TestDispatchingExecutorUsesLegacyNameWhenBound(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(AgentToolSpec{
		Name:        "web_search",
		Version:     "1",
		Description: "x",
		RiskLevel:   RiskLevelLow,
	}); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	allowlist := AllowlistFromNames([]string{"web_search"})
	policy := NewPolicyEnforcer(registry, allowlist)
	dispatch := NewDispatchingExecutor(registry, policy)

	exec := &recordingExecutor{}
	if err := dispatch.Bind("web_search", exec); err != nil {
		t.Fatalf("bind failed: %v", err)
	}

	ctx := context.Background()
	emit := events.NewEmitter("trace")
	result := dispatch.Execute(ctx, "web_search", map[string]any{"query": "x"}, ExecutionContext{Emitter: emit}, "call1")
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if got := exec.CalledWith(); got != "web_search" {
		t.Fatalf("expected web_search, got %q", got)
	}
}

func TestDispatchingExecutorBypassesCompressionAndSummarizationForGenerativeUIBootstrapTools(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(AgentToolSpec{
		Name:        "visualize_read_me",
		Version:     "1",
		Description: "x",
		RiskLevel:   RiskLevelLow,
	}); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	allowlist := AllowlistFromNames([]string{"visualize_read_me"})
	policy := NewPolicyEnforcer(registry, allowlist)
	dispatch := NewDispatchingExecutor(registry, policy)
	dispatch.SetSummarizer(NewResultSummarizer(&mockGateway{response: "should not run"}, "test-model", 10))

	longGuidelines := strings.Repeat("guideline line\n", 5000)
	exec := &fixedResultExecutor{
		result: ExecutionResult{
			ResultJSON: map[string]any{
				"guidelines": longGuidelines,
			},
		},
	}
	if err := dispatch.Bind("visualize_read_me", exec); err != nil {
		t.Fatalf("bind failed: %v", err)
	}

	result := dispatch.Execute(
		context.Background(),
		"visualize_read_me",
		map[string]any{"modules": []string{"interactive"}},
		ExecutionContext{Emitter: events.NewEmitter("trace")},
		"call_bootstrap",
	)
	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if result.ResultJSON["guidelines"] != longGuidelines {
		t.Fatal("guidelines should pass through without compression")
	}
	if _, ok := result.ResultJSON["_compressed"]; ok {
		t.Fatal("bootstrap result should not be compressed")
	}
	if _, ok := result.ResultJSON["_summarized"]; ok {
		t.Fatal("bootstrap result should not be summarized")
	}
	if exec.Context().GenerativeUIReadMeSeen {
		t.Fatal("bootstrap tool should not see read_me flag before it runs")
	}
	if !dispatch.generativeUIReadMeSeen {
		t.Fatal("dispatch should remember read_me at run scope after bootstrap tool succeeds")
	}
}
