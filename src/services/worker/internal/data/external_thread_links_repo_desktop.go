//go:build desktop

package data

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ExternalThreadLinksRepository struct{}

func (ExternalThreadLinksRepository) Get(ctx context.Context, pool DesktopDB, accountID, threadID uuid.UUID, provider string) (string, bool, error) {
	var externalThreadID string
	err := pool.QueryRow(ctx,
		`SELECT external_thread_id
		   FROM external_thread_links
		  WHERE account_id = $1 AND thread_id = $2 AND provider = $3`,
		accountID, threadID, provider,
	).Scan(&externalThreadID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return externalThreadID, true, nil
}

func (ExternalThreadLinksRepository) Upsert(ctx context.Context, pool DesktopDB, accountID, threadID uuid.UUID, provider, externalThreadID string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO external_thread_links (account_id, thread_id, provider, external_thread_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, datetime('now'), datetime('now'))
		 ON CONFLICT (account_id, thread_id, provider)
		 DO UPDATE SET external_thread_id = EXCLUDED.external_thread_id, updated_at = datetime('now')`,
		accountID, threadID, provider, externalThreadID,
	)
	return err
}
