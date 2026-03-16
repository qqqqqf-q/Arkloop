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

// WithTx 返回一个使用给定事务的 MCPConfigsRepository 副本。
func (r *MCPConfigsRepository) WithTx(tx pgx.Tx) *MCPConfigsRepository {
	return &MCPConfigsRepository{db: tx}
}

type MCPConfigNameConflictError struct {
	Name string
}

func (e MCPConfigNameConflictError) Error() string {
	return fmt.Sprintf("mcp config %q already exists", e.Name)
}

type MCPConfig struct {
	ID               uuid.UUID
	AccountID            uuid.UUID
	Name             string
	Transport        string
	URL              *string
	AuthSecretID     *uuid.UUID
	Command          *string
	ArgsJSON         json.RawMessage
	CwdPath          *string
	EnvJSON          json.RawMessage
	InheritParentEnv bool
	CallTimeoutMs    int
	IsActive         bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type MCPConfigPatch struct {
	Name          *string
	URL           *string
	CallTimeoutMs *int
	IsActive      *bool
}

type MCPConfigsRepository struct {
	db Querier
}

func NewMCPConfigsRepository(db Querier) (*MCPConfigsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &MCPConfigsRepository{db: db}, nil
}

func (r *MCPConfigsRepository) Create(
	ctx context.Context,
	accountID uuid.UUID,
	name string,
	transport string,
	url *string,
	authSecretID *uuid.UUID,
	command *string,
	argsJSON json.RawMessage,
	cwdPath *string,
	envJSON json.RawMessage,
	inheritParentEnv bool,
	callTimeoutMs int,
) (MCPConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return MCPConfig{}, fmt.Errorf("account_id must not be nil")
	}
	if strings.TrimSpace(name) == "" {
		return MCPConfig{}, fmt.Errorf("name must not be empty")
	}

	if len(argsJSON) == 0 {
		argsJSON = json.RawMessage("[]")
	}
	if len(envJSON) == 0 {
		envJSON = json.RawMessage("{}")
	}

	var cfg MCPConfig
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO mcp_configs
		    (account_id, name, transport, url, auth_secret_id, command, args_json, cwd,
		     env_json, inherit_parent_env, call_timeout_ms)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id, account_id, name, transport, url, auth_secret_id, command,
		           args_json, cwd, env_json, inherit_parent_env,
		           call_timeout_ms, is_active, created_at, updated_at`,
		accountID, name, transport, url, authSecretID, command, argsJSON, cwdPath,
		envJSON, inheritParentEnv, callTimeoutMs,
	).Scan(
		&cfg.ID, &cfg.AccountID, &cfg.Name, &cfg.Transport, &cfg.URL,
		&cfg.AuthSecretID, &cfg.Command, &cfg.ArgsJSON, &cfg.CwdPath,
		&cfg.EnvJSON, &cfg.InheritParentEnv, &cfg.CallTimeoutMs,
		&cfg.IsActive, &cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return MCPConfig{}, MCPConfigNameConflictError{Name: name}
		}
		return MCPConfig{}, err
	}
	return cfg, nil
}

func (r *MCPConfigsRepository) GetByID(ctx context.Context, accountID, id uuid.UUID) (*MCPConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var cfg MCPConfig
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, name, transport, url, auth_secret_id, command,
		        args_json, cwd, env_json, inherit_parent_env,
		        call_timeout_ms, is_active, created_at, updated_at
		 FROM mcp_configs
		 WHERE id = $1 AND account_id = $2`,
		id, accountID,
	).Scan(
		&cfg.ID, &cfg.AccountID, &cfg.Name, &cfg.Transport, &cfg.URL,
		&cfg.AuthSecretID, &cfg.Command, &cfg.ArgsJSON, &cfg.CwdPath,
		&cfg.EnvJSON, &cfg.InheritParentEnv, &cfg.CallTimeoutMs,
		&cfg.IsActive, &cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &cfg, nil
}

