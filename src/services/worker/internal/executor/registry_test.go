package executor

import (
	"context"
	"errors"
	"testing"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/pipeline"
)

// stubExecutor 最小实现，用于测试注册/构建流程。
type stubExecutor struct{ config map[string]any }

func (s *stubExecutor) Execute(_ context.Context, _ *pipeline.RunContext, _ events.Emitter, _ func(events.RunEvent) error) error {
	return nil
}

func makeStubFactory(err error) Factory {
	return func(config map[string]any) (pipeline.AgentExecutor, error) {
		if err != nil {
			return nil, err
		}
		return &stubExecutor{config: config}, nil
	}
}

func TestRegistry_RegisterAndBuild(t *testing.T) {
	reg := NewAgentRegistry()

	if err := reg.Register("test.executor", makeStubFactory(nil)); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	ex, err := reg.Build("test.executor", map[string]any{"k": "v"})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if ex == nil {
		t.Fatal("Build returned nil executor")
	}

	stub, ok := ex.(*stubExecutor)
	if !ok {
		t.Fatal("Build returned unexpected type")
	}
	if stub.config["k"] != "v" {
		t.Fatalf("config not passed through: %v", stub.config)
	}
}

func TestRegistry_BuildWithNilConfig(t *testing.T) {
	reg := NewAgentRegistry()
	if err := reg.Register("test.nilconfig", makeStubFactory(nil)); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// nil config 不应 panic，等价于空 map
	ex, err := reg.Build("test.nilconfig", nil)
	if err != nil {
		t.Fatalf("Build with nil config failed: %v", err)
	}
	if ex == nil {
		t.Fatal("Build returned nil executor")
	}
}

func TestRegistry_DuplicateRegistration(t *testing.T) {
	reg := NewAgentRegistry()
	if err := reg.Register("dup", makeStubFactory(nil)); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}
	err := reg.Register("dup", makeStubFactory(nil))
	if err == nil {
		t.Fatal("expected error on duplicate registration, got nil")
	}
}

func TestRegistry_UnknownType(t *testing.T) {
	reg := NewAgentRegistry()
	_, err := reg.Build("does.not.exist", nil)
	if err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
}

func TestRegistry_EmptyType(t *testing.T) {
	reg := NewAgentRegistry()
	err := reg.Register("", makeStubFactory(nil))
	if err == nil {
		t.Fatal("expected error for empty type, got nil")
	}
}

func TestRegistry_NilFactory(t *testing.T) {
	reg := NewAgentRegistry()
	err := reg.Register("some.type", nil)
	if err == nil {
		t.Fatal("expected error for nil factory, got nil")
	}
}

func TestRegistry_FactoryError(t *testing.T) {
	reg := NewAgentRegistry()
	factoryErr := errors.New("config validation failed")
	if err := reg.Register("bad.config", makeStubFactory(factoryErr)); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	_, err := reg.Build("bad.config", map[string]any{})
	if !errors.Is(err, factoryErr) {
		t.Fatalf("expected factory error, got: %v", err)
	}
}
