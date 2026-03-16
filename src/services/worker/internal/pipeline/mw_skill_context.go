package pipeline

import (
	"context"
	"fmt"
	"strings"

	"arkloop/services/shared/skillstore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SkillResolver interface {
	ResolveEnabledSkills(ctx context.Context, pool *pgxpool.Pool, accountID uuid.UUID, profileRef, workspaceRef string) ([]skillstore.ResolvedSkill, error)
}

func NewSkillContextMiddleware(pool *pgxpool.Pool, resolver SkillResolver) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if resolver == nil {
			resolver = defaultSkillResolver()
		}
		if pool == nil || resolver == nil || rc.Run.AccountID == uuid.Nil || strings.TrimSpace(rc.ProfileRef) == "" || strings.TrimSpace(rc.WorkspaceRef) == "" {
			return next(ctx, rc)
		}
		skills, err := resolver.ResolveEnabledSkills(ctx, pool, rc.Run.AccountID, rc.ProfileRef, rc.WorkspaceRef)
		if err != nil {
			return fmt.Errorf("resolve enabled skills: %w", err)
		}
		rc.EnabledSkills = append([]skillstore.ResolvedSkill(nil), skills...)
		if block := buildSkillPromptBlock(skills); block != "" {
			rc.SystemPrompt += block
		}
		return next(ctx, rc)
	}
}

func buildSkillPromptBlock(skills []skillstore.ResolvedSkill) string {
	if len(skills) == 0 {
		return ""
	}
	var autoSkills []skillstore.ResolvedSkill
	for _, s := range skills {
		if s.AutoInject {
			autoSkills = append(autoSkills, s)
		}
	}
	if len(autoSkills) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n<skills>\n")
	sb.WriteString("- Enabled skill index: ")
	sb.WriteString(skillstore.IndexPath)
	sb.WriteString("\n")
	sb.WriteString("- Skill files are mounted read-only under /opt/arkloop/skills. Read the relevant SKILL.md before using a skill.\n")
	for _, item := range autoSkills {
		sb.WriteString("- ")
		sb.WriteString(strings.TrimSpace(item.SkillKey))
		sb.WriteString("@")
		sb.WriteString(strings.TrimSpace(item.Version))
		sb.WriteString(" -> ")
		sb.WriteString(strings.TrimSpace(item.MountPath))
		instructionPath := strings.TrimSpace(item.InstructionPath)
		if instructionPath == "" {
			instructionPath = skillstore.InstructionPathDefault
		}
		sb.WriteString("/")
		sb.WriteString(instructionPath)
		sb.WriteString("\n")
	}
	sb.WriteString("</skills>")
	return sb.String()
}
