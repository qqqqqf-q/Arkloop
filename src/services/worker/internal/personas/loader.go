package personas

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

	sharedexec "arkloop/services/shared/executionconfig"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
)

var (
	idRegex    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
	budgetKeys = map[string]struct{}{"reasoning_iterations": {}, "tool_continuation_budget": {}, "max_output_tokens": {}, "tool_timeout_ms": {}, "tool_budget": {}, "per_tool_soft_limits": {}, "temperature": {}, "top_p": {}}
)

const defaultExecutorType = "agent.simple"

func BuiltinPersonasRoot() (string, error) {
	if envRoot := os.Getenv("ARKLOOP_PERSONAS_ROOT"); envRoot != "" {
		return envRoot, nil
	}
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate personas root directory")
	}
	dir := filepath.Dir(filename)
	for {
		if filepath.Base(dir) == "src" {
			return filepath.Join(dir, "personas"), nil
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	return "", fmt.Errorf("src directory not found, cannot locate personas root directory")
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
		return nil, fmt.Errorf("personas root must be a directory")
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
		yamlPath := filepath.Join(dir, "persona.yaml")
		promptPath := filepath.Join(dir, "prompt.md")
		if _, err := os.Stat(yamlPath); err != nil {
			continue
		}
		if _, err := os.Stat(promptPath); err != nil {
			continue
		}

		def, err := loadSinglePersona(yamlPath, promptPath)
		if err != nil {
			return nil, err
		}
		if err := registry.Register(def); err != nil {
			return nil, err
		}
	}

	return registry, nil
}

func loadSinglePersona(yamlPath string, promptPath string) (Definition, error) {
	rawYAML, err := os.ReadFile(yamlPath)
	if err != nil {
		return Definition{}, err
	}
	trimmed := strings.TrimSpace(string(rawYAML))
	if trimmed == "" {
		return Definition{}, fmt.Errorf("persona.yaml must not be empty: %s", yamlPath)
	}

	var obj map[string]any
	if err := yaml.Unmarshal([]byte(trimmed), &obj); err != nil {
		return Definition{}, fmt.Errorf("failed to parse persona.yaml: %s", yamlPath)
	}

	personaID, err := asID(obj["id"], "id")
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
	userSelectable := asOptionalBool(obj["user_selectable"])
	selectorName := asOptionalString(obj["selector_name"])
	selectorOrder, err := asOptionalInt(obj["selector_order"], "selector_order")
	if err != nil {
		return Definition{}, err
	}
	allowlist, err := asToolNameList(obj["tool_allowlist"], "tool_allowlist")
	if err != nil {
		return Definition{}, err
	}
	denylist, err := asToolNameList(obj["tool_denylist"], "tool_denylist")
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
	executorConfig, err := parseExecutorConfig(obj["executor_config"], executorType, filepath.Dir(yamlPath))
	if err != nil {
		return Definition{}, err
	}
	preferredCredential := asOptionalString(obj["preferred_credential"])
	agentConfigName := asOptionalString(obj["agent_config"])
	titleSummarizer, err := asTitleSummarizer(obj["title_summarize"])
	if err != nil {
		return Definition{}, err
	}

	rawPrompt, err := os.ReadFile(promptPath)
	if err != nil {
		return Definition{}, err
	}
	prompt := strings.TrimSpace(string(rawPrompt))
	if prompt == "" {
		return Definition{}, fmt.Errorf("prompt.md must not be empty: %s", promptPath)
	}

	return Definition{
		ID:                  personaID,
		Version:             version,
		Title:               title,
		Description:         description,
		UserSelectable:      userSelectable,
		SelectorName:        selectorName,
		SelectorOrder:       selectorOrder,
		ToolAllowlist:       allowlist,
		ToolDenylist:        denylist,
		Budgets:             budgets,
		PromptMD:            prompt,
		ExecutorType:        executorType,
		ExecutorConfig:      executorConfig,
		PreferredCredential: preferredCredential,
		AgentConfigName:     agentConfigName,
		TitleSummarizer:     titleSummarizer,
	}, nil
}

func asOptionalBool(value any) bool {
	flag, ok := value.(bool)
	if !ok {
		return false
	}
	return flag
}

func asOptionalInt(value any, label string) (*int, error) {
	if value == nil {
		return nil, nil
	}
	switch n := value.(type) {
	case int:
		out := n
		return &out, nil
	case int64:
		out := int(n)
		return &out, nil
	case float64:
		if float64(int(n)) != n {
			return nil, fmt.Errorf("%s must be an integer", label)
		}
		out := int(n)
		return &out, nil
	default:
		return nil, fmt.Errorf("%s must be an integer", label)
	}
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

func parseExecutorConfig(value any, executorType string, personaDir string) (map[string]any, error) {
	config := asOptionalMap(value)
	out := map[string]any{}
	for key, val := range config {
		out[key] = val
	}
	if executorType != "agent.lua" {
		return out, nil
	}

	scriptFileRaw, hasScriptFile := out["script_file"]
	if !hasScriptFile || scriptFileRaw == nil {
		return out, nil
	}
	if scriptRaw, ok := out["script"].(string); ok && strings.TrimSpace(scriptRaw) != "" {
		return nil, fmt.Errorf("executor_config.script and executor_config.script_file cannot both be set")
	}

	scriptFile, err := asNonEmptyString(scriptFileRaw, "executor_config.script_file")
	if err != nil {
		return nil, err
	}
	scriptPath, err := resolvePersonaLocalPath(personaDir, scriptFile)
	if err != nil {
		return nil, fmt.Errorf("executor_config.script_file: %w", err)
	}
	rawScript, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("executor_config.script_file: %w", err)
	}
	script := strings.TrimSpace(string(rawScript))
	if script == "" {
		return nil, fmt.Errorf("executor_config.script_file: file must not be empty")
	}
	out["script"] = script
	delete(out, "script_file")
	return out, nil
}

