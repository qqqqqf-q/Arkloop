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
	TokensInvalidBefore time.Time
	CreatedAt           time.Time
}

type UserRepository struct {
	db Querier
}

func NewUserRepository(db Querier) (*UserRepository, error) {
	if db == nil {
		return nil, errors.New("db 不能为空")
	}
	return &UserRepository{db: db}, nil
}

func (r *UserRepository) Create(ctx context.Context, displayName string) (User, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if displayName == "" {
		return User{}, fmt.Errorf("display_name 不能为空")
	}

	var user User
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO users (display_name)
		 VALUES ($1)
		 RETURNING id, display_name, tokens_invalid_before, created_at`,
		displayName,
	).Scan(&user.ID, &user.DisplayName, &user.TokensInvalidBefore, &user.CreatedAt)
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
		`SELECT id, display_name, tokens_invalid_before, created_at
		 FROM users
		 WHERE id = $1
		 LIMIT 1`,
		userID,
	).Scan(&user.ID, &user.DisplayName, &user.TokensInvalidBefore, &user.CreatedAt)
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
