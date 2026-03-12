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

// WithTx 返回一个使用给定事务的 ToolProviderConfigsRepository 副本。
func (r *ToolProviderConfigsRepository) WithTx(tx pgx.Tx) *ToolProviderConfigsRepository {
return &ToolProviderConfigsRepository{db: tx}
}

type ToolProviderConfig struct {
ID           uuid.UUID
OwnerKind    string
OwnerUserID  *uuid.UUID
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
db Querier
}

func NewToolProviderConfigsRepository(db Querier) (*ToolProviderConfigsRepository, error) {
if db == nil {
return nil, errors.New("db must not be nil")
}
return &ToolProviderConfigsRepository{db: db}, nil
}

const tpcCols = `id, owner_kind, owner_user_id, group_name, provider_name, is_active, secret_id, key_prefix, base_url, config_json, created_at, updated_at`

func scanToolProviderConfig(row interface{ Scan(dest ...any) error }) (ToolProviderConfig, error) {
var cfg ToolProviderConfig
err := row.Scan(
&cfg.ID, &cfg.OwnerKind, &cfg.OwnerUserID,
&cfg.GroupName, &cfg.ProviderName, &cfg.IsActive,
&cfg.SecretID, &cfg.KeyPrefix, &cfg.BaseURL,
&cfg.ConfigJSON, &cfg.CreatedAt, &cfg.UpdatedAt,
)
return cfg, err
}

func (r *ToolProviderConfigsRepository) ListByOwner(ctx context.Context, ownerKind string, ownerUserID *uuid.UUID) ([]ToolProviderConfig, error) {
if ctx == nil {
ctx = context.Background()
}
query := `SELECT ` + tpcCols + ` FROM tool_provider_configs WHERE 1=1`
args := []any{}
query, args = appendOwnerKindFilter(query, args, ownerKind, ownerUserID)
query += ` ORDER BY group_name ASC, provider_name ASC`

rows, err := r.db.Query(ctx, query, args...)
if err != nil {
return nil, err
}
defer rows.Close()

out := []ToolProviderConfig{}
for rows.Next() {
cfg, err := scanToolProviderConfig(rows)
if err != nil {
return nil, err
}
out = append(out, cfg)
}
return out, rows.Err()
}

