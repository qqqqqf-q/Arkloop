package skillstore

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// ExternalSkill 描述一个从外部目录发现的技能。
type ExternalSkill struct {
	Name            string `json:"name"`
	Path            string `json:"path"`
	InstructionPath string `json:"instruction_path"`
	Description     string `json:"description"`
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
			desc := extractSkillDescription(absPath, InstructionPathDefault)
			skills = append(skills, ExternalSkill{
				Name:            entry.Name(),
				Path:            absPath,
				InstructionPath: InstructionPathDefault,
				Description:     desc,
			})
		}
	}
	return skills
}

// extractSkillDescription 从 SKILL.md 中提取简短描述。
// 优先解析 YAML frontmatter 中的 description 字段，否则取第一个非空非标题行。
func extractSkillDescription(skillPath, instructionPath string) string {
	data, err := os.ReadFile(filepath.Join(skillPath, instructionPath))
	if err != nil {
		return ""
	}

	const maxLen = 120
	truncate := func(s string) string {
		if len(s) > maxLen {
			return s[:maxLen] + "..."
		}
		return s
	}

	// YAML frontmatter
	if bytes.HasPrefix(data, []byte("---")) {
		end := bytes.Index(data[3:], []byte("\n---"))
		if end != -1 {
			front := data[3 : 3+end]
			scanner := bufio.NewScanner(bytes.NewReader(front))
			for scanner.Scan() {
				line := scanner.Text()
				if after, ok := strings.CutPrefix(line, "description:"); ok {
					val := strings.TrimSpace(after)
					val = strings.Trim(val, "\"'")
					if val != "" {
						return truncate(val)
					}
				}
			}
		}
	}

	// 无 frontmatter，取第一个非空非标题行
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return truncate(line)
	}
	return ""
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
