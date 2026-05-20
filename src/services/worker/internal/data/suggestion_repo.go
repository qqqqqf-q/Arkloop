//go:build !desktop

package data

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SuggestionRepository struct{}

func (SuggestionRepository) GetByMode(ctx context.Context, pool *pgxpool.Pool, accountID, userID uuid.UUID, agentID, mode string) (string, *time.Time, bool, error) {
	var suggestionsJSON string
	var expiresAt *time.Time
	err := pool.QueryRow(ctx,
		`SELECT suggestions_json, expires_at FROM user_suggestion_snapshots
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3 AND mode = $4`,
		accountID, userID, agentID, mode,
	).Scan(&suggestionsJSON, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, false, nil
		}
		return "", nil, false, err
	}
	return suggestionsJSON, expiresAt, true, nil
}

func (SuggestionRepository) UpsertSuggestions(ctx context.Context, pool *pgxpool.Pool, accountID, userID uuid.UUID, agentID, mode, suggestionsJSON string, expiresAt time.Time) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO user_suggestion_snapshots (account_id, user_id, agent_id, mode, suggestions_json, last_build_at, expires_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, now(), $6, now())
		 ON CONFLICT (account_id, user_id, agent_id, mode)
		 DO UPDATE SET suggestions_json = EXCLUDED.suggestions_json, last_build_at = now(), expires_at = EXCLUDED.expires_at, updated_at = now()`,
		accountID, userID, agentID, mode, suggestionsJSON, expiresAt,
	)
	return err
}

func (SuggestionRepository) AddScore(ctx context.Context, pool *pgxpool.Pool, accountID, userID uuid.UUID, agentID, mode string, delta int) (int, *time.Time, error) {
	var newScore int
	var lastBuildAt *time.Time
	err := pool.QueryRow(ctx,
		`INSERT INTO user_suggestion_snapshots (account_id, user_id, agent_id, mode, suggestion_score, updated_at)
		 VALUES ($1, $2, $3, $4, $5, now())
		 ON CONFLICT (account_id, user_id, agent_id, mode)
		 DO UPDATE SET suggestion_score = user_suggestion_snapshots.suggestion_score + $5, updated_at = now()
		 RETURNING suggestion_score, last_build_at`,
		accountID, userID, agentID, mode, delta,
	).Scan(&newScore, &lastBuildAt)
	return newScore, lastBuildAt, err
}

func (SuggestionRepository) ResetScore(ctx context.Context, pool *pgxpool.Pool, accountID, userID uuid.UUID, agentID, mode string) error {
	_, err := pool.Exec(ctx,
		`UPDATE user_suggestion_snapshots
		 SET suggestion_score = 0, updated_at = now()
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3 AND mode = $4`,
		accountID, userID, agentID, mode,
	)
	return err
}
