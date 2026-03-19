//go:build desktop

package app

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/skillstore"
	"arkloop/services/shared/workspaceblob"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/executor"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

func TestLoadPersonaRegistryFromFSUsesEnvRoot(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	personaDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "personas")
	t.Setenv("ARKLOOP_PERSONAS_ROOT", personaDir)
	t.Chdir(t.TempDir())

	getter := loadPersonaRegistryFromFS()
	if getter == nil {
		t.Fatal("expected persona registry getter")
	}
	registry := getter()
	if registry == nil {
		t.Fatal("expected persona registry")
	}
	if _, ok := registry.Get("normal"); !ok {
		t.Fatal("expected normal persona loaded from env root")
	}
}

func TestComposeDesktopEngineRegistersArtifactTools(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	t.Setenv("ARKLOOP_DATA_DIR", dataDir)

	db, err := sqlitepgx.Open(filepath.Join(dataDir, "desktop.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	engine, err := ComposeDesktopEngine(ctx, db, eventbus.NewLocalEventBus(), executor.DefaultExecutorRegistry(), nil)
	if err != nil {
		t.Fatalf("compose desktop engine: %v", err)
	}

	for _, toolName := range []string{"visualize_read_me", "artifact_guidelines", "show_widget", "create_artifact", "document_write"} {
		if _, ok := engine.toolRegistry.Get(toolName); !ok {
			t.Fatalf("expected tool %s to be registered", toolName)
		}
		if _, ok := engine.baseAllowlist[toolName]; !ok {
			t.Fatalf("expected tool %s in desktop allowlist", toolName)
		}
	}

	specNames := map[string]struct{}{}
	for _, spec := range engine.allLlmSpecs {
		specNames[spec.Name] = struct{}{}
	}
	for _, toolName := range []string{"visualize_read_me", "artifact_guidelines", "show_widget", "create_artifact", "document_write"} {
		if _, ok := specNames[toolName]; !ok {
			t.Fatalf("expected tool spec %s in desktop llm specs", toolName)
		}
	}
}

func TestLoadPersonaRegistryFromFSPrefersBuiltinRootEnv(t *testing.T) {
	personasRoot := t.TempDir()
	personaDir := filepath.Join(personasRoot, "env-persona")
	if err := os.MkdirAll(personaDir, 0o755); err != nil {
		t.Fatalf("mkdir persona dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(personaDir, "persona.yaml"), []byte("id: env-persona\nversion: \"1\"\ntitle: Env Persona\n"), 0o644); err != nil {
		t.Fatalf("write persona yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(personaDir, "prompt.md"), []byte("# env prompt"), 0o644); err != nil {
		t.Fatalf("write persona prompt: %v", err)
	}
	t.Setenv("ARKLOOP_PERSONAS_ROOT", personasRoot)

	getter := loadPersonaRegistryFromFS()
	if getter == nil {
		t.Fatal("expected persona registry getter")
	}
	registry := getter()
	if registry == nil {
		t.Fatal("expected persona registry")
	}
	if _, ok := registry.Get("env-persona"); !ok {
		t.Fatalf("expected env persona loaded, got ids=%v", registry.ListIDs())
	}
}

func TestDesktopSkillLayoutUsesRunScopedPaths(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ARKLOOP_DATA_DIR", dataDir)

	runID := uuid.New()
	layout, err := desktopSkillLayout(false, runID)
	if err != nil {
		t.Fatalf("desktop skill layout: %v", err)
	}

	if layout.MountRoot != filepath.Join(dataDir, "skills") {
		t.Fatalf("unexpected mount root: %s", layout.MountRoot)
	}
	runtimeRoot := filepath.Join(dataDir, "runtime", "skills", runID.String())
	if layout.IndexPath != filepath.Join(runtimeRoot, "enabled-skills.json") {
		t.Fatalf("unexpected index path: %s", layout.IndexPath)
	}
}

func TestCleanupDesktopSkillRuntimeRemovesRunScopedDirectory(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ARKLOOP_DATA_DIR", dataDir)

	runID := uuid.New()
	layout, err := desktopSkillLayout(false, runID)
	if err != nil {
		t.Fatalf("desktop skill layout: %v", err)
	}
	runtimeRoot := filepath.Dir(layout.IndexPath)
	if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
		t.Fatalf("mkdir runtime root: %v", err)
	}
	if err := os.WriteFile(layout.IndexPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	// 持久化 skill store 不应被 cleanup 删除
	if err := os.MkdirAll(layout.MountRoot, 0o755); err != nil {
		t.Fatalf("mkdir skill store: %v", err)
	}

	if err := cleanupDesktopSkillRuntime(runID); err != nil {
		t.Fatalf("cleanup skill runtime: %v", err)
	}

	if _, err := os.Stat(runtimeRoot); !os.IsNotExist(err) {
		t.Fatalf("expected run-scoped runtime root removed, got err=%v", err)
	}
	if _, err := os.Stat(layout.MountRoot); err != nil {
		t.Fatalf("expected persistent skill store preserved, got err=%v", err)
	}
}

func TestPrepareDesktopHostSkillsMaterializesBundlesAndIndex(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	t.Setenv("ARKLOOP_DATA_DIR", dataDir)

	store, err := openDesktopSkillStore(ctx)
	if err != nil {
		t.Fatalf("open desktop skill store: %v", err)
	}
	bundleKey := skillstore.DerivedBundleKey("grep-helper", "1")
	if err := store.Put(ctx, bundleKey, buildDesktopSkillBundle(t, map[string]string{
		"skill.yaml":     "skill_key: grep-helper\nversion: \"1\"\ndisplay_name: Grep Helper\ninstruction_path: SKILL.md\n",
		"SKILL.md":       "Use grep carefully.\n",
		"scripts/run.sh": "#!/bin/sh\necho ok\n",
	})); err != nil {
		t.Fatalf("seed desktop skill bundle: %v", err)
	}

	runID := uuid.New()
	layout, err := desktopSkillLayout(false, runID)
	if err != nil {
		t.Fatalf("desktop skill layout: %v", err)
	}

	skills := []skillstore.ResolvedSkill{{
		SkillKey:        "grep-helper",
		Version:         "1",
		BundleRef:       bundleKey,
		MountPath:       layout.MountPath("grep-helper", "1"),
		InstructionPath: "SKILL.md",
		AutoInject:      true,
	}}
	if err := prepareDesktopHostSkills(ctx, skills, layout); err != nil {
		t.Fatalf("prepare desktop host skills: %v", err)
	}

	skillDocPath := filepath.Join(layout.MountPath("grep-helper", "1"), "SKILL.md")
	rawDoc, err := os.ReadFile(skillDocPath)
	if err != nil {
		t.Fatalf("read skill doc: %v", err)
	}
	if string(rawDoc) != "Use grep carefully.\n" {
		t.Fatalf("unexpected skill doc: %q", string(rawDoc))
	}

	rawIndex, err := os.ReadFile(layout.IndexPath)
	if err != nil {
		t.Fatalf("read skill index: %v", err)
	}
	var entries []skillstore.IndexEntry
	if err := json.Unmarshal(rawIndex, &entries); err != nil {
		t.Fatalf("decode skill index: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 skill index entry, got %d", len(entries))
	}
	if entries[0].MountPath != layout.MountPath("grep-helper", "1") {
		t.Fatalf("unexpected skill index entry: %#v", entries[0])
	}
}

func TestResolveDesktopRunBindingsPersistsAndExposesInheritedSkills(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	userID := uuid.New()
	threadID1 := uuid.New()
	threadID2 := uuid.New()
	runID1 := uuid.New()
	runID2 := uuid.New()

	seedDesktopRunBindingAccount(t, db, accountID, userID)
	seedDesktopRunBindingThread(t, db, accountID, threadID1, nil, &userID)
	seedDesktopRunBindingThread(t, db, accountID, threadID2, nil, &userID)
	seedDesktopRunBindingRun(t, db, accountID, threadID1, &userID, runID1)
	seedDesktopRunBindingRun(t, db, accountID, threadID2, &userID, runID2)

	first, err := resolveDesktopRunBindings(ctx, db, data.Run{
		ID:              runID1,
		AccountID:       accountID,
		ThreadID:        threadID1,
		CreatedByUserID: &userID,
	})
	if err != nil {
		t.Fatalf("resolve first desktop run bindings: %v", err)
	}
	seedDesktopOwnedSkillPackage(t, db, accountID, "grep-helper", "1")
	if _, err := db.Exec(
		ctx,
		`INSERT INTO profile_skill_installs (profile_ref, account_id, owner_user_id, skill_key, version)
		 VALUES ($1, $2, $3, 'grep-helper', '1')`,
		derefStr(first.ProfileRef),
		accountID,
		userID,
	); err != nil {
		t.Fatalf("seed profile install: %v", err)
	}
	if _, err := db.Exec(
		ctx,
		`INSERT INTO workspace_skill_enablements (workspace_ref, account_id, enabled_by_user_id, skill_key, version)
		 VALUES ($1, $2, $3, 'grep-helper', '1')`,
		derefStr(first.WorkspaceRef),
		accountID,
		userID,
	); err != nil {
		t.Fatalf("seed workspace enablement: %v", err)
	}

	second, err := resolveDesktopRunBindings(ctx, db, data.Run{
		ID:              runID2,
		AccountID:       accountID,
		ThreadID:        threadID2,
		CreatedByUserID: &userID,
	})
	if err != nil {
		t.Fatalf("resolve second desktop run bindings: %v", err)
	}
	if derefStr(first.WorkspaceRef) == derefStr(second.WorkspaceRef) {
		t.Fatalf("expected new thread to bind a different workspace, got %q", derefStr(second.WorkspaceRef))
	}

	var storedProfileRef string
	var storedWorkspaceRef string
	if err := db.QueryRow(
		ctx,
		`SELECT profile_ref, workspace_ref FROM runs WHERE id = $1`,
		runID2,
	).Scan(&storedProfileRef, &storedWorkspaceRef); err != nil {
		t.Fatalf("load persisted run bindings: %v", err)
	}
	if storedProfileRef != derefStr(second.ProfileRef) || storedWorkspaceRef != derefStr(second.WorkspaceRef) {
		t.Fatalf("unexpected persisted bindings: %q %q", storedProfileRef, storedWorkspaceRef)
	}

	resolver := desktopSkillResolver(db)
	items, err := resolver(ctx, accountID, storedProfileRef, storedWorkspaceRef)
	if err != nil {
		t.Fatalf("resolve desktop skills: %v", err)
	}
	if len(items) != 1 || items[0].SkillKey != "grep-helper" || items[0].Version != "1" {
		t.Fatalf("unexpected resolved skills: %#v", items)
	}
}

func TestResolveDesktopRunBindingsIgnoresWorkDirForWorkspaceAndSkills(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	userID := uuid.New()
	threadID := uuid.New()
	runID1 := uuid.New()
	runID2 := uuid.New()

	seedDesktopRunBindingAccount(t, db, accountID, userID)
	seedDesktopRunBindingThread(t, db, accountID, threadID, nil, &userID)
	seedDesktopRunBindingRun(t, db, accountID, threadID, &userID, runID1)
	seedDesktopRunBindingRun(t, db, accountID, threadID, &userID, runID2)
	if _, err := db.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{"work_dir":"/tmp/work-a"}')`, runID1); err != nil {
		t.Fatalf("seed first run.started: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{"work_dir":"/tmp/work-b"}')`, runID2); err != nil {
		t.Fatalf("seed second run.started: %v", err)
	}

	first, err := resolveDesktopRunBindings(ctx, db, data.Run{
		ID:              runID1,
		AccountID:       accountID,
		ThreadID:        threadID,
		CreatedByUserID: &userID,
	})
	if err != nil {
		t.Fatalf("resolve first desktop run bindings: %v", err)
	}
	seedDesktopOwnedSkillPackage(t, db, accountID, "write-helper", "1")
	if _, err := db.Exec(
		ctx,
		`INSERT INTO profile_skill_installs (profile_ref, account_id, owner_user_id, skill_key, version)
		 VALUES ($1, $2, $3, 'write-helper', '1')`,
		derefStr(first.ProfileRef),
		accountID,
		userID,
	); err != nil {
		t.Fatalf("seed profile install: %v", err)
	}
	if _, err := db.Exec(
		ctx,
		`INSERT INTO workspace_skill_enablements (workspace_ref, account_id, enabled_by_user_id, skill_key, version)
		 VALUES ($1, $2, $3, 'write-helper', '1')`,
		derefStr(first.WorkspaceRef),
		accountID,
		userID,
	); err != nil {
		t.Fatalf("seed workspace enablement: %v", err)
	}

	second, err := resolveDesktopRunBindings(ctx, db, data.Run{
		ID:              runID2,
		AccountID:       accountID,
		ThreadID:        threadID,
		CreatedByUserID: &userID,
	})
	if err != nil {
		t.Fatalf("resolve second desktop run bindings: %v", err)
	}
	if derefStr(first.WorkspaceRef) != derefStr(second.WorkspaceRef) {
		t.Fatalf("expected same thread to reuse workspace despite work_dir change, got %q vs %q", derefStr(first.WorkspaceRef), derefStr(second.WorkspaceRef))
	}

	resolver := desktopSkillResolver(db)
	firstItems, err := resolver(ctx, accountID, derefStr(first.ProfileRef), derefStr(first.WorkspaceRef))
	if err != nil {
		t.Fatalf("resolve first run skills: %v", err)
	}
	secondItems, err := resolver(ctx, accountID, derefStr(second.ProfileRef), derefStr(second.WorkspaceRef))
	if err != nil {
		t.Fatalf("resolve second run skills: %v", err)
	}
	if len(firstItems) != 1 || len(secondItems) != 1 {
		t.Fatalf("unexpected resolved skills: first=%#v second=%#v", firstItems, secondItems)
	}
	if firstItems[0].SkillKey != secondItems[0].SkillKey || firstItems[0].Version != secondItems[0].Version {
		t.Fatalf("expected identical skill sets, got first=%#v second=%#v", firstItems, secondItems)
	}

	loader := desktopInputLoader(db, data.DesktopRunEventsRepository{})
	firstRC := &pipeline.RunContext{Run: first, ThreadMessageHistoryLimit: 10}
	if err := loader(ctx, firstRC, func(_ context.Context, rc *pipeline.RunContext) error {
		if rc.WorkDir != "/tmp/work-a" {
			t.Fatalf("unexpected first work_dir: %q", rc.WorkDir)
		}
		return nil
	}); err != nil {
		t.Fatalf("load first desktop input: %v", err)
	}

	secondRC := &pipeline.RunContext{Run: second, ThreadMessageHistoryLimit: 10}
	if err := loader(ctx, secondRC, func(_ context.Context, rc *pipeline.RunContext) error {
		if rc.WorkDir != "/tmp/work-b" {
			t.Fatalf("unexpected second work_dir: %q", rc.WorkDir)
		}
		return nil
	}); err != nil {
		t.Fatalf("load second desktop input: %v", err)
	}
}

func TestDesktopEventWriterCommitsNonStreamingEventsBeforeToolExecution(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{
			sql:  `INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`,
			args: []any{accountID, "desktop-writer-test-" + accountID.String(), "Desktop Writer Test"},
		},
		{
			sql:  `INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, $3, 'private')`,
			args: []any{projectID, accountID, "Writer Project"},
		},
		{
			sql:  `INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`,
			args: []any{threadID, accountID, projectID},
		},
		{
			sql:  `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`,
			args: []any{runID, accountID, threadID},
		},
	} {
		if _, err := db.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed data: %v", err)
		}
	}

	writer := &desktopEventWriter{
		db:         db,
		run:        data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		traceID:    "test-trace",
		runsRepo:   data.DesktopRunsRepository{},
		eventsRepo: data.DesktopRunEventsRepository{},
	}

	completedTurn := events.RunEvent{
		Type: "llm.turn.completed",
		DataJSON: map[string]any{
			"usage": map[string]any{
				"input_tokens":  12,
				"output_tokens": 7,
			},
		},
	}
	if err := writer.append(ctx, runID, completedTurn, "normal"); err != nil {
		t.Fatalf("append non-streaming event: %v", err)
	}
	if writer.tx != nil {
		t.Fatal("expected non-streaming event to commit writer transaction")
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin sub-agent tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := (data.SubAgentRepository{}).Create(ctx, tx, data.SubAgentCreateParams{
		AccountID:      accountID,
		ParentRunID:    runID,
		ParentThreadID: threadID,
		RootRunID:      runID,
		RootThreadID:   threadID,
		Depth:          1,
		SourceType:     data.SubAgentSourceTypeThreadSpawn,
		ContextMode:    data.SubAgentContextModeIsolated,
	}); err != nil {
		t.Fatalf("create sub_agent after non-streaming commit: %v", err)
	}
}

func TestDesktopSubAgentContextRestoresRoutingFromSnapshotFallback(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())

	accountID := uuid.New()
	projectID := uuid.New()
	parentThreadID := uuid.New()
	childThreadID := uuid.New()
	parentRunID := uuid.New()
	childRunID := uuid.New()
	subAgentID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{
			sql:  `INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`,
			args: []any{accountID, "desktop-subagent-routing-" + accountID.String(), "Desktop SubAgent Routing"},
		},
		{
			sql:  `INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, $3, 'private')`,
			args: []any{projectID, accountID, "Routing Project"},
		},
		{
			sql:  `INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE), ($4, $2, $3, TRUE)`,
			args: []any{parentThreadID, accountID, projectID, childThreadID},
		},
		{
			sql:  `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running'), ($4, $2, $5, 'running')`,
			args: []any{parentRunID, accountID, parentThreadID, childRunID, childThreadID},
		},
		{
			sql: `INSERT INTO sub_agents
				(id, account_id, parent_run_id, parent_thread_id, root_run_id, root_thread_id, depth, source_type, context_mode, status, current_run_id)
				VALUES ($1, $2, $3, $4, $3, $4, 1, $5, $6, $7, $8)`,
			args: []any{subAgentID, accountID, parentRunID, parentThreadID, data.SubAgentSourceTypeThreadSpawn, data.SubAgentContextModeIsolated, data.SubAgentStatusQueued, childRunID},
		},
	} {
		if _, err := db.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed data: %v", err)
		}
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin snapshot tx: %v", err)
	}
	storage := subagentctl.NewSnapshotStorage()
	if err := storage.Save(ctx, tx, subAgentID, subagentctl.ContextSnapshot{
		ContextMode: data.SubAgentContextModeIsolated,
		Routing: &subagentctl.ContextSnapshotRouting{
			RouteID: "route-parent",
			Model:   "anthropic^claude-sonnet-4-5",
		},
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit snapshot: %v", err)
	}

	rc := &pipeline.RunContext{
		Run:       data.Run{ID: childRunID, AccountID: accountID, ThreadID: childThreadID, ParentRunID: &parentRunID},
		InputJSON: map[string]any{},
	}

	mw := desktopSubAgentContext(db, storage)
	if err := mw(ctx, rc, func(_ context.Context, rc *pipeline.RunContext) error {
		if got := rc.InputJSON["route_id"]; got != "route-parent" {
			t.Fatalf("unexpected route_id: %#v", got)
		}
		if got := rc.InputJSON["model"]; got != "anthropic^claude-sonnet-4-5" {
			t.Fatalf("unexpected model: %#v", got)
		}
		return nil
	}); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}
}

