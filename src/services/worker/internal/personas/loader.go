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
	"time"

	sharedexec "arkloop/services/shared/executionconfig"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
)

var idRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

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
		if _, err := os.Stat(yamlPath); err != nil {
			continue
		}

		def, err := loadSinglePersona(yamlPath)
		if err != nil {
			return nil, err
		}
		if err := registry.Register(def); err != nil {
			return nil, err
		}
	}

	return registry, nil
}

func loadSinglePersona(yamlPath string) (Definition, error) {
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
	coreTools, err := asToolNameList(obj["core_tools"], "core_tools")
	if err != nil {
		return Definition{}, err
	}
	conditionalTools, err := parseConditionalTools(obj["conditional_tools"])
	if err != nil {
		return Definition{}, err
	}
	budgets, err := asBudgets(obj["budgets"])
	if err != nil {
		return Definition{}, err
	}
	roles, err := parseRoleOverrides(obj["roles"])
	if err != nil {
		return Definition{}, err
	}

	personaDir := filepath.Dir(yamlPath)
	promptPath := filepath.Join(personaDir, "prompt.md")
	prompt, err := loadRequiredPersonaMarkdown(promptPath, "prompt.md")
	if err != nil {
		return Definition{}, err
	}

	soulFile, soulExplicit, err := parsePersonaSoulFile(obj)
	if err != nil {
		return Definition{}, err
	}
	soulMD, err := loadOptionalPersonaMarkdown(personaDir, soulFile, soulExplicit, "soul_file")
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
	executorConfig, err := parseExecutorConfig(obj["executor_config"], executorType, personaDir)
	if err != nil {
		return Definition{}, err
	}
	preferredCredential := asOptionalString(obj["preferred_credential"])
	model := asOptionalString(obj["model"])
	reasoningMode := normalizePersonaReasoningMode(asOptionalString(obj["reasoning_mode"]))
	streamThinking := normalizePersonaStreamThinking(obj["stream_thinking"])
	promptCacheControl := normalizePersonaPromptCacheControl(asOptionalString(obj["prompt_cache_control"]))
	titleSummarizer, err := asTitleSummarizer(obj["title_summarize"])
	if err != nil {
		return Definition{}, err
	}
	if titleSummarizer != nil {
		if prompt, err := loadSummarizePrompt(personaDir, obj["title_summarize"], "title_summarize"); err != nil {
			return Definition{}, err
		} else if prompt != "" {
			titleSummarizer.Prompt = prompt
		}
	}
	resultSummarizer, err := asResultSummarizer(obj["result_summarize"])
	if err != nil {
		return Definition{}, err
	}
	if resultSummarizer != nil {
		if prompt, err := loadSummarizePrompt(personaDir, obj["result_summarize"], "result_summarize"); err != nil {
			return Definition{}, err
		} else if prompt != "" {
			resultSummarizer.Prompt = prompt
		}
	}

	hbEnabled, hbInterval, err := parseHeartbeatBlock(obj["heartbeat"])
	if err != nil {
		return Definition{}, err
	}
	hbMD, err := loadOptionalPersonaMarkdown(personaDir, "heartbeat.md", false, "heartbeat.md")
	if err != nil {
		return Definition{}, err
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
		ConditionalTools:    conditionalTools,
		CoreTools:           coreTools,
		Budgets:             budgets,
		SoulMD:              soulMD,
		PromptMD:            prompt,
		ExecutorType:        executorType,
		ExecutorConfig:      executorConfig,
		PreferredCredential: preferredCredential,
		Model:               model,
		ReasoningMode:       reasoningMode,
		StreamThinking:      streamThinking,
		PromptCacheControl:  promptCacheControl,
		Roles:               roles,
		TitleSummarizer:     titleSummarizer,
		ResultSummarizer:    resultSummarizer,

		IsSystem:                asOptionalBool(obj["is_system"]),
		IsBuiltin:               asOptionalBool(obj["is_builtin"]),
		AllowPlatformDelegation: asOptionalBool(obj["allow_platform_delegation"]),

		HeartbeatEnabled:         hbEnabled,
		HeartbeatIntervalMinutes: hbInterval,
		HeartbeatMD:              hbMD,
	}, nil
}

