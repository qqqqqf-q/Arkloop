package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *PersonasRepository) WithTx(tx pgx.Tx) *PersonasRepository {
	return &PersonasRepository{db: tx}
}

type PersonaConflictError struct {
	PersonaKey string
	Version  string
}

func (e PersonaConflictError) Error() string {
	return fmt.Sprintf("persona %q@%q already exists", e.PersonaKey, e.Version)
}

type Persona struct {
	ID                  uuid.UUID
	OrgID               *uuid.UUID
	PersonaKey            string
	Version             string
	DisplayName         string
	Description         *string
	PromptMD            string
	ToolAllowlist       []string
	BudgetsJSON         json.RawMessage
	IsActive            bool
	CreatedAt           time.Time
	PreferredCredential *string
	ExecutorType        string
	ExecutorConfigJSON  json.RawMessage
}

type PersonaPatch struct {
	DisplayName         *string
	Description         *string
	PromptMD            *string
	ToolAllowlist       []string
	BudgetsJSON         json.RawMessage
	IsActive            *bool
	PreferredCredential *string
	ExecutorType        *string
	ExecutorConfigJSON  json.RawMessage
}

type PersonasRepository struct {
	db Querier
}

func NewPersonasRepository(db Querier) (*PersonasRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &PersonasRepository{db: db}, nil
}

func (r *PersonasRepository) Create(
	ctx context.Context,
	orgID uuid.UUID,
	personaKey string,
	version string,
	displayName string,
	description *string,
	promptMD string,
	toolAllowlist []string,
	budgetsJSON json.RawMessage,
	preferredCredential *string,
	executorType string,
	executorConfigJSON json.RawMessage,
) (Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return Persona{}, fmt.Errorf("org_id must not be nil")
	}
	if strings.TrimSpace(personaKey) == "" {
		return Persona{}, fmt.Errorf("persona_key must not be empty")
	}
	if strings.TrimSpace(version) == "" {
		return Persona{}, fmt.Errorf("version must not be empty")
	}
	if strings.TrimSpace(displayName) == "" {
		return Persona{}, fmt.Errorf("display_name must not be empty")
	}
	if strings.TrimSpace(promptMD) == "" {
		return Persona{}, fmt.Errorf("prompt_md must not be empty")
	}

	if len(budgetsJSON) == 0 {
		budgetsJSON = json.RawMessage("{}")
	}
	if toolAllowlist == nil {
		toolAllowlist = []string{}
	}
	if preferredCredential != nil && strings.TrimSpace(*preferredCredential) == "" {
		preferredCredential = nil
	}
	if strings.TrimSpace(executorType) == "" {
		executorType = "agent.simple"
	}
	if len(executorConfigJSON) == 0 {
		executorConfigJSON = json.RawMessage("{}")
	}

	var persona Persona
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO personas
		    (org_id, persona_key, version, display_name, description, prompt_md,
		     tool_allowlist, budgets_json, preferred_credential,
		     executor_type, executor_config_json)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id, org_id, persona_key, version, display_name, description,
		           prompt_md, tool_allowlist, budgets_json, is_active, created_at,
		           preferred_credential, executor_type, executor_config_json`,
		orgID, personaKey, version, displayName, description, promptMD,
		toolAllowlist, budgetsJSON, preferredCredential,
		executorType, executorConfigJSON,
	).Scan(
		&persona.ID, &persona.OrgID, &persona.PersonaKey, &persona.Version,
		&persona.DisplayName, &persona.Description, &persona.PromptMD,
		&persona.ToolAllowlist, &persona.BudgetsJSON, &persona.IsActive, &persona.CreatedAt,
		&persona.PreferredCredential, &persona.ExecutorType, &persona.ExecutorConfigJSON,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return Persona{}, PersonaConflictError{PersonaKey: personaKey, Version: version}
		}
		return Persona{}, err
	}
	return persona, nil
}

func (r *PersonasRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var persona Persona
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, persona_key, version, display_name, description,
		        prompt_md, tool_allowlist, budgets_json, is_active, created_at,
		        preferred_credential, executor_type, executor_config_json
		 FROM personas
		 WHERE id = $1 AND org_id = $2`,
		id, orgID,
	).Scan(
		&persona.ID, &persona.OrgID, &persona.PersonaKey, &persona.Version,
		&persona.DisplayName, &persona.Description, &persona.PromptMD,
		&persona.ToolAllowlist, &persona.BudgetsJSON, &persona.IsActive, &persona.CreatedAt,
		&persona.PreferredCredential, &persona.ExecutorType, &persona.ExecutorConfigJSON,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &persona, nil
}

// ListByOrg 返回该 org 的所有 persona（含 org_id IS NULL 的全局 persona）。
func (r *PersonasRepository) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, persona_key, version, display_name, description,
		        prompt_md, tool_allowlist, budgets_json, is_active, created_at,
		        preferred_credential, executor_type, executor_config_json
		 FROM personas
		 WHERE org_id = $1 OR org_id IS NULL
		 ORDER BY created_at ASC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPersonas(rows)
}

