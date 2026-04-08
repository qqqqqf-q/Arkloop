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

const (
	PersonaScopeProject  = "project"
	PersonaScopePlatform = "platform"

	PersonaSyncModeNone               = "none"
	PersonaSyncModePlatformFileMirror = "platform_file_mirror"
)

const personaSelectColumns = `id, account_id, project_id, persona_key, version, display_name, description,
	    soul_md, user_selectable, selector_name, selector_order,
	    prompt_md, tool_allowlist, tool_denylist, COALESCE(core_tools, '{}'), budgets_json, roles_json, title_summarize_json, result_summarize_json, conditional_tools_json,
	    is_active, created_at, updated_at,
	    preferred_credential, model, reasoning_mode, stream_thinking, prompt_cache_control,
	    executor_type, executor_config_json,
	    sync_mode, mirrored_file_dir, last_synced_at`

const personaSelectColumnsQualified = `p.id, p.account_id, p.project_id, p.persona_key, p.version, p.display_name, p.description,
	    p.soul_md, p.user_selectable, p.selector_name, p.selector_order,
	    p.prompt_md, p.tool_allowlist, p.tool_denylist, COALESCE(p.core_tools, '{}'), p.budgets_json, p.roles_json, p.title_summarize_json, p.result_summarize_json, p.conditional_tools_json,
	    p.is_active, p.created_at, p.updated_at,
	    p.preferred_credential, p.model, p.reasoning_mode, p.stream_thinking, p.prompt_cache_control,
	    p.executor_type, p.executor_config_json,
	    p.sync_mode, p.mirrored_file_dir, p.last_synced_at`

func (r *PersonasRepository) WithTx(tx pgx.Tx) *PersonasRepository {
	return &PersonasRepository{db: tx}
}

type PersonaConflictError struct {
	PersonaKey string
	Version    string
}

func (e PersonaConflictError) Error() string {
	return fmt.Sprintf("persona %q@%q already exists", e.PersonaKey, e.Version)
}

type Persona struct {
	ID                   uuid.UUID
	AccountID            *uuid.UUID
	ProjectID            *uuid.UUID
	PersonaKey           string
	Version              string
	DisplayName          string
	Description          *string
	SoulMD               string
	UserSelectable       bool
	SelectorName         *string
	SelectorOrder        *int
	PromptMD             string
	ToolAllowlist        []string
	ToolDenylist         []string
	CoreTools            []string
	BudgetsJSON          json.RawMessage
	RolesJSON            json.RawMessage
	TitleSummarizeJSON   json.RawMessage
	ResultSummarizeJSON  json.RawMessage
	ConditionalToolsJSON json.RawMessage
	IsActive             bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
	PreferredCredential  *string
	Model                *string
	ReasoningMode        string
	StreamThinking       bool
	PromptCacheControl   string
	ExecutorType         string
	ExecutorConfigJSON   json.RawMessage
	SyncMode             string
	MirroredFileDir      *string
	LastSyncedAt         *time.Time
}

type PersonaPatch struct {
	DisplayName          *string
	Description          *string
	PromptMD             *string
	ToolAllowlist        []string
	ToolDenylist         []string
	CoreTools            []string
	BudgetsJSON          json.RawMessage
	RolesJSON            json.RawMessage
	ResultSummarizeJSON  json.RawMessage
	ConditionalToolsJSON json.RawMessage
	IsActive             *bool
	PreferredCredential  *string
	Model                *string
	ReasoningMode        *string
	StreamThinking       *bool
	PromptCacheControl   *string
	ExecutorType         *string
	ExecutorConfigJSON   json.RawMessage
}