// parseHeartbeatBlock 解析 persona.yaml 中的 heartbeat: 块。
// 返回 (enabled, intervalMinutes, error)。
func parseHeartbeatBlock(value any) (bool, int, error) {
	if value == nil {
		return false, 0, nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return false, 0, fmt.Errorf("heartbeat must be an object")
	}
	enabled := asOptionalBool(m["enabled"])
	interval := 0
	if raw, exists := m["interval_minutes"]; exists && raw != nil {
		p, err := asOptionalInt(raw, "heartbeat.interval_minutes")
		if err != nil {
			return false, 0, err
		}
		if p != nil {
			if *p <= 0 {
				return false, 0, fmt.Errorf("heartbeat.interval_minutes must be a positive integer")
			}
			interval = *p
		}
	}
	return enabled, interval, nil
}

func loadRequiredPersonaMarkdown(path string, label string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(string(raw))
	if content == "" {
		return "", fmt.Errorf("%s must not be empty: %s", label, path)
	}
	return content, nil
}

func parsePersonaSoulFile(obj map[string]any) (string, bool, error) {
	const defaultSoulFile = "soul.md"
	rawSoulFile, ok := obj["soul_file"]
	if !ok {
		return defaultSoulFile, false, nil
	}
	soulFile, err := asNonEmptyString(rawSoulFile, "soul_file")
	if err != nil {
		return "", true, err
	}
	return soulFile, true, nil
}

func loadOptionalPersonaMarkdown(personaDir string, fileName string, required bool, label string) (string, error) {
	path, err := resolvePersonaLocalPath(personaDir, fileName)
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !required && os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("%s: %w", label, err)
	}
	content := strings.TrimSpace(string(raw))
	if content == "" {
		if required {
			return "", fmt.Errorf("%s: file must not be empty", label)
		}
		return "", nil
	}
	return content, nil
}

