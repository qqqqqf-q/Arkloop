//go:build !desktop

package pipeline

import (
	"context"
	"time"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgxSuggestionStore struct {
	pool *pgxpool.Pool
}

func NewPgxSuggestionStore(pool *pgxpool.Pool) SuggestionStore {
	return pgxSuggestionStore{pool: pool}
}

func (s pgxSuggestionStore) GetByMode(ctx context.Context, accountID, userID uuid.UUID, agentID, mode string) (string, *time.Time, bool, error) {
	if s.pool == nil {
		return "", nil, false, nil
	}
	return data.SuggestionRepository{}.GetByMode(ctx, s.pool, accountID, userID, agentID, mode)
}

func (s pgxSuggestionStore) UpsertSuggestions(ctx context.Context, accountID, userID uuid.UUID, agentID, mode, suggestionsJSON string, expiresAt time.Time) error {
	if s.pool == nil {
		return nil
	}
	return data.SuggestionRepository{}.UpsertSuggestions(ctx, s.pool, accountID, userID, agentID, mode, suggestionsJSON, expiresAt)
}

func (s pgxSuggestionStore) AddScore(ctx context.Context, accountID, userID uuid.UUID, agentID, mode string, delta int) (int, *time.Time, error) {
	if s.pool == nil {
		return 0, nil, nil
	}
	return data.SuggestionRepository{}.AddScore(ctx, s.pool, accountID, userID, agentID, mode, delta)
}

func (s pgxSuggestionStore) ResetScore(ctx context.Context, accountID, userID uuid.UUID, agentID, mode string) error {
	if s.pool == nil {
		return nil
	}
	return data.SuggestionRepository{}.ResetScore(ctx, s.pool, accountID, userID, agentID, mode)
}
