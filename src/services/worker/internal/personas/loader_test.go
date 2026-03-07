package personas

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLoadRegistryLoadsNormalPersona(t *testing.T) {
	root, err := BuiltinPersonasRoot()
	if err != nil {
		t.Fatalf("BuiltinPersonasRoot failed: %v", err)
	}
	registry, err := LoadRegistry(root)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("normal")
	if !ok {
		t.Fatalf("expected normal persona loaded")
	}
	if def.Version != "1" {
		t.Fatalf("unexpected version: %s", def.Version)
	}
	if def.PromptMD == "" {
		t.Fatalf("expected prompt md")
	}
}

func TestResolvePersonaVersionMismatch(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Definition{ID: "demo", Version: "1", Title: "t"}); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	decision := ResolvePersona(map[string]any{"persona_id": "demo@2"}, registry)
	if decision.Error == nil || decision.Error.ErrorClass != ErrorClassPersonaVersionMismatch {
		t.Fatalf("expected version mismatch, got %+v", decision)
	}
}

// TestLoadPersonaDefaultExecutorType 验证无 executor_type 字段的 yaml 使用默认值，向后兼容。
func TestLoadPersonaDefaultExecutorType(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "test_default_exec",
		"id: test_default_exec\nversion: \"1\"\ntitle: Test\n",
		"# prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("test_default_exec")
	if !ok {
		t.Fatalf("expected test_default_exec persona loaded")
	}
	if def.ExecutorType != "agent.simple" {
		t.Fatalf("expected default executor_type 'agent.simple', got %q", def.ExecutorType)
	}
	if def.ExecutorConfig == nil {
		t.Fatalf("expected non-nil ExecutorConfig")
	}
}

// TestLoadPersonaWithExecutorType 验证 executor_type 和 executor_config 字段可正确解析。
func TestLoadPersonaWithExecutorType(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "test_exec",
		"id: test_exec\nversion: \"1\"\ntitle: Test\nexecutor_type: task.classify_route\nexecutor_config:\n  check_in_every: 5\n",
		"# prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("test_exec")
	if !ok {
		t.Fatalf("expected test_exec persona loaded")
	}
	if def.ExecutorType != "task.classify_route" {
		t.Fatalf("expected executor_type 'task.classify_route', got %q", def.ExecutorType)
	}
	if def.ExecutorConfig["check_in_every"] == nil {
		t.Fatalf("expected executor_config.check_in_every to be set")
	}
}

func TestLoadPersonaWithExecutorScriptFile(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "test_lua",
		"id: test_lua\nversion: \"1\"\ntitle: Test Lua\nexecutor_type: agent.lua\nexecutor_config:\n  script_file: agent.lua\n",
		"# prompt",
	)
	if err := os.WriteFile(filepath.Join(dir, "test_lua", "agent.lua"), []byte("context.set_output('ok')\n"), 0644); err != nil {
		t.Fatalf("WriteFile agent.lua failed: %v", err)
	}

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("test_lua")
	if !ok {
		t.Fatalf("expected test_lua persona loaded")
	}
	script, ok := def.ExecutorConfig["script"].(string)
	if !ok || script == "" {
		t.Fatalf("expected executor_config.script from script_file")
	}
	if _, exists := def.ExecutorConfig["script_file"]; exists {
		t.Fatalf("expected script_file removed after loading")
	}
}

func TestLoadPersonaWithExecutorScriptFileConflict(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "bad_lua",
		"id: bad_lua\nversion: \"1\"\ntitle: Bad Lua\nexecutor_type: agent.lua\nexecutor_config:\n  script: |\n    context.set_output('inline')\n  script_file: agent.lua\n",
		"# prompt",
	)
	if err := os.WriteFile(filepath.Join(dir, "bad_lua", "agent.lua"), []byte("context.set_output('file')\n"), 0644); err != nil {
		t.Fatalf("WriteFile agent.lua failed: %v", err)
	}

	_, err := LoadRegistry(dir)
	if err == nil {
		t.Fatal("expected error for script and script_file conflict, got nil")
	}
}

func TestLoadPersonaWithExecutorScriptFileEscape(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "escape_lua",
		"id: escape_lua\nversion: \"1\"\ntitle: Escape Lua\nexecutor_type: agent.lua\nexecutor_config:\n  script_file: ../agent.lua\n",
		"# prompt",
	)

	_, err := LoadRegistry(dir)
	if err == nil {
		t.Fatal("expected error for escaped script_file path, got nil")
	}
}

func TestLoadPersonaWithNonLuaExecutorKeepsScriptFileRaw(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "route_keep_raw",
		"id: route_keep_raw\nversion: \"1\"\ntitle: Route Keep Raw\nexecutor_type: task.classify_route\nexecutor_config:\n  script_file: untouched.lua\n",
		"# prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("route_keep_raw")
	if !ok {
		t.Fatalf("expected route_keep_raw persona loaded")
	}
	raw, ok := def.ExecutorConfig["script_file"].(string)
	if !ok || raw != "untouched.lua" {
		t.Fatalf("expected script_file untouched for non-lua executor, got %#v", def.ExecutorConfig["script_file"])
	}
}