func (r *MCPConfigsRepository) ListByAccount(ctx context.Context, accountID uuid.UUID) ([]MCPConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, account_id, name, transport, url, auth_secret_id, command,
		        args_json, cwd, env_json, inherit_parent_env,
		        call_timeout_ms, is_active, created_at, updated_at
		 FROM mcp_configs
		 WHERE account_id = $1
		 ORDER BY created_at DESC`,
		accountID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	configs := []MCPConfig{}
	for rows.Next() {
		var cfg MCPConfig
		if err := rows.Scan(
			&cfg.ID, &cfg.AccountID, &cfg.Name, &cfg.Transport, &cfg.URL,
			&cfg.AuthSecretID, &cfg.Command, &cfg.ArgsJSON, &cfg.CwdPath,
			&cfg.EnvJSON, &cfg.InheritParentEnv, &cfg.CallTimeoutMs,
			&cfg.IsActive, &cfg.CreatedAt, &cfg.UpdatedAt,
		); err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}
	return configs, rows.Err()
}

// ListActiveByOrg 仅返回 is_active=true 的配置，供 Worker 执行时使用。
func (r *MCPConfigsRepository) ListActiveByOrg(ctx context.Context, accountID uuid.UUID) ([]MCPConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, account_id, name, transport, url, auth_secret_id, command,
		        args_json, cwd, env_json, inherit_parent_env,
		        call_timeout_ms, is_active, created_at, updated_at
		 FROM mcp_configs
		 WHERE account_id = $1 AND is_active = TRUE
		 ORDER BY created_at ASC`,
		accountID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	configs := []MCPConfig{}
	for rows.Next() {
		var cfg MCPConfig
		if err := rows.Scan(
			&cfg.ID, &cfg.AccountID, &cfg.Name, &cfg.Transport, &cfg.URL,
			&cfg.AuthSecretID, &cfg.Command, &cfg.ArgsJSON, &cfg.CwdPath,
			&cfg.EnvJSON, &cfg.InheritParentEnv, &cfg.CallTimeoutMs,
			&cfg.IsActive, &cfg.CreatedAt, &cfg.UpdatedAt,
		); err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}
	return configs, rows.Err()
}

func (r *MCPConfigsRepository) Patch(ctx context.Context, accountID, id uuid.UUID, patch MCPConfigPatch) (*MCPConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	setClauses := []string{"updated_at = now()"}
	args := []any{}
	argIdx := 1

	if patch.Name != nil {
		trimmed := strings.TrimSpace(*patch.Name)
		if trimmed == "" {
			return nil, fmt.Errorf("name must not be empty")
		}
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, trimmed)
		argIdx++
	}
	if patch.URL != nil {
		setClauses = append(setClauses, fmt.Sprintf("url = $%d", argIdx))
		args = append(args, *patch.URL)
		argIdx++
	}
	if patch.CallTimeoutMs != nil {
		if *patch.CallTimeoutMs <= 0 {
			return nil, fmt.Errorf("call_timeout_ms must be positive")
		}
		setClauses = append(setClauses, fmt.Sprintf("call_timeout_ms = $%d", argIdx))
		args = append(args, *patch.CallTimeoutMs)
		argIdx++
	}
	if patch.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIdx))
		args = append(args, *patch.IsActive)
		argIdx++
	}

	args = append(args, id, accountID)
	idIdx := argIdx
	orgIdx := argIdx + 1

	var cfg MCPConfig
	err := r.db.QueryRow(
		ctx,
		fmt.Sprintf(`UPDATE mcp_configs
		 SET %s
		 WHERE id = $%d AND account_id = $%d
		 RETURNING id, account_id, name, transport, url, auth_secret_id, command,
		           args_json, cwd, env_json, inherit_parent_env,
		           call_timeout_ms, is_active, created_at, updated_at`,
			strings.Join(setClauses, ", "), idIdx, orgIdx),
		args...,
	).Scan(
		&cfg.ID, &cfg.AccountID, &cfg.Name, &cfg.Transport, &cfg.URL,
		&cfg.AuthSecretID, &cfg.Command, &cfg.ArgsJSON, &cfg.CwdPath,
		&cfg.EnvJSON, &cfg.InheritParentEnv, &cfg.CallTimeoutMs,
		&cfg.IsActive, &cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if isUniqueViolation(err) {
			return nil, MCPConfigNameConflictError{Name: strings.TrimSpace(*patch.Name)}
		}
		return nil, err
	}
	return &cfg, nil
}

func (r *MCPConfigsRepository) Delete(ctx context.Context, accountID, id uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := r.db.Exec(
		ctx,
		`DELETE FROM mcp_configs WHERE id = $1 AND account_id = $2`,
		id, accountID,
	)
	return err
}

// UpdateAuthSecret 更新 auth_secret_id 字段（PATCH bearer_token 时使用）。
func (r *MCPConfigsRepository) UpdateAuthSecret(ctx context.Context, accountID, id, secretID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}

	tag, err := r.db.Exec(
		ctx,
		`UPDATE mcp_configs SET auth_secret_id = $1, updated_at = now()
		 WHERE id = $2 AND account_id = $3`,
		secretID, id, accountID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
