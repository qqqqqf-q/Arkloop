package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type EmailVerificationToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

type EmailVerificationTokenRepository struct {
	db Querier
}

func NewEmailVerificationTokenRepository(db Querier) (*EmailVerificationTokenRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &EmailVerificationTokenRepository{db: db}, nil
}

func (r *EmailVerificationTokenRepository) Create(
	ctx context.Context,
	userID uuid.UUID,
	tokenHash string,
	expiresAt time.Time,
) (EmailVerificationToken, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if userID == uuid.Nil {
		return EmailVerificationToken{}, fmt.Errorf("user_id must not be empty")
	}
	if tokenHash == "" {
		return EmailVerificationToken{}, fmt.Errorf("token_hash must not be empty")
	}

	var t EmailVerificationToken
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO email_verification_tokens (user_id, token_hash, expires_at)
		 VALUES ($1, $2, $3)
		 RETURNING id, user_id, token_hash, expires_at, used_at, created_at`,
		userID, tokenHash, expiresAt.UTC(),
	).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.UsedAt, &t.CreatedAt)
	if err != nil {
		return EmailVerificationToken{}, err
	}
	return t, nil
}

// Consume 原子地标记 token 为已使用并返回对应的 user_id。
// 只有未使用且未过期的 token 才会被消耗。
// 返回 (userID, true, nil) 表示成功；(uuid.Nil, false, nil) 表示 token 无效或已过期。
func (r *EmailVerificationTokenRepository) Consume(
	ctx context.Context,
	tokenHash string,
) (uuid.UUID, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var userID uuid.UUID
	err := r.db.QueryRow(
		ctx,
		`UPDATE email_verification_tokens
		 SET used_at = now()
		 WHERE token_hash = $1
		   AND used_at IS NULL
		   AND expires_at > now()
		 RETURNING user_id`,
		tokenHash,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, fmt.Errorf("email_verification_tokens.Consume: %w", err)
	}
	return userID, true, nil
}

// DeleteExpired 清理过期且已使用的 token，供定期维护调用。
func (r *EmailVerificationTokenRepository) DeleteExpired(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(
		ctx,
		`DELETE FROM email_verification_tokens WHERE expires_at < now() AND used_at IS NOT NULL`,
	)
	return err
}
