package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type EmailConfig struct {
	ID          uuid.UUID
	Name        string
	FromAddr    string
	SMTPHost    string
	SMTPPort    string
	SMTPUser    string
	SMTPPass    string
	SMTPTLSMode string
	IsDefault   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type EmailConfigsRepository struct {
	db Querier
}

func NewEmailConfigsRepository(db Querier) (*EmailConfigsRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	return &EmailConfigsRepository{db: db}, nil
}

func (r *EmailConfigsRepository) List(ctx context.Context) ([]EmailConfig, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass,
		       smtp_tls_mode, is_default, created_at, updated_at
		FROM email_configs
		ORDER BY is_default DESC, created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EmailConfig
	for rows.Next() {
		var c EmailConfig
		if err := rows.Scan(
			&c.ID, &c.Name, &c.FromAddr, &c.SMTPHost, &c.SMTPPort,
			&c.SMTPUser, &c.SMTPPass, &c.SMTPTLSMode, &c.IsDefault,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *EmailConfigsRepository) Get(ctx context.Context, id uuid.UUID) (*EmailConfig, error) {
	var c EmailConfig
	err := r.db.QueryRow(ctx, `
		SELECT id, name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass,
		       smtp_tls_mode, is_default, created_at, updated_at
		FROM email_configs WHERE id = $1
	`, id).Scan(
		&c.ID, &c.Name, &c.FromAddr, &c.SMTPHost, &c.SMTPPort,
		&c.SMTPUser, &c.SMTPPass, &c.SMTPTLSMode, &c.IsDefault,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *EmailConfigsRepository) Create(ctx context.Context, c EmailConfig) (*EmailConfig, error) {
	var out EmailConfig
	err := r.db.QueryRow(ctx, `
		INSERT INTO email_configs
		    (name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass, smtp_tls_mode, is_default)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass,
		          smtp_tls_mode, is_default, created_at, updated_at
	`, c.Name, c.FromAddr, c.SMTPHost, c.SMTPPort, c.SMTPUser, c.SMTPPass, c.SMTPTLSMode, c.IsDefault,
	).Scan(
		&out.ID, &out.Name, &out.FromAddr, &out.SMTPHost, &out.SMTPPort,
		&out.SMTPUser, &out.SMTPPass, &out.SMTPTLSMode, &out.IsDefault,
		&out.CreatedAt, &out.UpdatedAt,
	)
	return &out, err
}

func (r *EmailConfigsRepository) Update(ctx context.Context, id uuid.UUID, c EmailConfig) (*EmailConfig, error) {
	var out EmailConfig
	err := r.db.QueryRow(ctx, `
		UPDATE email_configs
		SET name = $2, from_addr = $3, smtp_host = $4, smtp_port = $5,
		    smtp_user = $6, smtp_tls_mode = $7, updated_at = now()
		WHERE id = $1
		RETURNING id, name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass,
		          smtp_tls_mode, is_default, created_at, updated_at
	`, id, c.Name, c.FromAddr, c.SMTPHost, c.SMTPPort, c.SMTPUser, c.SMTPTLSMode,
	).Scan(
		&out.ID, &out.Name, &out.FromAddr, &out.SMTPHost, &out.SMTPPort,
		&out.SMTPUser, &out.SMTPPass, &out.SMTPTLSMode, &out.IsDefault,
		&out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &out, err
}

// UpdatePass 单独更新密码（避免覆盖为空）。
func (r *EmailConfigsRepository) UpdatePass(ctx context.Context, id uuid.UUID, pass string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE email_configs SET smtp_pass = $2, updated_at = now() WHERE id = $1
	`, id, pass)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("email config not found")
	}
	return nil
}

func (r *EmailConfigsRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM email_configs WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("email config not found")
	}
	return nil
}

// SetDefault 将指定配置设为默认，清除其他配置的 is_default。
// 在同一事务内完成，使用 pgx 事务。
func (r *EmailConfigsRepository) SetDefault(ctx context.Context, id uuid.UUID) error {
	// 检查记录存在
	var exists bool
	if err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_configs WHERE id = $1)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("email config not found")
	}

	if _, err := r.db.Exec(ctx, `UPDATE email_configs SET is_default = false WHERE is_default = true`); err != nil {
		return err
	}
	_, err := r.db.Exec(ctx, `UPDATE email_configs SET is_default = true, updated_at = now() WHERE id = $1`, id)
	return err
}

// GetDefault 返回当前默认配置，若无则返回 nil。
func (r *EmailConfigsRepository) GetDefault(ctx context.Context) (*EmailConfig, error) {
	var c EmailConfig
	err := r.db.QueryRow(ctx, `
		SELECT id, name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass,
		       smtp_tls_mode, is_default, created_at, updated_at
		FROM email_configs WHERE is_default = true LIMIT 1
	`).Scan(
		&c.ID, &c.Name, &c.FromAddr, &c.SMTPHost, &c.SMTPPort,
		&c.SMTPUser, &c.SMTPPass, &c.SMTPTLSMode, &c.IsDefault,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
