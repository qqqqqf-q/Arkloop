package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AsrCredentialNameConflictError struct{ Name string }

func (e AsrCredentialNameConflictError) Error() string {
	return fmt.Sprintf("asr credential %q already exists", e.Name)
}

type AsrCredential struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	Provider  string
	Name      string
	SecretID  *uuid.UUID
	KeyPrefix *string
	BaseURL   *string
	Model     string
	IsDefault bool
	RevokedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AsrCredentialsRepository struct {
	db Querier
}

func NewAsrCredentialsRepository(db Querier) (*AsrCredentialsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &AsrCredentialsRepository{db: db}, nil
}

func (r *AsrCredentialsRepository) WithTx(tx pgx.Tx) *AsrCredentialsRepository {
	return &AsrCredentialsRepository{db: tx}
}

func (r *AsrCredentialsRepository) Create(
	ctx context.Context,
	id uuid.UUID,
	orgID uuid.UUID,
	provider string,
	name string,
	secretID *uuid.UUID,
	keyPrefix *string,
	baseURL *string,
	model string,
	isDefault bool,
) (AsrCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if id == uuid.Nil {
		return AsrCredential{}, fmt.Errorf("id must not be nil")
	}
	if orgID == uuid.Nil {
		return AsrCredential{}, fmt.Errorf("org_id must not be nil")
	}
	if strings.TrimSpace(provider) == "" {
		return AsrCredential{}, fmt.Errorf("provider must not be empty")
	}
	if strings.TrimSpace(name) == "" {
		return AsrCredential{}, fmt.Errorf("name must not be empty")
	}
	if strings.TrimSpace(model) == "" {
		return AsrCredential{}, fmt.Errorf("model must not be empty")
	}

	var c AsrCredential
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO asr_credentials
		    (id, org_id, provider, name, secret_id, key_prefix, base_url, model, is_default)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, org_id, provider, name, secret_id, key_prefix,
		           base_url, model, is_default, revoked_at, created_at, updated_at`,
		id, orgID, provider, name, secretID, keyPrefix, baseURL, model, isDefault,
	).Scan(
		&c.ID, &c.OrgID, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
		&c.BaseURL, &c.Model, &c.IsDefault, &c.RevokedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return AsrCredential{}, AsrCredentialNameConflictError{Name: name}
		}
		return AsrCredential{}, err
	}
	return c, nil
}

func (r *AsrCredentialsRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*AsrCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var c AsrCredential
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, provider, name, secret_id, key_prefix,
		        base_url, model, is_default, revoked_at, created_at, updated_at
		 FROM asr_credentials
		 WHERE id = $1 AND org_id = $2`,
		id, orgID,
	).Scan(
		&c.ID, &c.OrgID, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
		&c.BaseURL, &c.Model, &c.IsDefault, &c.RevokedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *AsrCredentialsRepository) GetDefault(ctx context.Context, orgID uuid.UUID) (*AsrCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var c AsrCredential
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, provider, name, secret_id, key_prefix,
		        base_url, model, is_default, revoked_at, created_at, updated_at
		 FROM asr_credentials
		 WHERE org_id = $1 AND is_default = true AND revoked_at IS NULL`,
		orgID,
	).Scan(
		&c.ID, &c.OrgID, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
		&c.BaseURL, &c.Model, &c.IsDefault, &c.RevokedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *AsrCredentialsRepository) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]AsrCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, provider, name, secret_id, key_prefix,
		        base_url, model, is_default, revoked_at, created_at, updated_at
		 FROM asr_credentials
		 WHERE org_id = $1 AND revoked_at IS NULL
		 ORDER BY created_at DESC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	creds := []AsrCredential{}
	for rows.Next() {
		var c AsrCredential
		if err := rows.Scan(
			&c.ID, &c.OrgID, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
			&c.BaseURL, &c.Model, &c.IsDefault, &c.RevokedAt, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		creds = append(creds, c)
	}
	return creds, rows.Err()
}

// SetDefault 原子地将指定凭证设为 default，同时清除同 org 其他凭证的 default 标记。
func (r *AsrCredentialsRepository) SetDefault(ctx context.Context, orgID, id uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(
		ctx,
		`UPDATE asr_credentials
		 SET is_default = (id = $2), updated_at = now()
		 WHERE org_id = $1 AND revoked_at IS NULL`,
		orgID, id,
	)
	return err
}

func (r *AsrCredentialsRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(
		ctx,
		`DELETE FROM asr_credentials WHERE id = $1 AND org_id = $2`,
		id, orgID,
	)
	return err
}
