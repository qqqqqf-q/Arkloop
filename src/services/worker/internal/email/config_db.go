package email

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	settingFrom     = "email.from"
	settingHost     = "email.smtp_host"
	settingPort     = "email.smtp_port"
	settingUser     = "email.smtp_user"
	settingPass     = "email.smtp_pass"
	settingTLSMode  = "email.smtp_tls_mode"
)

// LoadConfigFromDB reads SMTP config from platform_settings.
// Returns (cfg, true, nil) when "email.from" is present in DB.
// Returns (zero, false, nil) when not configured in DB — caller should fall back to env.
func LoadConfigFromDB(ctx context.Context, pool *pgxpool.Pool) (Config, bool, error) {
	rows, err := pool.Query(ctx,
		`SELECT key, value FROM platform_settings WHERE key LIKE 'email.%'`)
	if err != nil {
		return Config{}, false, fmt.Errorf("query email config: %w", err)
	}
	defer rows.Close()

	m := make(map[string]string, 6)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return Config{}, false, err
		}
		m[k] = v
	}
	if rows.Err() != nil {
		return Config{}, false, rows.Err()
	}

	from := strings.TrimSpace(m[settingFrom])
	if from == "" {
		return Config{}, false, nil
	}

	cfg := Config{
		From:    from,
		Host:    strings.TrimSpace(m[settingHost]),
		User:    strings.TrimSpace(m[settingUser]),
		Pass:    strings.TrimSpace(m[settingPass]),
		Port:    defaultPort,
		TLSMode: defaultTLSMode,
	}
	if p := strings.TrimSpace(m[settingPort]); p != "" {
		if port, err := strconv.Atoi(p); err == nil {
			cfg.Port = port
		}
	}
	if t := strings.TrimSpace(m[settingTLSMode]); t != "" {
		cfg.TLSMode = TLSMode(t)
	}
	return cfg, true, nil
}