type PlatformMirrorUpsertParams struct {
	PersonaKey           string
	Version              string
	DisplayName          string
	Description          *string
	SoulMD               string
	UserSelectable       bool
	SelectorName         *string
	SelectorOrder        *int
	PromptMD             string
	ToolAllowlist        []string
	ToolDenylist         []string
	CoreTools            []string
	BudgetsJSON          json.RawMessage
	RolesJSON            json.RawMessage
	TitleSummarizeJSON   json.RawMessage
	ResultSummarizeJSON  json.RawMessage
	ConditionalToolsJSON json.RawMessage
	PreferredCredential  *string
	Model                *string
	ReasoningMode        string
	StreamThinking       *bool // nil: default true (SaaS / YAML omit)
	PromptCacheControl   string
	ExecutorType         string
	ExecutorConfigJSON   json.RawMessage
	IsActive             bool
	MirroredFileDir      string
	LastSyncedAt         *time.Time
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

func (r *PersonasRepository) GetOrCreateDefaultProjectIDByOwner(
	ctx context.Context,
	accountID uuid.UUID,
	ownerUserID uuid.UUID,
) (uuid.UUID, error) {
	projectRepo, err := NewProjectRepository(r.db)
	if err != nil {
		return uuid.Nil, err
	}
	project, err := projectRepo.GetOrCreateDefaultByOwner(ctx, accountID, ownerUserID)
	if err != nil {
		return uuid.Nil, err
	}
	return project.ID, nil
}

type personaScanner interface {
	Scan(dest ...any) error
}

func scanPersona(scanner personaScanner, persona *Persona) error {
	return scanner.Scan(
		&persona.ID, &persona.AccountID, &persona.ProjectID, &persona.PersonaKey, &persona.Version,
		&persona.DisplayName, &persona.Description,
		&persona.SoulMD, &persona.UserSelectable, &persona.SelectorName, &persona.SelectorOrder,
		&persona.PromptMD, &persona.ToolAllowlist, &persona.ToolDenylist, &persona.CoreTools, &persona.BudgetsJSON, &persona.RolesJSON, &persona.TitleSummarizeJSON, &persona.ResultSummarizeJSON, &persona.ConditionalToolsJSON,
		&persona.IsActive, &persona.CreatedAt, &persona.UpdatedAt,
		&persona.PreferredCredential, &persona.Model, &persona.ReasoningMode, &persona.StreamThinking, &persona.PromptCacheControl,
		&persona.ExecutorType, &persona.ExecutorConfigJSON,
		&persona.SyncMode, &persona.MirroredFileDir, &persona.LastSyncedAt,
	)
}

func NormalizePersonaScope(scope string) (string, error) {
	switch strings.TrimSpace(scope) {
	case "user", PersonaScopeProject:
		return PersonaScopeProject, nil
	case PersonaScopePlatform:
		return PersonaScopePlatform, nil
	default:
		return "", fmt.Errorf("scope must be user or platform")
	}
}

func (r *PersonasRepository) Create(
	ctx context.Context,
	projectID uuid.UUID,
	personaKey string,
	version string,
	displayName string,
	description *string,
	promptMD string,
	toolAllowlist []string,
	toolDenylist []string,
	budgetsJSON json.RawMessage,
	rolesJSON json.RawMessage,
	conditionalToolsJSON json.RawMessage,
	preferredCredential *string,
	model *string,
	reasoningMode string,
	streamThinking bool,
	promptCacheControl string,
	executorType string,
	executorConfigJSON json.RawMessage,
) (Persona, error) {
	if projectID == uuid.Nil {
		return Persona{}, fmt.Errorf("project_id must not be nil")
	}
	projectIDCopy := projectID
	return r.createWithProjectID(
		ctx,
		&projectIDCopy,
		personaKey,
		version,
		displayName,
		description,
		promptMD,
		toolAllowlist,
		toolDenylist,
		budgetsJSON,
		rolesJSON,
		conditionalToolsJSON,
		preferredCredential,
		model,
		reasoningMode,
		streamThinking,
		promptCacheControl,
		executorType,
		executorConfigJSON,
	)
}

func (r *PersonasRepository) CreateInScope(
	ctx context.Context,
	projectID uuid.UUID,
	scope string,
	personaKey string,
	version string,
	displayName string,
	description *string,
	promptMD string,
	toolAllowlist []string,
	toolDenylist []string,
	budgetsJSON json.RawMessage,
	rolesJSON json.RawMessage,
	conditionalToolsJSON json.RawMessage,
	preferredCredential *string,
	model *string,
	reasoningMode string,
	streamThinking bool,
	promptCacheControl string,
	executorType string,
	executorConfigJSON json.RawMessage,
) (Persona, error) {
	normalized, err := NormalizePersonaScope(scope)
	if err != nil {
		return Persona{}, err
	}
	var projectIDPtr *uuid.UUID
	if normalized == PersonaScopeProject {
		if projectID == uuid.Nil {
			return Persona{}, fmt.Errorf("project_id must not be nil")
		}
		projectIDCopy := projectID
		projectIDPtr = &projectIDCopy
	}
	return r.createWithProjectID(
		ctx,
		projectIDPtr,
		personaKey,
		version,
		displayName,
		description,
		promptMD,
		toolAllowlist,
		toolDenylist,
		budgetsJSON,
		rolesJSON,
		conditionalToolsJSON,
		preferredCredential,
		model,
		reasoningMode,
		streamThinking,
		promptCacheControl,
		executorType,
		executorConfigJSON,
	)
}

func (r *PersonasRepository) createWithProjectID(
	ctx context.Context,
	projectID *uuid.UUID,
	personaKey string,
	version string,
	displayName string,
	description *string,
	promptMD string,
	toolAllowlist []string,
	toolDenylist []string,
	budgetsJSON json.RawMessage,
	rolesJSON json.RawMessage,
	conditionalToolsJSON json.RawMessage,
	preferredCredential *string,
	model *string,
	reasoningMode string,
	streamThinking bool,
	promptCacheControl string,
	executorType string,
	executorConfigJSON json.RawMessage,
) (Persona, error) {
	if ctx == nil {
		ctx = context.Background()
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
	if len(rolesJSON) == 0 {
		rolesJSON = json.RawMessage("{}")
	}
	validatedConditionalToolsJSON, err := validateConditionalToolsJSON(conditionalToolsJSON)
	if err != nil {
		return Persona{}, err
	}
	conditionalToolsJSON = validatedConditionalToolsJSON
	if toolAllowlist == nil {
		toolAllowlist = []string{}
	}
	if toolDenylist == nil {
		toolDenylist = []string{}
	}
	preferredCredential = normalizeOptionalPersonaString(preferredCredential)
	model = normalizeOptionalPersonaString(model)
	reasoningMode = normalizePersonaReasoningMode(reasoningMode)
	promptCacheControl = normalizePersonaPromptCacheControl(promptCacheControl)
	if strings.TrimSpace(executorType) == "" {
		executorType = "agent.simple"
	}
	validatedRolesJSON, err := NormalizePersonaRolesJSON(rolesJSON)
	if err != nil {
		return Persona{}, err
	}
	rolesJSON = validatedRolesJSON
	validatedExecutorConfigJSON, err := validateRuntimeExecutorConfigJSON(executorType, executorConfigJSON)
	if err != nil {
		return Persona{}, err
	}
	executorConfigJSON = validatedExecutorConfigJSON

	syncMode := PersonaSyncModeNone
	var mirroredFileDir *string
	if projectID == nil {
		syncMode = PersonaSyncModePlatformFileMirror
		value := strings.TrimSpace(personaKey)
		mirroredFileDir = &value
	}

	var persona Persona
	row := r.db.QueryRow(
		ctx,
		`INSERT INTO personas
		    (project_id, persona_key, version, display_name, description, soul_md,
		     user_selectable, selector_name, selector_order,
		     prompt_md, tool_allowlist, tool_denylist, budgets_json, roles_json, title_summarize_json, result_summarize_json, conditional_tools_json,
		     preferred_credential, model, reasoning_mode, stream_thinking, prompt_cache_control,
		     executor_type, executor_config_json,
		     sync_mode, mirrored_file_dir, updated_at)
		 VALUES ($1, $2, $3, $4, $5, '', FALSE, NULL, NULL, $6, $7, $8, $9, $10, NULL, NULL, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, now())
		 RETURNING `+personaSelectColumns,
		projectID, personaKey, version, displayName, description, promptMD,
		toolAllowlist, toolDenylist, budgetsJSON, rolesJSON, conditionalToolsJSON, preferredCredential,
		model, reasoningMode, streamThinking, promptCacheControl,
		executorType, executorConfigJSON, syncMode, mirroredFileDir,
	)
	err = scanPersona(row, &persona)
	if err != nil {
		if isUniqueViolation(err) {
			return Persona{}, PersonaConflictError{PersonaKey: personaKey, Version: version}
		}
		return Persona{}, err
	}
	return persona, nil
}

func (r *PersonasRepository) GetByID(ctx context.Context, projectID, id uuid.UUID) (*Persona, error) {
	return r.GetByIDInScope(ctx, projectID, id, PersonaScopeProject)
}

func (r *PersonasRepository) GetByIDForAccount(ctx context.Context, accountID, id uuid.UUID) (*Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be nil")
	}
	if id == uuid.Nil {
		return nil, fmt.Errorf("id must not be nil")
	}

	var persona Persona
	err := scanPersona(r.db.QueryRow(
		ctx,
		fmt.Sprintf(`SELECT %s
		 FROM personas p
		 LEFT JOIN projects pr ON pr.id = p.project_id
		 WHERE p.id = $1
		   AND (
		     pr.account_id = $2
		     OR p.project_id IS NULL
		   )`, personaSelectColumnsQualified),
		id,
		accountID,
	), &persona)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &persona, nil
}

func (r *PersonasRepository) GetByIDInScope(ctx context.Context, projectID, id uuid.UUID, scope string) (*Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	whereClause, scopeArgs, err := personaScopeWhereClause(scope, projectID, 2)
	if err != nil {
		return nil, err
	}

	var persona Persona
	args := append([]any{id}, scopeArgs...)
	err = scanPersona(r.db.QueryRow(
		ctx,
		fmt.Sprintf(`SELECT %s FROM personas WHERE id = $1 AND %s`, personaSelectColumns, whereClause),
		args...,
	), &persona)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &persona, nil
}

func (r *PersonasRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]Persona, error) {
	return r.ListByScope(ctx, projectID, PersonaScopeProject)
}