func (r *ToolProviderConfigsRepository) UpsertConfig(
ctx context.Context,
ownerKind string,
ownerUserID *uuid.UUID,
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
if ownerKind != "user" && ownerKind != "platform" {
return ToolProviderConfig{}, fmt.Errorf("owner_kind must be user or platform")
}
if ownerKind == "user" && (ownerUserID == nil || *ownerUserID == uuid.Nil) {
return ToolProviderConfig{}, fmt.Errorf("owner_user_id must not be empty for user owner_kind")
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

var row pgx.Row
if ownerKind == "platform" {
row = r.db.QueryRow(ctx, `
INSERT INTO tool_provider_configs (owner_kind, owner_user_id, group_name, provider_name, secret_id, key_prefix, base_url, config_json)
VALUES ('platform', NULL, $1, $2, $3, $4, $5, COALESCE($6, '{}'::jsonb))
ON CONFLICT (provider_name) WHERE owner_kind = 'platform'
DO UPDATE SET
group_name  = EXCLUDED.group_name,
secret_id   = COALESCE(EXCLUDED.secret_id, tool_provider_configs.secret_id),
key_prefix  = COALESCE(EXCLUDED.key_prefix, tool_provider_configs.key_prefix),
base_url    = COALESCE(EXCLUDED.base_url, tool_provider_configs.base_url),
config_json = CASE WHEN $6 IS NULL THEN tool_provider_configs.config_json ELSE EXCLUDED.config_json END,
updated_at  = now()
RETURNING `+tpcCols,
group, provider, secretID, keyPrefix, baseURL, configJSON)
} else {
row = r.db.QueryRow(ctx, `
INSERT INTO tool_provider_configs (owner_kind, owner_user_id, group_name, provider_name, secret_id, key_prefix, base_url, config_json)
VALUES ('user', $1, $2, $3, $4, $5, $6, COALESCE($7, '{}'::jsonb))
ON CONFLICT (owner_user_id, provider_name) WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL
DO UPDATE SET
group_name  = EXCLUDED.group_name,
secret_id   = COALESCE(EXCLUDED.secret_id, tool_provider_configs.secret_id),
key_prefix  = COALESCE(EXCLUDED.key_prefix, tool_provider_configs.key_prefix),
base_url    = COALESCE(EXCLUDED.base_url, tool_provider_configs.base_url),
config_json = CASE WHEN $7 IS NULL THEN tool_provider_configs.config_json ELSE EXCLUDED.config_json END,
updated_at  = now()
RETURNING `+tpcCols,
*ownerUserID, group, provider, secretID, keyPrefix, baseURL, configJSON)
}

cfg, err := scanToolProviderConfig(row)
if err != nil {
return ToolProviderConfig{}, err
}
return cfg, nil
}

// Activate 事务内调用：同组全关 + 指定 provider 打开。
func (r *ToolProviderConfigsRepository) Activate(ctx context.Context, ownerKind string, ownerUserID *uuid.UUID, groupName string, providerName string) error {
if ctx == nil {
ctx = context.Background()
}
if ownerKind != "user" && ownerKind != "platform" {
return fmt.Errorf("owner_kind must be user or platform")
}
if ownerKind == "user" && (ownerUserID == nil || *ownerUserID == uuid.Nil) {
return fmt.Errorf("owner_user_id must not be empty for user owner_kind")
}
group := strings.TrimSpace(groupName)
provider := strings.TrimSpace(providerName)
if group == "" || provider == "" {
return fmt.Errorf("group_name and provider_name must not be empty")
}

if ownerKind == "platform" {
if _, err := r.db.Exec(ctx, `
UPDATE tool_provider_configs
SET is_active = FALSE, updated_at = now()
WHERE owner_kind = 'platform' AND group_name = $1 AND is_active = TRUE
`, group); err != nil {
return err
}
_, err := r.db.Exec(ctx, `
INSERT INTO tool_provider_configs (owner_kind, owner_user_id, group_name, provider_name, is_active)
VALUES ('platform', NULL, $1, $2, TRUE)
ON CONFLICT (provider_name) WHERE owner_kind = 'platform'
DO UPDATE SET
group_name = EXCLUDED.group_name,
is_active  = TRUE,
updated_at = now()
`, group, provider)
return err
}

uid := *ownerUserID
if _, err := r.db.Exec(ctx, `
UPDATE tool_provider_configs
SET is_active = FALSE, updated_at = now()
WHERE owner_kind = 'user' AND owner_user_id = $1 AND group_name = $2 AND is_active = TRUE
`, uid, group); err != nil {
return err
}
_, err := r.db.Exec(ctx, `
INSERT INTO tool_provider_configs (owner_kind, owner_user_id, group_name, provider_name, is_active)
VALUES ('user', $1, $2, $3, TRUE)
ON CONFLICT (owner_user_id, provider_name) WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL
DO UPDATE SET
group_name = EXCLUDED.group_name,
is_active  = TRUE,
updated_at = now()
`, uid, group, provider)
return err
}

func (r *ToolProviderConfigsRepository) Deactivate(ctx context.Context, ownerKind string, ownerUserID *uuid.UUID, groupName string, providerName string) error {
if ctx == nil {
ctx = context.Background()
}
if ownerKind != "user" && ownerKind != "platform" {
return fmt.Errorf("owner_kind must be user or platform")
}
if ownerKind == "user" && (ownerUserID == nil || *ownerUserID == uuid.Nil) {
return fmt.Errorf("owner_user_id must not be empty for user owner_kind")
}
group := strings.TrimSpace(groupName)
provider := strings.TrimSpace(providerName)
if group == "" || provider == "" {
return fmt.Errorf("group_name and provider_name must not be empty")
}

if ownerKind == "platform" {
_, err := r.db.Exec(ctx, `
UPDATE tool_provider_configs
SET is_active = FALSE, updated_at = now()
WHERE owner_kind = 'platform' AND group_name = $1 AND provider_name = $2
`, group, provider)
return err
}

_, err := r.db.Exec(ctx, `
UPDATE tool_provider_configs
SET is_active = FALSE, updated_at = now()
WHERE owner_kind = 'user' AND owner_user_id = $1 AND group_name = $2 AND provider_name = $3
`, *ownerUserID, group, provider)
return err
}

func (r *ToolProviderConfigsRepository) ClearCredential(ctx context.Context, ownerKind string, ownerUserID *uuid.UUID, providerName string) error {
if ctx == nil {
ctx = context.Background()
}
if ownerKind != "user" && ownerKind != "platform" {
return fmt.Errorf("owner_kind must be user or platform")
}
if ownerKind == "user" && (ownerUserID == nil || *ownerUserID == uuid.Nil) {
return fmt.Errorf("owner_user_id must not be empty for user owner_kind")
}
provider := strings.TrimSpace(providerName)
if provider == "" {
return fmt.Errorf("provider_name must not be empty")
}

if ownerKind == "platform" {
_, err := r.db.Exec(ctx, `
UPDATE tool_provider_configs
SET is_active = FALSE,
    secret_id = NULL, key_prefix = NULL, base_url = NULL,
    config_json = '{}'::jsonb, updated_at = now()
WHERE owner_kind = 'platform' AND provider_name = $1
`, provider)
return err
}

_, err := r.db.Exec(ctx, `
UPDATE tool_provider_configs
SET is_active = FALSE,
    secret_id = NULL, key_prefix = NULL, base_url = NULL,
    config_json = '{}'::jsonb, updated_at = now()
WHERE owner_kind = 'user' AND owner_user_id = $1 AND provider_name = $2
`, *ownerUserID, provider)
return err
}
