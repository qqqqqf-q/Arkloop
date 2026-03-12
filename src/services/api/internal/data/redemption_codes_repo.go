package data

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const redemptionCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
const redemptionCodeSegmentLen = 4
const redemptionCodeSegments = 4

type RedemptionCode struct {
	ID              uuid.UUID
	Code            string
	Type            string
	Value           string
	MaxUses         int
	UseCount        int
	ExpiresAt       *time.Time
	IsActive        bool
	BatchID         *string
	CreatedByUserID uuid.UUID
	CreatedAt       time.Time
}

type RedemptionRecord struct {
	ID         uuid.UUID
	CodeID     uuid.UUID
	UserID     uuid.UUID
	AccountID      uuid.UUID
	RedeemedAt time.Time
}

type RedemptionCodesRepository struct {
	db Querier
}

func NewRedemptionCodesRepository(db Querier) (*RedemptionCodesRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &RedemptionCodesRepository{db: db}, nil
}

func (r *RedemptionCodesRepository) WithTx(tx pgx.Tx) *RedemptionCodesRepository {
	return &RedemptionCodesRepository{db: tx}
}

// GenerateRedemptionCode 生成 XXXX-XXXX-XXXX-XXXX 格式的兑换码。
func GenerateRedemptionCode() (string, error) {
	alphabetLen := big.NewInt(int64(len(redemptionCodeAlphabet)))
	totalChars := redemptionCodeSegmentLen * redemptionCodeSegments
	var sb strings.Builder
	sb.Grow(totalChars + redemptionCodeSegments - 1)
	for i := 0; i < totalChars; i++ {
		if i > 0 && i%redemptionCodeSegmentLen == 0 {
			sb.WriteByte('-')
		}
		idx, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", fmt.Errorf("redemption_codes.GenerateCode: %w", err)
		}
		sb.WriteByte(redemptionCodeAlphabet[idx.Int64()])
	}
	return sb.String(), nil
}

func (r *RedemptionCodesRepository) Create(
	ctx context.Context,
	code string,
	codeType string,
	value string,
	maxUses int,
	expiresAt *time.Time,
	batchID *string,
	createdByUserID uuid.UUID,
) (*RedemptionCode, error) {
	var rc RedemptionCode
	err := r.db.QueryRow(ctx,
		`INSERT INTO redemption_codes (code, type, value, max_uses, expires_at, batch_id, created_by_user_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, code, type, value, max_uses, use_count, expires_at, is_active, batch_id, created_by_user_id, created_at`,
		code, codeType, value, maxUses, expiresAt, batchID, createdByUserID,
	).Scan(&rc.ID, &rc.Code, &rc.Type, &rc.Value, &rc.MaxUses, &rc.UseCount, &rc.ExpiresAt, &rc.IsActive, &rc.BatchID, &rc.CreatedByUserID, &rc.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("redemption_codes.Create: %w", err)
	}
	return &rc, nil
}

func (r *RedemptionCodesRepository) GetByCode(ctx context.Context, code string) (*RedemptionCode, error) {
	var rc RedemptionCode
	err := r.db.QueryRow(ctx,
		`SELECT id, code, type, value, max_uses, use_count, expires_at, is_active, batch_id, created_by_user_id, created_at
		 FROM redemption_codes WHERE code = $1`,
		code,
	).Scan(&rc.ID, &rc.Code, &rc.Type, &rc.Value, &rc.MaxUses, &rc.UseCount, &rc.ExpiresAt, &rc.IsActive, &rc.BatchID, &rc.CreatedByUserID, &rc.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redemption_codes.GetByCode: %w", err)
	}
	return &rc, nil
}

func (r *RedemptionCodesRepository) GetByID(ctx context.Context, id uuid.UUID) (*RedemptionCode, error) {
	var rc RedemptionCode
	err := r.db.QueryRow(ctx,
		`SELECT id, code, type, value, max_uses, use_count, expires_at, is_active, batch_id, created_by_user_id, created_at
		 FROM redemption_codes WHERE id = $1`,
		id,
	).Scan(&rc.ID, &rc.Code, &rc.Type, &rc.Value, &rc.MaxUses, &rc.UseCount, &rc.ExpiresAt, &rc.IsActive, &rc.BatchID, &rc.CreatedByUserID, &rc.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redemption_codes.GetByID: %w", err)
	}
	return &rc, nil
}

