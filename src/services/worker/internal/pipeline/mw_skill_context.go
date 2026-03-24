package pipeline

import (
	"context"
	"fmt"
	"strings"

	"arkloop/services/shared/skillstore"
	"github.com/google/uuid"
)

type SkillResolver func(ctx context.Context, accountID uuid.UUID, profileRef, workspaceRef string) ([]skillstore.ResolvedSkill, error)

type SkillPreparer func(ctx context.Context, skills []skillstore.ResolvedSkill, layout skillstore.PathLayout) error

type SkillLayoutResolver func(ctx context.Context, rc *RunContext) (skillstore.PathLayout, error)

type ExternalSkillDirsResolver func(ctx context.Context) []string

type SkillContextConfig struct {
	Resolve        SkillResolver
	Prepare        SkillPreparer
	Layout         skillstore.PathLayout
	LayoutResolver SkillLayoutResolver
	ExternalDirs   ExternalSkillDirsResolver
}

func NewSkillContextMiddleware(cfg SkillContextConfig) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if cfg.Resolve == nil || rc.Run.AccountID == uuid.Nil {
			if cfg.ExternalDirs != nil {
				if extSkills := skillstore.DiscoverExternalSkills(cfg.ExternalDirs(ctx)); len(extSkills) > 0 {
					rc.SystemPrompt += buildExternalSkillPromptBlock(extSkills)
				}
			}
			return next(ctx, rc)
		}
		skills, err := cfg.Resolve(ctx, rc.Run.AccountID, rc.ProfileRef, rc.WorkspaceRef)
		if err != nil {
			return fmt.Errorf("resolve enabled skills: %w", err)
		}
		layout, err := resolveSkillLayout(ctx, rc, cfg)
		if err != nil {
			return fmt.Errorf("resolve skill layout: %w", err)
		}
		skills = applySkillLayout(skills, layout)
		if cfg.Prepare != nil {
			if err := cfg.Prepare(ctx, skills, layout); err != nil {
				return fmt.Errorf("prepare enabled skills: %w", err)
			}
		}
		rc.EnabledSkills = append([]skillstore.ResolvedSkill(nil), skills...)
		if block := buildSkillPromptBlock(skills, layout); block != "" {
			rc.SystemPrompt += block
		}
		if cfg.ExternalDirs != nil {
			if extSkills := skillstore.DiscoverExternalSkills(cfg.ExternalDirs(ctx)); len(extSkills) > 0 {
				rc.SystemPrompt += buildExternalSkillPromptBlock(extSkills)
			}
		}
		return next(ctx, rc)
	}
}

func resolveSkillLayout(ctx context.Context, rc *RunContext, cfg SkillContextConfig) (skillstore.PathLayout, error) {
	if cfg.LayoutResolver != nil {
		layout, err := cfg.LayoutResolver(ctx, rc)
		if err != nil {
			return skillstore.PathLayout{}, err
		}
		return skillstore.NormalizePathLayout(layout), nil
	}
	return skillstore.NormalizePathLayout(cfg.Layout), nil
}

func applySkillLayout(skills []skillstore.ResolvedSkill, layout skillstore.PathLayout) []skillstore.ResolvedSkill {
	if len(skills) == 0 {
		return nil
	}
	out := make([]skillstore.ResolvedSkill, len(skills))
	for i, item := range skills {
		out[i] = item
		out[i].MountPath = layout.MountPath(item.SkillKey, item.Version)
	}
	return out
}

func buildExternalSkillPromptBlock(skills []skillstore.ExternalSkill) string {
	if len(skills) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n<external_skills>\n")
	sb.WriteString("- External skill directories detected. Read the SKILL.md in each directory before using.\n")
	for _, s := range skills {
		sb.WriteString("- ")
		sb.WriteString(s.Name)
		if s.Description != "" {
			sb.WriteString(": ")
			sb.WriteString(s.Description)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("</external_skills>")
	return sb.String()
}

func buildSkillPromptBlock(skills []skillstore.ResolvedSkill, layout skillstore.PathLayout) string {
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
	sb.WriteString(layout.IndexPath)
	sb.WriteString("\n")
	sb.WriteString("- Skill files are available under ")
	sb.WriteString(layout.MountRoot)
	sb.WriteString(". Read the relevant SKILL.md before using a skill.\n")
	for _, item := range autoSkills {
		sb.WriteString("- ")
		sb.WriteString(strings.TrimSpace(item.SkillKey))
		sb.WriteString("\n")
	}
	sb.WriteString("</skills>")
	return sb.String()
}
