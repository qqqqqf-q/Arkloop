package data

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const shareTokenAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
const shareTokenLength = 22

type ThreadShare struct {
	ID                   uuid.UUID
	ThreadID             uuid.UUID
	Token                string
	AccessType           string // "public" | "password"
	PasswordHash         *string
	SnapshotMessageCount int
	CreatedByUserID      uuid.UUID
	CreatedAt            time.Time
}

type ThreadShareRepository struct {
	db Querier
}

func NewThreadShareRepository(db Querier) (*ThreadShareRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ThreadShareRepository{db: db}, nil
}

// GenerateShareToken 生成 22 字符的 base62 token
func GenerateShareToken() (string, error) {
	alphabetLen := big.NewInt(int64(len(shareTokenAlphabet)))
	buf := make([]byte, shareTokenLength)
	for i := range buf {
		n, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", fmt.Errorf("generate share token: %w", err)
		}
		buf[i] = shareTokenAlphabet[n.Int64()]
	}
	return string(buf), nil
}

// Upsert 创建或替换 thread 的分享链接（一个 thread 只有一个 share）
func (r *ThreadShareRepository) Upsert(
	ctx context.Context,
	threadID uuid.UUID,
	token string,
	accessType string,
	passwordHash *string,
	snapshotMessageCount int,
	createdByUserID uuid.UUID,
) (*ThreadShare, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	if token == "" {
		return nil, fmt.Errorf("token must not be empty")
	}
	if accessType != "public" && accessType != "password" {
		return nil, fmt.Errorf("access_type must be 'public' or 'password'")
	}

	var share ThreadShare
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO thread_shares (thread_id, token, access_type, password_hash, snapshot_message_count, created_by_user_id)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (thread_id) DO UPDATE
		   SET token = EXCLUDED.token,
		       access_type = EXCLUDED.access_type,
		       password_hash = EXCLUDED.password_hash,
		       snapshot_message_count = EXCLUDED.snapshot_message_count,
		       created_at = now()
		 RETURNING id, thread_id, token, access_type, password_hash, snapshot_message_count, created_by_user_id, created_at`,
		threadID, token, accessType, passwordHash, snapshotMessageCount, createdByUserID,
	).Scan(
		&share.ID, &share.ThreadID, &share.Token, &share.AccessType,
		&share.PasswordHash, &share.SnapshotMessageCount, &share.CreatedByUserID, &share.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &share, nil
}

func (r *ThreadShareRepository) GetByThreadID(ctx context.Context, threadID uuid.UUID) (*ThreadShare, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}

	var share ThreadShare
	err := r.db.QueryRow(
		ctx,
		`SELECT id, thread_id, token, access_type, password_hash, snapshot_message_count, created_by_user_id, created_at
		 FROM thread_shares
		 WHERE thread_id = $1`,
		threadID,
	).Scan(
		&share.ID, &share.ThreadID, &share.Token, &share.AccessType,
		&share.PasswordHash, &share.SnapshotMessageCount, &share.CreatedByUserID, &share.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &share, nil
}

func (r *ThreadShareRepository) GetByToken(ctx context.Context, token string) (*ThreadShare, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if token == "" {
		return nil, fmt.Errorf("token must not be empty")
	}

	var share ThreadShare
	err := r.db.QueryRow(
		ctx,
		`SELECT id, thread_id, token, access_type, password_hash, snapshot_message_count, created_by_user_id, created_at
		 FROM thread_shares
		 WHERE token = $1`,
		token,
	).Scan(
		&share.ID, &share.ThreadID, &share.Token, &share.AccessType,
		&share.PasswordHash, &share.SnapshotMessageCount, &share.CreatedByUserID, &share.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &share, nil
}

func (r *ThreadShareRepository) DeleteByThreadID(ctx context.Context, threadID uuid.UUID) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return false, fmt.Errorf("thread_id must not be empty")
	}

	tag, err := r.db.Exec(ctx, `DELETE FROM thread_shares WHERE thread_id = $1`, threadID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