func TestDesktopEventWriterTouchesRunActivityOnNonTerminalCommit(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{
			sql:  `INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`,
			args: []any{accountID, "desktop-activity-test-" + accountID.String(), "Desktop Activity Test"},
		},
		{
			sql:  `INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, $3, 'private')`,
			args: []any{projectID, accountID, "Activity Project"},
		},
		{
			sql:  `INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`,
			args: []any{threadID, accountID, projectID},
		},
		{
			sql:  `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`,
			args: []any{runID, accountID, threadID},
		},
	} {
		if _, err := db.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed data: %v", err)
		}
	}

	oldActivity := time.Date(2000, time.January, 2, 3, 4, 5, 0, time.UTC).Format("2006-01-02 15:04:05")
	if _, err := db.Exec(ctx, `UPDATE runs SET status_updated_at = $2 WHERE id = $1`, runID, oldActivity); err != nil {
		t.Fatalf("set old activity: %v", err)
	}

	writer := &desktopEventWriter{
		db:         db,
		run:        data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		traceID:    "desktop-activity-trace",
		runsRepo:   data.DesktopRunsRepository{},
		eventsRepo: data.DesktopRunEventsRepository{},
	}

	ev := events.RunEvent{
		Type: "llm.turn.completed",
		DataJSON: map[string]any{
			"usage": map[string]any{
				"input_tokens":  5,
				"output_tokens": 4,
			},
		},
	}
	if err := writer.append(ctx, runID, ev, "normal"); err != nil {
		t.Fatalf("append non-terminal event: %v", err)
	}
	if err := writer.flush(ctx); err != nil {
		t.Fatalf("flush writer: %v", err)
	}

	var (
		status  string
		touched int
	)
	if err := db.QueryRow(
		ctx,
		`SELECT status,
		        CASE WHEN status_updated_at > $2 THEN 1 ELSE 0 END
		   FROM runs
		  WHERE id = $1`,
		runID,
		oldActivity,
	).Scan(&status, &touched); err != nil {
		t.Fatalf("query run activity: %v", err)
	}
	if status != "running" {
		t.Fatalf("expected run to stay running, got %q", status)
	}
	if touched != 1 {
		t.Fatal("expected status_updated_at to refresh on non-terminal commit")
	}
}

