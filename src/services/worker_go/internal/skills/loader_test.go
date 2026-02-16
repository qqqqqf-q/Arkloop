package skills

import "testing"

func TestLoadRegistryLoadsDemoSkill(t *testing.T) {
	root, err := BuiltinSkillsRoot()
	if err != nil {
		t.Fatalf("BuiltinSkillsRoot failed: %v", err)
	}
	registry, err := LoadRegistry(root)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("demo_no_tools")
	if !ok {
		t.Fatalf("expected demo_no_tools skill loaded")
	}
	if def.Version != "1" {
		t.Fatalf("unexpected version: %s", def.Version)
	}
	if def.PromptMD == "" {
		t.Fatalf("expected prompt md")
	}
}

func TestResolveSkillVersionMismatch(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Definition{ID: "demo", Version: "1", Title: "t"}); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	decision := ResolveSkill(map[string]any{"skill_id": "demo@2"}, registry)
	if decision.Error == nil || decision.Error.ErrorClass != ErrorClassSkillVersionMismatch {
		t.Fatalf("expected version mismatch, got %+v", decision)
	}
}

