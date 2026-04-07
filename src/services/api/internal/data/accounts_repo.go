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

type Account struct {
	ID           uuid.UUID
	Slug         string
	Name         string
	Type         string // "personal" | "workspace"
	OwnerUserID  *uuid.UUID
	Status       string
	Country      *string
	Timezone     *string
	LogoURL      *string
	SettingsJSON json.RawMessage
	DeletedAt    *time.Time
	CreatedAt    time.Time
}

type AccountRepository struct {
	db Querier
}

func (r *AccountRepository) WithTx(tx pgx.Tx) *AccountRepository {
	return &AccountRepository{db: tx}
}

func NewAccountRepository(db Querier) (*AccountRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &AccountRepository{db: db}, nil
}

func (r *AccountRepository) Create(ctx context.Context, slug string, name string, accountType string) (Account, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if slug == "" {
		return Account{}, fmt.Errorf("slug must not be empty")
	}
	if name == "" {
		return Account{}, fmt.Errorf("name must not be empty")
	}
	if accountType != "personal" && accountType != "workspace" {
		return Account{}, fmt.Errorf("account type must be personal or workspace")
	}

	var account Account
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO accounts (slug, name, type)
		 VALUES ($1, $2, $3)
		 RETURNING id, slug, name, type, owner_user_id, status, country, timezone,
		           logo_url, settings_json, deleted_at, created_at`,
		slug,
		name,
		accountType,
	).Scan(
		&account.ID, &account.Slug, &account.Name, &account.Type,
		&account.OwnerUserID, &account.Status, &account.Country, &account.Timezone,
		&account.LogoURL, &account.SettingsJSON, &account.DeletedAt, &account.CreatedAt,
	)
	if err != nil {
		return Account{}, err
	}
	return account, nil
}

func (r *AccountRepository) GetByID(ctx context.Context, accountID uuid.UUID) (*Account, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
	}

	var account Account
	err := r.db.QueryRow(
		ctx,
		`SELECT id, slug, name, type, owner_user_id, status, country, timezone,
		        logo_url, settings_json, deleted_at, created_at
		 FROM accounts
		 WHERE id = $1
		 LIMIT 1`,
		accountID,
	).Scan(
		&account.ID, &account.Slug, &account.Name, &account.Type,
		&account.OwnerUserID, &account.Status, &account.Country, &account.Timezone,
		&account.LogoURL, &account.SettingsJSON, &account.DeletedAt, &account.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &account, nil
}

// ListByUser 返回用户所属的所有 account（通过 account_memberships JOIN）。
func (r *AccountRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]Account, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user_id must not be empty")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT o.id, o.slug, o.name, o.type, o.owner_user_id, o.status, o.country, o.timezone,
		        o.logo_url, o.settings_json, o.deleted_at, o.created_at
		 FROM accounts o
		 JOIN account_memberships m ON m.account_id = o.id
		 WHERE m.user_id = $1
		   AND o.deleted_at IS NULL
		 ORDER BY o.created_at ASC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("accounts.ListByUser: %w", err)
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var account Account
		if err := rows.Scan(
			&account.ID, &account.Slug, &account.Name, &account.Type,
			&account.OwnerUserID, &account.Status, &account.Country, &account.Timezone,
			&account.LogoURL, &account.SettingsJSON, &account.DeletedAt, &account.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("accounts.ListByUser scan: %w", err)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("accounts.ListByUser rows: %w", err)
	}
	return accounts, nil
}

func (r *AccountRepository) CountActive(ctx context.Context) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var count int64
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE deleted_at IS NULL`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("accounts.CountActive: %w", err)
	}
	return count, nil
}

func (r *AccountRepository) UpdateSettings(ctx context.Context, accountID uuid.UUID, key string, value any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return fmt.Errorf("account_id must not be empty")
	}
	key = strings.TrimSpace(key)
	if !isValidAccountSettingKey(key) {
		return fmt.Errorf("invalid account setting key")
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal settings value: %w", err)
	}
	tag, err := r.db.Exec(
		ctx,
		fmt.Sprintf(
			`UPDATE accounts
			    SET settings_json = jsonb_set(COALESCE(settings_json, '{}'::jsonb), '{%s}', $2::jsonb, true)
			  WHERE id = $1`,
			key,
		),
		accountID,
		string(payload),
	)
	if err != nil {
		return fmt.Errorf("accounts.UpdateSettings: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("account not found: %s", accountID)
	}
	return nil
}

func isValidAccountSettingKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return false
		}
	}
	return true
}
