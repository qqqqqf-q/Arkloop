package pipeline

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/shared/skillstore"
	"arkloop/services/worker/internal/data"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fakeSkillResolver struct {
	skills []skillstore.ResolvedSkill
}

func (f fakeSkillResolver) ResolveEnabledSkills(_ context.Context, _ *pgxpool.Pool, _ uuid.UUID, _ string, _ string) ([]skillstore.ResolvedSkill, error) {
	return append([]skillstore.ResolvedSkill(nil), f.skills...), nil
}

func TestSkillContextMiddlewareInjectsPromptAndContext(t *testing.T) {
	mw := NewSkillContextMiddleware(&pgxpool.Pool{}, fakeSkillResolver{skills: []skillstore.ResolvedSkill{{
		SkillKey:        "grep-helper",
		Version:         "1",
		MountPath:       skillstore.MountPath("grep-helper", "1"),
		InstructionPath: skillstore.InstructionPathDefault,
	}}})
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
	if !strings.Contains(rc.SystemPrompt, skillstore.IndexPath) {
		t.Fatalf("expected skill index path in prompt, got %q", rc.SystemPrompt)
	}
	if !strings.Contains(rc.SystemPrompt, "grep-helper@1") {
		t.Fatalf("expected skill identifier in prompt, got %q", rc.SystemPrompt)
	}
}
