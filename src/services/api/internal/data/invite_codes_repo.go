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

const inviteCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
const inviteCodeLength = 8

type InviteCode struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Code      string
	MaxUses   int
	UseCount  int
	IsActive  bool
	CreatedAt time.Time
}

// InviteCodeWithUser 管理端列表时附带用户信息。
type InviteCodeWithUser struct {
	InviteCode
	UserDisplayName string
	UserEmail       *string
}

type InviteCodeRepository struct {
	db Querier
}

func NewInviteCodeRepository(db Querier) (*InviteCodeRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &InviteCodeRepository{db: db}, nil
}

// GenerateCode 生成 8 位随机邀请码（大写字母 + 数字，排除易混淆的 0/O/1/I）。
func GenerateCode() (string, error) {
	alphabetLen := big.NewInt(int64(len(inviteCodeAlphabet)))
	var sb strings.Builder
	sb.Grow(inviteCodeLength)
	for i := 0; i < inviteCodeLength; i++ {
		idx, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", fmt.Errorf("invite_codes.GenerateCode: %w", err)
		}
		sb.WriteByte(inviteCodeAlphabet[idx.Int64()])
	}
	return sb.String(), nil
}

func (r *InviteCodeRepository) Create(ctx context.Context, userID uuid.UUID, code string, maxUses int) (*InviteCode, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("invite_codes.Create: user_id must not be empty")
	}
	if code == "" {
		return nil, fmt.Errorf("invite_codes.Create: code must not be empty")
	}
	if maxUses <= 0 {
		return nil, fmt.Errorf("invite_codes.Create: max_uses must be positive")
	}

	var ic InviteCode
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO invite_codes (user_id, code, max_uses)
		 VALUES ($1, $2, $3)
		 RETURNING id, user_id, code, max_uses, use_count, is_active, created_at`,
		userID, code, maxUses,
	).Scan(&ic.ID, &ic.UserID, &ic.Code, &ic.MaxUses, &ic.UseCount, &ic.IsActive, &ic.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("invite_codes.Create: %w", err)
	}
	return &ic, nil
}

func (r *InviteCodeRepository) GetByCode(ctx context.Context, code string) (*InviteCode, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var ic InviteCode
	err := r.db.QueryRow(
		ctx,
		`SELECT id, user_id, code, max_uses, use_count, is_active, created_at
		 FROM invite_codes WHERE code = $1`,
		code,
	).Scan(&ic.ID, &ic.UserID, &ic.Code, &ic.MaxUses, &ic.UseCount, &ic.IsActive, &ic.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("invite_codes.GetByCode: %w", err)
	}
	return &ic, nil
}

func (r *InviteCodeRepository) GetByID(ctx context.Context, id uuid.UUID) (*InviteCode, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var ic InviteCode
	err := r.db.QueryRow(
		ctx,
		`SELECT id, user_id, code, max_uses, use_count, is_active, created_at
		 FROM invite_codes WHERE id = $1`,
		id,
	).Scan(&ic.ID, &ic.UserID, &ic.Code, &ic.MaxUses, &ic.UseCount, &ic.IsActive, &ic.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("invite_codes.GetByID: %w", err)
	}
	return &ic, nil
}

func (r *InviteCodeRepository) ListByUserID(ctx context.Context, userID uuid.UUID) ([]InviteCode, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT id, user_id, code, max_uses, use_count, is_active, created_at
		 FROM invite_codes WHERE user_id = $1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("invite_codes.ListByUserID: %w", err)
	}
	defer rows.Close()

	var items []InviteCode
	for rows.Next() {
		var ic InviteCode
		if err := rows.Scan(&ic.ID, &ic.UserID, &ic.Code, &ic.MaxUses, &ic.UseCount, &ic.IsActive, &ic.CreatedAt); err != nil {
			return nil, fmt.Errorf("invite_codes.ListByUserID scan: %w", err)
		}
		items = append(items, ic)
	}
	return items, rows.Err()
}

// ListActiveByUserID 返回用户所有活跃的邀请码。
func (r *InviteCodeRepository) ListActiveByUserID(ctx context.Context, userID uuid.UUID) ([]InviteCode, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT id, user_id, code, max_uses, use_count, is_active, created_at
		 FROM invite_codes WHERE user_id = $1 AND is_active = true
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("invite_codes.ListActiveByUserID: %w", err)
	}
	defer rows.Close()

	var items []InviteCode
	for rows.Next() {
		var ic InviteCode
		if err := rows.Scan(&ic.ID, &ic.UserID, &ic.Code, &ic.MaxUses, &ic.UseCount, &ic.IsActive, &ic.CreatedAt); err != nil {
			return nil, fmt.Errorf("invite_codes.ListActiveByUserID scan: %w", err)
		}
		items = append(items, ic)
	}
	return items, rows.Err()
}

func (r *InviteCodeRepository) CountActiveByUserID(ctx context.Context, userID uuid.UUID) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var count int
	err := r.db.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM invite_codes WHERE user_id = $1 AND is_active = true`,
		userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("invite_codes.CountActiveByUserID: %w", err)
	}
	return count, nil
}

