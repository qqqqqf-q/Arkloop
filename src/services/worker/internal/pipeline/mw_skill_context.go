package pipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"arkloop/services/shared/skillstore"
	"github.com/google/uuid"
)

type SkillResolver func(ctx context.Context, accountID uuid.UUID, profileRef, workspaceRef string) ([]skillstore.ResolvedSkill, error)

type SkillPreparer func(ctx context.Context, skills []skillstore.ResolvedSkill, layout skillstore.PathLayout) error

type SkillLayoutResolver func(ctx context.Context, rc *RunContext) (skillstore.PathLayout, error)

type ExternalSkillDirsResolver func(ctx context.Context) []string

const (
	skillListingBudgetChars  = 3000
	skillListingDescMaxChars = 96
)

type SkillContextConfig struct {
	Resolve        SkillResolver
	Prepare        SkillPreparer
	Layout         skillstore.PathLayout
	LayoutResolver SkillLayoutResolver
	ExternalDirs   ExternalSkillDirsResolver
}

func NewSkillContextMiddleware(cfg SkillContextConfig) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		rc.RemovePromptSegment("skills.available")
		if isStickerRegisterRun(rc) {
			rc.EnabledSkills = nil
			rc.ExternalSkills = nil
			return next(ctx, rc)
		}
		var externalSkills []skillstore.ExternalSkill
		if cfg.ExternalDirs != nil {
			externalSkills = skillstore.DiscoverExternalSkills(cfg.ExternalDirs(ctx))
			rc.ExternalSkills = append([]skillstore.ExternalSkill(nil), externalSkills...)
		}
		if cfg.Resolve == nil || rc.Run.AccountID == uuid.Nil {
			if block := buildAvailableSkillsPromptBlock(nil, externalSkills); block != "" {
				rc.UpsertPromptSegment(PromptSegment{
					Name:          "skills.available",
					Target:        PromptTargetSystemPrefix,
					Role:          "system",
					Text:          block,
					Stability:     PromptStabilitySessionPrefix,
					CacheEligible: true,
				})
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
		if block := buildAvailableSkillsPromptBlock(skills, externalSkills); block != "" {
			rc.UpsertPromptSegment(PromptSegment{
				Name:          "skills.available",
				Target:        PromptTargetSystemPrefix,
				Role:          "system",
				Text:          block,
				Stability:     PromptStabilitySessionPrefix,
				CacheEligible: true,
			})
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

func buildAvailableSkillsPromptBlock(enabled []skillstore.ResolvedSkill, external []skillstore.ExternalSkill) string {
	lines := skillListingLines(enabled, external)
	if len(lines) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n<available_skills>\n")
	sb.WriteString("Use load_skill with the exact skill name below before relying on a skill.\n")
	for _, line := range lines {
		sb.WriteString("- ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("</available_skills>")
	return sb.String()
}

func skillListingLines(enabled []skillstore.ResolvedSkill, external []skillstore.ExternalSkill) []string {
	lines := make([]string, 0, len(enabled)+len(external))
	for _, item := range enabled {
		lines = append(lines, formatSkillListingLine(
			formatEnabledSkillName(item),
			firstNonEmpty(strings.TrimSpace(item.Description), strings.TrimSpace(item.DisplayName)),
			"enabled",
		))
	}
	for _, item := range external {
		lines = append(lines, formatSkillListingLine(
			strings.TrimSpace(item.Name),
			strings.TrimSpace(item.Description),
			"external",
		))
	}
	sort.SliceStable(lines, func(i, j int) bool {
		leftAuto := strings.Contains(lines[i], "(enabled)")
		rightAuto := strings.Contains(lines[j], "(enabled)")
		if leftAuto != rightAuto {
			return leftAuto
		}
		return lines[i] < lines[j]
	})
	return trimSkillListing(lines)
}

func formatSkillListingLine(name, description, source string) string {
	label := name
	desc := truncateSkillDescription(description)
	if desc == "" {
		if source != "" {
			return label + " (" + source + ")"
		}
		return label
	}
	if source != "" {
		desc += " (" + source + ")"
	}
	return label + ": " + desc
}

func formatEnabledSkillName(item skillstore.ResolvedSkill) string {
	key := strings.TrimSpace(item.SkillKey)
	version := strings.TrimSpace(item.Version)
	if key == "" || version == "" {
		return key
	}
	return key + "@" + version
}

func trimSkillListing(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	total := 0
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		width := len(line) + 1
		if total+width > skillListingBudgetChars {
			break
		}
		out = append(out, line)
		total += width
	}
	return out
}

func truncateSkillDescription(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= skillListingDescMaxChars {
		return value
	}
	return string(runes[:skillListingDescMaxChars-1]) + "…"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
