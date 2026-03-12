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

const (
	LlmCredentialScopeOrg      = "org"
	LlmCredentialScopePlatform = "platform"
)

// WithTx 返回一个使用给定事务的 LlmCredentialsRepository 副本。
func (r *LlmCredentialsRepository) WithTx(tx database.Tx) *LlmCredentialsRepository {
	return &LlmCredentialsRepository{db: tx, dialect: r.dialect}
}

type LlmCredentialNameConflictError struct {
	Name string
}

func (e LlmCredentialNameConflictError) Error() string {
	return fmt.Sprintf("llm credential %q already exists", e.Name)
}

type LlmCredential struct {
	ID            uuid.UUID
	OrgID         *uuid.UUID
	Scope         string
	Provider      string
	Name          string
	SecretID      *uuid.UUID
	KeyPrefix     *string
	BaseURL       *string
	OpenAIAPIMode *string
	AdvancedJSON  map[string]any
	RevokedAt     *time.Time
	LastUsedAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type LlmCredentialsRepository struct {
	db      Querier
	dialect database.DialectHelper
}

func NewLlmCredentialsRepository(db Querier, dialect ...database.DialectHelper) (*LlmCredentialsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	d := database.DialectHelper(database.PostgresDialect{})
	if len(dialect) > 0 && dialect[0] != nil {
		d = dialect[0]
	}
	return &LlmCredentialsRepository{db: db, dialect: d}, nil
}

func (r *LlmCredentialsRepository) Create(
	ctx context.Context,
	id uuid.UUID,
	orgID uuid.UUID,
	scope string,
	provider string,
	name string,
	secretID *uuid.UUID,
	keyPrefix *string,
	baseURL *string,
	openaiAPIMode *string,
	advancedJSON map[string]any,
) (LlmCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if id == uuid.Nil {
		return LlmCredential{}, fmt.Errorf("id must not be nil")
	}
	if scope != LlmCredentialScopeOrg && scope != LlmCredentialScopePlatform {
		return LlmCredential{}, fmt.Errorf("scope must be org or platform")
	}
	if scope == LlmCredentialScopeOrg && orgID == uuid.Nil {
		return LlmCredential{}, fmt.Errorf("org_id must not be nil for org scope")
	}
	if strings.TrimSpace(provider) == "" {
		return LlmCredential{}, fmt.Errorf("provider must not be empty")
	}
	if strings.TrimSpace(name) == "" {
		return LlmCredential{}, fmt.Errorf("name must not be empty")
	}

	advJSONBytes, err := json.Marshal(advancedJSON)
	if err != nil {
		return LlmCredential{}, fmt.Errorf("marshal advanced_json: %w", err)
	}

	var orgIDParam any
	if scope == LlmCredentialScopePlatform {
		orgIDParam = nil
	} else {
		orgIDParam = orgID
	}

	var c LlmCredential
	var advJSON []byte
	err = r.db.QueryRow(
		ctx,
		`INSERT INTO llm_credentials
		    (id, org_id, scope, provider, name, secret_id, key_prefix, base_url, openai_api_mode, advanced_json)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, `+r.dialect.JSONCast("$10")+`)
		 RETURNING id, org_id, scope, provider, name, secret_id, key_prefix,
		           base_url, openai_api_mode, advanced_json, revoked_at, last_used_at, created_at, updated_at`,
		id, orgIDParam, scope, provider, name, secretID, keyPrefix, baseURL, openaiAPIMode, string(advJSONBytes),
	).Scan(
		&c.ID, &c.OrgID, &c.Scope, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
		&c.BaseURL, &c.OpenAIAPIMode, &advJSON, &c.RevokedAt, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return LlmCredential{}, LlmCredentialNameConflictError{Name: name}
		}
		return LlmCredential{}, err
	}
	if len(advJSON) > 0 {
		_ = json.Unmarshal(advJSON, &c.AdvancedJSON)
	}
	return c, nil
}

