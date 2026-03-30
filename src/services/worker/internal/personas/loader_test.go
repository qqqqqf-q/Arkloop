package personas

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"arkloop/services/worker/internal/testutil"
	"arkloop/services/worker/internal/tools"

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

func TestLoadRegistryLoadsClawPersona(t *testing.T) {
	root, err := BuiltinPersonasRoot()
	if err != nil {
		t.Fatalf("BuiltinPersonasRoot failed: %v", err)
	}
	registry, err := LoadRegistry(root)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("claw")
	if !ok {
		t.Fatalf("expected claw persona loaded")
	}
	if def.UserSelectable {
		t.Fatal("expected claw persona not user selectable")
	}
	if def.ExecutorType != "agent.simple" {
		t.Fatalf("unexpected executor_type: %s", def.ExecutorType)
	}
	if def.Budgets.MaxOutputTokens != nil {
		t.Fatalf("claw persona.yaml omits max_output_tokens; got %#v", def.Budgets.MaxOutputTokens)
	}
	if len(def.ToolAllowlist) == 0 {
		t.Fatal("expected claw tool allowlist")
	}
	if def.PromptMD == "" || def.SoulMD == "" {
		t.Fatal("expected claw prompt and soul")
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

func TestLoadPersonaWithSelectorFields(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "selectable_persona",
		"id: selectable_persona\nversion: \"1\"\ntitle: Selectable\nuser_selectable: true\nselector_name: Search\nselector_order: 2\n",
		"# prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("selectable_persona")
	if !ok {
		t.Fatalf("expected selectable_persona persona loaded")
	}
	if !def.UserSelectable {
		t.Fatal("expected UserSelectable to be true")
	}
	if def.SelectorName == nil || *def.SelectorName != "Search" {
		t.Fatalf("expected selector_name Search, got %#v", def.SelectorName)
	}
	if def.SelectorOrder == nil || *def.SelectorOrder != 2 {
		t.Fatalf("expected selector_order 2, got %#v", def.SelectorOrder)
	}
}

func TestLoadPersonaWithConditionalTools(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "conditional_persona",
		"id: conditional_persona\nversion: \"1\"\ntitle: Conditional\nconditional_tools:\n  - when:\n      lacks_input_modalities:\n        - image\n    tools:\n      - understand_image\n",
		"# prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("conditional_persona")
	if !ok {
		t.Fatalf("expected conditional_persona loaded")
	}
	if len(def.ConditionalTools) != 1 {
		t.Fatalf("expected 1 conditional tool rule, got %d", len(def.ConditionalTools))
	}
	rule := def.ConditionalTools[0]
	if len(rule.When.LacksInputModalities) != 1 || rule.When.LacksInputModalities[0] != "image" {
		t.Fatalf("unexpected modalities: %#v", rule.When.LacksInputModalities)
	}
	if len(rule.Tools) != 1 || rule.Tools[0] != "understand_image" {
		t.Fatalf("unexpected tools: %#v", rule.Tools)
	}
}

func TestLoadPersonaWithoutSelectorFieldsUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "default_selector_persona",
		"id: default_selector_persona\nversion: \"1\"\ntitle: Default\n",
		"# prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("default_selector_persona")
	if !ok {
		t.Fatalf("expected default_selector_persona persona loaded")
	}
	if def.UserSelectable {
		t.Fatal("expected UserSelectable default false")
	}
	if def.SelectorName != nil {
		t.Fatalf("expected nil SelectorName, got %#v", def.SelectorName)
	}
	if def.SelectorOrder != nil {
		t.Fatalf("expected nil SelectorOrder, got %#v", def.SelectorOrder)
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

func TestLoadPersonaDefaultSoulMissingKeepsCompatibility(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "no_soul",
		"id: no_soul\nversion: \"1\"\ntitle: No Soul\n",
		"# prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("no_soul")
	if !ok {
		t.Fatal("expected no_soul persona loaded")
	}
	if def.SoulMD != "" {
		t.Fatalf("expected empty SoulMD, got %q", def.SoulMD)
	}
}

func TestLoadPersonaDefaultSoulLoadedWhenPresent(t *testing.T) {
	dir := t.TempDir()
	writePersonaFilesWithSoul(t, dir, "with_soul",
		"id: with_soul\nversion: \"1\"\ntitle: With Soul\n",
		"soul content",
		"# prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("with_soul")
	if !ok {
		t.Fatal("expected with_soul persona loaded")
	}
	if def.SoulMD != "soul content" {
		t.Fatalf("unexpected SoulMD: %q", def.SoulMD)
	}
}

func TestLoadPersonaExplicitSoulFileMissingFails(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "missing_soul",
		"id: missing_soul\nversion: \"1\"\ntitle: Missing Soul\nsoul_file: custom-soul.md\n",
		"# prompt",
	)

	_, err := LoadRegistry(dir)
	if err == nil {
		t.Fatal("expected explicit soul_file error")
	}
}

func TestLoadPersonaExplicitSoulFileEmptyFails(t *testing.T) {
	dir := t.TempDir()
	writePersonaFilesWithNamedSoul(t, dir, "empty_soul",
		"id: empty_soul\nversion: \"1\"\ntitle: Empty Soul\nsoul_file: custom-soul.md\n",
		"custom-soul.md",
		"   \n",
		"# prompt",
	)

	_, err := LoadRegistry(dir)
	if err == nil {
		t.Fatal("expected empty explicit soul_file error")
	}
	if err.Error() != "soul_file: file must not be empty" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPersonaExplicitSoulFileEscapeFails(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "escape_soul",
		"id: escape_soul\nversion: \"1\"\ntitle: Escape Soul\nsoul_file: ../soul.md\n",
		"# prompt",
	)

	_, err := LoadRegistry(dir)
	if err == nil {
		t.Fatal("expected soul_file escape error")
	}
	if err.Error() != "soul_file: path escapes persona directory" {
		t.Fatalf("unexpected error: %v", err)
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

func writePersonaFilesWithSoul(t *testing.T, root, name, yamlContent, soulContent, promptContent string) {
	t.Helper()
	writePersonaFilesWithNamedSoul(t, root, name, yamlContent, "soul.md", soulContent, promptContent)
}

func writePersonaFilesWithNamedSoul(t *testing.T, root, name, yamlContent, soulFileName, soulContent, promptContent string) {
	t.Helper()
	writePersonaFiles(t, root, name, yamlContent, promptContent)
	if err := os.WriteFile(filepath.Join(root, name, soulFileName), []byte(soulContent), 0644); err != nil {
		t.Fatalf("WriteFile %s failed: %v", soulFileName, err)
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

func TestLoadPersonaParsesResultSummarize(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "summarizer",
		"id: summarizer\nversion: \"1\"\ntitle: Summarizer\nresult_summarize:\n  prompt: compress output\n  max_tokens: 300\n  threshold_bytes: 2048\n",
		"# prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("summarizer")
	if !ok {
		t.Fatal("expected summarizer persona to load")
	}
	if def.ResultSummarizer == nil {
		t.Fatal("expected result summarizer config")
	}
	if def.ResultSummarizer.Prompt != "compress output" {
		t.Fatalf("unexpected prompt: %q", def.ResultSummarizer.Prompt)
	}
	if def.ResultSummarizer.MaxTokens != 300 {
		t.Fatalf("unexpected max tokens: %d", def.ResultSummarizer.MaxTokens)
	}
	if def.ResultSummarizer.ThresholdBytes != 2048 {
		t.Fatalf("unexpected threshold: %d", def.ResultSummarizer.ThresholdBytes)
	}
}

func TestMergeRegistryKeepsBaseResultSummarizerWhenOverrideMissing(t *testing.T) {
	base := NewRegistry()
	if err := base.Register(Definition{
		ID:      "summarizer",
		Version: "1",
		Title:   "Summarizer",
		ResultSummarizer: &ResultSummarizerConfig{
			Prompt:         "base result prompt",
			MaxTokens:      128,
			ThresholdBytes: 2048,
		},
	}); err != nil {
		t.Fatalf("register base failed: %v", err)
	}

	merged := MergeRegistry(base, []Definition{
		{
			ID:      "summarizer",
			Version: "1",
			Title:   "Summarizer Override",
		},
	})

	def, ok := merged.Get("summarizer")
	if !ok {
		t.Fatal("expected merged registry has summarizer")
	}
	if def.ResultSummarizer == nil {
		t.Fatal("expected result summarizer preserved from base")
	}
	if def.ResultSummarizer.Prompt != "base result prompt" {
		t.Fatalf("unexpected prompt: %q", def.ResultSummarizer.Prompt)
	}
}

func TestLoadPersonaUsesSummarizePromptFiles(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "summarizer",
		"id: summarizer\nversion: \"1\"\ntitle: Summarizer\ntitle_summarize:\n  prompt_file: title_summarize.md\nresult_summarize:\n  prompt_file: result_summarize.md\n",
		"# prompt",
	)
	if err := os.WriteFile(filepath.Join(dir, "summarizer", "title_summarize.md"), []byte("title prompt"), 0644); err != nil {
		t.Fatalf("WriteFile title_summarize.md failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "summarizer", "result_summarize.md"), []byte("result prompt"), 0644); err != nil {
		t.Fatalf("WriteFile result_summarize.md failed: %v", err)
	}

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("summarizer")
	if !ok {
		t.Fatal("expected summarizer persona to load")
	}
	if def.TitleSummarizer == nil || def.TitleSummarizer.Prompt != "title prompt" {
		t.Fatalf("unexpected title summarizer: %#v", def.TitleSummarizer)
	}
	if def.ResultSummarizer == nil || def.ResultSummarizer.Prompt != "result prompt" {
		t.Fatalf("unexpected result summarizer: %#v", def.ResultSummarizer)
	}
}

func TestMergeRegistryKeepsBaseSoulWhenOverrideMissing(t *testing.T) {
	base := NewRegistry()
	if err := base.Register(Definition{ID: "normal", Version: "1", Title: "Normal", SoulMD: "base soul"}); err != nil {
		t.Fatalf("register base failed: %v", err)
	}

	merged := MergeRegistry(base, []Definition{{ID: "normal", Version: "1", Title: "Normal Override"}})
	def, ok := merged.Get("normal")
	if !ok {
		t.Fatal("expected merged registry has normal")
	}
	if def.SoulMD != "base soul" {
		t.Fatalf("expected base soul preserved, got %q", def.SoulMD)
	}
}

func TestLoadFromDBIgnoresGlobalPersonaRows(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_personas_loader")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	projectID := uuid.New()
	insertWorkerPersonaRow(t, pool, &projectID, "custom-only", "Custom Only")
	insertWorkerPersonaRow(t, pool, nil, "ghost", "Ghost")

	defs, err := LoadFromDB(context.Background(), pool, &projectID)
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

	projectID := uuid.New()
	insertWorkerPersonaRow(t, pool, &projectID, "normal", "Custom Normal")
	insertWorkerPersonaRow(t, pool, nil, "normal", "Ghost Normal")

	defs, err := LoadFromDB(context.Background(), pool, &projectID)
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

func TestLoadPersonaRejectsLegacyMaxIterations(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "legacy_budget",
		"id: legacy_budget\nversion: \"1\"\ntitle: Legacy\nbudgets:\n  max_iterations: 3\n",
		"# prompt",
	)

	_, err := LoadRegistry(dir)
	if err == nil || err.Error() != "budgets contains unsupported field: max_iterations" {
		t.Fatalf("expected legacy max_iterations error, got %v", err)
	}
}

func TestLoadPersonaParsesLayeredBudgets(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "layered_budget",
		"id: layered_budget\nversion: \"1\"\ntitle: Layered\nbudgets:\n  reasoning_iterations: 6\n  tool_continuation_budget: 18\n  per_tool_soft_limits:\n    exec_command:\n      max_output_bytes: 12000\n    write_stdin:\n      max_continuations: 9\n      max_yield_time_ms: 2500\n      max_output_bytes: 15000\n",
		"# prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("layered_budget")
	if !ok {
		t.Fatal("expected layered_budget persona")
	}
	if def.Budgets.ReasoningIterations == nil || *def.Budgets.ReasoningIterations != 6 {
		t.Fatalf("unexpected reasoning_iterations: %v", def.Budgets.ReasoningIterations)
	}
	if def.Budgets.ToolContinuationBudget == nil || *def.Budgets.ToolContinuationBudget != 18 {
		t.Fatalf("unexpected tool_continuation_budget: %v", def.Budgets.ToolContinuationBudget)
	}
	execLimit := def.Budgets.PerToolSoftLimits["exec_command"]
	if execLimit.MaxOutputBytes == nil || *execLimit.MaxOutputBytes != 12000 {
		t.Fatalf("unexpected exec_command limit: %v", execLimit.MaxOutputBytes)
	}
	writeLimit := def.Budgets.PerToolSoftLimits["write_stdin"]
	if writeLimit.MaxContinuations == nil || *writeLimit.MaxContinuations != 9 {
		t.Fatalf("unexpected write_stdin max_continuations: %v", writeLimit.MaxContinuations)
	}
	if writeLimit.MaxYieldTimeMs == nil || *writeLimit.MaxYieldTimeMs != 2500 {
		t.Fatalf("unexpected write_stdin max_yield_time_ms: %v", writeLimit.MaxYieldTimeMs)
	}
	if writeLimit.MaxOutputBytes == nil || *writeLimit.MaxOutputBytes != 15000 {
		t.Fatalf("unexpected write_stdin max_output_bytes: %v", writeLimit.MaxOutputBytes)
	}
}

func TestLoadPersonaRejectsSoftLimitAboveHardCap(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "bad_soft_limit",
		"id: bad_soft_limit\nversion: \"1\"\ntitle: Bad\nbudgets:\n  per_tool_soft_limits:\n    write_stdin:\n      max_yield_time_ms: 40000\n",
		"# prompt",
	)

	_, err := LoadRegistry(dir)
	if err == nil {
		t.Fatal("expected max_yield_time_ms validation error")
	}
	want := "budgets.per_tool_soft_limits.write_stdin.max_yield_time_ms must be less than or equal to 30000"
	if err.Error() != want {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromDBParsesLayeredBudgets(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_personas_layered_db")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	projectID := uuid.New()
	_, err = pool.Exec(
		context.Background(),
		`INSERT INTO personas
			(project_id, persona_key, version, display_name, prompt_md, tool_allowlist, tool_denylist, budgets_json, executor_type, executor_config_json)
		 VALUES ($1, 'db-budget', '1', 'DB Budget', 'prompt', '{}', '{}', $2::jsonb, 'agent.simple', '{}'::jsonb)`,
		projectID,
		`{"reasoning_iterations":4,"tool_continuation_budget":12,"per_tool_soft_limits":{"write_stdin":{"max_continuations":7,"max_output_bytes":12345}}}`,
	)
	if err != nil {
		t.Fatalf("insert persona row failed: %v", err)
	}

	defs, err := LoadFromDB(context.Background(), pool, &projectID)
	if err != nil {
		t.Fatalf("LoadFromDB failed: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 persona, got %d", len(defs))
	}
	if defs[0].Budgets.ReasoningIterations == nil || *defs[0].Budgets.ReasoningIterations != 4 {
		t.Fatalf("unexpected reasoning_iterations: %v", defs[0].Budgets.ReasoningIterations)
	}
	if defs[0].Budgets.ToolContinuationBudget == nil || *defs[0].Budgets.ToolContinuationBudget != 12 {
		t.Fatalf("unexpected tool_continuation_budget: %v", defs[0].Budgets.ToolContinuationBudget)
	}
	writeLimit := defs[0].Budgets.PerToolSoftLimits["write_stdin"]
	if writeLimit.MaxContinuations == nil || *writeLimit.MaxContinuations != 7 {
		t.Fatalf("unexpected write_stdin max_continuations: %v", writeLimit.MaxContinuations)
	}
	if writeLimit.MaxOutputBytes == nil || *writeLimit.MaxOutputBytes != 12345 {
		t.Fatalf("unexpected write_stdin max_output_bytes: %v", writeLimit.MaxOutputBytes)
	}
	if tools.ResolveToolSoftLimit(defs[0].Budgets.PerToolSoftLimits, "exec_command").MaxOutputBytes != nil {
		t.Fatal("unexpected exec_command override from db budgets")
	}
}

func TestLoadPersonaParsesRoleOverrides(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "role_overlay",
		"id: role_overlay\nversion: \"1\"\ntitle: Role Overlay\nroles:\n  worker:\n    soul_md: worker soul\n    prompt_md: worker prompt\n    tool_allowlist:\n      - web_search\n    tool_denylist:\n      - exec_command\n    budgets:\n      max_output_tokens: 256\n    model: worker^gpt-5-mini\n    preferred_credential: worker-cred\n    reasoning_mode: high\n    prompt_cache_control: system_prompt\n",
		"base prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("role_overlay")
	if !ok {
		t.Fatal("expected role_overlay persona")
	}
	override, ok := def.Roles["worker"]
	if !ok {
		t.Fatal("expected worker role override")
	}
	if !override.SoulMD.Set || override.SoulMD.Value != "worker soul" {
		t.Fatalf("unexpected soul override: %#v", override.SoulMD)
	}
	if !override.PromptMD.Set || override.PromptMD.Value != "worker prompt" {
		t.Fatalf("unexpected prompt override: %#v", override.PromptMD)
	}
	if !override.HasToolAllowlist || len(override.ToolAllowlist) != 1 || override.ToolAllowlist[0] != "web_search" {
		t.Fatalf("unexpected tool allowlist override: %#v", override.ToolAllowlist)
	}
	if !override.HasToolDenylist || len(override.ToolDenylist) != 1 || override.ToolDenylist[0] != "exec_command" {
		t.Fatalf("unexpected tool denylist override: %#v", override.ToolDenylist)
	}
	if !override.Budgets.HasMaxOutputTokens || override.Budgets.MaxOutputTokens == nil || *override.Budgets.MaxOutputTokens != 256 {
		t.Fatalf("unexpected max_output_tokens override: %#v", override.Budgets)
	}
	if !override.Model.Set || override.Model.Value == nil || *override.Model.Value != "worker^gpt-5-mini" {
		t.Fatalf("unexpected model override: %#v", override.Model)
	}
	if !override.PreferredCredential.Set || override.PreferredCredential.Value == nil || *override.PreferredCredential.Value != "worker-cred" {
		t.Fatalf("unexpected credential override: %#v", override.PreferredCredential)
	}
	if !override.ReasoningMode.Set || override.ReasoningMode.Value != "high" {
		t.Fatalf("unexpected reasoning override: %#v", override.ReasoningMode)
	}
	if !override.PromptCacheControl.Set || override.PromptCacheControl.Value != "system_prompt" {
		t.Fatalf("unexpected prompt cache override: %#v", override.PromptCacheControl)
	}
}

func TestLoadPersonaRejectsUnsupportedRoleField(t *testing.T) {
	dir := t.TempDir()
	writePersonaFiles(t, dir, "bad_role",
		"id: bad_role\nversion: \"1\"\ntitle: Bad Role\nroles:\n  worker:\n    executor_type: agent.lua\n",
		"base prompt",
	)

	_, err := LoadRegistry(dir)
	if err == nil || err.Error() != "roles.worker contains unsupported field: executor_type" {
		t.Fatalf("expected unsupported role field error, got %v", err)
	}
}

func TestLoadFromDBParsesRoleOverrides(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_personas_role_db")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	projectID := uuid.New()
	_, err = pool.Exec(
		context.Background(),
		`INSERT INTO personas
			(project_id, persona_key, version, display_name, prompt_md, tool_allowlist, tool_denylist, budgets_json, roles_json, executor_type, executor_config_json)
		 VALUES ($1, 'db-role', '1', 'DB Role', 'prompt', '{}', '{}', '{}'::jsonb, $2::jsonb, 'agent.simple', '{}'::jsonb)`,
		projectID,
		`{"worker":{"prompt_md":"db worker","model":"db^gpt-5-mini","budgets":{"temperature":0.2}}}`,
	)
	if err != nil {
		t.Fatalf("insert persona row failed: %v", err)
	}

	defs, err := LoadFromDB(context.Background(), pool, &projectID)
	if err != nil {
		t.Fatalf("LoadFromDB failed: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 persona, got %d", len(defs))
	}
	override, ok := defs[0].Roles["worker"]
	if !ok {
		t.Fatal("expected worker role override from db")
	}
	if !override.PromptMD.Set || override.PromptMD.Value != "db worker" {
		t.Fatalf("unexpected db prompt override: %#v", override.PromptMD)
	}
	if !override.Model.Set || override.Model.Value == nil || *override.Model.Value != "db^gpt-5-mini" {
		t.Fatalf("unexpected db model override: %#v", override.Model)
	}
	if !override.Budgets.HasTemperature || override.Budgets.Temperature == nil || *override.Budgets.Temperature != 0.2 {
		t.Fatalf("unexpected db temperature override: %#v", override.Budgets)
	}
}

func insertWorkerPersonaRow(t *testing.T, pool *pgxpool.Pool, projectID *uuid.UUID, personaKey string, displayName string) {
	t.Helper()

	_, err := pool.Exec(
		context.Background(),
		`INSERT INTO personas
			(project_id, persona_key, version, display_name, prompt_md, tool_allowlist, tool_denylist, budgets_json, executor_type, executor_config_json)
		 VALUES ($1, $2, '1', $3, 'prompt', '{}', '{}', '{}'::jsonb, 'agent.simple', '{}'::jsonb)`,
		projectID,
		personaKey,
		displayName,
	)
	if err != nil {
		t.Fatalf("insert persona row failed: %v", err)
	}
}
