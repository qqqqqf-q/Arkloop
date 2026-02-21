package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/api/internal/crypto"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SecretNameConflictError 在同一 org 下 name 已存在时返回。
type SecretNameConflictError struct {
	Name string
}

func (e SecretNameConflictError) Error() string {
	return fmt.Sprintf("secret %q already exists", e.Name)
}

type Secret struct {
	ID             uuid.UUID
	OrgID          uuid.UUID
	Name           string
	EncryptedValue string // 持久化存储的密文，永不对外暴露明文
	KeyVersion     int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	RotatedAt      *time.Time
}

type SecretsRepository struct {
	db      Querier
	keyRing *crypto.KeyRing
}

func NewSecretsRepository(db Querier, keyRing *crypto.KeyRing) (*SecretsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	if keyRing == nil {
		return nil, errors.New("keyRing must not be nil")
	}
	return &SecretsRepository{db: db, keyRing: keyRing}, nil
}

// Create 加密明文后写入数据库。同一 org 下 name 重复返回 SecretNameConflictError。
func (r *SecretsRepository) Create(ctx context.Context, orgID uuid.UUID, name, plaintext string) (Secret, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return Secret{}, fmt.Errorf("org_id must not be empty")
	}
	if strings.TrimSpace(name) == "" {
		return Secret{}, fmt.Errorf("name must not be empty")
	}
	if plaintext == "" {
		return Secret{}, fmt.Errorf("plaintext must not be empty")
	}

	encoded, keyVer, err := r.keyRing.Encrypt([]byte(plaintext))
	if err != nil {
		return Secret{}, fmt.Errorf("secrets: encrypt: %w", err)
	}

	var s Secret
	err = r.db.QueryRow(
		ctx,
		`INSERT INTO secrets (org_id, name, encrypted_value, key_version)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, org_id, name, encrypted_value, key_version, created_at, updated_at, rotated_at`,
		orgID, name, encoded, keyVer,
	).Scan(&s.ID, &s.OrgID, &s.Name, &s.EncryptedValue, &s.KeyVersion, &s.CreatedAt, &s.UpdatedAt, &s.RotatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Secret{}, SecretNameConflictError{Name: name}
		}
		return Secret{}, err
	}
	return s, nil
}

// Upsert 创建或更新 secret。若 name 已存在则覆写密文和 key_version。
func (r *SecretsRepository) Upsert(ctx context.Context, orgID uuid.UUID, name, plaintext string) (Secret, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return Secret{}, fmt.Errorf("org_id must not be empty")
	}
	if strings.TrimSpace(name) == "" {
		return Secret{}, fmt.Errorf("name must not be empty")
	}
	if plaintext == "" {
		return Secret{}, fmt.Errorf("plaintext must not be empty")
	}

	encoded, keyVer, err := r.keyRing.Encrypt([]byte(plaintext))
	if err != nil {
		return Secret{}, fmt.Errorf("secrets: encrypt: %w", err)
	}

	var s Secret
	err = r.db.QueryRow(
		ctx,
		`INSERT INTO secrets (org_id, name, encrypted_value, key_version)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT ON CONSTRAINT uq_secrets_org_name
		 DO UPDATE SET
		     encrypted_value = EXCLUDED.encrypted_value,
		     key_version     = EXCLUDED.key_version,
		     updated_at      = now()
		 RETURNING id, org_id, name, encrypted_value, key_version, created_at, updated_at, rotated_at`,
		orgID, name, encoded, keyVer,
	).Scan(&s.ID, &s.OrgID, &s.Name, &s.EncryptedValue, &s.KeyVersion, &s.CreatedAt, &s.UpdatedAt, &s.RotatedAt)
	if err != nil {
		return Secret{}, err
	}
	return s, nil
}

// GetByName 返回 secret 元数据（不解密），找不到返回 nil。
func (r *SecretsRepository) GetByName(ctx context.Context, orgID uuid.UUID, name string) (*Secret, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return nil, fmt.Errorf("org_id must not be empty")
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("name must not be empty")
	}

	var s Secret
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, name, encrypted_value, key_version, created_at, updated_at, rotated_at
		 FROM secrets
		 WHERE org_id = $1 AND name = $2
		 LIMIT 1`,
		orgID, name,
	).Scan(&s.ID, &s.OrgID, &s.Name, &s.EncryptedValue, &s.KeyVersion, &s.CreatedAt, &s.UpdatedAt, &s.RotatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// DecryptByName 查库后解密，返回明文。找不到返回 nil, nil。
func (r *SecretsRepository) DecryptByName(ctx context.Context, orgID uuid.UUID, name string) (*string, error) {
	s, err := r.GetByName(ctx, orgID, name)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, nil
	}

	plainBytes, err := r.keyRing.Decrypt(s.EncryptedValue, s.KeyVersion)
	if err != nil {
		return nil, fmt.Errorf("secrets: decrypt %q: %w", name, err)
	}
	plain := string(plainBytes)
	return &plain, nil
}

// Delete 物理删除。找不到时静默成功。
func (r *SecretsRepository) Delete(ctx context.Context, orgID uuid.UUID, name string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return fmt.Errorf("org_id must not be empty")
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name must not be empty")
	}

	_, err := r.db.Exec(
		ctx,
		`DELETE FROM secrets WHERE org_id = $1 AND name = $2`,
		orgID, name,
	)
	return err
}

// List 返回 org 下所有 secret 元数据，按 name 升序。不含明文。
func (r *SecretsRepository) List(ctx context.Context, orgID uuid.UUID) ([]Secret, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return nil, fmt.Errorf("org_id must not be empty")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, name, encrypted_value, key_version, created_at, updated_at, rotated_at
		 FROM secrets
		 WHERE org_id = $1
		 ORDER BY name ASC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	secrets := []Secret{}
	for rows.Next() {
		var s Secret
		if err := rows.Scan(
			&s.ID, &s.OrgID, &s.Name, &s.EncryptedValue,
			&s.KeyVersion, &s.CreatedAt, &s.UpdatedAt, &s.RotatedAt,
		); err != nil {
			return nil, err
		}
		secrets = append(secrets, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return secrets, nil
}

// isUniqueViolation 检查是否为 PostgreSQL 唯一约束冲突（错误码 23505）。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