func (r *PersonasRepository) GetByKeyVersionInProject(ctx context.Context, projectID uuid.UUID, personaKey, version string) (*Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if projectID == uuid.Nil {
		return nil, fmt.Errorf("project_id must not be nil")
	}
	if strings.TrimSpace(personaKey) == "" {
		return nil, fmt.Errorf("persona_key must not be empty")
	}
	if strings.TrimSpace(version) == "" {
		return nil, fmt.Errorf("version must not be empty")
	}

	var persona Persona
	err := scanPersona(r.db.QueryRow(
		ctx,
		`SELECT `+personaSelectColumns+`
		 FROM personas
		 WHERE project_id = $1
		   AND persona_key = $2
		   AND version = $3
		 LIMIT 1`,
		projectID,
		strings.TrimSpace(personaKey),
		strings.TrimSpace(version),
	), &persona)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &persona, nil
}

func (r *PersonasRepository) ListByScope(ctx context.Context, projectID uuid.UUID, scope string) ([]Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	whereClause, scopeArgs, err := personaScopeWhereClause(scope, projectID, 1)
	if err != nil {
		return nil, err
	}

	rows, err := r.db.Query(
		ctx,
		fmt.Sprintf(`SELECT %s FROM personas
		 WHERE %s
		 ORDER BY created_at ASC`, personaSelectColumns, whereClause),
		scopeArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPersonas(rows)
}

func (r *PersonasRepository) ListActiveByProject(ctx context.Context, projectID uuid.UUID) ([]Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT `+personaSelectColumns+`
		 FROM personas
		 WHERE project_id = $1 AND is_active = TRUE
		 ORDER BY created_at ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPersonas(rows)
}

func (r *PersonasRepository) ListActiveEffective(ctx context.Context, projectID uuid.UUID) ([]Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT `+personaSelectColumns+`
		 FROM personas
		 WHERE is_active = TRUE AND (project_id IS NULL OR project_id = $1)
		 ORDER BY CASE WHEN project_id IS NULL THEN 0 ELSE 1 END ASC, created_at ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPersonas(rows)
}

func (r *PersonasRepository) CloneToProject(ctx context.Context, projectID uuid.UUID, source Persona) (Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if projectID == uuid.Nil {
		return Persona{}, fmt.Errorf("project_id must not be nil")
	}
	if source.ID == uuid.Nil {
		return Persona{}, fmt.Errorf("source persona id must not be nil")
	}
	if strings.TrimSpace(source.PersonaKey) == "" {
		return Persona{}, fmt.Errorf("source persona_key must not be empty")
	}
	if strings.TrimSpace(source.Version) == "" {
		return Persona{}, fmt.Errorf("source version must not be empty")
	}
	if strings.TrimSpace(source.DisplayName) == "" {
		return Persona{}, fmt.Errorf("source display_name must not be empty")
	}
	if strings.TrimSpace(source.PromptMD) == "" {
		return Persona{}, fmt.Errorf("source prompt_md must not be empty")
	}

	toolAllowlist := source.ToolAllowlist
	if toolAllowlist == nil {
		toolAllowlist = []string{}
	}
	toolDenylist := source.ToolDenylist
	if toolDenylist == nil {
		toolDenylist = []string{}
	}
	coreTools := source.CoreTools
	if coreTools == nil {
		coreTools = []string{}
	}
	budgetsJSON := source.BudgetsJSON
	if len(budgetsJSON) == 0 {
		budgetsJSON = json.RawMessage("{}")
	}
	rolesJSON := source.RolesJSON
	if len(rolesJSON) == 0 {
		rolesJSON = json.RawMessage("{}")
	}
	conditionalToolsJSON, err := validateConditionalToolsJSON(source.ConditionalToolsJSON)
	if err != nil {
		return Persona{}, err
	}
	executorConfigJSON := source.ExecutorConfigJSON
	if len(executorConfigJSON) == 0 {
		executorConfigJSON = json.RawMessage("{}")
	}

	var titleSummarizeJSON any
	if len(source.TitleSummarizeJSON) > 0 {
		titleSummarizeJSON = source.TitleSummarizeJSON
	}
	var resultSummarizeJSON any
	if len(source.ResultSummarizeJSON) > 0 {
		resultSummarizeJSON = source.ResultSummarizeJSON
	}

	var persona Persona
	err = scanPersona(r.db.QueryRow(
		ctx,
		`INSERT INTO personas
		    (project_id, persona_key, version, display_name, description, soul_md,
		     user_selectable, selector_name, selector_order,
		     prompt_md, tool_allowlist, tool_denylist, core_tools, budgets_json, roles_json, title_summarize_json, result_summarize_json, conditional_tools_json,
		     is_active, preferred_credential, model, reasoning_mode, stream_thinking, prompt_cache_control,
		     executor_type, executor_config_json,
		     sync_mode, mirrored_file_dir, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, now())
		 RETURNING `+personaSelectColumns,
		projectID,
		source.PersonaKey,
		source.Version,
		source.DisplayName,
		source.Description,
		source.SoulMD,
		source.UserSelectable,
		source.SelectorName,
		source.SelectorOrder,
		source.PromptMD,
		toolAllowlist,
		toolDenylist,
		coreTools,
		budgetsJSON,
		rolesJSON,
		titleSummarizeJSON,
		resultSummarizeJSON,
		conditionalToolsJSON,
		source.IsActive,
		source.PreferredCredential,
		source.Model,
		normalizePersonaReasoningMode(source.ReasoningMode),
		source.StreamThinking,
		normalizePersonaPromptCacheControl(source.PromptCacheControl),
		source.ExecutorType,
		executorConfigJSON,
		PersonaSyncModeNone,
		nil,
	), &persona)
	if err != nil {
		if isUniqueViolation(err) {
			return Persona{}, PersonaConflictError{PersonaKey: source.PersonaKey, Version: source.Version}
		}
		return Persona{}, err
	}
	return persona, nil
}

func (r *PersonasRepository) Patch(ctx context.Context, projectID, id uuid.UUID, patch PersonaPatch) (*Persona, error) {
	return r.PatchInScope(ctx, projectID, id, PersonaScopeProject, patch)
}

func (r *PersonasRepository) PatchInScope(ctx context.Context, projectID, id uuid.UUID, scope string, patch PersonaPatch) (*Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	setClauses := make([]string, 0, 12)
	args := make([]any, 0, 12)
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
	if patch.ToolDenylist != nil {
		setClauses = append(setClauses, fmt.Sprintf("tool_denylist = $%d", argIdx))
		args = append(args, patch.ToolDenylist)
		argIdx++
	}
	if patch.CoreTools != nil {
		setClauses = append(setClauses, fmt.Sprintf("core_tools = $%d", argIdx))
		args = append(args, patch.CoreTools)
		argIdx++
	}
	if len(patch.BudgetsJSON) > 0 {
		setClauses = append(setClauses, fmt.Sprintf("budgets_json = $%d", argIdx))
		args = append(args, patch.BudgetsJSON)
		argIdx++
	}
	if len(patch.RolesJSON) > 0 {
		validatedRolesJSON, err := NormalizePersonaRolesJSON(patch.RolesJSON)
		if err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("roles_json = $%d", argIdx))
		args = append(args, validatedRolesJSON)
		argIdx++
	}
	if len(patch.ConditionalToolsJSON) > 0 {
		validatedConditionalToolsJSON, err := validateConditionalToolsJSON(patch.ConditionalToolsJSON)
		if err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("conditional_tools_json = $%d", argIdx))
		args = append(args, validatedConditionalToolsJSON)
		argIdx++
	}
	if patch.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIdx))
		args = append(args, *patch.IsActive)
		argIdx++
	}
	if patch.PreferredCredential != nil {
		value := normalizeOptionalPersonaString(patch.PreferredCredential)
		if value == nil {
			setClauses = append(setClauses, "preferred_credential = NULL")
		} else {
			setClauses = append(setClauses, fmt.Sprintf("preferred_credential = $%d", argIdx))
			args = append(args, *value)
			argIdx++
		}
	}
	if patch.Model != nil {
		value := normalizeOptionalPersonaString(patch.Model)
		if value == nil {
			setClauses = append(setClauses, "model = NULL")
		} else {
			setClauses = append(setClauses, fmt.Sprintf("model = $%d", argIdx))
			args = append(args, *value)
			argIdx++
		}
	}
	if patch.ReasoningMode != nil {
		setClauses = append(setClauses, fmt.Sprintf("reasoning_mode = $%d", argIdx))
		args = append(args, normalizePersonaReasoningMode(*patch.ReasoningMode))
		argIdx++
	}
	if patch.StreamThinking != nil {
		setClauses = append(setClauses, fmt.Sprintf("stream_thinking = $%d", argIdx))
		args = append(args, *patch.StreamThinking)
		argIdx++
	}
	if patch.PromptCacheControl != nil {
		setClauses = append(setClauses, fmt.Sprintf("prompt_cache_control = $%d", argIdx))
		args = append(args, normalizePersonaPromptCacheControl(*patch.PromptCacheControl))
		argIdx++
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
	effectiveExecutorType := ""
	if patch.ExecutorType != nil {
		effectiveExecutorType = strings.TrimSpace(*patch.ExecutorType)
	}
	validatedExecutorConfigJSON, err := normalizePatchedRuntimeExecutorConfig(ctx, r.db, id, scope, projectID, effectiveExecutorType, patch.ExecutorConfigJSON)
	if err != nil {
		return nil, err
	}
	if len(validatedExecutorConfigJSON) > 0 {
		setClauses = append(setClauses, fmt.Sprintf("executor_config_json = $%d", argIdx))
		args = append(args, validatedExecutorConfigJSON)
		argIdx++
	}

	if len(setClauses) == 0 {
		return r.GetByIDInScope(ctx, projectID, id, scope)
	}

	setClauses = append(setClauses, "updated_at = now()")

	whereClause, scopeArgs, err := personaScopeWhereClause(scope, projectID, argIdx+1)
	if err != nil {
		return nil, err
	}
	args = append(args, id)
	args = append(args, scopeArgs...)
	idIdx := argIdx

	var persona Persona
	err = scanPersona(r.db.QueryRow(
		ctx,
		fmt.Sprintf(`UPDATE personas
		 SET %s
		 WHERE id = $%d AND %s
		 RETURNING %s`,
			strings.Join(setClauses, ", "), idIdx, whereClause, personaSelectColumns),
		args...,
	), &persona)
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
		if err := scanPersona(rows, &s); err != nil {
			return nil, err
		}
		personas = append(personas, s)
	}
	return personas, rows.Err()
}

