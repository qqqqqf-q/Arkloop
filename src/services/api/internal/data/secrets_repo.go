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

// WithTx 返回一个使用给定事务的 SecretsRepository 副本。
func (r *SecretsRepository) WithTx(tx pgx.Tx) *SecretsRepository {
return &SecretsRepository{db: tx, keyRing: r.keyRing}
}

type SecretNameConflictError struct {
Name string
}

func (e SecretNameConflictError) Error() string {
return fmt.Sprintf("secret %q already exists", e.Name)
}

type SecretNotFoundError struct {
Name string
}

func (e SecretNotFoundError) Error() string {
return fmt.Sprintf("secret %q not found", e.Name)
}

// Secret 是内部完整记录，包含密文，仅供 repo 内部使用。
type Secret struct {
ID             uuid.UUID
OwnerKind      string
OwnerUserID    *uuid.UUID
Name           string
EncryptedValue string
KeyVersion     int
CreatedAt      time.Time
UpdatedAt      time.Time
RotatedAt      *time.Time
}

// SecretMeta 是对外安全的元数据视图，不含密文。
type SecretMeta struct {
ID          uuid.UUID
OwnerKind   string
OwnerUserID *uuid.UUID
Name        string
KeyVersion  int
CreatedAt   time.Time
UpdatedAt   time.Time
RotatedAt   *time.Time
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

// Create 加密明文后写入数据库。同一 owner 下 name 重复返回 SecretNameConflictError。
func (r *SecretsRepository) Create(ctx context.Context, ownerUserID uuid.UUID, name, plaintext string) (Secret, error) {
if ctx == nil {
ctx = context.Background()
}
if ownerUserID == uuid.Nil {
return Secret{}, fmt.Errorf("owner_user_id must not be empty")
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
`INSERT INTO secrets (owner_kind, owner_user_id, name, encrypted_value, key_version)
 VALUES ('user', $1, $2, $3, $4)
 RETURNING id, owner_kind, owner_user_id, name, encrypted_value, key_version, created_at, updated_at, rotated_at`,
ownerUserID, name, encoded, keyVer,
).Scan(&s.ID, &s.OwnerKind, &s.OwnerUserID, &s.Name, &s.EncryptedValue, &s.KeyVersion, &s.CreatedAt, &s.UpdatedAt, &s.RotatedAt)
if err != nil {
if isUniqueViolation(err) {
return Secret{}, SecretNameConflictError{Name: name}
}
return Secret{}, err
}
return s, nil
}

// Upsert 创建或更新 secret。若 name 已存在则覆写密文和 key_version。
func (r *SecretsRepository) Upsert(ctx context.Context, ownerUserID uuid.UUID, name, plaintext string) (Secret, error) {
if ctx == nil {
ctx = context.Background()
}
if ownerUserID == uuid.Nil {
return Secret{}, fmt.Errorf("owner_user_id must not be empty")
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
`INSERT INTO secrets (owner_kind, owner_user_id, name, encrypted_value, key_version)
 VALUES ('user', $1, $2, $3, $4)
 ON CONFLICT (owner_user_id, name) WHERE owner_kind = 'user'
 DO UPDATE SET
     encrypted_value = EXCLUDED.encrypted_value,
     key_version     = EXCLUDED.key_version,
     updated_at      = now()
 RETURNING id, owner_kind, owner_user_id, name, encrypted_value, key_version, created_at, updated_at, rotated_at`,
ownerUserID, name, encoded, keyVer,
).Scan(&s.ID, &s.OwnerKind, &s.OwnerUserID, &s.Name, &s.EncryptedValue, &s.KeyVersion, &s.CreatedAt, &s.UpdatedAt, &s.RotatedAt)
if err != nil {
return Secret{}, err
}
return s, nil
}

// UpsertPlatform 创建或更新 platform secret。name 全局唯一。
func (r *SecretsRepository) UpsertPlatform(ctx context.Context, name, plaintext string) (Secret, error) {
if ctx == nil {
ctx = context.Background()
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
`INSERT INTO secrets (owner_kind, owner_user_id, name, encrypted_value, key_version)
 VALUES ('platform', NULL, $1, $2, $3)
 ON CONFLICT (name) WHERE owner_kind = 'platform'
 DO UPDATE SET
     encrypted_value = EXCLUDED.encrypted_value,
     key_version     = EXCLUDED.key_version,
     updated_at      = now()
 RETURNING id, owner_kind, owner_user_id, name, encrypted_value, key_version, created_at, updated_at, rotated_at`,
name, encoded, keyVer,
).Scan(&s.ID, &s.OwnerKind, &s.OwnerUserID, &s.Name, &s.EncryptedValue, &s.KeyVersion, &s.CreatedAt, &s.UpdatedAt, &s.RotatedAt)
if err != nil {
return Secret{}, err
}
return s, nil
}

// GetByName 返回 secret 元数据（不解密），找不到返回 nil。
func (r *SecretsRepository) GetByName(ctx context.Context, ownerUserID uuid.UUID, name string) (*Secret, error) {
if ctx == nil {
ctx = context.Background()
}
if ownerUserID == uuid.Nil {
return nil, fmt.Errorf("owner_user_id must not be empty")
}
if strings.TrimSpace(name) == "" {
return nil, fmt.Errorf("name must not be empty")
}

var s Secret
err := r.db.QueryRow(
ctx,
`SELECT id, owner_kind, owner_user_id, name, encrypted_value, key_version, created_at, updated_at, rotated_at
 FROM secrets
 WHERE owner_kind = 'user' AND owner_user_id = $1 AND name = $2
 LIMIT 1`,
ownerUserID, name,
).Scan(&s.ID, &s.OwnerKind, &s.OwnerUserID, &s.Name, &s.EncryptedValue, &s.KeyVersion, &s.CreatedAt, &s.UpdatedAt, &s.RotatedAt)
if err != nil {
if errors.Is(err, pgx.ErrNoRows) {
return nil, nil
}
return nil, err
}
return &s, nil
}

// DecryptByID 按 secret ID 解密，不依赖 owner 上下文。找不到返回 nil, nil。
func (r *SecretsRepository) DecryptByID(ctx context.Context, id uuid.UUID) (*string, error) {
if ctx == nil {
ctx = context.Background()
}
if id == uuid.Nil {
return nil, fmt.Errorf("id must not be empty")
}

var s Secret
err := r.db.QueryRow(
ctx,
`SELECT id, owner_kind, owner_user_id, name, encrypted_value, key_version, created_at, updated_at, rotated_at
 FROM secrets
 WHERE id = $1`,
id,
).Scan(&s.ID, &s.OwnerKind, &s.OwnerUserID, &s.Name, &s.EncryptedValue, &s.KeyVersion, &s.CreatedAt, &s.UpdatedAt, &s.RotatedAt)
if err != nil {
if errors.Is(err, pgx.ErrNoRows) {
return nil, nil
}
return nil, err
}

plainBytes, err := r.keyRing.Decrypt(s.EncryptedValue, s.KeyVersion)
if err != nil {
return nil, fmt.Errorf("secrets: decrypt %q: %w", s.Name, err)
}
plain := string(plainBytes)
return &plain, nil
}

// DecryptByName 查库后解密，返回明文。找不到返回 nil, nil。
func (r *SecretsRepository) DecryptByName(ctx context.Context, ownerUserID uuid.UUID, name string) (*string, error) {
s, err := r.GetByName(ctx, ownerUserID, name)
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

// Delete 物理删除。找不到时返回 SecretNotFoundError。
func (r *SecretsRepository) Delete(ctx context.Context, ownerUserID uuid.UUID, name string) error {
if ctx == nil {
ctx = context.Background()
}
if ownerUserID == uuid.Nil {
return fmt.Errorf("owner_user_id must not be empty")
}
if strings.TrimSpace(name) == "" {
return fmt.Errorf("name must not be empty")
}

tag, err := r.db.Exec(
ctx,
`DELETE FROM secrets WHERE owner_kind = 'user' AND owner_user_id = $1 AND name = $2`,
ownerUserID, name,
)
if err != nil {
return err
}
if tag.RowsAffected() == 0 {
return SecretNotFoundError{Name: name}
}
return nil
}

// DeletePlatform 物理删除 platform secret。找不到时返回 SecretNotFoundError。
func (r *SecretsRepository) DeletePlatform(ctx context.Context, name string) error {
if ctx == nil {
ctx = context.Background()
}
if strings.TrimSpace(name) == "" {
return fmt.Errorf("name must not be empty")
}

tag, err := r.db.Exec(
ctx,
`DELETE FROM secrets WHERE owner_kind = 'platform' AND name = $1`,
name,
)
if err != nil {
return err
}
if tag.RowsAffected() == 0 {
return SecretNotFoundError{Name: name}
}
return nil
}

// List 返回 user 下所有 secret 的元数据，按 name 升序，不含密文。
func (r *SecretsRepository) List(ctx context.Context, ownerUserID uuid.UUID) ([]SecretMeta, error) {
if ctx == nil {
ctx = context.Background()
}
if ownerUserID == uuid.Nil {
return nil, fmt.Errorf("owner_user_id must not be empty")
}

rows, err := r.db.Query(
ctx,
`SELECT id, owner_kind, owner_user_id, name, key_version, created_at, updated_at, rotated_at
 FROM secrets
 WHERE owner_kind = 'user' AND owner_user_id = $1
 ORDER BY name ASC`,
ownerUserID,
)
if err != nil {
return nil, err
}
defer rows.Close()

metas := []SecretMeta{}
for rows.Next() {
var m SecretMeta
if err := rows.Scan(
&m.ID, &m.OwnerKind, &m.OwnerUserID, &m.Name,
&m.KeyVersion, &m.CreatedAt, &m.UpdatedAt, &m.RotatedAt,
); err != nil {
return nil, err
}
metas = append(metas, m)
}
if err := rows.Err(); err != nil {
return nil, err
}
return metas, nil
}

// isUniqueViolation 检查是否为 PostgreSQL 唯一约束冲突（错误码 23505）。
func isUniqueViolation(err error) bool {
var pgErr *pgconn.PgError
return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
