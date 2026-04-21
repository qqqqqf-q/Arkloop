package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arkloop/services/shared/skillstore"
	"arkloop/services/worker/internal/data"
	"github.com/google/uuid"
)

func newSkillTestRunContext() *RunContext {
	rc := &RunContext{
		Run: data.Run{AccountID: uuid.New()},
		PromptAssembly: PromptAssembly{
			Segments: []PromptSegment{{
				Name:      "test.system",
				Target:    PromptTargetSystemPrefix,
				Role:      "system",
				Text:      "base",
				Stability: PromptStabilityStablePrefix,
			}},
		},
	}
	rc.SystemPrompt = rc.MaterializedSystemPrompt()
	return rc
}

func TestSkillContextMiddlewareInjectsPromptAndContext(t *testing.T) {
	layout := skillstore.PathLayout{
		MountRoot: "/tmp/skills",
		IndexPath: "/tmp/enabled-skills.json",
	}
	mw := NewSkillContextMiddleware(SkillContextConfig{
		Resolve: func(_ context.Context, _ uuid.UUID, _ string, _ string) ([]skillstore.ResolvedSkill, error) {
			return []skillstore.ResolvedSkill{{
				SkillKey:        "grep-helper",
				Version:         "1",
				InstructionPath: skillstore.InstructionPathDefault,
				AutoInject:      true,
			}}, nil
		},
		Layout: layout,
	})
	rc := newSkillTestRunContext()
	rc.ProfileRef = "pref_test"
	rc.WorkspaceRef = "wsref_test"
	called := false
	err := mw(context.Background(), rc, func(ctx context.Context, rc *RunContext) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("middleware failed: %v", err)
	}
	if !called {
		t.Fatal("expected next handler called")
	}
	if len(rc.EnabledSkills) != 1 {
		t.Fatalf("expected enabled skills injected, got %#v", rc.EnabledSkills)
	}
	if rc.EnabledSkills[0].MountPath != layout.MountPath("grep-helper", "1") {
		t.Fatalf("expected layout mount path applied, got %#v", rc.EnabledSkills[0])
	}
	if strings.Contains(rc.SystemPrompt, layout.IndexPath) {
		t.Fatalf("did not expect skill index path in prompt, got %q", rc.SystemPrompt)
	}
	if !strings.Contains(rc.SystemPrompt, "<available_skills>") {
		t.Fatalf("expected available skills block in prompt, got %q", rc.SystemPrompt)
	}
	if !strings.Contains(rc.SystemPrompt, "grep-helper@1 (enabled)") {
		t.Fatalf("expected enabled skill listing in prompt, got %q", rc.SystemPrompt)
	}
}

