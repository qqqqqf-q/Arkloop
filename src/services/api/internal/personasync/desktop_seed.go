//go:build desktop

package personasync

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"arkloop/services/api/internal/data"
	repopersonas "arkloop/services/api/internal/personas"
	"arkloop/services/shared/database"
)

// SeedDesktopPersonas loads personas from the filesystem and inserts them
// into SQLite if they don't already exist. Called once during desktop startup.
func SeedDesktopPersonas(ctx context.Context, db database.DB, personasRoot string) error {
	if db == nil {
		return fmt.Errorf("db must not be nil")
	}
	root := strings.TrimSpace(personasRoot)
	if root == "" {
		return fmt.Errorf("personasRoot must not be empty")
	}

	loaded, err := repopersonas.LoadFromDir(root)
	if err != nil {
		return fmt.Errorf("load personas: %w", err)
	}

	for _, p := range loaded {
		if err := seedOnePersona(ctx, db, p); err != nil {
			return fmt.Errorf("seed persona %q: %w", p.ID, err)
		}
	}
	return nil
}

func seedOnePersona(ctx context.Context, db database.DB, p repopersonas.RepoPersona) error {
	row := db.QueryRow(ctx,
		`SELECT COUNT(*) FROM personas WHERE persona_key = ?`,
		strings.TrimSpace(p.ID),
	)
	var count int
	if err := row.Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return updatePersona(ctx, db, p)
	}
	return insertPersona(ctx, db, p)
}

func insertPersona(ctx context.Context, db database.DB, p repopersonas.RepoPersona) error {
	executorType := strings.TrimSpace(p.ExecutorType)
	if executorType == "" {
		executorType = "agent.simple"
	}

	_, err := db.Exec(ctx,
		`INSERT INTO personas (
			persona_key, version, display_name, description,
			soul_md, user_selectable, selector_name, selector_order,
			prompt_md, tool_allowlist, tool_denylist, core_tools, budgets_json,
			roles_json, title_summarize_json, conditional_tools_json,
			is_active, executor_type, executor_config_json,
			preferred_credential, model, reasoning_mode, stream_thinking, prompt_cache_control,
			sync_mode, mirrored_file_dir, updated_at
		) VALUES (
			?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?,
			1, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, datetime('now')
		)`,
		strings.TrimSpace(p.ID),
		strings.TrimSpace(p.Version),
		strings.TrimSpace(p.Title),
		seedNullableStr(p.Description),
		strings.TrimSpace(p.SoulMD),
		seedBoolInt(p.UserSelectable),
		seedNullableStr(p.SelectorName),
		p.SelectorOrder,
		strings.TrimSpace(p.PromptMD),
		seedJSONArray(p.ToolAllowlist),
		seedJSONArray(p.ToolDenylist),
		seedJSONArray(p.CoreTools),
		seedJSONObject(p.Budgets),
		seedJSONObject(p.Roles),
		seedJSONObjectNullable(p.TitleSummarize),
		seedConditionalToolsNullable(p.ConditionalTools),
		executorType,
		seedJSONObjectWithDefault(p.ExecutorConfig),
		seedNullableStr(p.PreferredCredential),
		seedNullableStr(p.Model),
		seedReasoningMode(p.ReasoningMode),
		seedBoolInt(data.NormalizePersonaStreamThinkingPtr(p.StreamThinking)),
		seedPromptCacheControl(p.PromptCacheControl),
		"platform_file_mirror",
		strings.TrimSpace(p.DirName),
	)
	return err
}

func updatePersona(ctx context.Context, db database.DB, p repopersonas.RepoPersona) error {
	executorType := strings.TrimSpace(p.ExecutorType)
	if executorType == "" {
		executorType = "agent.simple"
	}

	_, err := db.Exec(ctx,
		`UPDATE personas SET
			version = ?, display_name = ?, description = ?,
			soul_md = ?, user_selectable = ?, selector_name = ?, selector_order = ?,
			prompt_md = ?, tool_allowlist = ?, tool_denylist = ?, core_tools = ?, budgets_json = ?,
			roles_json = ?, title_summarize_json = ?, conditional_tools_json = ?,
			is_active = 1, executor_type = ?, executor_config_json = ?,
			preferred_credential = ?, model = ?, reasoning_mode = ?, stream_thinking = ?, prompt_cache_control = ?,
			sync_mode = ?, mirrored_file_dir = ?, updated_at = datetime('now')
		WHERE persona_key = ?`,
		strings.TrimSpace(p.Version),
		strings.TrimSpace(p.Title),
		seedNullableStr(p.Description),
		strings.TrimSpace(p.SoulMD),
		seedBoolInt(p.UserSelectable),
		seedNullableStr(p.SelectorName),
		p.SelectorOrder,
		strings.TrimSpace(p.PromptMD),
		seedJSONArray(p.ToolAllowlist),
		seedJSONArray(p.ToolDenylist),
		seedJSONArray(p.CoreTools),
		seedJSONObject(p.Budgets),
		seedJSONObject(p.Roles),
		seedJSONObjectNullable(p.TitleSummarize),
		seedConditionalToolsNullable(p.ConditionalTools),
		executorType,
		seedJSONObjectWithDefault(p.ExecutorConfig),
		seedNullableStr(p.PreferredCredential),
		seedNullableStr(p.Model),
		seedReasoningMode(p.ReasoningMode),
		seedBoolInt(data.NormalizePersonaStreamThinkingPtr(p.StreamThinking)),
		seedPromptCacheControl(p.PromptCacheControl),
		"platform_file_mirror",
		strings.TrimSpace(p.DirName),
		strings.TrimSpace(p.ID),
	)
	return err
}

// --- helpers (prefixed with "seed" to avoid collision with manager.go) ---

func seedJSONArray(vals []string) string {
	if len(vals) == 0 {
		return "[]"
	}
	data, err := json.Marshal(vals)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func seedJSONObject(obj map[string]any) string {
	if len(obj) == 0 {
		return "{}"
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func seedJSONObjectWithDefault(obj map[string]any) string {
	if len(obj) == 0 {
		return "{}"
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func seedJSONObjectNullable(obj map[string]any) *string {
	if len(obj) == 0 {
		return nil
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return nil
	}
	s := string(data)
	return &s
}

func seedConditionalToolsNullable(rules []repopersonas.ConditionalToolRule) *string {
	if len(rules) == 0 {
		return nil
	}
	data, err := json.Marshal(rules)
	if err != nil {
		return nil
	}
	s := string(data)
	return &s
}

func seedNullableStr(val string) *string {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func seedBoolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func seedReasoningMode(val string) string {
	if strings.TrimSpace(val) == "" {
		return "auto"
	}
	return data.NormalizePersonaReasoningMode(val)
}

func seedPromptCacheControl(val string) string {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return "none"
	}
	return trimmed
}
