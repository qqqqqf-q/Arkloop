//go:build desktop

package personas

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"arkloop/services/shared/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// pgxQuerier is the minimal pgx-compatible query interface (satisfied by
// sqlitepgx.Pool and data.DesktopDB).
type pgxQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const personaSelectSQL = `SELECT persona_key, version, display_name, description,
	        soul_md, user_selectable, selector_name, selector_order,
	        prompt_md, tool_allowlist, tool_denylist, COALESCE(core_tools, '[]'), budgets_json,
	        roles_json, title_summarize_json, conditional_tools_json,
	        executor_type, executor_config_json,
	        preferred_credential, model, reasoning_mode, COALESCE(stream_thinking, 1), prompt_cache_control,
	        COALESCE(heartbeat_enabled, 0), COALESCE(heartbeat_interval_minutes, 30)
	 FROM personas
	 WHERE is_active = 1
	 ORDER BY created_at ASC`

// LoadPersonasFromDesktopDB loads active persona definitions using a
// pgx-compatible querier (data.DesktopDB / sqlitepgx.Pool).
func LoadPersonasFromDesktopDB(ctx context.Context, db pgxQuerier) ([]Definition, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	rows, err := db.Query(ctx, personaSelectSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPersonaRows(rows)
}

// LoadFromDesktopDB loads active persona definitions from the SQLite database
// using the shared database.DB interface. Kept for backward compatibility.
func LoadFromDesktopDB(ctx context.Context, db database.DB) ([]Definition, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	rows, err := db.Query(ctx, personaSelectSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPersonaRows(rows)
}

type personaRowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanPersonaRows(rows personaRowScanner) ([]Definition, error) {
	var defs []Definition
	for rows.Next() {
		var (
			personaKey               string
			version                  string
			displayName              string
			description              *string
			soulMD                   string
			userSelectable           int
			selectorName             *string
			selectorOrder            *int
			promptMD                 string
			toolAllowlistStr         string
			toolDenylistStr          string
			coreToolsStr             string
			budgetsStr               string
			rolesStr                 *string
			titleSummarizeStr        *string
			conditionalToolsStr      *string
			executorType             string
			executorConfigStr        string
			preferredCredential      *string
			model                    *string
			reasoningMode            string
			streamThinking           int
			promptCacheControl       string
			heartbeatEnabled         int
			heartbeatIntervalMinutes int
		)
		if err := rows.Scan(
			&personaKey, &version, &displayName, &description,
			&soulMD, &userSelectable, &selectorName, &selectorOrder,
			&promptMD, &toolAllowlistStr, &toolDenylistStr, &coreToolsStr, &budgetsStr,
			&rolesStr, &titleSummarizeStr, &conditionalToolsStr,
			&executorType, &executorConfigStr,
			&preferredCredential, &model, &reasoningMode, &streamThinking, &promptCacheControl,
			&heartbeatEnabled, &heartbeatIntervalMinutes,
		); err != nil {
			return nil, err
		}

		toolAllowlist, err := desktopParseJSONStringArray(toolAllowlistStr)
		if err != nil {
			return nil, fmt.Errorf("persona %q tool_allowlist: %w", personaKey, err)
		}
		toolDenylist, err := desktopParseJSONStringArray(toolDenylistStr)
		if err != nil {
			return nil, fmt.Errorf("persona %q tool_denylist: %w", personaKey, err)
		}
		coreTools, err := desktopParseJSONStringArray(coreToolsStr)
		if err != nil {
			return nil, fmt.Errorf("persona %q core_tools: %w", personaKey, err)
		}

		budgets, err := parseBudgetsJSON([]byte(budgetsStr))
		if err != nil {
			return nil, fmt.Errorf("persona %q: %w", personaKey, err)
		}

		executorConfig, err := parseExecutorConfigJSON([]byte(executorConfigStr))
		if err != nil {
			return nil, fmt.Errorf("persona %q executor_config_json: %w", personaKey, err)
		}
		if err := validateRuntimeExecutorConfig(executorType, executorConfig); err != nil {
			return nil, fmt.Errorf("persona %q executor_config_json: %w", personaKey, err)
		}

		var titleSummarizeRaw []byte
		if titleSummarizeStr != nil {
			titleSummarizeRaw = []byte(*titleSummarizeStr)
		}
		titleSummarizer, err := parseTitleSummarizeJSON(titleSummarizeRaw)
		if err != nil {
			return nil, fmt.Errorf("persona %q title_summarize_json: %w", personaKey, err)
		}
		var conditionalToolsRaw []byte
		if conditionalToolsStr != nil {
			conditionalToolsRaw = []byte(*conditionalToolsStr)
		}
		conditionalTools, err := parseConditionalToolsJSON(conditionalToolsRaw)
		if err != nil {
			return nil, fmt.Errorf("persona %q conditional_tools_json: %w", personaKey, err)
		}

		var rolesRaw []byte
		if rolesStr != nil {
			rolesRaw = []byte(*rolesStr)
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
			UserSelectable:      userSelectable != 0,
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
			StreamThinking:      streamThinking != 0,
			PromptCacheControl:  normalizePersonaPromptCacheControl(strPtrOrNil(promptCacheControl)),
			Roles:               roles,
			TitleSummarizer:     titleSummarizer,

			HeartbeatEnabled:         heartbeatEnabled != 0,
			HeartbeatIntervalMinutes: heartbeatIntervalMinutes,
		}
		if description != nil && strings.TrimSpace(*description) != "" {
			s := strings.TrimSpace(*description)
			def.Description = &s
		}

		defs = append(defs, def)
	}
	return defs, rows.Err()
}

func desktopParseJSONStringArray(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" || raw == "null" {
		return nil, nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return nil, err
	}
	return arr, nil
}