// IncrementUseCount 原子递增 use_count，仅在码可用时生效。返回是否成功。
func (r *InviteCodeRepository) IncrementUseCount(ctx context.Context, id uuid.UUID) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tag, err := r.db.Exec(
		ctx,
		`UPDATE invite_codes
		 SET use_count = use_count + 1
		 WHERE id = $1 AND is_active = true AND use_count < max_uses`,
		id,
	)
	if err != nil {
		return false, fmt.Errorf("invite_codes.IncrementUseCount: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// DeactivateByUserID 停用某用户的所有邀请码（封禁时调用）。
func (r *InviteCodeRepository) DeactivateByUserID(ctx context.Context, userID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(
		ctx,
		`UPDATE invite_codes SET is_active = false WHERE user_id = $1 AND is_active = true`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("invite_codes.DeactivateByUserID: %w", err)
	}
	return nil
}

func (r *InviteCodeRepository) UpdateMaxUses(ctx context.Context, id uuid.UUID, maxUses int) (*InviteCode, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxUses <= 0 {
		return nil, fmt.Errorf("invite_codes.UpdateMaxUses: max_uses must be positive")
	}
	var ic InviteCode
	err := r.db.QueryRow(
		ctx,
		`UPDATE invite_codes SET max_uses = $1 WHERE id = $2
		 RETURNING id, user_id, code, max_uses, use_count, is_active, created_at`,
		maxUses, id,
	).Scan(&ic.ID, &ic.UserID, &ic.Code, &ic.MaxUses, &ic.UseCount, &ic.IsActive, &ic.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("invite_codes.UpdateMaxUses: %w", err)
	}
	return &ic, nil
}

func (r *InviteCodeRepository) SetActive(ctx context.Context, id uuid.UUID, isActive bool) (*InviteCode, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var ic InviteCode
	err := r.db.QueryRow(
		ctx,
		`UPDATE invite_codes SET is_active = $1 WHERE id = $2
		 RETURNING id, user_id, code, max_uses, use_count, is_active, created_at`,
		isActive, id,
	).Scan(&ic.ID, &ic.UserID, &ic.Code, &ic.MaxUses, &ic.UseCount, &ic.IsActive, &ic.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("invite_codes.SetActive: %w", err)
	}
	return &ic, nil
}

// List 管理端分页列表，支持按 code 或用户名搜索。
func (r *InviteCodeRepository) List(
	ctx context.Context,
	limit int,
	beforeCreatedAt *time.Time,
	beforeID *uuid.UUID,
	query string,
) ([]InviteCodeWithUser, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		return nil, fmt.Errorf("invite_codes.List: limit must be positive")
	}
	if (beforeCreatedAt == nil) != (beforeID == nil) {
		return nil, fmt.Errorf("invite_codes.List: before_created_at and before_id must be provided together")
	}

	sql := `SELECT ic.id, ic.user_id, ic.code, ic.max_uses, ic.use_count, ic.is_active, ic.created_at,
	               u.display_name, u.email
	        FROM invite_codes ic
	        JOIN users u ON u.id = ic.user_id
	        WHERE 1=1`
	args := []any{}
	argIdx := 1

	if query != "" {
		pattern := "%" + query + "%"
		sql += fmt.Sprintf(" AND (ic.code ILIKE $%d OR u.display_name ILIKE $%d OR u.email ILIKE $%d)", argIdx, argIdx, argIdx)
		args = append(args, pattern)
		argIdx++
	}

	if beforeCreatedAt != nil && beforeID != nil {
		sql += fmt.Sprintf(" AND (ic.created_at < $%d OR (ic.created_at = $%d AND ic.id < $%d))", argIdx, argIdx, argIdx+1)
		args = append(args, beforeCreatedAt.UTC(), *beforeID)
		argIdx += 2
	}

	sql += fmt.Sprintf(" ORDER BY ic.created_at DESC, ic.id DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("invite_codes.List: %w", err)
	}
	defer rows.Close()

	var items []InviteCodeWithUser
	for rows.Next() {
		var item InviteCodeWithUser
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.Code, &item.MaxUses, &item.UseCount, &item.IsActive, &item.CreatedAt,
			&item.UserDisplayName, &item.UserEmail,
		); err != nil {
			return nil, fmt.Errorf("invite_codes.List scan: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
