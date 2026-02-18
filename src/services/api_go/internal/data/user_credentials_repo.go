package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type UserCredential struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Login        string
	PasswordHash string
	CreatedAt    time.Time
}

type UserCredentialRepository struct {
	db Querier
}

func NewUserCredentialRepository(db Querier) (*UserCredentialRepository, error) {
	if db == nil {
		return nil, errors.New("db 不能为空")
	}
	return &UserCredentialRepository{db: db}, nil
}

func (r *UserCredentialRepository) Create(
	ctx context.Context,
	userID uuid.UUID,
	login string,
	passwordHash string,
) (UserCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if login == "" {
		return UserCredential{}, fmt.Errorf("login 不能为空")
	}
	if passwordHash == "" {
		return UserCredential{}, fmt.Errorf("password_hash 不能为空")
	}

	var credential UserCredential
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO user_credentials (user_id, login, password_hash)
		 VALUES ($1, $2, $3)
		 RETURNING id, user_id, login, password_hash, created_at`,
		userID,
		login,
		passwordHash,
	).Scan(&credential.ID, &credential.UserID, &credential.Login, &credential.PasswordHash, &credential.CreatedAt)
	if err != nil {
		return UserCredential{}, err
	}
	return credential, nil
}

func (r *UserCredentialRepository) GetByLogin(ctx context.Context, login string) (*UserCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if login == "" {
		return nil, nil
	}

	var credential UserCredential
	err := r.db.QueryRow(
		ctx,
		`SELECT id, user_id, login, password_hash, created_at
		 FROM user_credentials
		 WHERE login = $1
		 LIMIT 1`,
		login,
	).Scan(&credential.ID, &credential.UserID, &credential.Login, &credential.PasswordHash, &credential.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &credential, nil
}
