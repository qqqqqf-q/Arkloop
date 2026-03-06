package personas

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// RepoPersona 表示从 src/personas/ 目录加载的仓库 persona。
type RepoPersona struct {
	ID              string            `yaml:"id"`
	Version         string            `yaml:"version"`
	Title           string            `yaml:"title"`
	Description     string            `yaml:"description"`
	ToolAllowlist   []string          `yaml:"tool_allowlist"`
	ToolDenylist    []string          `yaml:"tool_denylist"`
	Budgets         map[string]any    `yaml:"budgets"`
	AgentConfigName string            `yaml:"agent_config"`
	ExecutorType    string            `yaml:"executor_type"`
	ExecutorConfig  map[string]any    `yaml:"executor_config"`
	PromptMD        string            `yaml:"-"`
}

// LoadFromDir 扫描指定目录下的所有 persona 子目录，读取 persona.yaml 和 prompt.md。
// 如果目录不存在或无有效 persona，返回空 slice（不报错）。
func LoadFromDir(root string) ([]RepoPersona, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var result []RepoPersona
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		dir := filepath.Join(root, entry.Name())
		yamlPath := filepath.Join(dir, "persona.yaml")
		promptPath := filepath.Join(dir, "prompt.md")

		yamlData, err := os.ReadFile(yamlPath)
		if err != nil {
			continue
		}

		var p RepoPersona
		if err := yaml.Unmarshal(yamlData, &p); err != nil {
			continue
		}
		if p.ID == "" {
			continue
		}
		if p.Version == "" {
			p.Version = "1"
		}

		if promptData, err := os.ReadFile(promptPath); err == nil {
			p.PromptMD = string(promptData)
		}

		result = append(result, p)
	}
	return result, nil
}
