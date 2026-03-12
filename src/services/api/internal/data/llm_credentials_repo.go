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
OwnerKind     string
OwnerUserID   *uuid.UUID
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
db Querier
}

func NewLlmCredentialsRepository(db Querier) (*LlmCredentialsRepository, error) {
if db == nil {
return nil, errors.New("db must not be nil")
}
return &LlmCredentialsRepository{db: db}, nil
}

func (r *LlmCredentialsRepository) Create(
ctx context.Context,
id uuid.UUID,
ownerKind string,
ownerUserID *uuid.UUID,
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
if ownerKind != "user" && ownerKind != "platform" {
return LlmCredential{}, fmt.Errorf("owner_kind must be user or platform")
}
if ownerKind == "user" && (ownerUserID == nil || *ownerUserID == uuid.Nil) {
return LlmCredential{}, fmt.Errorf("owner_user_id must not be nil for user owner_kind")
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

var c LlmCredential
var advJSON []byte
err = r.db.QueryRow(
ctx,
`INSERT INTO llm_credentials
    (id, owner_kind, owner_user_id, provider, name, secret_id, key_prefix, base_url, openai_api_mode, advanced_json)
 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)
 RETURNING id, owner_kind, owner_user_id, provider, name, secret_id, key_prefix,
           base_url, openai_api_mode, advanced_json, revoked_at, last_used_at, created_at, updated_at`,
id, ownerKind, ownerUserID, provider, name, secretID, keyPrefix, baseURL, openaiAPIMode, string(advJSONBytes),
).Scan(
&c.ID, &c.OwnerKind, &c.OwnerUserID, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
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

func (r *LlmCredentialsRepository) GetByID(ctx context.Context, ownerKind string, ownerUserID *uuid.UUID, id uuid.UUID) (*LlmCredential, error) {
if ctx == nil {
ctx = context.Background()
}

query := `SELECT id, owner_kind, owner_user_id, provider, name, secret_id, key_prefix,
        base_url, openai_api_mode, advanced_json, revoked_at, last_used_at, created_at, updated_at
 FROM llm_credentials
 WHERE id = $1 AND revoked_at IS NULL`
args := []any{id}
query, args = appendOwnerKindFilter(query, args, ownerKind, ownerUserID)

var c LlmCredential
var advJSON []byte
err := r.db.QueryRow(ctx, query, args...).Scan(
&c.ID, &c.OwnerKind, &c.OwnerUserID, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
&c.BaseURL, &c.OpenAIAPIMode, &advJSON, &c.RevokedAt, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt,
)
if err != nil {
if errors.Is(err, pgx.ErrNoRows) {
return nil, nil
}
return nil, err
}
if len(advJSON) > 0 {
_ = json.Unmarshal(advJSON, &c.AdvancedJSON)
}
return &c, nil
}

func (r *LlmCredentialsRepository) ListByOwner(ctx context.Context, ownerKind string, ownerUserID *uuid.UUID) ([]LlmCredential, error) {
if ctx == nil {
ctx = context.Background()
}
query := `SELECT id, owner_kind, owner_user_id, provider, name, secret_id, key_prefix,
        base_url, openai_api_mode, advanced_json, revoked_at, last_used_at, created_at, updated_at
 FROM llm_credentials
 WHERE revoked_at IS NULL`
args := []any{}
query, args = appendOwnerKindFilter(query, args, ownerKind, ownerUserID)
query += ` ORDER BY created_at DESC`

rows, err := r.db.Query(ctx, query, args...)
if err != nil {
return nil, err
}
defer rows.Close()

return scanLlmCredentials(rows)
}

func (r *LlmCredentialsRepository) ListAllActive(ctx context.Context) ([]LlmCredential, error) {
if ctx == nil {
ctx = context.Background()
}

rows, err := r.db.Query(
ctx,
`SELECT id, owner_kind, owner_user_id, provider, name, secret_id, key_prefix,
        base_url, openai_api_mode, advanced_json, revoked_at, last_used_at, created_at, updated_at
 FROM llm_credentials
 WHERE revoked_at IS NULL`,
)
if err != nil {
return nil, err
}
defer rows.Close()

return scanLlmCredentials(rows)
}

func (r *LlmCredentialsRepository) Delete(ctx context.Context, ownerKind string, ownerUserID *uuid.UUID, id uuid.UUID) error {
if ctx == nil {
ctx = context.Background()
}
query := `DELETE FROM llm_credentials WHERE id = $1`
args := []any{id}
query, args = appendOwnerKindFilter(query, args, ownerKind, ownerUserID)
_, err := r.db.Exec(ctx, query, args...)
return err
}

func (r *LlmCredentialsRepository) Update(
ctx context.Context,
ownerKind string,
ownerUserID *uuid.UUID,
id uuid.UUID,
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
     advanced_json = $6::jsonb, updated_at = now()
 WHERE id = $1`
args := []any{id, provider, name, baseURL, openAIAPIMode, string(advJSONBytes)}
query, args = appendOwnerKindFilter(query, args, ownerKind, ownerUserID)
query += ` RETURNING id, owner_kind, owner_user_id, provider, name, secret_id, key_prefix, base_url, openai_api_mode, advanced_json, revoked_at, last_used_at, created_at, updated_at`

var c LlmCredential
var advJSON []byte
err = r.db.QueryRow(ctx, query, args...).Scan(
&c.ID, &c.OwnerKind, &c.OwnerUserID, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
&c.BaseURL, &c.OpenAIAPIMode, &advJSON, &c.RevokedAt, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt,
)
if err != nil {
if errors.Is(err, pgx.ErrNoRows) {
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
ownerKind string,
ownerUserID *uuid.UUID,
id uuid.UUID,
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
query, args = appendOwnerKindFilter(query, args, ownerKind, ownerUserID)
_, err := r.db.Exec(ctx, query, args...)
return err
}

func scanLlmCredentials(rows pgx.Rows) ([]LlmCredential, error) {
creds := []LlmCredential{}
for rows.Next() {
var c LlmCredential
var advJSON []byte
if err := rows.Scan(
&c.ID, &c.OwnerKind, &c.OwnerUserID, &c.Provider, &c.Name, &c.SecretID, &c.KeyPrefix,
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

// appendOwnerKindFilter 追加 owner_kind 过滤条件。
func appendOwnerKindFilter(query string, args []any, ownerKind string, ownerUserID *uuid.UUID) (string, []any) {
if ownerKind == "platform" {
return query + ` AND owner_kind = 'platform'`, args
}
args = append(args, *ownerUserID)
return query + fmt.Sprintf(` AND owner_kind = 'user' AND owner_user_id = $%d`, len(args)), args
}