func TestDesktopChannelContextOverridesUserIDFromPayload(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())

	if _, err := db.Exec(
		ctx,
		`CREATE TABLE channel_identities (
			id TEXT PRIMARY KEY,
			channel_type TEXT NOT NULL,
			platform_subject_id TEXT NOT NULL,
			user_id TEXT NULL,
			metadata_json TEXT NOT NULL DEFAULT '{}'
		)`,
	); err != nil {
		t.Fatalf("create channel_identities table: %v", err)
	}

	identityID := uuid.New()
	senderUserID := uuid.New()
	if _, err := db.Exec(
		ctx,
		`INSERT INTO channel_identities (id, channel_type, platform_subject_id, user_id, metadata_json)
		 VALUES ($1, 'telegram', '10001', $2, '{}')`,
		identityID,
		senderUserID,
	); err != nil {
		t.Fatalf("insert channel identity: %v", err)
	}

	originalUserID := uuid.New()
	channelID := uuid.New()
	rc := &pipeline.RunContext{
		UserID: &originalUserID,
		JobPayload: map[string]any{
			"channel_delivery": map[string]any{
				"channel_id":                 channelID.String(),
				"channel_type":               "telegram",
				"platform_chat_id":           "10001",
				"platform_message_id":        "55",
				"sender_channel_identity_id": identityID.String(),
			},
		},
	}

	mw := desktopChannelContext(db)
	if err := mw(ctx, rc, func(_ context.Context, rc *pipeline.RunContext) error {
		if rc.ChannelContext == nil {
			t.Fatal("expected channel context to be populated")
		}
		if rc.UserID == nil || *rc.UserID != senderUserID {
			t.Fatalf("expected user override to sender user, got %#v", rc.UserID)
		}
		if rc.ChannelContext.SenderUserID == nil || *rc.ChannelContext.SenderUserID != senderUserID {
			t.Fatalf("unexpected sender user id: %#v", rc.ChannelContext.SenderUserID)
		}
		return nil
	}); err != nil {
		t.Fatalf("desktop channel context failed: %v", err)
	}
}