// ListActiveByOrg 仅返回该 org 的 is_active=true 的 persona，供 Worker 执行时使用。
// 不包含全局（org_id IS NULL）persona，全局 persona 由文件系统负责。
func (r *PersonasRepository) ListActiveByOrg(ctx context.Context, orgID uuid.UUID) ([]Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, persona_key, version, display_name, description,
		        prompt_md, tool_allowlist, budgets_json, is_active, created_at,
		        preferred_credential, executor_type, executor_config_json
		 FROM personas
		 WHERE org_id = $1 AND is_active = TRUE
		 ORDER BY created_at ASC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPersonas(rows)
}

func (r *PersonasRepository) Patch(ctx context.Context, orgID, id uuid.UUID, patch PersonaPatch) (*Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	setClauses := []string{}
	args := []any{}
	argIdx := 1

	if patch.DisplayName != nil {
		trimmed := strings.TrimSpace(*patch.DisplayName)
		if trimmed == "" {
			return nil, fmt.Errorf("display_name must not be empty")
		}
		setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argIdx))
		args = append(args, trimmed)
		argIdx++
	}
	if patch.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, *patch.Description)
		argIdx++
	}
	if patch.PromptMD != nil {
		trimmed := strings.TrimSpace(*patch.PromptMD)
		if trimmed == "" {
			return nil, fmt.Errorf("prompt_md must not be empty")
		}
		setClauses = append(setClauses, fmt.Sprintf("prompt_md = $%d", argIdx))
		args = append(args, trimmed)
		argIdx++
	}
	if patch.ToolAllowlist != nil {
		setClauses = append(setClauses, fmt.Sprintf("tool_allowlist = $%d", argIdx))
		args = append(args, patch.ToolAllowlist)
		argIdx++
	}
	if len(patch.BudgetsJSON) > 0 {
		setClauses = append(setClauses, fmt.Sprintf("budgets_json = $%d", argIdx))
		args = append(args, patch.BudgetsJSON)
		argIdx++
	}
	if patch.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIdx))
		args = append(args, *patch.IsActive)
		argIdx++
	}
	if patch.PreferredCredential != nil {
		if strings.TrimSpace(*patch.PreferredCredential) == "" {
			setClauses = append(setClauses, "preferred_credential = NULL")
		} else {
			setClauses = append(setClauses, fmt.Sprintf("preferred_credential = $%d", argIdx))
			args = append(args, strings.TrimSpace(*patch.PreferredCredential))
			argIdx++
		}
	}
	if patch.ExecutorType != nil {
		trimmed := strings.TrimSpace(*patch.ExecutorType)
		if trimmed == "" {
			trimmed = "agent.simple"
		}
		setClauses = append(setClauses, fmt.Sprintf("executor_type = $%d", argIdx))
		args = append(args, trimmed)
		argIdx++
	}
	if len(patch.ExecutorConfigJSON) > 0 {
		setClauses = append(setClauses, fmt.Sprintf("executor_config_json = $%d", argIdx))
		args = append(args, patch.ExecutorConfigJSON)
		argIdx++
	}

	if len(setClauses) == 0 {
		return r.GetByID(ctx, orgID, id)
	}

	args = append(args, id, orgID)
	idIdx := argIdx
	orgIdx := argIdx + 1

	var persona Persona
	err := r.db.QueryRow(
		ctx,
		fmt.Sprintf(`UPDATE personas
		 SET %s
		 WHERE id = $%d AND org_id = $%d
		 RETURNING id, org_id, persona_key, version, display_name, description,
		           prompt_md, tool_allowlist, budgets_json, is_active, created_at,
		           preferred_credential, executor_type, executor_config_json`,
			strings.Join(setClauses, ", "), idIdx, orgIdx),
		args...,
	).Scan(
		&persona.ID, &persona.OrgID, &persona.PersonaKey, &persona.Version,
		&persona.DisplayName, &persona.Description, &persona.PromptMD,
		&persona.ToolAllowlist, &persona.BudgetsJSON, &persona.IsActive, &persona.CreatedAt,
		&persona.PreferredCredential, &persona.ExecutorType, &persona.ExecutorConfigJSON,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if isUniqueViolation(err) {
			return nil, PersonaConflictError{}
		}
		return nil, err
	}
	return &persona, nil
}

func scanPersonas(rows pgx.Rows) ([]Persona, error) {
	personas := []Persona{}
	for rows.Next() {
		var s Persona
		if err := rows.Scan(
			&s.ID, &s.OrgID, &s.PersonaKey, &s.Version,
			&s.DisplayName, &s.Description, &s.PromptMD,
			&s.ToolAllowlist, &s.BudgetsJSON, &s.IsActive, &s.CreatedAt,
			&s.PreferredCredential, &s.ExecutorType, &s.ExecutorConfigJSON,
		); err != nil {
			return nil, err
		}
		personas = append(personas, s)
	}
	return personas, rows.Err()
}
