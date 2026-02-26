package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
)

var (
	idRegex    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
	budgetKeys = map[string]struct{}{"max_iterations": {}, "max_output_tokens": {}, "tool_timeout_ms": {}, "tool_budget": {}}
)

const defaultExecutorType = "agent.simple"

func BuiltinSkillsRoot() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate skills root directory")
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
	return "", fmt.Errorf("src directory not found, cannot locate skills root directory")
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
		return nil, fmt.Errorf("skills root must be a directory")
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
		return Definition{}, fmt.Errorf("skill.yaml must not be empty: %s", yamlPath)
	}

	var obj map[string]any
	if err := yaml.Unmarshal([]byte(trimmed), &obj); err != nil {
		return Definition{}, fmt.Errorf("failed to parse skill.yaml: %s", yamlPath)
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

	executorType := defaultExecutorType
	if raw := asOptionalString(obj["executor_type"]); raw != nil {
		if !idRegex.MatchString(*raw) {
			return Definition{}, fmt.Errorf("executor_type is invalid: %s", *raw)
		}
		executorType = *raw
	}
	executorConfig := asOptionalMap(obj["executor_config"])
	preferredCredential := asOptionalString(obj["preferred_credential"])

	rawPrompt, err := os.ReadFile(promptPath)
	if err != nil {
		return Definition{}, err
	}
	prompt := strings.TrimSpace(string(rawPrompt))
	if prompt == "" {
		return Definition{}, fmt.Errorf("prompt.md must not be empty: %s", promptPath)
	}

	return Definition{
		ID:               skillID,
		Version:          version,
		Title:            title,
		Description:      description,
		ToolAllowlist:    allowlist,
		Budgets:          budgets,
		PromptMD:         prompt,
		ExecutorType:     executorType,
		ExecutorConfig:   executorConfig,
		PreferredCredential: preferredCredential,
	}, nil
}

func asNonEmptyString(value any, label string) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", label)
	}
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return "", fmt.Errorf("%s must not be empty", label)
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

func asOptionalMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	m, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

func asID(value any, label string) (string, error) {
	cleaned, err := asNonEmptyString(value, label)
	if err != nil {
		return "", err
	}
	if !idRegex.MatchString(cleaned) {
		return "", fmt.Errorf("%s is invalid: %s", label, cleaned)
	}
	return cleaned, nil
}

func asToolAllowlist(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	rawList, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("tool_allowlist must be an array")
	}
	seen := map[string]struct{}{}
	out := []string{}
	for idx, item := range rawList {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("tool_allowlist[%d] must be a string", idx)
		}
		cleaned := strings.TrimSpace(text)
		if cleaned == "" {
			return nil, fmt.Errorf("tool_allowlist[%d] must not be empty", idx)
		}
		if !idRegex.MatchString(cleaned) {
			return nil, fmt.Errorf("tool_allowlist[%d] is invalid: %s", idx, cleaned)
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
		return Budgets{}, fmt.Errorf("budgets must be an object")
	}

	for key := range raw {
		if _, allowed := budgetKeys[key]; allowed {
			continue
		}
		return Budgets{}, fmt.Errorf("budgets contains unsupported field: %s", key)
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
			return Budgets{}, fmt.Errorf("budgets.tool_budget must be an object")
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

// LoadFromDB 从数据库加载 orgID 对应的活跃 skill，用于 per-run 动态覆盖。
func LoadFromDB(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID) ([]Definition, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}

	rows, err := pool.Query(
		ctx,
		`SELECT skill_key, version, display_name, description,
		        prompt_md, tool_allowlist, budgets_json,
		        executor_type, executor_config_json,
		        preferred_credential
		 FROM skills
		 WHERE org_id = $1 AND is_active = TRUE
		 ORDER BY created_at ASC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var defs []Definition
	for rows.Next() {
		var (
			skillKey              string
			version               string
			displayName           string
			description           *string
			promptMD              string
			toolAllowlist         []string
			budgetsRaw            []byte
			executorType          string
			executorConfigRaw     []byte
			preferredCredential   *string
		)
		if err := rows.Scan(&skillKey, &version, &displayName, &description,
			&promptMD, &toolAllowlist, &budgetsRaw,
			&executorType, &executorConfigRaw, &preferredCredential); err != nil {
			return nil, err
		}

		budgets, err := parseBudgetsJSON(budgetsRaw)
		if err != nil {
			return nil, fmt.Errorf("skill %q: %w", skillKey, err)
		}

		executorConfig, err := parseExecutorConfigJSON(executorConfigRaw)
		if err != nil {
			return nil, fmt.Errorf("skill %q executor_config_json: %w", skillKey, err)
		}

		if strings.TrimSpace(executorType) == "" {
			executorType = defaultExecutorType
		}

		def := Definition{
			ID:               skillKey,
			Version:          version,
			Title:            displayName,
			ToolAllowlist:    toolAllowlist,
			Budgets:          budgets,
			PromptMD:         promptMD,
			ExecutorType:     executorType,
			ExecutorConfig:   executorConfig,
			PreferredCredential: preferredCredential,
		}
		if description != nil && strings.TrimSpace(*description) != "" {
			s := strings.TrimSpace(*description)
			def.Description = &s
		}

		defs = append(defs, def)
	}
	return defs, rows.Err()
}

// MergeRegistry 以 base 为底，DB skill 按 ID 覆盖同名文件系统 skill。
func MergeRegistry(base *Registry, overrides []Definition) *Registry {
	merged := NewRegistry()
	for _, id := range base.ListIDs() {
		def, _ := base.Get(id)
		merged.Set(def)
	}
	for _, def := range overrides {
		merged.Set(def)
	}
	return merged
}

func parseBudgetsJSON(raw []byte) (Budgets, error) {
	if len(raw) == 0 {
		return Budgets{ToolBudget: map[string]any{}}, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return Budgets{}, fmt.Errorf("invalid budgets_json: %w", err)
	}
	return asBudgets(obj)
}

func parseExecutorConfigJSON(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("invalid executor_config_json: %w", err)
	}
	return obj, nil
}

func asOptionalPositiveInt(value any, label string) (*int, error) {
	if value == nil {
		return nil, nil
	}
	var number int
	switch v := value.(type) {
	case int:
		number = v
	case float64:
		// json.Unmarshal 将 map[string]any 中的数字统一解为 float64
		if v != float64(int(v)) {
			return nil, fmt.Errorf("%s must be an integer", label)
		}
		number = int(v)
	default:
		return nil, fmt.Errorf("%s must be an integer", label)
	}
	if number <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer", label)
	}
	return &number, nil
}

