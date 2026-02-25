package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5"
)

// WithTx 返回一个使用给定事务的 LlmCredentialsRepository 副本。
func (r *LlmCredentialsRepository) WithTx(tx pgx.Tx) *LlmCredentialsRepository {
	return &LlmCredentialsRepository{db: tx}
}

type LlmCredentialNameConflictError struct {
	Name string
}

func (e LlmCredentialNameConflictError) Error() string {
	return fmt.Sprintf("llm credential %q already exists", e.Name)
}

type LlmCredential struct {
	ID            uuid.UUID
	OrgID         uuid.UUID
	Provider      string
	Name          string
	SecretID      *uuid.UUID
	KeyPrefix     *string
	BaseURL       *string
	OpenAIAPIMode *string
	RevokedAt     *time.Time
	LastUsedAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type LlmCredentialsRepository struct {
	db Querier
}

func NewLlmCredentialsRepository(db Querier) (*LlmCredentialsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &LlmCredentialsRepository{db: db}, nil
}

// Create 插入一条凭证记录，id 必须由调用方预生成（保证 secret 命名可引用此 id）。
// name 在同 org 下唯一，重复返回 LlmCredentialNameConflictError。
func (r *LlmCredentialsRepository) Create(
	ctx context.Context,
	id uuid.UUID,
	orgID uuid.UUID,
	provider string,
	name string,
	secretID *uuid.UUID,
	keyPrefix *string,
	baseURL *string,
	openaiAPIMode *string,
) (LlmCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if id == uuid.Nil {
		return LlmCredential{}, fmt.Errorf("id must not be nil")
	}
	if orgID == uuid.Nil {
		return LlmCredential{}, fmt.Errorf("org_id must not be nil")
	}
	if strings.TrimSpace(provider) == "" {
		return LlmCredential{}, fmt.Errorf("provider must not be empty")
	}
	if strings.TrimSpace(name) == "" {
		return LlmCredential{}, fmt.Errorf("name must not be empty")
	}

	var c LlmCredential
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO llm_credentials
		    (id, org_id, provider, name, secret_id, key_prefix, base_url, openai_api_mode)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, org_id, provider, name, secret_id, key_prefix,
		           base_url, openai_api_mode, revoked_at, last_used_at, created_at, updated_at`,
		id, orgID, provider, name, secretID, keyPrefix, baseURL, openaiAPIMode,
	).Scan(
		&c.ID, &c.OrgID, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
		&c.BaseURL, &c.OpenAIAPIMode, &c.RevokedAt, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return LlmCredential{}, LlmCredentialNameConflictError{Name: name}
		}
		return LlmCredential{}, err
	}
	return c, nil
}

// GetByID 按 ID 查询，要求属于指定 org，找不到返回 nil。
func (r *LlmCredentialsRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*LlmCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var c LlmCredential
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, provider, name, secret_id, key_prefix,
		        base_url, openai_api_mode, revoked_at, last_used_at, created_at, updated_at
		 FROM llm_credentials
		 WHERE id = $1 AND org_id = $2`,
		id, orgID,
	).Scan(
		&c.ID, &c.OrgID, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
		&c.BaseURL, &c.OpenAIAPIMode, &c.RevokedAt, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// ListByOrg 返回 org 下所有未吊销的凭证，按创建时间降序。
func (r *LlmCredentialsRepository) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]LlmCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, provider, name, secret_id, key_prefix,
		        base_url, openai_api_mode, revoked_at, last_used_at, created_at, updated_at
		 FROM llm_credentials
		 WHERE org_id = $1 AND revoked_at IS NULL
		 ORDER BY created_at DESC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	creds := []LlmCredential{}
	for rows.Next() {
		var c LlmCredential
		if err := rows.Scan(
			&c.ID, &c.OrgID, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
			&c.BaseURL, &c.OpenAIAPIMode, &c.RevokedAt, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		creds = append(creds, c)
	}
	return creds, rows.Err()
}

// ListAllActive 返回所有 org 中未吊销的凭证（供 Worker 启动时加载全局路由配置）。
func (r *LlmCredentialsRepository) ListAllActive(ctx context.Context) ([]LlmCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, provider, name, secret_id, key_prefix,
		        base_url, openai_api_mode, revoked_at, last_used_at, created_at, updated_at
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
		if err := rows.Scan(
			&c.ID, &c.OrgID, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
			&c.BaseURL, &c.OpenAIAPIMode, &c.RevokedAt, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		creds = append(creds, c)
	}
	return creds, rows.Err()
}

// Delete 物理删除（级联删除关联的 llm_routes）。找不到时静默成功。
func (r *LlmCredentialsRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := r.db.Exec(
		ctx,
		`DELETE FROM llm_credentials WHERE id = $1 AND org_id = $2`,
		id, orgID,
	)
	return err
}

// Update 更新凭证的可编辑字段（名称、base_url、openai_api_mode）。
func (r *LlmCredentialsRepository) Update(
	ctx context.Context,
	orgID uuid.UUID,
	id uuid.UUID,
	name string,
	baseURL *string,
	openAIAPIMode *string,
) (LlmCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var c LlmCredential
	err := r.db.QueryRow(
		ctx,
		`UPDATE llm_credentials
		 SET name = $3, base_url = $4, openai_api_mode = $5, updated_at = NOW()
		 WHERE id = $1 AND org_id = $2 AND revoked_at IS NULL
		 RETURNING id, org_id, provider, name, secret_id, key_prefix, base_url, openai_api_mode,
		           revoked_at, last_used_at, created_at, updated_at`,
		id, orgID, name, baseURL, openAIAPIMode,
	).Scan(
		&c.ID, &c.OrgID, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
		&c.BaseURL, &c.OpenAIAPIMode, &c.RevokedAt, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LlmCredential{}, fmt.Errorf("credential not found")
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return LlmCredential{}, LlmCredentialNameConflictError{Name: name}
		}
		return LlmCredential{}, err
	}
	return c, nil
}
