package pipeline

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewAgentConfigMiddleware 按继承链 thread→project→org 解析 agent_config 并写入 RunContext.AgentConfig。
// 无任何层级配置时不写入（下游使用平台默认值）。
func NewAgentConfigMiddleware(dbPool *pgxpool.Pool) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if dbPool == nil {
			return next(ctx, rc)
		}

		// 查询 thread 的 agent_config_id 和 project_id
		var agentConfigID, projectID *uuid.UUID
		err := dbPool.QueryRow(ctx,
			`SELECT agent_config_id, project_id FROM threads WHERE id = $1 AND deleted_at IS NULL`,
			rc.Run.ThreadID,
		).Scan(&agentConfigID, &projectID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			slog.WarnContext(ctx, "agent_config: thread lookup failed", "err", err.Error())
			return next(ctx, rc)
		}

		// 按继承链查找：thread → project → org
		configID := resolveAgentConfigID(ctx, dbPool, agentConfigID, projectID, rc.Run.OrgID)
		if configID == nil {
			return next(ctx, rc)
		}

		ac, err := loadAgentConfig(ctx, dbPool, *configID)
		if err != nil {
			slog.WarnContext(ctx, "agent_config: load failed", "id", configID.String(), "err", err.Error())
			return next(ctx, rc)
		}
		if ac != nil {
			rc.AgentConfig = ac
		}

		return next(ctx, rc)
	}
}

// resolveAgentConfigID 按优先级返回第一个有效的 agent_config id。
func resolveAgentConfigID(
	ctx context.Context,
	pool *pgxpool.Pool,
	threadConfigID *uuid.UUID,
	projectID *uuid.UUID,
	orgID uuid.UUID,
) *uuid.UUID {
	// thread 级
	if threadConfigID != nil {
		return threadConfigID
	}

	// project 级（is_default=true，附带 org_id 约束保证隔离）
	if projectID != nil {
		var id uuid.UUID
		err := pool.QueryRow(ctx,
			`SELECT id FROM agent_configs WHERE org_id = $1 AND project_id = $2 AND is_default = true LIMIT 1`,
			orgID, *projectID,
		).Scan(&id)
		if err == nil {
			return &id
		}
	}

	// org 级（is_default=true，无 project 绑定）
	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`SELECT id FROM agent_configs WHERE org_id = $1 AND is_default = true AND project_id IS NULL LIMIT 1`,
		orgID,
	).Scan(&id)
	if err == nil {
		return &id
	}

	return nil
}

func loadAgentConfig(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*ResolvedAgentConfig, error) {
	var (
		systemPromptOverride *string
		model                *string
		temperature          *float64
		maxOutputTokens      *int
		topP                 *float64
		contextWindowLimit   *int
		toolPolicy           string
		toolAllowlist        []string
		toolDenylist         []string
		contentFilterLevel   string
		safetyRulesJSON      map[string]any

		// system_prompt_template_id 用于关联查询 prompt template
		systemPromptTemplateID *uuid.UUID
	)

	err := pool.QueryRow(ctx,
		`SELECT system_prompt_template_id, system_prompt_override,
		        model, temperature, max_output_tokens, top_p, context_window_limit,
		        tool_policy, tool_allowlist, tool_denylist, content_filter_level, safety_rules_json
		 FROM agent_configs WHERE id = $1`,
		id,
	).Scan(
		&systemPromptTemplateID, &systemPromptOverride,
		&model, &temperature, &maxOutputTokens, &topP, &contextWindowLimit,
		&toolPolicy, &toolAllowlist, &toolDenylist, &contentFilterLevel, &safetyRulesJSON,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// 决定最终 system prompt：override 优先，其次查 template（通过 JOIN 确保 org 隔离）
	var resolvedPrompt *string
	if systemPromptOverride != nil && *systemPromptOverride != "" {
		resolvedPrompt = systemPromptOverride
	} else if systemPromptTemplateID != nil {
		var content string
		tErr := pool.QueryRow(ctx,
			`SELECT pt.content
			 FROM prompt_templates pt
			 JOIN agent_configs ac ON ac.system_prompt_template_id = pt.id
			 WHERE ac.id = $1`,
			id,
		).Scan(&content)
		if tErr == nil && content != "" {
			resolvedPrompt = &content
		}
	}

	if safetyRulesJSON == nil {
		safetyRulesJSON = map[string]any{}
	}

	return &ResolvedAgentConfig{
		SystemPrompt:       resolvedPrompt,
		Model:              model,
		Temperature:        temperature,
		MaxOutputTokens:    maxOutputTokens,
		TopP:               topP,
		ContextWindowLimit: contextWindowLimit,
		ToolPolicy:         toolPolicy,
		ToolAllowlist:      toolAllowlist,
		ToolDenylist:       toolDenylist,
		ContentFilterLevel: contentFilterLevel,
		SafetyRulesJSON:    safetyRulesJSON,
	}, nil
}
