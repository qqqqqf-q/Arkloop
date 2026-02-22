package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AgentConfig struct {
	ID                     uuid.UUID
	OrgID                  uuid.UUID
	Name                   string
	SystemPromptTemplateID *uuid.UUID
	SystemPromptOverride   *string
	Model                  *string
	Temperature            *float64
	MaxOutputTokens        *int
	TopP                   *float64
	ContextWindowLimit     *int
	ToolPolicy             string
	ToolAllowlist          []string
	ToolDenylist           []string
	ContentFilterLevel     string
	SafetyRulesJSON        map[string]any
	ProjectID              *uuid.UUID
	SkillID                *uuid.UUID
	IsDefault              bool
	CreatedAt              time.Time
}

type CreateAgentConfigRequest struct {
	Name                   string
	SystemPromptTemplateID *uuid.UUID
	SystemPromptOverride   *string
	Model                  *string
	Temperature            *float64
	MaxOutputTokens        *int
	TopP                   *float64
	ContextWindowLimit     *int
	ToolPolicy             string
	ToolAllowlist          []string
	ToolDenylist           []string
	ContentFilterLevel     string
	SafetyRulesJSON        map[string]any
	ProjectID              *uuid.UUID
	SkillID                *uuid.UUID
	IsDefault              bool
}

type AgentConfigUpdateFields struct {
	SetName                   bool
	Name                      string
	SetSystemPromptTemplateID bool
	SystemPromptTemplateID    *uuid.UUID
	SetSystemPromptOverride   bool
	SystemPromptOverride      *string
	SetModel                  bool
	Model                     *string
	SetTemperature            bool
	Temperature               *float64
	SetMaxOutputTokens        bool
	MaxOutputTokens           *int
	SetTopP                   bool
	TopP                      *float64
	SetContextWindowLimit     bool
	ContextWindowLimit        *int
	SetToolPolicy             bool
	ToolPolicy                string
	SetIsDefault              bool
	IsDefault                 bool
}

type AgentConfigRepository struct {
	db Querier
}

func NewAgentConfigRepository(db Querier) (*AgentConfigRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &AgentConfigRepository{db: db}, nil
}

const agentConfigColumns = `id, org_id, name, system_prompt_template_id, system_prompt_override,
	model, temperature, max_output_tokens, top_p, context_window_limit,
	tool_policy, tool_allowlist, tool_denylist, content_filter_level, safety_rules_json,
	project_id, skill_id, is_default, created_at`

// agentConfigScanner 覆盖 pgx.Row（struct）和 pgx.Rows（interface）共有的 Scan 方法。
type agentConfigScanner interface {
	Scan(dest ...any) error
}

func scanAgentConfig(row agentConfigScanner) (AgentConfig, error) {
	var ac AgentConfig
	err := row.Scan(
		&ac.ID, &ac.OrgID, &ac.Name, &ac.SystemPromptTemplateID, &ac.SystemPromptOverride,
		&ac.Model, &ac.Temperature, &ac.MaxOutputTokens, &ac.TopP, &ac.ContextWindowLimit,
		&ac.ToolPolicy, &ac.ToolAllowlist, &ac.ToolDenylist, &ac.ContentFilterLevel, &ac.SafetyRulesJSON,
		&ac.ProjectID, &ac.SkillID, &ac.IsDefault, &ac.CreatedAt,
	)
	return ac, err
}

func (r *AgentConfigRepository) Create(ctx context.Context, orgID uuid.UUID, req CreateAgentConfigRequest) (AgentConfig, error) {
	if orgID == uuid.Nil {
		return AgentConfig{}, fmt.Errorf("agent_configs: org_id must not be empty")
	}
	if req.Name == "" {
		return AgentConfig{}, fmt.Errorf("agent_configs: name must not be empty")
	}

	toolPolicy := req.ToolPolicy
	if toolPolicy == "" {
		toolPolicy = "allowlist"
	}
	contentFilterLevel := req.ContentFilterLevel
	if contentFilterLevel == "" {
		contentFilterLevel = "standard"
	}
	if req.ToolAllowlist == nil {
		req.ToolAllowlist = []string{}
	}
	if req.ToolDenylist == nil {
		req.ToolDenylist = []string{}
	}
	if req.SafetyRulesJSON == nil {
		req.SafetyRulesJSON = map[string]any{}
	}

	ac, err := scanAgentConfig(r.db.QueryRow(
		ctx,
		`INSERT INTO agent_configs (
			org_id, name, system_prompt_template_id, system_prompt_override,
			model, temperature, max_output_tokens, top_p, context_window_limit,
			tool_policy, tool_allowlist, tool_denylist, content_filter_level, safety_rules_json,
			project_id, skill_id, is_default
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15,$16,$17)
		RETURNING `+agentConfigColumns,
		orgID, req.Name, req.SystemPromptTemplateID, req.SystemPromptOverride,
		req.Model, req.Temperature, req.MaxOutputTokens, req.TopP, req.ContextWindowLimit,
		toolPolicy, req.ToolAllowlist, req.ToolDenylist, contentFilterLevel, req.SafetyRulesJSON,
		req.ProjectID, req.SkillID, req.IsDefault,
	))
	if err != nil {
		return AgentConfig{}, fmt.Errorf("agent_configs.Create: %w", err)
	}
	return ac, nil
}

