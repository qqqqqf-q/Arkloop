//go:build desktop

package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedDesktopUser 在桌面模式首次启动时写入固定的用户、账户和成员关系。
// 已存在时静默跳过（ON CONFLICT DO NOTHING），保证幂等。
func SeedDesktopUser(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("database pool is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	return pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO users (id, username, email, status)
			VALUES ($1, 'desktop', 'desktop@localhost', 'active')
			ON CONFLICT (id) DO NOTHING`,
			DesktopUserID,
		)
		if err != nil {
			return fmt.Errorf("seed desktop user: %w", err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO accounts (id, slug, name, type, owner_user_id)
			VALUES ($1, 'desktop', 'Desktop', 'personal', $2)
			ON CONFLICT (id) DO NOTHING`,
			DesktopAccountID, DesktopUserID,
		)
		if err != nil {
			return fmt.Errorf("seed desktop account: %w", err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO account_memberships (account_id, user_id, role)
			VALUES ($1, $2, $3)
			ON CONFLICT (account_id, user_id) DO NOTHING`,
			DesktopAccountID, DesktopUserID, DesktopRole,
		)
		if err != nil {
			return fmt.Errorf("seed desktop membership: %w", err)
		}

		return nil
	})
}