func resolvePersonaLocalPath(personaDir string, pathValue string) (string, error) {
	if filepath.IsAbs(pathValue) {
		return "", fmt.Errorf("must be a relative path")
	}
	cleaned := filepath.Clean(pathValue)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes persona directory")
	}
	return filepath.Join(personaDir, cleaned), nil
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

func asToolNameList(value any, label string) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	rawList, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", label)
	}
	seen := map[string]struct{}{}
	out := []string{}
	for idx, item := range rawList {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", label, idx)
		}
		cleaned := strings.TrimSpace(text)
		if cleaned == "" {
			return nil, fmt.Errorf("%s[%d] must not be empty", label, idx)
		}
		if !idRegex.MatchString(cleaned) {
			return nil, fmt.Errorf("%s[%d] is invalid: %s", label, idx, cleaned)
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
		return Budgets{ToolBudget: map[string]any{}, PerToolSoftLimits: tools.PerToolSoftLimits{}}, nil
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return Budgets{}, fmt.Errorf("budgets must be an object")
	}
	parsed, err := sharedexec.ParseRequestedBudgetsMap(raw)
	if err != nil {
		return Budgets{}, err
	}
	return Budgets{
		ReasoningIterations:    parsed.ReasoningIterations,
		ToolContinuationBudget: parsed.ToolContinuationBudget,
		MaxOutputTokens:        parsed.MaxOutputTokens,
		ToolTimeoutMs:          parsed.ToolTimeoutMs,
		ToolBudget:             parsed.ToolBudget,
		PerToolSoftLimits:      parsed.PerToolSoftLimits,
		Temperature:            parsed.Temperature,
		TopP:                   parsed.TopP,
	}, nil
}

