//go:build desktop

package app

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin"

	"github.com/google/uuid"
)

func TestDesktopSubAgentSchemaAvailable(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitepgx.Open(filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if desktopSubAgentSchemaAvailable(ctx, db) {
		t.Fatal("expected sub-agent schema to be absent")
	}

	for _, stmt := range []string{
		`CREATE TABLE sub_agents (id TEXT PRIMARY KEY)`,
		`CREATE TABLE sub_agent_events (id TEXT PRIMARY KEY)`,
		`CREATE TABLE sub_agent_pending_inputs (id TEXT PRIMARY KEY)`,
		`CREATE TABLE sub_agent_context_snapshots (id TEXT PRIMARY KEY)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}

	if !desktopSubAgentSchemaAvailable(ctx, db) {
		t.Fatal("expected sub-agent schema to be detected")
	}
}

type desktopNoopSubAgentControl struct{}

func (desktopNoopSubAgentControl) Spawn(context.Context, subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{SubAgentID: uuid.New()}, nil
}
func (desktopNoopSubAgentControl) SendInput(context.Context, subagentctl.SendInputRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (desktopNoopSubAgentControl) Wait(context.Context, subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (desktopNoopSubAgentControl) Resume(context.Context, subagentctl.ResumeRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (desktopNoopSubAgentControl) Close(context.Context, subagentctl.CloseRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (desktopNoopSubAgentControl) Interrupt(context.Context, subagentctl.InterruptRequest) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (desktopNoopSubAgentControl) GetStatus(context.Context, uuid.UUID) (subagentctl.StatusSnapshot, error) {
	return subagentctl.StatusSnapshot{}, nil
}
func (desktopNoopSubAgentControl) ListChildren(context.Context) ([]subagentctl.StatusSnapshot, error) {
	return nil, nil
}

func TestDesktopNormalPersonaSearchableIncludesSpawnAgent(t *testing.T) {
	registry := tools.NewRegistry()
	for _, spec := range builtin.AgentSpecs() {
		if err := registry.Register(spec); err != nil {
			t.Fatalf("register builtin tool: %v", err)
		}
	}

	executors := builtin.Executors(nil, nil, nil)
	allowlist := map[string]struct{}{}
	for _, name := range registry.ListNames() {
		if executors[name] != nil {
			allowlist[name] = struct{}{}
		}
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	personaDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "personas")
	personaRegistry, err := personas.LoadRegistry(personaDir)
	if err != nil {
		t.Fatalf("load personas: %v", err)
	}
	def, ok := personaRegistry.Get("normal")
	if !ok {
		t.Fatal("normal persona not found")
	}

	rc := &pipeline.RunContext{
		Run:               dataRunForDesktopTest(),
		Emitter:           events.NewEmitter("test"),
		ToolRegistry:      registry,
		ToolExecutors:     pipeline.CopyToolExecutors(executors),
		ToolSpecs:         append([]llm.ToolSpec{}, builtin.LlmSpecs()...),
		AllowlistSet:      pipeline.CopyStringSet(allowlist),
		PersonaDefinition: &def,
		SubAgentControl:   desktopNoopSubAgentControl{},
	}

	handler := pipeline.Build([]pipeline.RunMiddleware{
		pipeline.NewSpawnAgentMiddleware(),
		pipeline.NewToolBuildMiddleware(),
	}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := handler(context.Background(), rc); err != nil {
		t.Fatalf("build pipeline: %v", err)
	}

	searchable := rc.ToolExecutor.SearchableSpecs()
	if _, ok := searchable["spawn_agent"]; !ok {
		t.Fatalf("spawn_agent missing from searchable specs: %v", mapKeys(searchable))
	}
}

func dataRunForDesktopTest() data.Run {
	return data.Run{
		ID:        uuid.New(),
		AccountID: uuid.New(),
		ThreadID:  uuid.New(),
	}
}

func mapKeys[K comparable, V any](items map[K]V) []K {
	keys := make([]K, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	return keys
}
