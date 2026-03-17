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

type ChannelBindCode struct {
	ID                      uuid.UUID
	Token                   string
	IssuedByUserID          uuid.UUID
	ChannelType             *string
	UsedAt                  *time.Time
	UsedByChannelIdentityID *uuid.UUID
	ExpiresAt               time.Time
	CreatedAt               time.Time
}

type ChannelBindCodesRepository struct {
	db Querier
}

func NewChannelBindCodesRepository(db Querier) (*ChannelBindCodesRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ChannelBindCodesRepository{db: db}, nil
}

func (r *ChannelBindCodesRepository) WithTx(tx pgx.Tx) *ChannelBindCodesRepository {
	return &ChannelBindCodesRepository{db: tx}
}

const bindCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
const bindCodeLength = 8

func generateBindCode() (string, error) {
	b := make([]byte, bindCodeLength)
	max := big.NewInt(int64(len(bindCodeAlphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = bindCodeAlphabet[n.Int64()]
	}
	return string(b), nil
}

var bindCodeColumns = `id, token, issued_by_user_id, channel_type, used_at, used_by_channel_identity_id, expires_at, created_at`

func scanBindCode(row interface{ Scan(dest ...any) error }) (ChannelBindCode, error) {
	var bc ChannelBindCode
	err := row.Scan(
		&bc.ID, &bc.Token, &bc.IssuedByUserID, &bc.ChannelType,
		&bc.UsedAt, &bc.UsedByChannelIdentityID, &bc.ExpiresAt, &bc.CreatedAt,
	)
	return bc, err
}

// Create 生成一个新的 bind code，有效期由 ttl 指定。
func (r *ChannelBindCodesRepository) Create(ctx context.Context, userID uuid.UUID, channelType *string, ttl time.Duration) (ChannelBindCode, error) {
	if userID == uuid.Nil {
		return ChannelBindCode{}, fmt.Errorf("bind_codes: user_id must not be empty")
	}

	const maxRetries = 5
	for i := 0; i < maxRetries; i++ {
		token, err := generateBindCode()
		if err != nil {
			return ChannelBindCode{}, fmt.Errorf("bind_codes: generate: %w", err)
		}

		expiresAt := time.Now().UTC().Add(ttl)
		bc, err := scanBindCode(r.db.QueryRow(ctx,
			`INSERT INTO channel_identity_bind_codes (token, issued_by_user_id, channel_type, expires_at)
			 VALUES ($1, $2, $3, $4)
			 RETURNING `+bindCodeColumns,
			token, userID, channelType, expiresAt,
		))
		if err != nil {
			if isUniqueViolation(err) {
				continue
			}
			return ChannelBindCode{}, fmt.Errorf("bind_codes.Create: %w", err)
		}
		return bc, nil
	}
	return ChannelBindCode{}, fmt.Errorf("bind_codes: failed to generate unique token after %d retries", maxRetries)
}

// Consume 原子消费一个 bind code。成功返回 code 详情，code 无效/已用/过期返回 nil。
func (r *ChannelBindCodesRepository) Consume(ctx context.Context, token string, channelIdentityID uuid.UUID) (*ChannelBindCode, error) {
	if token == "" {
		return nil, fmt.Errorf("bind_codes: token must not be empty")
	}
	if channelIdentityID == uuid.Nil {
		return nil, fmt.Errorf("bind_codes: channel_identity_id must not be empty")
	}

	bc, err := scanBindCode(r.db.QueryRow(ctx,
		`UPDATE channel_identity_bind_codes
		 SET used_at = now(), used_by_channel_identity_id = $2
		 WHERE token = $1 AND used_at IS NULL AND expires_at > now()
		 RETURNING `+bindCodeColumns,
		token, channelIdentityID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("bind_codes.Consume: %w", err)
	}
	return &bc, nil
}

func (r *ChannelBindCodesRepository) ConsumeForChannel(
	ctx context.Context,
	token string,
	channelIdentityID uuid.UUID,
	channelType string,
) (*ChannelBindCode, error) {
	if token == "" {
		return nil, fmt.Errorf("bind_codes: token must not be empty")
	}
	if channelIdentityID == uuid.Nil {
		return nil, fmt.Errorf("bind_codes: channel_identity_id must not be empty")
	}
	if channelType == "" {
		return nil, fmt.Errorf("bind_codes: channel_type must not be empty")
	}

	bc, err := scanBindCode(r.db.QueryRow(ctx,
		`UPDATE channel_identity_bind_codes
		 SET used_at = now(), used_by_channel_identity_id = $2
		 WHERE token = $1
		   AND used_at IS NULL
		   AND expires_at > now()
		   AND (channel_type IS NULL OR channel_type = $3)
		 RETURNING `+bindCodeColumns,
		token, channelIdentityID, channelType,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("bind_codes.ConsumeForChannel: %w", err)
	}
	return &bc, nil
}

func (r *ChannelBindCodesRepository) GetActiveByToken(ctx context.Context, token string) (*ChannelBindCode, error) {
	if token == "" {
		return nil, fmt.Errorf("bind_codes: token must not be empty")
	}
	bc, err := scanBindCode(r.db.QueryRow(ctx,
		`SELECT `+bindCodeColumns+`
		 FROM channel_identity_bind_codes
		 WHERE token = $1
		   AND used_at IS NULL
		   AND expires_at > now()`,
		token,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("bind_codes.GetActiveByToken: %w", err)
	}
	return &bc, nil
}

// ListActiveByUser 返回用户的未使用且未过期的 bind codes。
func (r *ChannelBindCodesRepository) ListActiveByUser(ctx context.Context, userID uuid.UUID) ([]ChannelBindCode, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+bindCodeColumns+`
		 FROM channel_identity_bind_codes
		 WHERE issued_by_user_id = $1 AND used_at IS NULL AND expires_at > now()
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("bind_codes.ListActiveByUser: %w", err)
	}
	defer rows.Close()

	var codes []ChannelBindCode
	for rows.Next() {
		bc, err := scanBindCode(rows)
		if err != nil {
			return nil, fmt.Errorf("bind_codes.ListActiveByUser scan: %w", err)
		}
		codes = append(codes, bc)
	}
	return codes, rows.Err()
}