// List 管理端分页列表，支持搜索和按 type 过滤。
func (r *RedemptionCodesRepository) List(
	ctx context.Context,
	limit int,
	beforeCreatedAt *time.Time,
	beforeID *uuid.UUID,
	query string,
	codeType string,
) ([]RedemptionCode, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("redemption_codes.List: limit must be positive")
	}
	if (beforeCreatedAt == nil) != (beforeID == nil) {
		return nil, fmt.Errorf("redemption_codes.List: before_created_at and before_id must be provided together")
	}

	sql := `SELECT id, code, type, value, max_uses, use_count, expires_at, is_active, batch_id, created_by_user_id, created_at
	        FROM redemption_codes WHERE 1=1`
	args := []any{}
	argIdx := 1

	if query != "" {
		pattern := "%" + query + "%"
		sql += fmt.Sprintf(" AND (code ILIKE $%d OR batch_id ILIKE $%d)", argIdx, argIdx)
		args = append(args, pattern)
		argIdx++
	}

	if codeType != "" {
		sql += fmt.Sprintf(" AND type = $%d", argIdx)
		args = append(args, codeType)
		argIdx++
	}

	if beforeCreatedAt != nil && beforeID != nil {
		sql += fmt.Sprintf(" AND (created_at < $%d OR (created_at = $%d AND id < $%d))", argIdx, argIdx, argIdx+1)
		args = append(args, beforeCreatedAt.UTC(), *beforeID)
		argIdx += 2
	}

	sql += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("redemption_codes.List: %w", err)
	}
	defer rows.Close()

	var items []RedemptionCode
	for rows.Next() {
		var rc RedemptionCode
		if err := rows.Scan(&rc.ID, &rc.Code, &rc.Type, &rc.Value, &rc.MaxUses, &rc.UseCount, &rc.ExpiresAt, &rc.IsActive, &rc.BatchID, &rc.CreatedByUserID, &rc.CreatedAt); err != nil {
			return nil, fmt.Errorf("redemption_codes.List scan: %w", err)
		}
		items = append(items, rc)
	}
	return items, rows.Err()
}

// Deactivate 停用兑换码。
func (r *RedemptionCodesRepository) Deactivate(ctx context.Context, id uuid.UUID) (*RedemptionCode, error) {
	var rc RedemptionCode
	err := r.db.QueryRow(ctx,
		`UPDATE redemption_codes SET is_active = false WHERE id = $1
		 RETURNING id, code, type, value, max_uses, use_count, expires_at, is_active, batch_id, created_by_user_id, created_at`,
		id,
	).Scan(&rc.ID, &rc.Code, &rc.Type, &rc.Value, &rc.MaxUses, &rc.UseCount, &rc.ExpiresAt, &rc.IsActive, &rc.BatchID, &rc.CreatedByUserID, &rc.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redemption_codes.Deactivate: %w", err)
	}
	return &rc, nil
}

// IncrementUseCount 原子递增 use_count，仅在码可用时生效。
func (r *RedemptionCodesRepository) IncrementUseCount(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`UPDATE redemption_codes
		 SET use_count = use_count + 1
		 WHERE id = $1 AND is_active = true AND use_count < max_uses
		   AND (expires_at IS NULL OR expires_at > now())`,
		id,
	)
	if err != nil {
		return false, fmt.Errorf("redemption_codes.IncrementUseCount: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// RecordRedemption 记录兑换行为。
func (r *RedemptionCodesRepository) RecordRedemption(ctx context.Context, codeID, userID, accountID uuid.UUID) (*RedemptionRecord, error) {
	var rr RedemptionRecord
	err := r.db.QueryRow(ctx,
		`INSERT INTO redemption_records (code_id, user_id, account_id)
		 VALUES ($1, $2, $3)
		 RETURNING id, code_id, user_id, account_id, redeemed_at`,
		codeID, userID, accountID,
	).Scan(&rr.ID, &rr.CodeID, &rr.UserID, &rr.AccountID, &rr.RedeemedAt)
	if err != nil {
		return nil, fmt.Errorf("redemption_codes.RecordRedemption: %w", err)
	}
	return &rr, nil
}

// HasRedeemed 检查用户是否已兑换过某码。
func (r *RedemptionCodesRepository) HasRedeemed(ctx context.Context, codeID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM redemption_records WHERE code_id = $1 AND user_id = $2)`,
		codeID, userID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("redemption_codes.HasRedeemed: %w", err)
	}
	return exists, nil
}
