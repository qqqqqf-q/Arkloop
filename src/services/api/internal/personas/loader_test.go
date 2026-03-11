package personas

import (
	"os"
	"path/filepath"
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