func TestDesktopChannelDeliveryRecordsFailureWhenChannelMissing(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())

	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS channels (
			id TEXT PRIMARY KEY,
			channel_type TEXT NOT NULL,
			credentials_id TEXT NULL,
			is_active INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS secrets (
			id TEXT PRIMARY KEY,
			encrypted_value TEXT NULL,
			key_version INTEGER NULL
		)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatalf("create channel tables: %v", err)
		}
	}

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{
			sql:  `INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`,
			args: []any{accountID, "desktop-channel-test-" + accountID.String(), "Desktop Channel Test"},
		},
		{
			sql:  `INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, $3, 'private')`,
			args: []any{projectID, accountID, "Channel Project"},
		},
		{
			sql:  `INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`,
			args: []any{threadID, accountID, projectID},
		},
		{
			sql:  `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`,
			args: []any{runID, accountID, threadID},
		},
	} {
		if _, err := db.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed data: %v", err)
		}
	}

	rc := &pipeline.RunContext{
		Run:                  data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		FinalAssistantOutput: "你好，来自 desktop。",
		ChannelContext: &pipeline.ChannelContext{
			ChannelID:      uuid.New(),
			ChannelType:    "telegram",
			PlatformChatID: "10001",
		},
	}

	mw := desktopChannelDelivery(db)
	if err := mw(ctx, rc, func(_ context.Context, _ *pipeline.RunContext) error { return nil }); err != nil {
		t.Fatalf("desktop channel delivery middleware failed: %v", err)
	}

	var errorMessage string
	if err := db.QueryRow(
		ctx,
		`SELECT json_extract(data_json, '$.error')
		   FROM run_events
		  WHERE run_id = $1
		    AND type = 'run.channel_delivery_failed'
		  ORDER BY seq DESC
		  LIMIT 1`,
		runID,
	).Scan(&errorMessage); err != nil {
		t.Fatalf("load delivery failure event: %v", err)
	}
	if errorMessage != "channel not found or inactive" {
		t.Fatalf("unexpected delivery failure error: %q", errorMessage)
	}
}

