package tools

import (
	"context"
	"sync"
	"testing"

	"arkloop/services/worker/internal/events"
)

type recordingExecutor struct {
	mu         sync.Mutex
	calledWith string
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
