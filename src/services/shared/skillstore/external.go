package skillstore

import (
	"os"
	"path/filepath"
	"strings"
)

// ExternalSkill 描述一个从外部目录发现的技能。
type ExternalSkill struct {
	Name            string `json:"name"`
	Path            string `json:"path"`
	InstructionPath string `json:"instruction_path"`
}

// DiscoverExternalSkills 扫描给定目录列表，发现包含 SKILL.md 的子目录。
// 每个包含 SKILL.md 的直接子目录被视为一个外部技能。
func DiscoverExternalSkills(dirs []string) []ExternalSkill {
	var skills []ExternalSkill
	seen := make(map[string]struct{})
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			skillPath := filepath.Join(dir, entry.Name())
			absPath, err := filepath.Abs(skillPath)
			if err != nil {
				continue
			}
			if _, ok := seen[absPath]; ok {
				continue
			}
			if _, err := os.Stat(filepath.Join(skillPath, InstructionPathDefault)); err != nil {
				continue
			}
			seen[absPath] = struct{}{}
			skills = append(skills, ExternalSkill{
				Name:            entry.Name(),
				Path:            absPath,
				InstructionPath: InstructionPathDefault,
			})
		}
	}
	return skills
}

// WellKnownSkillDirs 返回常见的外部技能目录路径（仅当目录存在时才包含）。
func WellKnownSkillDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	candidates := []string{
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".config", "alma", "skills"),
	}
	var dirs []string
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}