// mapStore 是一个简单的内存 objectstore.Store 实现，用于测试。
type mapStore struct {
	data map[string][]byte
}

func newMapStore() *mapStore {
	return &mapStore{data: make(map[string][]byte)}
}

func (m *mapStore) Put(_ context.Context, key string, d []byte) error {
	m.data[key] = d
	return nil
}
func (m *mapStore) PutObject(_ context.Context, key string, d []byte, _ objectstore.PutOptions) error {
	m.data[key] = d
	return nil
}
func (m *mapStore) Get(_ context.Context, key string) ([]byte, error) {
	d, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return d, nil
}
func (m *mapStore) GetWithContentType(_ context.Context, key string) ([]byte, string, error) {
	d, ok := m.data[key]
	if !ok {
		return nil, "", fmt.Errorf("key not found: %s", key)
	}
	return d, "application/octet-stream", nil
}
func (m *mapStore) Head(_ context.Context, key string) (objectstore.ObjectInfo, error) {
	_, ok := m.data[key]
	if !ok {
		return objectstore.ObjectInfo{}, fmt.Errorf("key not found: %s", key)
	}
	return objectstore.ObjectInfo{Key: key}, nil
}
func (m *mapStore) Delete(_ context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func TestEnsureSkillExtractedSkipsWhenHashMatches(t *testing.T) {
	ctx := context.Background()
	storeRoot := t.TempDir()

	store := newMapStore()
	bundleKey := skillstore.DerivedBundleKey("cached-skill", "1")
	store.Put(ctx, bundleKey, buildDesktopSkillBundle(t, map[string]string{
		"skill.yaml": "skill_key: cached-skill\nversion: \"1\"\ndisplay_name: Cached Skill\n",
		"SKILL.md":   "# cached\n",
	}))

	skill := skillstore.ResolvedSkill{
		SkillKey:    "cached-skill",
		Version:     "1",
		BundleRef:   bundleKey,
		ContentHash: "abc123",
	}

	// 首次解包
	if err := ensureSkillExtracted(ctx, store, skill, storeRoot); err != nil {
		t.Fatalf("first extraction: %v", err)
	}
	skillDocPath := filepath.Join(storeRoot, "cached-skill@1", "SKILL.md")
	if _, err := os.Stat(skillDocPath); err != nil {
		t.Fatalf("skill doc not extracted: %v", err)
	}

	// 篡改文件内容，验证 hash 匹配时不会重新解包
	os.WriteFile(skillDocPath, []byte("tampered"), 0o644)

	if err := ensureSkillExtracted(ctx, store, skill, storeRoot); err != nil {
		t.Fatalf("second extraction: %v", err)
	}
	content, _ := os.ReadFile(skillDocPath)
	if string(content) != "tampered" {
		t.Fatalf("expected file not overwritten when hash matches, got %q", string(content))
	}
}

func TestEnsureSkillExtractedReExtractsWhenHashDiffers(t *testing.T) {
	ctx := context.Background()
	storeRoot := t.TempDir()

	store := newMapStore()
	bundleKey := skillstore.DerivedBundleKey("updating-skill", "1")
	store.Put(ctx, bundleKey, buildDesktopSkillBundle(t, map[string]string{
		"skill.yaml": "skill_key: updating-skill\nversion: \"1\"\ndisplay_name: Updating Skill\n",
		"SKILL.md":   "# version 1\n",
	}))

	skill := skillstore.ResolvedSkill{
		SkillKey:    "updating-skill",
		Version:     "1",
		BundleRef:   bundleKey,
		ContentHash: "hash-v1",
	}

	if err := ensureSkillExtracted(ctx, store, skill, storeRoot); err != nil {
		t.Fatalf("first extraction: %v", err)
	}

	// 更新 bundle 和 hash
	store.Put(ctx, bundleKey, buildDesktopSkillBundle(t, map[string]string{
		"skill.yaml": "skill_key: updating-skill\nversion: \"1\"\ndisplay_name: Updating Skill\n",
		"SKILL.md":   "# version 2\n",
	}))
	skill.ContentHash = "hash-v2"

	if err := ensureSkillExtracted(ctx, store, skill, storeRoot); err != nil {
		t.Fatalf("re-extraction: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(storeRoot, "updating-skill@1", "SKILL.md"))
	if string(content) != "# version 2\n" {
		t.Fatalf("expected re-extracted content, got %q", string(content))
	}
}

func TestEnsureSkillExtractedAlwaysExtractsWhenHashEmpty(t *testing.T) {
	ctx := context.Background()
	storeRoot := t.TempDir()

	store := newMapStore()
	bundleKey := skillstore.DerivedBundleKey("no-hash-skill", "1")
	store.Put(ctx, bundleKey, buildDesktopSkillBundle(t, map[string]string{
		"skill.yaml": "skill_key: no-hash-skill\nversion: \"1\"\ndisplay_name: No Hash Skill\n",
		"SKILL.md":   "# no hash\n",
	}))

	skill := skillstore.ResolvedSkill{
		SkillKey:    "no-hash-skill",
		Version:     "1",
		BundleRef:   bundleKey,
		ContentHash: "", // 空 hash
	}

	if err := ensureSkillExtracted(ctx, store, skill, storeRoot); err != nil {
		t.Fatalf("first extraction: %v", err)
	}

	// 更新 bundle，因为 hash 为空应总是重新解包
	store.Put(ctx, bundleKey, buildDesktopSkillBundle(t, map[string]string{
		"skill.yaml": "skill_key: no-hash-skill\nversion: \"1\"\ndisplay_name: No Hash Skill\n",
		"SKILL.md":   "# updated no hash\n",
	}))

	if err := ensureSkillExtracted(ctx, store, skill, storeRoot); err != nil {
		t.Fatalf("second extraction: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(storeRoot, "no-hash-skill@1", "SKILL.md"))
	if string(content) != "# updated no hash\n" {
		t.Fatalf("expected re-extracted when hash empty, got %q", string(content))
	}
}

func TestEnsureSkillExtractedExtractsWhenHashFileMissing(t *testing.T) {
	ctx := context.Background()
	storeRoot := t.TempDir()

	store := newMapStore()
	bundleKey := skillstore.DerivedBundleKey("fresh-skill", "1")
	store.Put(ctx, bundleKey, buildDesktopSkillBundle(t, map[string]string{
		"skill.yaml": "skill_key: fresh-skill\nversion: \"1\"\ndisplay_name: Fresh Skill\n",
		"SKILL.md":   "# fresh\n",
	}))

	skill := skillstore.ResolvedSkill{
		SkillKey:    "fresh-skill",
		Version:     "1",
		BundleRef:   bundleKey,
		ContentHash: "some-hash",
	}

	// 目标目录不存在 .content_hash 文件，应正常解包
	if err := ensureSkillExtracted(ctx, store, skill, storeRoot); err != nil {
		t.Fatalf("extraction: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(storeRoot, "fresh-skill@1", "SKILL.md"))
	if string(content) != "# fresh\n" {
		t.Fatalf("expected extracted content, got %q", string(content))
	}
	hashContent, _ := os.ReadFile(filepath.Join(storeRoot, "fresh-skill@1", ".content_hash"))
	if string(hashContent) != "some-hash" {
		t.Fatalf("expected hash file written, got %q", string(hashContent))
	}
}

func seedDesktopRunBindingAccount(t *testing.T, db data.DB, accountID, userID uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(
		context.Background(),
		`INSERT INTO users (id, username, email, status)
		 VALUES ($1, $2, $3, 'active')`,
		userID,
		"desktop-run-user-"+userID.String(),
		"desktop-run-"+userID.String()+"@test.local",
	); err != nil {
		t.Fatalf("seed desktop run user: %v", err)
	}
	if _, err := db.Exec(
		context.Background(),
		`INSERT INTO accounts (id, slug, name, type, status, owner_user_id)
		 VALUES ($1, $2, $3, 'personal', 'active', $4)`,
		accountID,
		"desktop-run-account-"+accountID.String(),
		"Desktop Run Bindings",
		userID,
	); err != nil {
		t.Fatalf("seed desktop run account: %v", err)
	}
}

func seedDesktopRunBindingThread(t *testing.T, db data.DB, accountID, threadID uuid.UUID, projectID, userID *uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(
		context.Background(),
		`INSERT INTO threads (id, account_id, created_by_user_id, project_id, is_private)
		 VALUES ($1, $2, $3, $4, TRUE)`,
		threadID,
		accountID,
		userID,
		projectID,
	); err != nil {
		t.Fatalf("seed desktop run thread: %v", err)
	}
}

func seedDesktopRunBindingRun(t *testing.T, db data.DB, accountID, threadID uuid.UUID, userID *uuid.UUID, runID uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(
		context.Background(),
		`INSERT INTO runs (id, account_id, thread_id, created_by_user_id, status)
		 VALUES ($1, $2, $3, $4, 'running')`,
		runID,
		accountID,
		threadID,
		userID,
	); err != nil {
		t.Fatalf("seed desktop run: %v", err)
	}
}

func seedDesktopOwnedSkillPackage(t *testing.T, db data.DB, accountID uuid.UUID, skillKey string, version string) {
	t.Helper()
	if _, err := db.Exec(
		context.Background(),
		`INSERT INTO skill_packages (account_id, skill_key, version, display_name, instruction_path, manifest_key, bundle_key, files_prefix)
		 VALUES ($1, $2, $3, $4, 'SKILL.md', $5, $6, $7)`,
		accountID,
		skillKey,
		version,
		skillKey,
		skillKey+"-manifest",
		skillKey+"-bundle",
		skillKey+"-files",
	); err != nil {
		t.Fatalf("seed desktop skill package %s@%s: %v", skillKey, version, err)
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

func buildDesktopSkillBundle(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var tarBuffer bytes.Buffer
	writer := tar.NewWriter(&tarBuffer)
	for name, content := range files {
		data := []byte(content)
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatalf("write tar data: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	encoded, err := workspaceblob.Encode(tarBuffer.Bytes())
	if err != nil {
		t.Fatalf("encode skill bundle: %v", err)
	}
	return encoded
}
