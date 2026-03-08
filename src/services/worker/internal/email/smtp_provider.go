package email

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Querier 用于数据库查询 (pgxpool.Pool 和 pgx.Tx 均满足)。
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PGSmtpProvider 从 smtp_providers 表查询默认 SMTP 配置。
type PGSmtpProvider struct {
	db Querier
}

func NewPGSmtpProvider(db Querier) *PGSmtpProvider {
	return &PGSmtpProvider{db: db}
}

func (p *PGSmtpProvider) DefaultSmtpConfig(ctx context.Context) (*Config, error) {
	row := p.db.QueryRow(ctx,
		`SELECT from_addr, smtp_host, smtp_port, smtp_user, smtp_pass, tls_mode
		   FROM smtp_providers WHERE is_default = true LIMIT 1`)

	var cfg Config
	var tlsMode string
	err := row.Scan(&cfg.From, &cfg.Host, &cfg.Port, &cfg.User, &cfg.Pass, &tlsMode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cfg.TLSMode = TLSMode(tlsMode)
	return &cfg, nil
}