func loadSummarizePrompt(personaDir string, value any, label string) (string, error) {
	if value == nil {
		return "", nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("%s must be an object", label)
	}
	rawFile, exists := m["prompt_file"]
	if !exists || rawFile == nil {
		return "", nil
	}
	fileName, err := asNonEmptyString(rawFile, label+".prompt_file")
	if err != nil {
		return "", err
	}
	return loadOptionalPersonaMarkdown(personaDir, fileName, true, label+".prompt_file")
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

func strPtrOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizePersonaReasoningMode(value *string) string {
	if value == nil {
		return "auto"
	}
	s := strings.TrimSpace(*value)
	switch s {
	case "enabled", "启用":
		return "enabled"
	case "disabled", "禁用":
		return "disabled"
	case "none", "off", "无", "关闭":
		return "none"
	case "auto", "自动":
		return "auto"
	case "minimal", "low", "medium", "high", "xhigh":
		return s
	case "max", "maximum", "extra high", "extra_high", "extra-high", "超高":
		return "xhigh"
	default:
		return "auto"
	}
}

func normalizePersonaStreamThinking(value any) bool {
	if value == nil {
		return true
	}
	b, ok := value.(bool)
	if !ok {
		return true
	}
	return b
}

func normalizePersonaPromptCacheControl(value *string) string {
	if value == nil {
		return "none"
	}
	switch strings.TrimSpace(*value) {
	case "system_prompt", "none":
		return strings.TrimSpace(*value)
	default:
		return "none"
	}
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

// LoadFromDB 从数据库加载运行期有效的 persona。
func LoadFromDB(ctx context.Context, pool *pgxpool.Pool, projectID *uuid.UUID) ([]Definition, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}

	query := `SELECT persona_key, version, display_name, description,
		        soul_md, user_selectable, selector_name, selector_order,
		        prompt_md, tool_allowlist, COALESCE(tool_denylist, '{}'), COALESCE(core_tools, '{}'), budgets_json, COALESCE(roles_json, '{}'::jsonb), title_summarize_json, result_summarize_json, conditional_tools_json,
		        executor_type, executor_config_json,
		        preferred_credential, model, reasoning_mode, stream_thinking, prompt_cache_control,
		        updated_at
		 FROM personas
		 WHERE is_active = TRUE AND (project_id = $1 OR (project_id IS NULL AND persona_key = $2))
		 ORDER BY CASE WHEN project_id IS NULL THEN 0 ELSE 1 END ASC, created_at ASC`

	rows, err := pool.Query(ctx, query, projectID, SystemSummarizerPersonaID)
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
			soulMD              string
			userSelectable      bool
			selectorName        *string
			selectorOrder       *int
			promptMD            string
			toolAllowlist       []string
			toolDenylist        []string
			coreTools           []string
			budgetsRaw          []byte
			rolesRaw            []byte
			titleSummarizeRaw   []byte
			resultSummarizeRaw  []byte
			conditionalToolsRaw []byte
			executorType        string
			executorConfigRaw   []byte
			preferredCredential *string
			model               *string
			reasoningMode       string
			streamThinking      bool
			promptCacheControl  string
			updatedAt           time.Time
		)
		if err := rows.Scan(&personaKey, &version, &displayName, &description,
			&soulMD, &userSelectable, &selectorName, &selectorOrder,
			&promptMD, &toolAllowlist, &toolDenylist, &coreTools, &budgetsRaw, &rolesRaw, &titleSummarizeRaw, &resultSummarizeRaw, &conditionalToolsRaw,
			&executorType, &executorConfigRaw, &preferredCredential, &model, &reasoningMode, &streamThinking, &promptCacheControl, &updatedAt); err != nil {
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
		if err := validateRuntimeExecutorConfig(executorType, executorConfig); err != nil {
			return nil, fmt.Errorf("persona %q executor_config_json: %w", personaKey, err)
		}
		titleSummarizer, err := parseTitleSummarizeJSON(titleSummarizeRaw)
		if err != nil {
			return nil, fmt.Errorf("persona %q title_summarize_json: %w", personaKey, err)
		}
		resultSummarizer, err := parseResultSummarizeJSON(resultSummarizeRaw)
		if err != nil {
			return nil, fmt.Errorf("persona %q result_summarize_json: %w", personaKey, err)
		}
		conditionalTools, err := parseConditionalToolsJSON(conditionalToolsRaw)
		if err != nil {
			return nil, fmt.Errorf("persona %q conditional_tools_json: %w", personaKey, err)
		}
		roles, err := parseRoleOverridesJSON(rolesRaw)
		if err != nil {
			return nil, fmt.Errorf("persona %q roles_json: %w", personaKey, err)
		}

		if strings.TrimSpace(executorType) == "" {
			executorType = defaultExecutorType
		}

		def := Definition{
			ID:                  personaKey,
			Version:             version,
			Title:               displayName,
			UserSelectable:      userSelectable,
			SelectorName:        strPtrOrNilPtr(selectorName),
			SelectorOrder:       selectorOrder,
			ToolAllowlist:       toolAllowlist,
			ToolDenylist:        toolDenylist,
			ConditionalTools:    conditionalTools,
			CoreTools:           coreTools,
			Budgets:             budgets,
			SoulMD:              strings.TrimSpace(soulMD),
			PromptMD:            promptMD,
			ExecutorType:        executorType,
			ExecutorConfig:      executorConfig,
			PreferredCredential: preferredCredential,
			Model:               model,
			ReasoningMode:       normalizePersonaReasoningMode(strPtrOrNil(reasoningMode)),
			StreamThinking:      streamThinking,
			PromptCacheControl:  normalizePersonaPromptCacheControl(strPtrOrNil(promptCacheControl)),
			Roles:               roles,
			TitleSummarizer:     titleSummarizer,
			ResultSummarizer:    resultSummarizer,
			UpdatedAt:           updatedAt,
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
// is_system 的 persona 不可被 DB 覆盖。
func MergeRegistry(base *Registry, overrides []Definition) *Registry {
	merged := NewRegistry()
	for _, id := range base.ListIDs() {
		def, _ := base.Get(id)
		merged.Set(def)
	}
	for _, def := range overrides {
		if baseDef, ok := merged.Get(def.ID); ok {
			if baseDef.IsSystem {
				continue
			}
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
	if merged.ResultSummarizer == nil {
		merged.ResultSummarizer = base.ResultSummarizer
	}
	if strings.TrimSpace(merged.SoulMD) == "" {
		merged.SoulMD = base.SoulMD
	}
	// DB 无对应列的字段保留文件系统值
	merged.IsSystem = base.IsSystem
	merged.IsBuiltin = base.IsBuiltin
	merged.AllowPlatformDelegation = base.AllowPlatformDelegation
	if len(merged.CoreTools) == 0 {
		merged.CoreTools = base.CoreTools
	}
	if len(merged.ConditionalTools) == 0 {
		merged.ConditionalTools = append([]ConditionalToolRule(nil), base.ConditionalTools...)
	}
	// heartbeat 配置和任务清单 DB 无对应列，始终保留文件系统值
	merged.HeartbeatEnabled = base.HeartbeatEnabled
	merged.HeartbeatIntervalMinutes = base.HeartbeatIntervalMinutes
	if strings.TrimSpace(merged.HeartbeatMD) == "" {
		merged.HeartbeatMD = base.HeartbeatMD
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

func validateRuntimeExecutorConfig(executorType string, config map[string]any) error {
	if strings.TrimSpace(executorType) != "agent.lua" {
		return nil
	}
	if _, exists := config["script_file"]; exists {
		return fmt.Errorf("script_file is not allowed in runtime executor config")
	}
	script, _ := config["script"].(string)
	if strings.TrimSpace(script) == "" {
		return fmt.Errorf("script is required in runtime executor config")
	}
	return nil
}

func parseTitleSummarizeJSON(raw []byte) (*TitleSummarizerConfig, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("invalid title_summarize_json: %w", err)
	}
	return asTitleSummarizer(obj)
}

func parseResultSummarizeJSON(raw []byte) (*ResultSummarizerConfig, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("invalid result_summarize_json: %w", err)
	}
	return asResultSummarizer(obj)
}

func strPtrOrNilPtr(value *string) *string {
	if value == nil {
		return nil
	}
	return strPtrOrNil(*value)
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

// asTitleSummarizer 从 YAML 解析 title_summarize 字段。
func asTitleSummarizer(value any) (*TitleSummarizerConfig, error) {
	if value == nil {
		return nil, nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("title_summarize must be an object")
	}
	prompt := ""
	if rawPrompt, exists := m["prompt"]; exists && rawPrompt != nil {
		parsed, err := asNonEmptyString(rawPrompt, "title_summarize.prompt")
		if err != nil {
			return nil, err
		}
		prompt = parsed
	}
	if prompt == "" {
		if rawPromptFile, exists := m["prompt_file"]; !exists || rawPromptFile == nil {
			return nil, fmt.Errorf("title_summarize.prompt or title_summarize.prompt_file is required")
		}
	}
	maxTokens := DefaultTitleSummarizeMaxOutputTokens
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

func asResultSummarizer(value any) (*ResultSummarizerConfig, error) {
	if value == nil {
		return nil, nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("result_summarize must be an object")
	}
	prompt := ""
	if rawPrompt, exists := m["prompt"]; exists && rawPrompt != nil {
		parsed, err := asNonEmptyString(rawPrompt, "result_summarize.prompt")
		if err != nil {
			return nil, err
		}
		prompt = parsed
	}
	if prompt == "" {
		if rawPromptFile, exists := m["prompt_file"]; !exists || rawPromptFile == nil {
			return nil, fmt.Errorf("result_summarize.prompt or result_summarize.prompt_file is required")
		}
	}
	maxTokens := DefaultResultSummarizeMaxOutputTokens
	if raw, exists := m["max_tokens"]; exists {
		parsed, err := asOptionalPositiveInt(raw, "result_summarize.max_tokens")
		if err != nil {
			return nil, err
		}
		if parsed != nil {
			maxTokens = *parsed
		}
	}
	thresholdBytes := DefaultResultSummarizeThresholdBytes
	if raw, exists := m["threshold_bytes"]; exists {
		parsed, err := asOptionalPositiveInt(raw, "result_summarize.threshold_bytes")
		if err != nil {
			return nil, err
		}
		if parsed != nil {
			thresholdBytes = *parsed
		}
	}
	return &ResultSummarizerConfig{
		Prompt:         prompt,
		MaxTokens:      maxTokens,
		ThresholdBytes: thresholdBytes,
	}, nil
}