func TestSkillContextMiddlewareSkipsWhenResolverMissing(t *testing.T) {
	mw := NewSkillContextMiddleware(SkillContextConfig{})
	rc := newSkillTestRunContext()
	if err := mw(context.Background(), rc, func(ctx context.Context, rc *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}
	if rc.SystemPrompt != "base" {
		t.Fatalf("expected prompt unchanged, got %q", rc.SystemPrompt)
	}
}

func TestSkillContextMiddlewareUsesLayoutResolver(t *testing.T) {
	layout := skillstore.PathLayout{
		MountRoot: "/tmp/run-skills/files",
		IndexPath: "/tmp/run-skills/enabled-skills.json",
	}
	resolved := false
	mw := NewSkillContextMiddleware(SkillContextConfig{
		Resolve: func(_ context.Context, _ uuid.UUID, _ string, _ string) ([]skillstore.ResolvedSkill, error) {
			return []skillstore.ResolvedSkill{{
				SkillKey:   "grep-helper",
				Version:    "1",
				AutoInject: true,
			}}, nil
		},
		LayoutResolver: func(_ context.Context, rc *RunContext) (skillstore.PathLayout, error) {
			resolved = rc.Run.ID != uuid.Nil
			return layout, nil
		},
	})
	rc := newSkillTestRunContext()
	rc.Run.ID = uuid.New()
	if err := mw(context.Background(), rc, func(ctx context.Context, rc *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}
	if !resolved {
		t.Fatal("expected layout resolver to be called with run context")
	}
	if got := rc.EnabledSkills[0].MountPath; got != layout.MountPath("grep-helper", "1") {
		t.Fatalf("expected dynamic layout mount path, got %q", got)
	}
	if strings.Contains(rc.SystemPrompt, layout.IndexPath) {
		t.Fatalf("did not expect dynamic layout index path in prompt, got %q", rc.SystemPrompt)
	}
}

func TestSkillContextMiddlewareKeepsManualSkillsOutOfPrompt(t *testing.T) {
	layout := skillstore.PathLayout{
		MountRoot: "/tmp/skills",
		IndexPath: "/tmp/enabled-skills.json",
	}
	mw := NewSkillContextMiddleware(SkillContextConfig{
		Resolve: func(_ context.Context, _ uuid.UUID, _ string, _ string) ([]skillstore.ResolvedSkill, error) {
			return []skillstore.ResolvedSkill{
				{
					SkillKey:   "builtin-auto",
					Version:    "1",
					AutoInject: true,
				},
				{
					SkillKey:   "builtin-manual",
					Version:    "1",
					AutoInject: false,
				},
			}, nil
		},
		Layout: layout,
	})
	rc := newSkillTestRunContext()
	if err := mw(context.Background(), rc, func(ctx context.Context, rc *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}
	if len(rc.EnabledSkills) != 2 {
		t.Fatalf("expected both auto and manual skills in context, got %#v", rc.EnabledSkills)
	}
	if !strings.Contains(rc.SystemPrompt, "builtin-auto@1 (enabled)") {
		t.Fatalf("expected auto skill in prompt, got %q", rc.SystemPrompt)
	}
	if !strings.Contains(rc.SystemPrompt, "builtin-manual@1 (enabled)") {
		t.Fatalf("expected manual skill listed in prompt, got %q", rc.SystemPrompt)
	}
}

func TestSkillContextMiddlewareExternalSkillsEmptyDirsNoEffect(t *testing.T) {
	emptyRoot := t.TempDir() // 目录存在但无任何 skill 子目录

	mw := NewSkillContextMiddleware(SkillContextConfig{
		ExternalDirs: func(_ context.Context) []string {
			return []string{emptyRoot}
		},
	})
	rc := newSkillTestRunContext()
	if err := mw(context.Background(), rc, func(ctx context.Context, rc *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}
	if rc.SystemPrompt != "base" {
		t.Fatalf("expected prompt unchanged when external dirs are empty, got %q", rc.SystemPrompt)
	}
}

func TestSkillContextMiddlewareExternalSkills(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "test-external")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# External test skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	layout := skillstore.PathLayout{MountRoot: "/tmp/skills", IndexPath: "/tmp/index.json"}
	mw := NewSkillContextMiddleware(SkillContextConfig{
		Layout: layout,
		ExternalDirs: func(_ context.Context) []string {
			return []string{root}
		},
	})
	rc := newSkillTestRunContext()
	if err := mw(context.Background(), rc, func(ctx context.Context, rc *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}
	if !strings.Contains(rc.SystemPrompt, "<available_skills>") {
		t.Fatalf("expected available skills block in prompt, got: %s", rc.SystemPrompt)
	}
	if !strings.Contains(rc.SystemPrompt, "test-external") || !strings.Contains(rc.SystemPrompt, "(external)") {
		t.Fatalf("expected 'test-external' in prompt, got: %s", rc.SystemPrompt)
	}
	if len(rc.ExternalSkills) != 1 {
		t.Fatalf("expected external skills in context, got %#v", rc.ExternalSkills)
	}
}

func TestSkillContextMiddlewareSkipsStickerRegisterRuns(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "test-external")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# External test skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	mw := NewSkillContextMiddleware(SkillContextConfig{
		Resolve: func(_ context.Context, _ uuid.UUID, _ string, _ string) ([]skillstore.ResolvedSkill, error) {
			return []skillstore.ResolvedSkill{{
				SkillKey:   "grep-helper",
				Version:    "1",
				AutoInject: true,
			}}, nil
		},
		ExternalDirs: func(_ context.Context) []string {
			return []string{root}
		},
	})
	rc := newSkillTestRunContext()
	rc.EnabledSkills = []skillstore.ResolvedSkill{{SkillKey: "old", Version: "1"}}
	rc.ExternalSkills = []skillstore.ExternalSkill{{Name: "old"}}
	rc.InputJSON = map[string]any{"run_kind": "sticker_register"}
	if err := mw(context.Background(), rc, func(ctx context.Context, rc *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}
	if len(rc.EnabledSkills) != 0 {
		t.Fatalf("expected enabled skills cleared, got %#v", rc.EnabledSkills)
	}
	if len(rc.ExternalSkills) != 0 {
		t.Fatalf("expected external skills cleared, got %#v", rc.ExternalSkills)
	}
	if strings.Contains(rc.SystemPrompt, "<available_skills>") {
		t.Fatalf("did not expect available skills prompt for sticker run, got %q", rc.SystemPrompt)
	}
}
