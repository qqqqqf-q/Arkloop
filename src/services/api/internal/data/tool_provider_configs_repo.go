package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
"arkloop/services/shared/database"
)

// WithTx 返回一个使用给定事务的 ToolProviderConfigsRepository 副本。
func (r *ToolProviderConfigsRepository) WithTx(tx database.Tx) *ToolProviderConfigsRepository {
	return &ToolProviderConfigsRepository{db: tx, dialect: r.dialect}
}

type ToolProviderConfig struct {
	ID           uuid.UUID
	OrgID        *uuid.UUID // nil = platform scope
	Scope        string     // "org" | "platform"
	GroupName    string
	ProviderName string
	IsActive     bool
	SecretID     *uuid.UUID
	KeyPrefix    *string
	BaseURL      *string
	ConfigJSON   json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ToolProviderConfigsRepository struct {
	db      Querier
	dialect database.DialectHelper
}

func NewToolProviderConfigsRepository(db Querier, dialect ...database.DialectHelper) (*ToolProviderConfigsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	d := database.DialectHelper(database.PostgresDialect{})
	if len(dialect) > 0 && dialect[0] != nil {
		d = dialect[0]
	}
	return &ToolProviderConfigsRepository{db: db, dialect: d}, nil
}

func (r *ToolProviderConfigsRepository) ListByScope(ctx context.Context, orgID uuid.UUID, scope string) ([]ToolProviderConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if scope != "org" && scope != "platform" {
		return nil, fmt.Errorf("scope must be org or platform")
	}
	if scope == "org" && orgID == uuid.Nil {
		return nil, fmt.Errorf("org_id must not be empty for org scope")
	}

	var rows database.Rows
	var err error
	if scope == "platform" {
		rows, err = r.db.Query(ctx, `
			SELECT id, org_id, scope, group_name, provider_name, is_active, secret_id, key_prefix, base_url, config_json, created_at, updated_at
			FROM tool_provider_configs
			WHERE scope = 'platform'
			ORDER BY group_name ASC, provider_name ASC
		`)
	} else {
		rows, err = r.db.Query(ctx, `
			SELECT id, org_id, scope, group_name, provider_name, is_active, secret_id, key_prefix, base_url, config_json, created_at, updated_at
			FROM tool_provider_configs
			WHERE org_id = $1 AND scope = 'org'
			ORDER BY group_name ASC, provider_name ASC
		`, orgID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ToolProviderConfig{}
	for rows.Next() {
		var cfg ToolProviderConfig
		if err := rows.Scan(
			&cfg.ID,
			&cfg.OrgID,
			&cfg.Scope,
			&cfg.GroupName,
			&cfg.ProviderName,
			&cfg.IsActive,
			&cfg.SecretID,
			&cfg.KeyPrefix,
			&cfg.BaseURL,
			&cfg.ConfigJSON,
			&cfg.CreatedAt,
			&cfg.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	return out, rows.Err()
}

func (r *ToolProviderConfigsRepository) UpsertConfig(
	ctx context.Context,
	orgID uuid.UUID,
	scope string, // "org" | "platform"
	groupName string,
	providerName string,
	secretID *uuid.UUID,
	keyPrefix *string,
	baseURL *string,
	configJSON []byte,
) (ToolProviderConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if scope != "org" && scope != "platform" {
		return ToolProviderConfig{}, fmt.Errorf("scope must be org or platform")
	}
	if scope == "org" && orgID == uuid.Nil {
		return ToolProviderConfig{}, fmt.Errorf("org_id must not be empty for org scope")
	}

	group := strings.TrimSpace(groupName)
	provider := strings.TrimSpace(providerName)
	if group == "" {
		return ToolProviderConfig{}, fmt.Errorf("group_name must not be empty")
	}
	if provider == "" {
		return ToolProviderConfig{}, fmt.Errorf("provider_name must not be empty")
	}

	if configJSON != nil && len(configJSON) == 0 {
		configJSON = []byte("{}")
	}

	var orgIDParam any
	if scope == "platform" {
		orgIDParam = nil
	} else {
		orgIDParam = orgID
	}

	jsonCastEmpty := r.dialect.JSONCast("'{}'")
	var cfg ToolProviderConfig
	var row database.Row
	if scope == "platform" {
		row = r.db.QueryRow(ctx, `
			INSERT INTO tool_provider_configs (org_id, scope, group_name, provider_name, secret_id, key_prefix, base_url, config_json)
			VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, `+jsonCastEmpty+`))
			ON CONFLICT (provider_name) WHERE scope = 'platform'
			DO UPDATE SET
				group_name  = EXCLUDED.group_name,
				secret_id   = COALESCE(EXCLUDED.secret_id, tool_provider_configs.secret_id),
				key_prefix  = COALESCE(EXCLUDED.key_prefix, tool_provider_configs.key_prefix),
				base_url    = COALESCE(EXCLUDED.base_url, tool_provider_configs.base_url),
				config_json = CASE WHEN $8 IS NULL THEN tool_provider_configs.config_json ELSE EXCLUDED.config_json END,
				updated_at  = now()
			RETURNING id, org_id, scope, group_name, provider_name, is_active, secret_id, key_prefix, base_url, config_json, created_at, updated_at
		`, orgIDParam, scope, group, provider, secretID, keyPrefix, baseURL, configJSON)
	} else {
		row = r.db.QueryRow(ctx, `
			INSERT INTO tool_provider_configs (org_id, scope, group_name, provider_name, secret_id, key_prefix, base_url, config_json)
			VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, `+jsonCastEmpty+`))
			ON CONFLICT (org_id, provider_name) WHERE scope = 'org'
			DO UPDATE SET
				group_name  = EXCLUDED.group_name,
				secret_id   = COALESCE(EXCLUDED.secret_id, tool_provider_configs.secret_id),
				key_prefix  = COALESCE(EXCLUDED.key_prefix, tool_provider_configs.key_prefix),
				base_url    = COALESCE(EXCLUDED.base_url, tool_provider_configs.base_url),
				config_json = CASE WHEN $8 IS NULL THEN tool_provider_configs.config_json ELSE EXCLUDED.config_json END,
				updated_at  = now()
			RETURNING id, org_id, scope, group_name, provider_name, is_active, secret_id, key_prefix, base_url, config_json, created_at, updated_at
		`, orgIDParam, scope, group, provider, secretID, keyPrefix, baseURL, configJSON)
	}

	err := row.Scan(
		&cfg.ID,
		&cfg.OrgID,
		&cfg.Scope,
		&cfg.GroupName,
		&cfg.ProviderName,
		&cfg.IsActive,
		&cfg.SecretID,
		&cfg.KeyPrefix,
		&cfg.BaseURL,
		&cfg.ConfigJSON,
		&cfg.CreatedAt,
		&cfg.UpdatedAt,
	)
	if err != nil {
		return ToolProviderConfig{}, err
	}
	return cfg, nil
}

// Activate 事务内调用：同组全关 + 指定 provider 打开。
func (r *ToolProviderConfigsRepository) Activate(ctx context.Context, orgID uuid.UUID, scope string, groupName string, providerName string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if scope != "org" && scope != "platform" {
		return fmt.Errorf("scope must be org or platform")
	}
	if scope == "org" && orgID == uuid.Nil {
		return fmt.Errorf("org_id must not be empty for org scope")
	}
	group := strings.TrimSpace(groupName)
	provider := strings.TrimSpace(providerName)
	if group == "" || provider == "" {
		return fmt.Errorf("group_name and provider_name must not be empty")
	}

	if scope == "platform" {
		if _, err := r.db.Exec(ctx, `
			UPDATE tool_provider_configs
			SET is_active = FALSE, updated_at = now()
			WHERE scope = 'platform' AND group_name = $1 AND is_active = TRUE
		`, group); err != nil {
			return err
		}

		_, err := r.db.Exec(ctx, `
			INSERT INTO tool_provider_configs (org_id, scope, group_name, provider_name, is_active)
			VALUES (NULL, 'platform', $1, $2, TRUE)
			ON CONFLICT (provider_name) WHERE scope = 'platform'
			DO UPDATE SET
				group_name = EXCLUDED.group_name,
				is_active  = TRUE,
				updated_at = now()
		`, group, provider)
		return err
	}

	if _, err := r.db.Exec(ctx, `
		UPDATE tool_provider_configs
		SET is_active = FALSE, updated_at = now()
		WHERE org_id = $1 AND scope = 'org' AND group_name = $2 AND is_active = TRUE
	`, orgID, group); err != nil {
		return err
	}

	_, err := r.db.Exec(ctx, `
		INSERT INTO tool_provider_configs (org_id, scope, group_name, provider_name, is_active)
		VALUES ($1, 'org', $2, $3, TRUE)
		ON CONFLICT (org_id, provider_name) WHERE scope = 'org'
		DO UPDATE SET
			group_name = EXCLUDED.group_name,
			is_active  = TRUE,
			updated_at = now()
	`, orgID, group, provider)
	return err
}

func (r *ToolProviderConfigsRepository) Deactivate(ctx context.Context, orgID uuid.UUID, scope string, groupName string, providerName string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if scope != "org" && scope != "platform" {
		return fmt.Errorf("scope must be org or platform")
	}
	if scope == "org" && orgID == uuid.Nil {
		return fmt.Errorf("org_id must not be empty for org scope")
	}
	group := strings.TrimSpace(groupName)
	provider := strings.TrimSpace(providerName)
	if group == "" || provider == "" {
		return fmt.Errorf("group_name and provider_name must not be empty")
	}

	if scope == "platform" {
		_, err := r.db.Exec(ctx, `
			UPDATE tool_provider_configs
			SET is_active = FALSE, updated_at = now()
			WHERE scope = 'platform' AND group_name = $1 AND provider_name = $2
		`, group, provider)
		return err
	}

	_, err := r.db.Exec(ctx, `
		UPDATE tool_provider_configs
		SET is_active = FALSE, updated_at = now()
		WHERE org_id = $1 AND scope = 'org' AND group_name = $2 AND provider_name = $3
	`, orgID, group, provider)
	return err
}

func (r *ToolProviderConfigsRepository) ClearCredential(ctx context.Context, orgID uuid.UUID, scope string, providerName string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if scope != "org" && scope != "platform" {
		return fmt.Errorf("scope must be org or platform")
	}
	if scope == "org" && orgID == uuid.Nil {
		return fmt.Errorf("org_id must not be empty for org scope")
	}
	provider := strings.TrimSpace(providerName)
	if provider == "" {
		return fmt.Errorf("provider_name must not be empty")
	}

	if scope == "platform" {
		_, err := r.db.Exec(ctx, `
			UPDATE tool_provider_configs
			SET is_active = FALSE,
			    secret_id = NULL,
			    key_prefix = NULL,
			    base_url = NULL,
			    config_json = `+r.dialect.JSONCast("'{}'")+`,
			    updated_at = now()
			WHERE scope = 'platform' AND provider_name = $1
		`, provider)
		return err
	}

	_, err := r.db.Exec(ctx, `
		UPDATE tool_provider_configs
		SET is_active = FALSE,
		    secret_id = NULL,
		    key_prefix = NULL,
		    base_url = NULL,
		    config_json = `+r.dialect.JSONCast("'{}'")+`,
		    updated_at = now()
		WHERE org_id = $1 AND scope = 'org' AND provider_name = $2
	`, orgID, provider)
	return err
}
