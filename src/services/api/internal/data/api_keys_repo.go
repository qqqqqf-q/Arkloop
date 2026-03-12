package data

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type APIKey struct {
	ID         uuid.UUID
	AccountID      uuid.UUID
	UserID     uuid.UUID
	Name       string
	KeyPrefix  string
	Scopes     []string
	RevokedAt  *time.Time
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

type APIKeysRepository struct {
	db Querier
}

func NewAPIKeysRepository(db Querier) (*APIKeysRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &APIKeysRepository{db: db}, nil
}

// Create 生成随机 API Key，返回明文（仅此一次）和入库的 APIKey 记录。
func (r *APIKeysRepository) Create(
	ctx context.Context,
	accountID uuid.UUID,
	userID uuid.UUID,
	name string,
	scopes []string,
) (APIKey, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return APIKey{}, "", fmt.Errorf("account_id must not be nil")
	}
	if userID == uuid.Nil {
		return APIKey{}, "", fmt.Errorf("user_id must not be nil")
	}
	if name == "" {
		return APIKey{}, "", fmt.Errorf("name must not be empty")
	}
	if scopes == nil {
		scopes = []string{}
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return APIKey{}, "", fmt.Errorf("generate key: %w", err)
	}

	hexPart := hex.EncodeToString(raw)
	fullKey := "ak-" + hexPart
	keyPrefix := "ak-" + hexPart[:8]
	keyHash := hashKey(fullKey)

	var key APIKey
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO api_keys (account_id, user_id, name, key_prefix, key_hash, scopes)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, account_id, user_id, name, key_prefix, scopes, revoked_at, last_used_at, created_at`,
		accountID, userID, name, keyPrefix, keyHash, scopes,
	).Scan(
		&key.ID, &key.AccountID, &key.UserID, &key.Name, &key.KeyPrefix,
		&key.Scopes, &key.RevokedAt, &key.LastUsedAt, &key.CreatedAt,
	)
	if err != nil {
		return APIKey{}, "", err
	}
	return key, fullKey, nil
}

func (r *APIKeysRepository) ListByOrg(ctx context.Context, accountID uuid.UUID) ([]APIKey, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, account_id, user_id, name, key_prefix, scopes, revoked_at, last_used_at, created_at
		 FROM api_keys
		 WHERE account_id = $1
		 ORDER BY created_at DESC`,
		accountID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(
			&k.ID, &k.AccountID, &k.UserID, &k.Name, &k.KeyPrefix,
			&k.Scopes, &k.RevokedAt, &k.LastUsedAt, &k.CreatedAt,
		); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (r *APIKeysRepository) ListByOrgAndUser(ctx context.Context, accountID, userID uuid.UUID) ([]APIKey, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, account_id, user_id, name, key_prefix, scopes, revoked_at, last_used_at, created_at
		 FROM api_keys
		 WHERE account_id = $1 AND user_id = $2
		 ORDER BY created_at DESC`,
		accountID,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(
			&k.ID, &k.AccountID, &k.UserID, &k.Name, &k.KeyPrefix,
			&k.Scopes, &k.RevokedAt, &k.LastUsedAt, &k.CreatedAt,
		); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// GetByHash 根据 key hash 查询，用于鉴权路径。
func (r *APIKeysRepository) GetByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var k APIKey
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, user_id, name, key_prefix, scopes, revoked_at, last_used_at, created_at
		 FROM api_keys
		 WHERE key_hash = $1`,
		keyHash,
	).Scan(
		&k.ID, &k.AccountID, &k.UserID, &k.Name, &k.KeyPrefix,
		&k.Scopes, &k.RevokedAt, &k.LastUsedAt, &k.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &k, nil
}

// Revoke 软删除（设置 revoked_at），返回 key_hash 用于清理缓存。
// key_hash 为空字符串表示未命中。
func (r *APIKeysRepository) Revoke(ctx context.Context, accountID, keyID uuid.UUID) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var keyHash string
	err := r.db.QueryRow(
		ctx,
		`UPDATE api_keys SET revoked_at = now()
		 WHERE id = $1 AND account_id = $2 AND revoked_at IS NULL
		 RETURNING key_hash`,
		keyID, accountID,
	).Scan(&keyHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return keyHash, nil
}

func (r *APIKeysRepository) RevokeOwned(ctx context.Context, accountID, userID, keyID uuid.UUID) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var keyHash string
	err := r.db.QueryRow(
		ctx,
		`UPDATE api_keys SET revoked_at = now()
		 WHERE id = $1 AND account_id = $2 AND user_id = $3 AND revoked_at IS NULL
		 RETURNING key_hash`,
		keyID,
		accountID,
		userID,
	).Scan(&keyHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return keyHash, nil
}

// UpdateLastUsed 异步友好：更新 last_used_at，失败不影响主流程。
func (r *APIKeysRepository) UpdateLastUsed(ctx context.Context, keyID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := r.db.Exec(
		ctx,
		`UPDATE api_keys SET last_used_at = now() WHERE id = $1`,
		keyID,
	)
	return err
}

// HashAPIKey 计算 API Key 的 SHA-256 hash（供外部包使用）。
func HashAPIKey(rawKey string) string {
	return hashKey(rawKey)
}

func hashKey(rawKey string) string {
	digest := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(digest[:])
}