func (r *PersonasRepository) DeleteInvalidLuaRuntimeRows(ctx context.Context) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tag, err := r.db.Exec(
		ctx,
		`DELETE FROM personas
		 WHERE executor_type = 'agent.lua'
		   AND COALESCE(executor_config_json ? 'script', FALSE) = FALSE
		   AND COALESCE(executor_config_json ? 'script_file', FALSE) = TRUE`,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *PersonasRepository) ListLatestPlatformMirrors(ctx context.Context) ([]Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT DISTINCT ON (persona_key) `+personaSelectColumns+`
		 FROM personas
		 WHERE project_id IS NULL AND sync_mode = $1 AND is_active = TRUE
		 ORDER BY persona_key ASC, is_active DESC, updated_at DESC, created_at DESC`,
		PersonaSyncModePlatformFileMirror,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPersonas(rows)
}

func (r *PersonasRepository) UpsertPlatformMirror(ctx context.Context, params PlatformMirrorUpsertParams) (*Persona, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(params.PersonaKey) == "" {
		return nil, fmt.Errorf("persona_key must not be empty")
	}
	if strings.TrimSpace(params.Version) == "" {
		return nil, fmt.Errorf("version must not be empty")
	}
	if strings.TrimSpace(params.DisplayName) == "" {
		return nil, fmt.Errorf("display_name must not be empty")
	}
	if strings.TrimSpace(params.PromptMD) == "" {
		return nil, fmt.Errorf("prompt_md must not be empty")
	}
	if params.ToolAllowlist == nil {
		params.ToolAllowlist = []string{}
	}
	if params.ToolDenylist == nil {
		params.ToolDenylist = []string{}
	}
	if len(params.BudgetsJSON) == 0 {
		params.BudgetsJSON = json.RawMessage("{}")
	}
	if len(params.RolesJSON) == 0 {
		params.RolesJSON = json.RawMessage("{}")
	}
	validatedConditionalToolsJSON, err := validateConditionalToolsJSON(params.ConditionalToolsJSON)
	if err != nil {
		return nil, err
	}
	params.ConditionalToolsJSON = validatedConditionalToolsJSON
	params.PreferredCredential = normalizeOptionalPersonaString(params.PreferredCredential)
	params.Model = normalizeOptionalPersonaString(params.Model)
	params.ReasoningMode = normalizePersonaReasoningMode(params.ReasoningMode)
	streamThinking := true
	if params.StreamThinking != nil {
		streamThinking = *params.StreamThinking
	}
	params.PromptCacheControl = normalizePersonaPromptCacheControl(params.PromptCacheControl)
	if strings.TrimSpace(params.ExecutorType) == "" {
		params.ExecutorType = "agent.simple"
	}
	validatedRolesJSON, err := NormalizePersonaRolesJSON(params.RolesJSON)
	if err != nil {
		return nil, err
	}
	params.RolesJSON = validatedRolesJSON
	validatedExecutorConfigJSON, err := validateRuntimeExecutorConfigJSON(params.ExecutorType, params.ExecutorConfigJSON)
	if err != nil {
		return nil, err
	}
	params.ExecutorConfigJSON = validatedExecutorConfigJSON

	if strings.TrimSpace(params.MirroredFileDir) == "" {
		params.MirroredFileDir = strings.TrimSpace(params.PersonaKey)
	}

	tx, err := beginTx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var persona Persona
	err = scanPersona(tx.QueryRow(
		ctx,
		`INSERT INTO personas (
			account_id, persona_key, version, display_name, description, soul_md,
			user_selectable, selector_name, selector_order,
			prompt_md, tool_allowlist, tool_denylist, core_tools, budgets_json, roles_json, title_summarize_json, result_summarize_json, conditional_tools_json,
			preferred_credential, model, reasoning_mode, stream_thinking, prompt_cache_control,
			executor_type, executor_config_json,
			is_active, sync_mode, mirrored_file_dir, last_synced_at, updated_at
		) VALUES (
			NULL, $1, $2, $3, $4, $5,
			$6, $7, $8,
			$9, $10, $11, $12, $13, $14, $15, $16, $17,
			$18, $19, $20, $21, $22,
			$23, $24,
			$25, $26, $27, $28, now()
		)
		ON CONFLICT (persona_key, version) WHERE project_id IS NULL
		DO UPDATE SET
			display_name = EXCLUDED.display_name,
			description = EXCLUDED.description,
			soul_md = EXCLUDED.soul_md,
			user_selectable = EXCLUDED.user_selectable,
			selector_name = EXCLUDED.selector_name,
			selector_order = EXCLUDED.selector_order,
			prompt_md = EXCLUDED.prompt_md,
			tool_allowlist = EXCLUDED.tool_allowlist,
			tool_denylist = EXCLUDED.tool_denylist,
			core_tools = EXCLUDED.core_tools,
			budgets_json = EXCLUDED.budgets_json,
			roles_json = EXCLUDED.roles_json,
			title_summarize_json = EXCLUDED.title_summarize_json,
			result_summarize_json = EXCLUDED.result_summarize_json,
			conditional_tools_json = EXCLUDED.conditional_tools_json,
			reasoning_mode = EXCLUDED.reasoning_mode,
			stream_thinking = EXCLUDED.stream_thinking,
			prompt_cache_control = EXCLUDED.prompt_cache_control,
			executor_type = EXCLUDED.executor_type,
			executor_config_json = EXCLUDED.executor_config_json,
			is_active = EXCLUDED.is_active,
			sync_mode = EXCLUDED.sync_mode,
			mirrored_file_dir = EXCLUDED.mirrored_file_dir,
			last_synced_at = EXCLUDED.last_synced_at,
			updated_at = now()
		RETURNING `+personaSelectColumns,
		params.PersonaKey, params.Version, params.DisplayName, params.Description, strings.TrimSpace(params.SoulMD),
		params.UserSelectable, params.SelectorName, params.SelectorOrder,
		strings.TrimSpace(params.PromptMD), params.ToolAllowlist, params.ToolDenylist, params.CoreTools, params.BudgetsJSON, params.RolesJSON, params.TitleSummarizeJSON, params.ResultSummarizeJSON, params.ConditionalToolsJSON,
		params.PreferredCredential, params.Model, params.ReasoningMode, streamThinking, params.PromptCacheControl,
		params.ExecutorType, params.ExecutorConfigJSON,
		params.IsActive, PersonaSyncModePlatformFileMirror, params.MirroredFileDir, params.LastSyncedAt,
	), &persona)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE personas
		 SET is_active = FALSE,
		     updated_at = now()
		 WHERE account_id IS NULL
		   AND project_id IS NULL
		   AND sync_mode = $1
		   AND persona_key = $2
		   AND id <> $3
		   AND is_active = TRUE`,
		PersonaSyncModePlatformFileMirror,
		params.PersonaKey,
		persona.ID,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &persona, nil
}

