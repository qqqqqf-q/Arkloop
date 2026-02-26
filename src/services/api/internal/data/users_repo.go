package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type User struct {
	ID                  uuid.UUID
	DisplayName         string
	Email               *string
	EmailVerifiedAt     *time.Time
	Status              string
	DeletedAt           *time.Time
	AvatarURL           *string
	Locale              *string
	Timezone            *string
	LastLoginAt         *time.Time
	TokensInvalidBefore time.Time
	CreatedAt           time.Time
}

type UserRepository struct {
	db Querier
}

func NewUserRepository(db Querier) (*UserRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &UserRepository{db: db}, nil
}

func (r *UserRepository) Create(ctx context.Context, displayName string, email string) (User, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if displayName == "" {
		return User{}, fmt.Errorf("display_name must not be empty")
	}

	var user User
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO users (display_name, email)
		 VALUES ($1, NULLIF($2, ''))
		 RETURNING id, display_name, email, email_verified_at, status, deleted_at,
		           avatar_url, locale, timezone, last_login_at, tokens_invalid_before, created_at`,
		displayName, email,
	).Scan(
		&user.ID, &user.DisplayName, &user.Email, &user.EmailVerifiedAt,
		&user.Status, &user.DeletedAt, &user.AvatarURL, &user.Locale,
		&user.Timezone, &user.LastLoginAt, &user.TokensInvalidBefore, &user.CreatedAt,
	)
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (r *UserRepository) GetByID(ctx context.Context, userID uuid.UUID) (*User, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var user User
	err := r.db.QueryRow(
		ctx,
		`SELECT id, display_name, email, email_verified_at, status, deleted_at,
		        avatar_url, locale, timezone, last_login_at, tokens_invalid_before, created_at
		 FROM users
		 WHERE id = $1
		 LIMIT 1`,
		userID,
	).Scan(
		&user.ID, &user.DisplayName, &user.Email, &user.EmailVerifiedAt,
		&user.Status, &user.DeletedAt, &user.AvatarURL, &user.Locale,
		&user.Timezone, &user.LastLoginAt, &user.TokensInvalidBefore, &user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) BumpTokensInvalidBefore(ctx context.Context, userID uuid.UUID, tokensInvalidBefore time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := r.db.Exec(
		ctx,
		`UPDATE users
		 SET tokens_invalid_before = GREATEST(tokens_invalid_before, $1)
		 WHERE id = $2`,
		tokensInvalidBefore.UTC(),
		userID,
	)
	return err
}

func (r *UserRepository) CountAll(ctx context.Context) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var count int64
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("users.CountAll: %w", err)
	}
	return count, nil
}

func (r *UserRepository) List(
	ctx context.Context,
	limit int,
	beforeCreatedAt *time.Time,
	beforeID *uuid.UUID,
	query string,
	status string,
) ([]User, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}
	if (beforeCreatedAt == nil) != (beforeID == nil) {
		return nil, fmt.Errorf("before_created_at and before_id must be provided together")
	}

	sql := `SELECT id, display_name, email, email_verified_at, status, deleted_at,
	               avatar_url, locale, timezone, last_login_at, tokens_invalid_before, created_at
	        FROM users
	        WHERE deleted_at IS NULL`
	args := []any{}
	argIdx := 1

	if status != "" {
		sql += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}

	if query != "" {
		pattern := "%" + query + "%"
		sql += fmt.Sprintf(" AND (display_name ILIKE $%d OR email ILIKE $%d)", argIdx, argIdx)
		args = append(args, pattern)
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
		return nil, fmt.Errorf("users.List: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(
			&u.ID, &u.DisplayName, &u.Email, &u.EmailVerifiedAt,
			&u.Status, &u.DeletedAt, &u.AvatarURL, &u.Locale,
			&u.Timezone, &u.LastLoginAt, &u.TokensInvalidBefore, &u.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("users.List scan: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("users.List rows: %w", err)
	}
	return users, nil
}

func (r *UserRepository) UpdateStatus(ctx context.Context, userID uuid.UUID, status string) (*User, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user_id must not be empty")
	}
	if status != "active" && status != "suspended" {
		return nil, fmt.Errorf("status must be 'active' or 'suspended'")
	}

	var user User
	err := r.db.QueryRow(
		ctx,
		`UPDATE users SET status = $1
		 WHERE id = $2 AND deleted_at IS NULL
		 RETURNING id, display_name, email, email_verified_at, status, deleted_at,
		           avatar_url, locale, timezone, last_login_at, tokens_invalid_before, created_at`,
		status, userID,
	).Scan(
		&user.ID, &user.DisplayName, &user.Email, &user.EmailVerifiedAt,
		&user.Status, &user.DeletedAt, &user.AvatarURL, &user.Locale,
		&user.Timezone, &user.LastLoginAt, &user.TokensInvalidBefore, &user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("users.UpdateStatus: %w", err)
	}
	return &user, nil
}

func (r *UserRepository) CountActiveSince(ctx context.Context, since time.Time) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var count int64
	err := r.db.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND last_login_at >= $1`,
		since.UTC(),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("users.CountActiveSince: %w", err)
	}
	return count, nil
}

type UpdateProfileParams struct {
	DisplayName     string
	Email           *string
	EmailVerifiedAt *time.Time
	Locale          *string
	Timezone        *string
}

func (r *UserRepository) UpdateProfile(ctx context.Context, userID uuid.UUID, params UpdateProfileParams) (*User, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user_id must not be empty")
	}
	if params.DisplayName == "" {
		return nil, fmt.Errorf("display_name must not be empty")
	}

	var user User
	err := r.db.QueryRow(
		ctx,
		`UPDATE users
		 SET display_name = $1, email = $2, email_verified_at = $3, locale = $4, timezone = $5
		 WHERE id = $6 AND deleted_at IS NULL
		 RETURNING id, display_name, email, email_verified_at, status, deleted_at,
		           avatar_url, locale, timezone, last_login_at, tokens_invalid_before, created_at`,
		params.DisplayName, params.Email, params.EmailVerifiedAt, params.Locale, params.Timezone, userID,
	).Scan(
		&user.ID, &user.DisplayName, &user.Email, &user.EmailVerifiedAt,
		&user.Status, &user.DeletedAt, &user.AvatarURL, &user.Locale,
		&user.Timezone, &user.LastLoginAt, &user.TokensInvalidBefore, &user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("users.UpdateProfile: %w", err)
	}
	return &user, nil
}

func (r *UserRepository) SoftDelete(ctx context.Context, userID uuid.UUID) error {
if ctx == nil {
ctx = context.Background()
}
tag, err := r.db.Exec(ctx,
`UPDATE users SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`,
userID,
)
if err != nil {
return fmt.Errorf("users.SoftDelete: %w", err)
}
if tag.RowsAffected() == 0 {
return fmt.Errorf("users.SoftDelete: not found")
}
return nil
}

// SetEmailVerified 将 email_verified_at 标记为当前时间，表示邮箱已通过验证。
func (r *UserRepository) SetEmailVerified(ctx context.Context, userID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if userID == uuid.Nil {
		return fmt.Errorf("user_id must not be empty")
	}
	tag, err := r.db.Exec(
		ctx,
		`UPDATE users SET email_verified_at = now() WHERE id = $1 AND deleted_at IS NULL`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("users.SetEmailVerified: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("users.SetEmailVerified: not found")
	}
	return nil
}
