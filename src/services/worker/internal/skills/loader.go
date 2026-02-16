package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	idRegex     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
	budgetKeys  = map[string]struct{}{"max_iterations": {}, "max_output_tokens": {}, "tool_timeout_ms": {}, "tool_budget": {}}
)

func BuiltinSkillsRoot() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("无法定位 skills 根目录")
	}
	dir := filepath.Dir(filename)
	for {
		if filepath.Base(dir) == "src" {
			return filepath.Join(dir, "skills"), nil
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	return "", fmt.Errorf("未找到 src 目录，无法定位 skills 根目录")
}

func LoadRegistry(root string) (*Registry, error) {
	registry := NewRegistry()
	if strings.TrimSpace(root) == "" {
		return registry, nil
	}

	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return registry, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("skills 根目录必须为目录")
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	sort.Strings(dirs)

	for _, name := range dirs {
		dir := filepath.Join(root, name)
		yamlPath := filepath.Join(dir, "skill.yaml")
		promptPath := filepath.Join(dir, "prompt.md")
		if _, err := os.Stat(yamlPath); err != nil {
			continue
		}
		if _, err := os.Stat(promptPath); err != nil {
			continue
		}

		def, err := loadSingleSkill(yamlPath, promptPath)
		if err != nil {
			return nil, err
		}
		if err := registry.Register(def); err != nil {
			return nil, err
		}
	}

	return registry, nil
}

func loadSingleSkill(yamlPath string, promptPath string) (Definition, error) {
	rawYAML, err := os.ReadFile(yamlPath)
	if err != nil {
		return Definition{}, err
	}
	trimmed := strings.TrimSpace(string(rawYAML))
	if trimmed == "" {
		return Definition{}, fmt.Errorf("skill.yaml 不能为空: %s", yamlPath)
	}

	var obj map[string]any
	if err := yaml.Unmarshal([]byte(trimmed), &obj); err != nil {
		return Definition{}, fmt.Errorf("解析 skill.yaml 失败: %s", yamlPath)
	}

	skillID, err := asID(obj["id"], "id")
	if err != nil {
		return Definition{}, err
	}
	version, err := asID(obj["version"], "version")
	if err != nil {
		return Definition{}, err
	}
	title, err := asNonEmptyString(obj["title"], "title")
	if err != nil {
		return Definition{}, err
	}

	description := asOptionalString(obj["description"])
	allowlist, err := asToolAllowlist(obj["tool_allowlist"])
	if err != nil {
		return Definition{}, err
	}
	budgets, err := asBudgets(obj["budgets"])
	if err != nil {
		return Definition{}, err
	}

	rawPrompt, err := os.ReadFile(promptPath)
	if err != nil {
		return Definition{}, err
	}
	prompt := strings.TrimSpace(string(rawPrompt))
	if prompt == "" {
		return Definition{}, fmt.Errorf("prompt.md 不能为空: %s", promptPath)
	}

	return Definition{
		ID:            skillID,
		Version:       version,
		Title:         title,
		Description:   description,
		ToolAllowlist: allowlist,
		Budgets:       budgets,
		PromptMD:      prompt,
	}, nil
}

func asNonEmptyString(value any, label string) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s 必须为字符串", label)
	}
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return "", fmt.Errorf("%s 不能为空", label)
	}
	return cleaned, nil
}

func asOptionalString(value any) *string {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func asID(value any, label string) (string, error) {
	cleaned, err := asNonEmptyString(value, label)
	if err != nil {
		return "", err
	}
	if !idRegex.MatchString(cleaned) {
		return "", fmt.Errorf("%s 不合法: %s", label, cleaned)
	}
	return cleaned, nil
}

func asToolAllowlist(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	rawList, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("tool_allowlist 必须为数组")
	}
	seen := map[string]struct{}{}
	out := []string{}
	for idx, item := range rawList {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("tool_allowlist[%d] 必须为字符串", idx)
		}
		cleaned := strings.TrimSpace(text)
		if cleaned == "" {
			return nil, fmt.Errorf("tool_allowlist[%d] 不能为空", idx)
		}
		if !idRegex.MatchString(cleaned) {
			return nil, fmt.Errorf("tool_allowlist[%d] 不合法: %s", idx, cleaned)
		}
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	return out, nil
}

func asBudgets(value any) (Budgets, error) {
	if value == nil {
		return Budgets{ToolBudget: map[string]any{}}, nil
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return Budgets{}, fmt.Errorf("budgets 必须为对象")
	}

	for key := range raw {
		if _, allowed := budgetKeys[key]; allowed {
			continue
		}
		return Budgets{}, fmt.Errorf("budgets 包含不支持字段: %s", key)
	}

	maxIterations, err := asOptionalPositiveInt(raw["max_iterations"], "budgets.max_iterations")
	if err != nil {
		return Budgets{}, err
	}
	maxOutputTokens, err := asOptionalPositiveInt(raw["max_output_tokens"], "budgets.max_output_tokens")
	if err != nil {
		return Budgets{}, err
	}
	toolTimeoutMs, err := asOptionalPositiveInt(raw["tool_timeout_ms"], "budgets.tool_timeout_ms")
	if err != nil {
		return Budgets{}, err
	}

	toolBudget := map[string]any{}
	if rawBudget, ok := raw["tool_budget"]; ok && rawBudget != nil {
		mapped, ok := rawBudget.(map[string]any)
		if !ok {
			return Budgets{}, fmt.Errorf("budgets.tool_budget 必须为对象")
		}
		for key, value := range mapped {
			toolBudget[key] = value
		}
	}

	return Budgets{
		MaxIterations:   maxIterations,
		MaxOutputTokens: maxOutputTokens,
		ToolTimeoutMs:   toolTimeoutMs,
		ToolBudget:      toolBudget,
	}, nil
}

func asOptionalPositiveInt(value any, label string) (*int, error) {
	if value == nil {
		return nil, nil
	}
	number, ok := value.(int)
	if !ok {
		return nil, fmt.Errorf("%s 必须为整数", label)
	}
	if number <= 0 {
		return nil, fmt.Errorf("%s 必须为正整数", label)
	}
	out := int(number)
	return &out, nil
}