func (r *PersonasRepository) DeactivatePlatformMirrorsByKey(ctx context.Context, personaKey string) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tag, err := r.db.Exec(
		ctx,
		`UPDATE personas
		 SET is_active = FALSE,
		     updated_at = now()
		 WHERE account_id IS NULL
		   AND project_id IS NULL
		   AND sync_mode = $1
		   AND persona_key = $2
		   AND is_active = TRUE`,
		PersonaSyncModePlatformFileMirror,
		strings.TrimSpace(personaKey),
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *PersonasRepository) Delete(ctx context.Context, projectID, id uuid.UUID) (bool, error) {
	return r.DeleteInScope(ctx, projectID, id, PersonaScopeProject)
}

func (r *PersonasRepository) DeleteInScope(ctx context.Context, projectID, id uuid.UUID, scope string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	whereClause, scopeArgs, err := personaScopeWhereClause(scope, projectID, 2)
	if err != nil {
		return false, err
	}
	args := append([]any{id}, scopeArgs...)
	tag, err := r.db.Exec(
		ctx,
		fmt.Sprintf(`DELETE FROM personas WHERE id = $1 AND %s`, whereClause),
		args...,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func personaScopeWhereClause(scope string, projectID uuid.UUID, argIdx int) (string, []any, error) {
	normalized, err := NormalizePersonaScope(scope)
	if err != nil {
		return "", nil, err
	}
	if normalized == PersonaScopePlatform {
		return "project_id IS NULL", nil, nil
	}
	if projectID == uuid.Nil {
		return "", nil, fmt.Errorf("project_id must not be nil")
	}
	return fmt.Sprintf("project_id = $%d", argIdx), []any{projectID}, nil
}

func normalizeOptionalPersonaString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizePersonaReasoningMode(value string) string {
	s := strings.TrimSpace(value)
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

// NormalizePersonaReasoningMode 供 desktop 同步等跨包调用，与 normalizePersonaReasoningMode 一致。
func NormalizePersonaReasoningMode(value string) string {
	return normalizePersonaReasoningMode(value)
}

// NormalizePersonaStreamThinkingPtr 将 API / YAML 省略值视为 true（默认向客户端下发思维链流）。
func NormalizePersonaStreamThinkingPtr(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

func normalizePersonaPromptCacheControl(value string) string {
	switch strings.TrimSpace(value) {
	case "system_prompt", "none":
		return strings.TrimSpace(value)
	default:
		return "none"
	}
}

type conditionalToolRulePayload struct {
	When  conditionalToolWhenPayload `json:"when"`
	Tools []string                   `json:"tools"`
}

type conditionalToolWhenPayload struct {
	LacksInputModalities []string `json:"lacks_input_modalities,omitempty"`
}

func validateConditionalToolsJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var rules []conditionalToolRulePayload
	if err := json.Unmarshal(raw, &rules); err != nil {
		return nil, fmt.Errorf("conditional_tools must be valid json array: %w", err)
	}
	normalized := make([]conditionalToolRulePayload, 0, len(rules))
	for i, rule := range rules {
		tools, err := normalizeConditionalItems(rule.Tools, fmt.Sprintf("conditional_tools[%d].tools", i))
		if err != nil {
			return nil, err
		}
		modalities, err := normalizeConditionalItems(rule.When.LacksInputModalities, fmt.Sprintf("conditional_tools[%d].when.lacks_input_modalities", i))
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, conditionalToolRulePayload{
			When: conditionalToolWhenPayload{
				LacksInputModalities: modalities,
			},
			Tools: tools,
		})
	}
	if len(normalized) == 0 {
		return json.RawMessage("[]"), nil
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func normalizeConditionalItems(items []string, field string) ([]string, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("%s must not be empty", field)
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		cleaned := strings.TrimSpace(item)
		if cleaned == "" {
			continue
		}
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s must not be empty", field)
	}
	return out, nil
}

func validateRuntimeExecutorConfigJSON(executorType string, raw json.RawMessage) (json.RawMessage, error) {
	trimmedType := strings.TrimSpace(executorType)
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("executor_config must be valid json object: %w", err)
	}
	if obj == nil {
		obj = map[string]any{}
	}
	if trimmedType == "agent.lua" {
		if _, exists := obj["script_file"]; exists {
			return nil, fmt.Errorf("executor_config.script_file is not allowed for agent.lua runtime")
		}
		script, _ := obj["script"].(string)
		if strings.TrimSpace(script) == "" {
			return nil, fmt.Errorf("executor_config.script is required for agent.lua runtime")
		}
	} else {
		delete(obj, "script_file")
	}
	encoded, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func normalizePatchedRuntimeExecutorConfig(
	ctx context.Context,
	db Querier,
	id uuid.UUID,
	scope string,
	projectID uuid.UUID,
	requestedType string,
	patchConfig json.RawMessage,
) (json.RawMessage, error) {
	if len(patchConfig) == 0 && strings.TrimSpace(requestedType) == "" {
		return nil, nil
	}
	whereClause, scopeArgs, err := personaScopeWhereClause(scope, projectID, 2)
	if err != nil {
		return nil, err
	}
	var (
		currentType   string
		currentConfig json.RawMessage
	)
	args := append([]any{id}, scopeArgs...)
	err = db.QueryRow(
		ctx,
		fmt.Sprintf(`SELECT executor_type, executor_config_json FROM personas WHERE id = $1 AND %s`, whereClause),
		args...,
	).Scan(&currentType, &currentConfig)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	effectiveType := strings.TrimSpace(requestedType)
	if effectiveType == "" {
		effectiveType = currentType
	}
	effectiveConfig := currentConfig
	if len(patchConfig) > 0 {
		effectiveConfig = patchConfig
	}
	validated, err := validateRuntimeExecutorConfigJSON(effectiveType, effectiveConfig)
	if err != nil {
		return nil, err
	}
	if len(patchConfig) == 0 && effectiveType == currentType {
		return nil, nil
	}
	return validated, nil
}

type txBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

func beginTx(ctx context.Context, db Querier) (pgx.Tx, error) {
	beginner, ok := db.(txBeginner)
	if !ok {
		return nil, fmt.Errorf("querier does not support transactions")
	}
	return beginner.BeginTx(ctx, pgx.TxOptions{})
}

func (r *PersonasRepository) MarkSynced(ctx context.Context, id uuid.UUID, syncedAt time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(
		ctx,
		`UPDATE personas SET last_synced_at = $2 WHERE id = $1`,
		id,
		syncedAt.UTC(),
	)
	return err
}