func (r *LlmCredentialsRepository) GetByID(ctx context.Context, orgID, id uuid.UUID, scope string) (*LlmCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	query, args, err := llmCredentialScopeQuery(
		`SELECT id, org_id, scope, provider, name, secret_id, key_prefix,
		        base_url, openai_api_mode, advanced_json, revoked_at, last_used_at, created_at, updated_at
		 FROM llm_credentials
		 WHERE id = $1 AND revoked_at IS NULL`,
		id,
		orgID,
		scope,
	)
	if err != nil {
		return nil, err
	}

	var c LlmCredential
	var advJSON []byte
	err = r.db.QueryRow(ctx, query, args...).Scan(
		&c.ID, &c.OrgID, &c.Scope, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
		&c.BaseURL, &c.OpenAIAPIMode, &advJSON, &c.RevokedAt, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if len(advJSON) > 0 {
		_ = json.Unmarshal(advJSON, &c.AdvancedJSON)
	}
	return &c, nil
}

func (r *LlmCredentialsRepository) ListByScope(ctx context.Context, orgID uuid.UUID, scope string) ([]LlmCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	query := `SELECT id, org_id, scope, provider, name, secret_id, key_prefix,
	        base_url, openai_api_mode, advanced_json, revoked_at, last_used_at, created_at, updated_at
	 FROM llm_credentials
	 WHERE revoked_at IS NULL`
	args := []any{}
	var err error
	query, args, err = appendLlmCredentialScopeFilter(query, args, orgID, scope)
	if err != nil {
		return nil, err
	}
	query += ` ORDER BY created_at DESC`

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	creds := []LlmCredential{}
	for rows.Next() {
		var c LlmCredential
		var advJSON []byte
		if err := rows.Scan(
			&c.ID, &c.OrgID, &c.Scope, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
			&c.BaseURL, &c.OpenAIAPIMode, &advJSON, &c.RevokedAt, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(advJSON) > 0 {
			_ = json.Unmarshal(advJSON, &c.AdvancedJSON)
		}
		creds = append(creds, c)
	}
	return creds, rows.Err()
}

func (r *LlmCredentialsRepository) ListAllActive(ctx context.Context) ([]LlmCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, scope, provider, name, secret_id, key_prefix,
		        base_url, openai_api_mode, advanced_json, revoked_at, last_used_at, created_at, updated_at
		 FROM llm_credentials
		 WHERE revoked_at IS NULL`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	creds := []LlmCredential{}
	for rows.Next() {
		var c LlmCredential
		var advJSON []byte
		if err := rows.Scan(
			&c.ID, &c.OrgID, &c.Scope, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
			&c.BaseURL, &c.OpenAIAPIMode, &advJSON, &c.RevokedAt, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(advJSON) > 0 {
			_ = json.Unmarshal(advJSON, &c.AdvancedJSON)
		}
		creds = append(creds, c)
	}
	return creds, rows.Err()
}

func (r *LlmCredentialsRepository) Delete(ctx context.Context, orgID, id uuid.UUID, scope string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	query, args, err := llmCredentialScopeQuery(
		`DELETE FROM llm_credentials WHERE id = $1`,
		id,
		orgID,
		scope,
	)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, query, args...)
	return err
}

func (r *LlmCredentialsRepository) Update(
	ctx context.Context,
	orgID, id uuid.UUID,
	scope string,
	provider string,
	name string,
	baseURL *string,
	openAIAPIMode *string,
	advancedJSON map[string]any,
) (*LlmCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	advJSONBytes, err := json.Marshal(advancedJSON)
	if err != nil {
		return nil, fmt.Errorf("marshal advanced_json: %w", err)
	}
	query := `UPDATE llm_credentials
		 SET provider = $2, name = $3, base_url = $4, openai_api_mode = $5,
		     advanced_json = ` + r.dialect.JSONCast("$6") + `, updated_at = now()
		 WHERE id = $1`
	args := []any{id, provider, name, baseURL, openAIAPIMode, string(advJSONBytes)}
	if scope == LlmCredentialScopePlatform {
		query += ` AND scope = 'platform'`
	} else if scope == LlmCredentialScopeOrg {
		if orgID == uuid.Nil {
			return nil, fmt.Errorf("org_id must not be nil for org scope")
		}
		args = append(args, orgID)
		query += ` AND org_id = $7 AND scope = 'org'`
	} else {
		return nil, fmt.Errorf("scope must be org or platform")
	}
	query += ` RETURNING id, org_id, scope, provider, name, secret_id, key_prefix, base_url, openai_api_mode, advanced_json, revoked_at, last_used_at, created_at, updated_at`

	var c LlmCredential
	var advJSON []byte
	err = r.db.QueryRow(ctx, query, args...).Scan(
		&c.ID, &c.OrgID, &c.Scope, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
		&c.BaseURL, &c.OpenAIAPIMode, &advJSON, &c.RevokedAt, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, database.ErrNoRows) {
			return nil, nil
		}
		if isUniqueViolation(err) {
			return nil, LlmCredentialNameConflictError{Name: name}
		}
		return nil, err
	}
	if len(advJSON) > 0 {
		_ = json.Unmarshal(advJSON, &c.AdvancedJSON)
	}
	return &c, nil
}

func (r *LlmCredentialsRepository) UpdateSecret(
	ctx context.Context,
	orgID, id uuid.UUID,
	scope string,
	secretID *uuid.UUID,
	keyPrefix *string,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	query := `UPDATE llm_credentials
		 SET secret_id = $2, key_prefix = $3, updated_at = now()
		 WHERE id = $1`
	args := []any{id, secretID, keyPrefix}
	if scope == LlmCredentialScopePlatform {
		query += ` AND scope = 'platform'`
	} else if scope == LlmCredentialScopeOrg {
		if orgID == uuid.Nil {
			return fmt.Errorf("org_id must not be nil for org scope")
		}
		args = append(args, orgID)
		query += ` AND org_id = $4 AND scope = 'org'`
	} else {
		return fmt.Errorf("scope must be org or platform")
	}
	_, err := r.db.Exec(ctx, query, args...)
	return err
}

func llmCredentialScopeQuery(base string, id uuid.UUID, orgID uuid.UUID, scope string) (string, []any, error) {
	if scope == LlmCredentialScopePlatform {
		return base + ` AND scope = 'platform'`, []any{id}, nil
	}
	if scope != LlmCredentialScopeOrg {
		return "", nil, fmt.Errorf("scope must be org or platform")
	}
	if orgID == uuid.Nil {
		return "", nil, fmt.Errorf("org_id must not be nil for org scope")
	}
	return base + ` AND org_id = $2 AND scope = 'org'`, []any{id, orgID}, nil
}

func appendLlmCredentialScopeFilter(base string, args []any, orgID uuid.UUID, scope string) (string, []any, error) {
	if scope == LlmCredentialScopePlatform {
		return base + ` AND scope = 'platform'`, args, nil
	}
	if scope != LlmCredentialScopeOrg {
		return "", nil, fmt.Errorf("scope must be org or platform")
	}
	if orgID == uuid.Nil {
		return "", nil, fmt.Errorf("org_id must not be nil for org scope")
	}
	args = append(args, orgID)
	return base + fmt.Sprintf(` AND org_id = $%d AND scope = 'org'`, len(args)), args, nil
}