// TestLoadPersonaInvalidExecutorType 验证非法 executor_type 返回错误。
func TestLoadPersonaInvalidExecutorType(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "bad_exec",
		"id: bad_exec\nversion: \"1\"\ntitle: Bad\nexecutor_type: \"!!invalid type!!\"\n",
		"# prompt",
	)

	_, err := LoadRegistry(dir)
	if err == nil {
		t.Fatal("expected error for invalid executor_type, got nil")
	}
}

func writePersonaFiles(t *testing.T, root, name, yamlContent, promptContent string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "persona.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile persona.yaml failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte(promptContent), 0644); err != nil {
		t.Fatalf("WriteFile prompt.md failed: %v", err)
	}
}

// TestLoadPersonaWithPreferredCredential 验证 preferred_credential 字段可正确解析。
func TestLoadPersonaWithPreferredCredential(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "test_route",
		"id: test_route\nversion: \"1\"\ntitle: Test\npreferred_credential: my-anthropic\n",
		"# prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("test_route")
	if !ok {
		t.Fatalf("expected test_route persona loaded")
	}
	if def.PreferredCredential == nil {
		t.Fatal("expected PreferredCredential to be set")
	}
	if *def.PreferredCredential != "my-anthropic" {
		t.Fatalf("expected preferred_credential 'my-anthropic', got %q", *def.PreferredCredential)
	}
}

// TestLoadPersonaWithoutPreferredCredential 验证无 preferred_credential 字段时 PreferredCredential 为 nil。
func TestLoadPersonaWithoutPreferredCredential(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "no_cred_persona",
		"id: no_cred_persona\nversion: \"1\"\ntitle: No Cred\n",
		"# prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("no_cred_persona")
	if !ok {
		t.Fatal("expected no_cred_persona to be loaded")
	}
	if def.PreferredCredential != nil {
		t.Fatalf("expected PreferredCredential nil, got %q", *def.PreferredCredential)
	}
}

func TestMergeRegistryKeepsBaseTitleSummarizerWhenOverrideMissing(t *testing.T) {
	base := NewRegistry()
	if err := base.Register(Definition{
		ID:      "normal",
		Version: "1",
		Title:   "Normal",
		TitleSummarizer: &TitleSummarizerConfig{
			Prompt:    "base prompt",
			MaxTokens: 15,
		},
	}); err != nil {
		t.Fatalf("register base failed: %v", err)
	}

	merged := MergeRegistry(base, []Definition{
		{
			ID:      "normal",
			Version: "1",
			Title:   "Normal Override",
		},
	})

	def, ok := merged.Get("normal")
	if !ok {
		t.Fatal("expected merged registry has normal")
	}
	if def.TitleSummarizer == nil {
		t.Fatal("expected title summarizer preserved from base")
	}
	if def.TitleSummarizer.Prompt != "base prompt" {
		t.Fatalf("unexpected prompt: %q", def.TitleSummarizer.Prompt)
	}
	if def.Title != "Normal Override" {
		t.Fatalf("expected override title, got %q", def.Title)
	}
}

func TestLoadFromDBIgnoresGlobalPersonaRows(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_personas_loader")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	insertWorkerPersonaRow(t, pool, &orgID, "custom-only", "Custom Only")
	insertWorkerPersonaRow(t, pool, nil, "ghost", "Ghost")

	defs, err := LoadFromDB(context.Background(), pool, orgID)
	if err != nil {
		t.Fatalf("LoadFromDB failed: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 persona, got %d", len(defs))
	}
	if defs[0].ID != "custom-only" {
		t.Fatalf("expected custom-only, got %q", defs[0].ID)
	}
}

func TestMergeRegistryUsesOrgOverrideOnly(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_personas_merge")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	insertWorkerPersonaRow(t, pool, &orgID, "normal", "Custom Normal")
	insertWorkerPersonaRow(t, pool, nil, "normal", "Ghost Normal")

	defs, err := LoadFromDB(context.Background(), pool, orgID)
	if err != nil {
		t.Fatalf("LoadFromDB failed: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 override persona, got %d", len(defs))
	}

	base := NewRegistry()
	if err := base.Register(Definition{ID: "normal", Version: "1", Title: "Builtin Normal"}); err != nil {
		t.Fatalf("register base failed: %v", err)
	}

	merged := MergeRegistry(base, defs)
	def, ok := merged.Get("normal")
	if !ok {
		t.Fatal("expected merged normal persona")
	}
	if def.Title != "Custom Normal" {
		t.Fatalf("expected custom override title, got %q", def.Title)
	}
}

func insertWorkerPersonaRow(t *testing.T, pool *pgxpool.Pool, orgID *uuid.UUID, personaKey string, displayName string) {
	t.Helper()

	_, err := pool.Exec(
		context.Background(),
		`INSERT INTO personas
			(org_id, persona_key, version, display_name, prompt_md, tool_allowlist, tool_denylist, budgets_json, executor_type, executor_config_json)
		 VALUES ($1, $2, '1', $3, 'prompt', '{}', '{}', '{}'::jsonb, 'agent.simple', '{}'::jsonb)`,
		orgID,
		personaKey,
		displayName,
	)
	if err != nil {
		t.Fatalf("insert persona row failed: %v", err)
	}
}