func (r *AgentConfigRepository) GetByID(ctx context.Context, id uuid.UUID) (*AgentConfig, error) {
	ac, err := scanAgentConfig(r.db.QueryRow(
		ctx,
		`SELECT `+agentConfigColumns+` FROM agent_configs WHERE id = $1`,
		id,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("agent_configs.GetByID: %w", err)
	}
	return &ac, nil
}

func (r *AgentConfigRepository) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]AgentConfig, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT `+agentConfigColumns+` FROM agent_configs WHERE org_id = $1 ORDER BY created_at ASC`,
		orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("agent_configs.ListByOrg: %w", err)
	}
	defer rows.Close()

	var configs []AgentConfig
	for rows.Next() {
		ac, err := scanAgentConfig(rows)
		if err != nil {
			return nil, fmt.Errorf("agent_configs.ListByOrg scan: %w", err)
		}
		configs = append(configs, ac)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agent_configs.ListByOrg rows: %w", err)
	}
	return configs, nil
}

// GetDefaultForOrg 返回 org 级默认配置（无 project 绑定）。
func (r *AgentConfigRepository) GetDefaultForOrg(ctx context.Context, orgID uuid.UUID) (*AgentConfig, error) {
	ac, err := scanAgentConfig(r.db.QueryRow(
		ctx,
		`SELECT `+agentConfigColumns+`
		 FROM agent_configs
		 WHERE org_id = $1 AND is_default = true AND project_id IS NULL
		 LIMIT 1`,
		orgID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("agent_configs.GetDefaultForOrg: %w", err)
	}
	return &ac, nil
}

// GetDefaultForProject 返回 project 级默认配置。
func (r *AgentConfigRepository) GetDefaultForProject(ctx context.Context, orgID uuid.UUID, projectID uuid.UUID) (*AgentConfig, error) {
	ac, err := scanAgentConfig(r.db.QueryRow(
		ctx,
		`SELECT `+agentConfigColumns+`
		 FROM agent_configs
		 WHERE org_id = $1 AND project_id = $2 AND is_default = true
		 LIMIT 1`,
		orgID, projectID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("agent_configs.GetDefaultForProject: %w", err)
	}
	return &ac, nil
}

func (r *AgentConfigRepository) Update(ctx context.Context, id uuid.UUID, orgID uuid.UUID, fields AgentConfigUpdateFields) (*AgentConfig, error) {
	if !fields.SetName && !fields.SetSystemPromptTemplateID && !fields.SetSystemPromptOverride &&
		!fields.SetModel && !fields.SetTemperature && !fields.SetMaxOutputTokens &&
		!fields.SetTopP && !fields.SetContextWindowLimit && !fields.SetToolPolicy && !fields.SetIsDefault {
		return nil, fmt.Errorf("agent_configs.Update: no fields to update")
	}

	ac, err := scanAgentConfig(r.db.QueryRow(
		ctx,
		`UPDATE agent_configs
		 SET name                      = CASE WHEN $3  THEN $4  ELSE name END,
		     system_prompt_template_id = CASE WHEN $5  THEN $6  ELSE system_prompt_template_id END,
		     system_prompt_override    = CASE WHEN $7  THEN $8  ELSE system_prompt_override END,
		     model                     = CASE WHEN $9  THEN $10 ELSE model END,
		     temperature               = CASE WHEN $11 THEN $12 ELSE temperature END,
		     max_output_tokens         = CASE WHEN $13 THEN $14 ELSE max_output_tokens END,
		     top_p                     = CASE WHEN $15 THEN $16 ELSE top_p END,
		     context_window_limit      = CASE WHEN $17 THEN $18 ELSE context_window_limit END,
		     tool_policy               = CASE WHEN $19 THEN $20 ELSE tool_policy END,
		     is_default                = CASE WHEN $21 THEN $22 ELSE is_default END
		 WHERE id = $1 AND org_id = $2
		 RETURNING `+agentConfigColumns,
		id, orgID,
		fields.SetName, fields.Name,
		fields.SetSystemPromptTemplateID, fields.SystemPromptTemplateID,
		fields.SetSystemPromptOverride, fields.SystemPromptOverride,
		fields.SetModel, fields.Model,
		fields.SetTemperature, fields.Temperature,
		fields.SetMaxOutputTokens, fields.MaxOutputTokens,
		fields.SetTopP, fields.TopP,
		fields.SetContextWindowLimit, fields.ContextWindowLimit,
		fields.SetToolPolicy, fields.ToolPolicy,
		fields.SetIsDefault, fields.IsDefault,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("agent_configs.Update: %w", err)
	}
	return &ac, nil
}
