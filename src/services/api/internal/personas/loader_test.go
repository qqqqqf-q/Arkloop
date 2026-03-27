package personas

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinPersonasRootLoadsRepoPersonas(t *testing.T) {
	root, err := BuiltinPersonasRoot()
	if err != nil {
		t.Fatalf("BuiltinPersonasRoot failed: %v", err)
	}
	personas, err := LoadFromDir(root)
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}
	if len(personas) == 0 {
		t.Fatal("expected repo personas loaded")
	}

	seen := map[string]RepoPersona{}
	for _, persona := range personas {
		seen[persona.ID] = persona
	}
	if _, ok := seen["normal"]; !ok {
		t.Fatal("expected normal persona loaded")
	}
	if _, ok := seen["extended-search"]; !ok {
		t.Fatal("expected extended-search persona loaded")
	}
	if claw, ok := seen["claw"]; !ok {
		t.Fatal("expected claw persona loaded")
	} else {
		if claw.UserSelectable {
			t.Fatal("expected claw persona hidden from selectors")
		}
		if strings.TrimSpace(claw.PromptMD) == "" {
			t.Fatal("expected claw prompt md")
		}
	}
}

func TestLoadFromDirReadsSoulMDWhenPresent(t *testing.T) {
	root := t.TempDir()
	writeRepoPersonaFiles(t, root, "with-soul",
		"id: with-soul\nversion: \"1\"\ntitle: With Soul\nsoul_file: soul.md\n",
		"persona soul",
		"persona prompt",
	)

	personas, err := LoadFromDir(root)
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}
	if len(personas) != 1 {
		t.Fatalf("expected 1 persona, got %d", len(personas))
	}
	if personas[0].SoulMD != "persona soul" {
		t.Fatalf("unexpected SoulMD: %q", personas[0].SoulMD)
	}
}

func TestLoadFromDirKeepsSoulMDEmptyWhenDefaultMissing(t *testing.T) {
	root := t.TempDir()
	writeRepoPersonaFiles(t, root, "without-soul",
		"id: without-soul\nversion: \"1\"\ntitle: Without Soul\n",
		"",
		"persona prompt",
	)

	personas, err := LoadFromDir(root)
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}
	if len(personas) != 1 {
		t.Fatalf("expected 1 persona, got %d", len(personas))
	}
	if personas[0].SoulMD != "" {
		t.Fatalf("expected empty SoulMD, got %q", personas[0].SoulMD)
	}
}

func TestLoadFromDirInlinesLuaScriptFile(t *testing.T) {
	root := t.TempDir()
	writeRepoPersonaFiles(t, root, "lua-persona",
		"id: lua-persona\nversion: \"1\"\ntitle: Lua Persona\nexecutor_type: agent.lua\nexecutor_config:\n  script_file: agent.lua\n",
		"",
		"persona prompt",
	)
	if err := os.WriteFile(filepath.Join(root, "lua-persona", "agent.lua"), []byte("context.set_output('ok')\n"), 0644); err != nil {
		t.Fatalf("WriteFile agent.lua failed: %v", err)
	}

	personas, err := LoadFromDir(root)
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}
	if len(personas) != 1 {
		t.Fatalf("expected 1 persona, got %d", len(personas))
	}
	script, ok := personas[0].ExecutorConfig["script"].(string)
	if !ok || script == "" {
		t.Fatalf("expected inlined script, got %#v", personas[0].ExecutorConfig)
	}
	if _, exists := personas[0].ExecutorConfig["script_file"]; exists {
		t.Fatalf("expected script_file removed, got %#v", personas[0].ExecutorConfig)
	}
}

func TestLoadFromDirParsesRoles(t *testing.T) {
	root := t.TempDir()
	writeRepoPersonaFiles(t, root, "role-persona",
		"id: role-persona\nversion: \"1\"\ntitle: Role Persona\nroles:\n  worker:\n    prompt_md: worker prompt\n    model: worker^gpt-5-mini\n    budgets:\n      max_output_tokens: 256\n",
		"",
		"persona prompt",
	)

	personas, err := LoadFromDir(root)
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}
	if len(personas) != 1 {
		t.Fatalf("expected 1 persona, got %d", len(personas))
	}
	worker, ok := personas[0].Roles["worker"].(map[string]any)
	if !ok {
		t.Fatalf("expected worker role map, got %#v", personas[0].Roles)
	}
	if worker["prompt_md"] != "worker prompt" {
		t.Fatalf("unexpected worker prompt: %#v", worker)
	}
	if worker["model"] != "worker^gpt-5-mini" {
		t.Fatalf("unexpected worker model: %#v", worker)
	}
	budgets, ok := worker["budgets"].(map[string]any)
	if !ok || budgets["max_output_tokens"] != float64(256) && budgets["max_output_tokens"] != 256 {
		t.Fatalf("unexpected worker budgets: %#v", worker)
	}
}

func TestLoadFromDirParsesConditionalTools(t *testing.T) {
	root := t.TempDir()
	writeRepoPersonaFiles(t, root, "conditional-persona",
		"id: conditional-persona\nversion: \"1\"\ntitle: Conditional Persona\nconditional_tools:\n  - when:\n      lacks_input_modalities:\n        - image\n    tools:\n      - understand_image\n",
		"",
		"persona prompt",
	)

	personas, err := LoadFromDir(root)
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}
	if len(personas) != 1 {
		t.Fatalf("expected 1 persona, got %d", len(personas))
	}
	if len(personas[0].ConditionalTools) != 1 {
		t.Fatalf("expected 1 conditional tool rule, got %d", len(personas[0].ConditionalTools))
	}
	rule := personas[0].ConditionalTools[0]
	if len(rule.When.LacksInputModalities) != 1 || rule.When.LacksInputModalities[0] != "image" {
		t.Fatalf("unexpected modalities: %#v", rule.When.LacksInputModalities)
	}
	if len(rule.Tools) != 1 || rule.Tools[0] != "understand_image" {
		t.Fatalf("unexpected tools: %#v", rule.Tools)
	}
}

func TestLoadFromDirRejectsUnsupportedRoleField(t *testing.T) {
	root := t.TempDir()
	writeRepoPersonaFiles(t, root, "bad-role",
		"id: bad-role\nversion: \"1\"\ntitle: Bad Role\nroles:\n  worker:\n    executor_type: agent.lua\n",
		"",
		"persona prompt",
	)

	_, err := LoadFromDir(root)
	if err == nil || err.Error() != "persona bad-role roles: roles.worker contains unsupported field: executor_type" {
		t.Fatalf("expected unsupported role field error, got %v", err)
	}
}

func writeRepoPersonaFiles(t *testing.T, root, name, yamlContent, soulContent, promptContent string) {
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
	if soulContent == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "soul.md"), []byte(soulContent), 0644); err != nil {
		t.Fatalf("WriteFile soul.md failed: %v", err)
	}
}
