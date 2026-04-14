package skillstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverExternalSkills(t *testing.T) {
	skills := DiscoverExternalSkills(nil)
	if len(skills) != 0 {
		t.Fatalf("expected 0 skills from nil dirs, got %d", len(skills))
	}

	root := t.TempDir()
	skillDir := filepath.Join(root, "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# test skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	noSkillDir := filepath.Join(root, "not-a-skill")
	if err := os.MkdirAll(noSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	hiddenDir := filepath.Join(root, ".hidden-skill")
	if err := os.MkdirAll(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hiddenDir, "SKILL.md"), []byte("# hidden"), 0o644); err != nil {
		t.Fatal(err)
	}

	skills = DiscoverExternalSkills([]string{root})
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "my-skill" {
		t.Errorf("expected name 'my-skill', got %q", skills[0].Name)
	}
	if skills[0].InstructionPath != "SKILL.md" {
		t.Errorf("expected instruction_path 'SKILL.md', got %q", skills[0].InstructionPath)
	}
}

func TestDiscoverExternalSkillsDedup(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "dedup-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# test"), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := DiscoverExternalSkills([]string{root, root})
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill after dedup, got %d", len(skills))
	}
}

func TestDiscoverExternalSkillsNonexistentDir(t *testing.T) {
	skills := DiscoverExternalSkills([]string{"/nonexistent/path/that/should/not/exist"})
	if len(skills) != 0 {
		t.Fatalf("expected 0 skills from nonexistent dir, got %d", len(skills))
	}
}

func TestDiscoverExternalSkillsSkipsRegularFiles(t *testing.T) {
	root := t.TempDir()

	// 在搜索根目录下放一个普通文件（非子目录），应被跳过
	if err := os.WriteFile(filepath.Join(root, "not-a-dir.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 同时放一个合法 skill 目录作为对照
	skillDir := filepath.Join(root, "real-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := DiscoverExternalSkills([]string{root})
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill (regular file skipped), got %d", len(skills))
	}
	if skills[0].Name != "real-skill" {
		t.Errorf("expected 'real-skill', got %q", skills[0].Name)
	}
}
