//go:build desktop

package pipeline

import (
	"context"
	"time"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
)

type desktopSuggestionStore struct {
	db data.DesktopDB
}

func NewDesktopSuggestionStore(db data.DesktopDB) SuggestionStore {
	return desktopSuggestionStore{db: db}
}

func (s desktopSuggestionStore) GetByMode(ctx context.Context, accountID, userID uuid.UUID, agentID, mode string) (string, *time.Time, bool, error) {
	if s.db == nil {
		return "", nil, false, nil
	}
	return data.SuggestionRepository{}.GetByMode(ctx, s.db, accountID, userID, agentID, mode)
}

func (s desktopSuggestionStore) UpsertSuggestions(ctx context.Context, accountID, userID uuid.UUID, agentID, mode, suggestionsJSON string, expiresAt time.Time) error {
	if s.db == nil {
		return nil
	}
	return data.SuggestionRepository{}.UpsertSuggestions(ctx, s.db, accountID, userID, agentID, mode, suggestionsJSON, expiresAt)
}

func (s desktopSuggestionStore) AddScore(ctx context.Context, accountID, userID uuid.UUID, agentID, mode string, delta int) (int, *time.Time, error) {
	if s.db == nil {
		return 0, nil, nil
	}
	return data.SuggestionRepository{}.AddScore(ctx, s.db, accountID, userID, agentID, mode, delta)
}

func (s desktopSuggestionStore) ResetScore(ctx context.Context, accountID, userID uuid.UUID, agentID, mode string) error {
	if s.db == nil {
		return nil
	}
	return data.SuggestionRepository{}.ResetScore(ctx, s.db, accountID, userID, agentID, mode)
}
