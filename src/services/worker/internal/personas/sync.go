package personas

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SyncBuiltinPersonasToDB 将文件系统加载的 persona 定义以 org_id IS NULL 写入 DB。
// 已存在的 persona 按 (persona_key, version) 匹配后 UPDATE，不存在的 INSERT。
func SyncBuiltinPersonasToDB(ctx context.Context, pool *pgxpool.Pool, registry *Registry) error {
	if pool == nil || registry == nil {
		return nil
	}

	for _, id := range registry.ListIDs() {
		def, _ := registry.Get(id)
		if err := upsertGlobalPersona(ctx, pool, def); err != nil {
			return fmt.Errorf("sync persona %q: %w", def.ID, err)
		}
		slog.InfoContext(ctx, "personas: synced builtin", "persona_id", def.ID, "version", def.Version)
	}
	return nil
}

func upsertGlobalPersona(ctx context.Context, pool *pgxpool.Pool, def Definition) error {
	budgetsJSON, err := marshalBudgets(def.Budgets)
	if err != nil {
		return err
	}
	executorConfigJSON, err := json.Marshal(def.ExecutorConfig)
	if err != nil {
		return fmt.Errorf("marshal executor_config: %w", err)
	}
	if def.ToolAllowlist == nil {
		def.ToolAllowlist = []string{}
	}
	if def.ToolDenylist == nil {
		def.ToolDenylist = []string{}
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO personas
			(org_id, persona_key, version, display_name, description,
			 prompt_md, tool_allowlist, tool_denylist, budgets_json,
			 executor_type, executor_config_json, preferred_credential, agent_config_name)
		 VALUES
			(NULL, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 ON CONFLICT (org_id, persona_key, version) DO UPDATE SET
			display_name        = EXCLUDED.display_name,
			description         = EXCLUDED.description,
			prompt_md           = EXCLUDED.prompt_md,
			tool_allowlist      = EXCLUDED.tool_allowlist,
			tool_denylist       = EXCLUDED.tool_denylist,
			budgets_json        = EXCLUDED.budgets_json,
			executor_type       = EXCLUDED.executor_type,
			executor_config_json = EXCLUDED.executor_config_json,
			preferred_credential = EXCLUDED.preferred_credential,
			agent_config_name   = EXCLUDED.agent_config_name`,
		def.ID,               // $1
		def.Version,           // $2
		def.Title,             // $3
		def.Description,       // $4
		def.PromptMD,          // $5
		def.ToolAllowlist,     // $6
		def.ToolDenylist,      // $7
		budgetsJSON,           // $8
		def.ExecutorType,      // $9
		executorConfigJSON,    // $10
		def.PreferredCredential, // $11
		def.AgentConfigName,   // $12
	)
	return err
}

func marshalBudgets(b Budgets) ([]byte, error) {
	obj := map[string]any{}
	if b.MaxIterations != nil {
		obj["max_iterations"] = *b.MaxIterations
	}
	if b.MaxOutputTokens != nil {
		obj["max_output_tokens"] = *b.MaxOutputTokens
	}
	if b.ToolTimeoutMs != nil {
		obj["tool_timeout_ms"] = *b.ToolTimeoutMs
	}
	if len(b.ToolBudget) > 0 {
		obj["tool_budget"] = b.ToolBudget
	}
	if b.Temperature != nil {
		obj["temperature"] = *b.Temperature
	}
	if b.TopP != nil {
		obj["top_p"] = *b.TopP
	}
	return json.Marshal(obj)
}
