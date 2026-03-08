package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SmtpProvider struct {
	ID        uuid.UUID
	Name      string
	FromAddr  string
	SmtpHost  string
	SmtpPort  int
	SmtpUser  string
	SmtpPass  string
	TLSMode   string
	IsDefault bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateSmtpProviderParams struct {
	Name     string
	FromAddr string
	SmtpHost string
	SmtpPort int
	SmtpUser string
	SmtpPass string
	TLSMode  string
}

type UpdateSmtpProviderParams struct {
	Name     string
	FromAddr string
	SmtpHost string
	SmtpPort int
	SmtpUser string
	SmtpPass string // 空字符串 = 保留原值
	TLSMode  string
}

type SmtpProviderRepository struct {
	db Querier
}

func NewSmtpProviderRepository(db Querier) (*SmtpProviderRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &SmtpProviderRepository{db: db}, nil
}

func (r *SmtpProviderRepository) List(ctx context.Context) ([]SmtpProvider, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass, tls_mode, is_default, created_at, updated_at
		 FROM smtp_providers ORDER BY is_default DESC, created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("smtp_providers.List: %w", err)
	}
	defer rows.Close()

	var items []SmtpProvider
	for rows.Next() {
		var s SmtpProvider
		if err := rows.Scan(&s.ID, &s.Name, &s.FromAddr, &s.SmtpHost, &s.SmtpPort,
			&s.SmtpUser, &s.SmtpPass, &s.TLSMode, &s.IsDefault, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("smtp_providers.List scan: %w", err)
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

func (r *SmtpProviderRepository) Get(ctx context.Context, id uuid.UUID) (*SmtpProvider, error) {
	var s SmtpProvider
	err := r.db.QueryRow(ctx,
		`SELECT id, name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass, tls_mode, is_default, created_at, updated_at
		 FROM smtp_providers WHERE id = $1`, id,
	).Scan(&s.ID, &s.Name, &s.FromAddr, &s.SmtpHost, &s.SmtpPort,
		&s.SmtpUser, &s.SmtpPass, &s.TLSMode, &s.IsDefault, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("smtp_providers.Get: %w", err)
	}
	return &s, nil
}

func (r *SmtpProviderRepository) GetDefault(ctx context.Context) (*SmtpProvider, error) {
	var s SmtpProvider
	err := r.db.QueryRow(ctx,
		`SELECT id, name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass, tls_mode, is_default, created_at, updated_at
		 FROM smtp_providers WHERE is_default = true LIMIT 1`,
	).Scan(&s.ID, &s.Name, &s.FromAddr, &s.SmtpHost, &s.SmtpPort,
		&s.SmtpUser, &s.SmtpPass, &s.TLSMode, &s.IsDefault, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("smtp_providers.GetDefault: %w", err)
	}
	return &s, nil
}

func (r *SmtpProviderRepository) Create(ctx context.Context, p CreateSmtpProviderParams) (*SmtpProvider, error) {
	p.Name = strings.TrimSpace(p.Name)
	p.FromAddr = strings.TrimSpace(p.FromAddr)
	p.SmtpHost = strings.TrimSpace(p.SmtpHost)
	if p.Name == "" {
		return nil, fmt.Errorf("smtp_providers.Create: name is required")
	}
	if p.FromAddr == "" {
		return nil, fmt.Errorf("smtp_providers.Create: from_addr is required")
	}
	if p.SmtpHost == "" {
		return nil, fmt.Errorf("smtp_providers.Create: smtp_host is required")
	}
	if p.SmtpPort <= 0 || p.SmtpPort > 65535 {
		p.SmtpPort = 587
	}
	if p.TLSMode == "" {
		p.TLSMode = "starttls"
	}

	// 如果是第一个 provider，自动设为 default
	var count int
	_ = r.db.QueryRow(ctx, `SELECT count(*) FROM smtp_providers`).Scan(&count)
	isDefault := count == 0

	var s SmtpProvider
	err := r.db.QueryRow(ctx,
		`INSERT INTO smtp_providers (name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass, tls_mode, is_default)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass, tls_mode, is_default, created_at, updated_at`,
		p.Name, p.FromAddr, p.SmtpHost, p.SmtpPort, p.SmtpUser, p.SmtpPass, p.TLSMode, isDefault,
	).Scan(&s.ID, &s.Name, &s.FromAddr, &s.SmtpHost, &s.SmtpPort,
		&s.SmtpUser, &s.SmtpPass, &s.TLSMode, &s.IsDefault, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("smtp_providers.Create: %w", err)
	}
	return &s, nil
}

func (r *SmtpProviderRepository) Update(ctx context.Context, id uuid.UUID, p UpdateSmtpProviderParams) (*SmtpProvider, error) {
	p.Name = strings.TrimSpace(p.Name)
	p.FromAddr = strings.TrimSpace(p.FromAddr)
	p.SmtpHost = strings.TrimSpace(p.SmtpHost)
	if p.SmtpPort <= 0 || p.SmtpPort > 65535 {
		p.SmtpPort = 587
	}
	if p.TLSMode == "" {
		p.TLSMode = "starttls"
	}

	var query string
	var args []any
	if p.SmtpPass != "" {
		query = `UPDATE smtp_providers
			SET name = $2, from_addr = $3, smtp_host = $4, smtp_port = $5, smtp_user = $6, smtp_pass = $7, tls_mode = $8, updated_at = now()
			WHERE id = $1
			RETURNING id, name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass, tls_mode, is_default, created_at, updated_at`
		args = []any{id, p.Name, p.FromAddr, p.SmtpHost, p.SmtpPort, p.SmtpUser, p.SmtpPass, p.TLSMode}
	} else {
		query = `UPDATE smtp_providers
			SET name = $2, from_addr = $3, smtp_host = $4, smtp_port = $5, smtp_user = $6, tls_mode = $7, updated_at = now()
			WHERE id = $1
			RETURNING id, name, from_addr, smtp_host, smtp_port, smtp_user, smtp_pass, tls_mode, is_default, created_at, updated_at`
		args = []any{id, p.Name, p.FromAddr, p.SmtpHost, p.SmtpPort, p.SmtpUser, p.TLSMode}
	}

	var s SmtpProvider
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&s.ID, &s.Name, &s.FromAddr, &s.SmtpHost, &s.SmtpPort,
		&s.SmtpUser, &s.SmtpPass, &s.TLSMode, &s.IsDefault, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("smtp_providers.Update: %w", err)
	}
	return &s, nil
}

func (r *SmtpProviderRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM smtp_providers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("smtp_providers.Delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("smtp_providers.Delete: not found")
	}
	return nil
}

func (r *SmtpProviderRepository) SetDefault(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE smtp_providers SET is_default = false WHERE is_default = true`)
	if err != nil {
		return fmt.Errorf("smtp_providers.SetDefault clear: %w", err)
	}
	tag, err := r.db.Exec(ctx, `UPDATE smtp_providers SET is_default = true, updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("smtp_providers.SetDefault: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("smtp_providers.SetDefault: not found")
	}
	return nil
}
