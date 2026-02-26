package skills

import (
	"os"
	"path/filepath"
	"testing"
)

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

// TestLoadSkillDefaultExecutorType 验证无 executor_type 字段的 yaml 使用默认值，向后兼容。
func TestLoadSkillDefaultExecutorType(t *testing.T) {
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
	if def.ExecutorType != "agent.simple" {
		t.Fatalf("expected default executor_type 'agent.simple', got %q", def.ExecutorType)
	}
	if def.ExecutorConfig == nil {
		t.Fatalf("expected non-nil ExecutorConfig")
	}
}

// TestLoadSkillWithExecutorType 验证 executor_type 和 executor_config 字段可正确解析。
func TestLoadSkillWithExecutorType(t *testing.T) {
	dir := t.TempDir()
	writeSkillFiles(t, dir, "test_exec",
		"id: test_exec\nversion: \"1\"\ntitle: Test\nexecutor_type: task.classify_route\nexecutor_config:\n  check_in_every: 5\n",
		"# prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("test_exec")
	if !ok {
		t.Fatalf("expected test_exec skill loaded")
	}
	if def.ExecutorType != "task.classify_route" {
		t.Fatalf("expected executor_type 'task.classify_route', got %q", def.ExecutorType)
	}
	if def.ExecutorConfig["check_in_every"] == nil {
		t.Fatalf("expected executor_config.check_in_every to be set")
	}
}

// TestLoadSkillInvalidExecutorType 验证非法 executor_type 返回错误。
func TestLoadSkillInvalidExecutorType(t *testing.T) {
	dir := t.TempDir()
	writeSkillFiles(t, dir, "bad_exec",
		"id: bad_exec\nversion: \"1\"\ntitle: Bad\nexecutor_type: \"!!invalid type!!\"\n",
		"# prompt",
	)

	_, err := LoadRegistry(dir)
	if err == nil {
		t.Fatal("expected error for invalid executor_type, got nil")
	}
}

func writeSkillFiles(t *testing.T, root, name, yamlContent, promptContent string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile skill.yaml failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte(promptContent), 0644); err != nil {
		t.Fatalf("WriteFile prompt.md failed: %v", err)
	}
}

// TestLoadSkillWithPreferredRouteID 验证 preferred_route_id 字段可正确解析。
func TestLoadSkillWithPreferredRouteID(t *testing.T) {
	dir := t.TempDir()
	writeSkillFiles(t, dir, "test_route",
		"id: test_route\nversion: \"1\"\ntitle: Test\npreferred_route_id: anthropic-opus\n",
		"# prompt",
	)

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	def, ok := registry.Get("test_route")
	if !ok {
		t.Fatalf("expected test_route skill loaded")
	}
	if def.PreferredRouteID == nil {
		t.Fatal("expected PreferredRouteID to be set")
	}
	if *def.PreferredRouteID != "anthropic-opus" {
		t.Fatalf("expected preferred_route_id 'anthropic-opus', got %q", *def.PreferredRouteID)
	}
}

// TestLoadSkillWithoutPreferredRouteID 验证无 preferred_route_id 字段时 PreferredRouteID 为 nil。
func TestLoadSkillWithoutPreferredRouteID(t *testing.T) {
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
	if def.PreferredRouteID != nil {
		t.Fatalf("expected PreferredRouteID nil, got %q", *def.PreferredRouteID)
	}
}

