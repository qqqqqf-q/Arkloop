package pipeline

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/shared/skillstore"
	"arkloop/services/worker/internal/data"
	"github.com/google/uuid"
)

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
	rc := &RunContext{Run: data.Run{AccountID: uuid.New()}, ProfileRef: "pref_test", WorkspaceRef: "wsref_test", SystemPrompt: "base"}
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
	if !strings.Contains(rc.SystemPrompt, layout.IndexPath) {
		t.Fatalf("expected skill index path in prompt, got %q", rc.SystemPrompt)
	}
	if !strings.Contains(rc.SystemPrompt, "grep-helper@1") {
		t.Fatalf("expected skill identifier in prompt, got %q", rc.SystemPrompt)
	}
}

func TestSkillContextMiddlewareSkipsWhenResolverMissing(t *testing.T) {
	mw := NewSkillContextMiddleware(SkillContextConfig{})
	rc := &RunContext{Run: data.Run{AccountID: uuid.New()}, SystemPrompt: "base"}
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
	rc := &RunContext{Run: data.Run{ID: uuid.New(), AccountID: uuid.New()}, SystemPrompt: "base"}
	if err := mw(context.Background(), rc, func(ctx context.Context, rc *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}
	if !resolved {
		t.Fatal("expected layout resolver to be called with run context")
	}
	if got := rc.EnabledSkills[0].MountPath; got != layout.MountPath("grep-helper", "1") {
		t.Fatalf("expected dynamic layout mount path, got %q", got)
	}
	if !strings.Contains(rc.SystemPrompt, layout.IndexPath) {
		t.Fatalf("expected dynamic layout index path in prompt, got %q", rc.SystemPrompt)
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
	rc := &RunContext{Run: data.Run{AccountID: uuid.New()}, SystemPrompt: "base"}
	if err := mw(context.Background(), rc, func(ctx context.Context, rc *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware failed: %v", err)
	}
	if len(rc.EnabledSkills) != 2 {
		t.Fatalf("expected both auto and manual skills in context, got %#v", rc.EnabledSkills)
	}
	if !strings.Contains(rc.SystemPrompt, "builtin-auto@1") {
		t.Fatalf("expected auto skill in prompt, got %q", rc.SystemPrompt)
	}
	if strings.Contains(rc.SystemPrompt, "builtin-manual@1") {
		t.Fatalf("expected manual skill omitted from prompt, got %q", rc.SystemPrompt)
	}
}