// LoadFromDB 从数据库加载 orgID 对应的活跃 persona，用于 per-run 动态覆盖。
func LoadFromDB(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID) ([]Definition, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}

	rows, err := pool.Query(
		ctx,
		`SELECT persona_key, version, display_name, description,
		        prompt_md, tool_allowlist, COALESCE(tool_denylist, '{}'), budgets_json,
		        executor_type, executor_config_json,
		        preferred_credential, agent_config_name
		 FROM personas
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
			personaKey          string
			version             string
			displayName         string
			description         *string
			promptMD            string
			toolAllowlist       []string
			toolDenylist        []string
			budgetsRaw          []byte
			executorType        string
			executorConfigRaw   []byte
			preferredCredential *string
			agentConfigName     *string
		)
		if err := rows.Scan(&personaKey, &version, &displayName, &description,
			&promptMD, &toolAllowlist, &toolDenylist, &budgetsRaw,
			&executorType, &executorConfigRaw, &preferredCredential, &agentConfigName); err != nil {
			return nil, err
		}

		budgets, err := parseBudgetsJSON(budgetsRaw)
		if err != nil {
			return nil, fmt.Errorf("persona %q: %w", personaKey, err)
		}

		executorConfig, err := parseExecutorConfigJSON(executorConfigRaw)
		if err != nil {
			return nil, fmt.Errorf("persona %q executor_config_json: %w", personaKey, err)
		}

		if strings.TrimSpace(executorType) == "" {
			executorType = defaultExecutorType
		}

		def := Definition{
			ID:                  personaKey,
			Version:             version,
			Title:               displayName,
			ToolAllowlist:       toolAllowlist,
			ToolDenylist:        toolDenylist,
			Budgets:             budgets,
			PromptMD:            promptMD,
			ExecutorType:        executorType,
			ExecutorConfig:      executorConfig,
			PreferredCredential: preferredCredential,
			AgentConfigName:     agentConfigName,
		}
		if description != nil && strings.TrimSpace(*description) != "" {
			s := strings.TrimSpace(*description)
			def.Description = &s
		}

		defs = append(defs, def)
	}
	return defs, rows.Err()
}

// MergeRegistry 以 base 为底，DB persona 按 ID 覆盖同名文件系统 persona。
func MergeRegistry(base *Registry, overrides []Definition) *Registry {
	merged := NewRegistry()
	for _, id := range base.ListIDs() {
		def, _ := base.Get(id)
		merged.Set(def)
	}
	for _, def := range overrides {
		if baseDef, ok := merged.Get(def.ID); ok {
			merged.Set(mergeDefinition(baseDef, def))
			continue
		}
		merged.Set(def)
	}
	return merged
}

func mergeDefinition(base Definition, override Definition) Definition {
	merged := override
	if merged.TitleSummarizer == nil {
		merged.TitleSummarizer = base.TitleSummarizer
	}
	return merged
}

func parseBudgetsJSON(raw []byte) (Budgets, error) {
	parsed, err := sharedexec.ParseRequestedBudgetsJSON(raw)
	if err != nil {
		return Budgets{}, err
	}
	return Budgets{
		ReasoningIterations:    parsed.ReasoningIterations,
		ToolContinuationBudget: parsed.ToolContinuationBudget,
		MaxOutputTokens:        parsed.MaxOutputTokens,
		ToolTimeoutMs:          parsed.ToolTimeoutMs,
		ToolBudget:             parsed.ToolBudget,
		PerToolSoftLimits:      parsed.PerToolSoftLimits,
		Temperature:            parsed.Temperature,
		TopP:                   parsed.TopP,
	}, nil
}

func asPerToolSoftLimits(value any) (tools.PerToolSoftLimits, error) {
	if value == nil {
		return tools.PerToolSoftLimits{}, nil
	}
	mapped, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("budgets.per_tool_soft_limits must be an object")
	}
	limits := tools.PerToolSoftLimits{}
	for toolName, rawLimit := range mapped {
		if toolName != "exec_command" && toolName != "write_stdin" {
			return nil, fmt.Errorf("budgets.per_tool_soft_limits.%s is unsupported", toolName)
		}
		limitObj, ok := rawLimit.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("budgets.per_tool_soft_limits.%s must be an object", toolName)
		}
		limit, err := asToolSoftLimit(toolName, limitObj)
		if err != nil {
			return nil, err
		}
		limits[toolName] = limit
	}
	return limits, nil
}

func asToolSoftLimit(toolName string, raw map[string]any) (tools.ToolSoftLimit, error) {
	allowed := map[string]struct{}{"max_output_bytes": {}}
	if toolName == "write_stdin" {
		allowed["max_continuations"] = struct{}{}
		allowed["max_yield_time_ms"] = struct{}{}
	}
	for key := range raw {
		if _, ok := allowed[key]; ok {
			continue
		}
		return tools.ToolSoftLimit{}, fmt.Errorf("budgets.per_tool_soft_limits.%s.%s is unsupported", toolName, key)
	}
	maxOutputBytes, err := asOptionalBoundedPositiveInt(raw["max_output_bytes"], fmt.Sprintf("budgets.per_tool_soft_limits.%s.max_output_bytes", toolName), tools.HardMaxToolSoftLimitOutputBytes)
	if err != nil {
		return tools.ToolSoftLimit{}, err
	}
	limit := tools.ToolSoftLimit{MaxOutputBytes: maxOutputBytes}
	if toolName != "write_stdin" {
		return limit, nil
	}
	maxContinuations, err := asOptionalBoundedPositiveInt(raw["max_continuations"], "budgets.per_tool_soft_limits.write_stdin.max_continuations", tools.HardMaxToolSoftLimitContinuations)
	if err != nil {
		return tools.ToolSoftLimit{}, err
	}
	maxYieldTimeMs, err := asOptionalBoundedPositiveInt(raw["max_yield_time_ms"], "budgets.per_tool_soft_limits.write_stdin.max_yield_time_ms", tools.HardMaxToolSoftLimitYieldTimeMs)
	if err != nil {
		return tools.ToolSoftLimit{}, err
	}
	limit.MaxContinuations = maxContinuations
	limit.MaxYieldTimeMs = maxYieldTimeMs
	return limit, nil
}

func asOptionalBoundedPositiveInt(value any, label string, max int) (*int, error) {
	parsed, err := asOptionalPositiveInt(value, label)
	if err != nil {
		return nil, err
	}
	if parsed != nil && *parsed > max {
		return nil, fmt.Errorf("%s must be less than or equal to %d", label, max)
	}
	return parsed, nil
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

func asOptionalFloat64(value any, label string) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	var f float64
	switch v := value.(type) {
	case float64:
		f = v
	case int:
		f = float64(v)
	default:
		return nil, fmt.Errorf("%s must be a number", label)
	}
	return &f, nil
}

const defaultTitleSummarizeMaxTokens = 20

// asTitleSummarizer 从 YAML 解析 title_summarize 字段。
func asTitleSummarizer(value any) (*TitleSummarizerConfig, error) {
	if value == nil {
		return nil, nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("title_summarize must be an object")
	}
	prompt, err := asNonEmptyString(m["prompt"], "title_summarize.prompt")
	if err != nil {
		return nil, err
	}
	maxTokens := defaultTitleSummarizeMaxTokens
	if raw, exists := m["max_tokens"]; exists {
		parsed, err := asOptionalPositiveInt(raw, "title_summarize.max_tokens")
		if err != nil {
			return nil, err
		}
		if parsed != nil {
			maxTokens = *parsed
		}
	}
	return &TitleSummarizerConfig{
		Prompt:    prompt,
		MaxTokens: maxTokens,
	}, nil
}
