package pipeline

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type SuggestionStore interface {
	GetByMode(ctx context.Context, accountID, userID uuid.UUID, agentID, mode string) (suggestionsJSON string, expiresAt *time.Time, found bool, err error)
	UpsertSuggestions(ctx context.Context, accountID, userID uuid.UUID, agentID, mode, suggestionsJSON string, expiresAt time.Time) error
	AddScore(ctx context.Context, accountID, userID uuid.UUID, agentID, mode string, delta int) (newScore int, lastBuildAt *time.Time, err error)
	ResetScore(ctx context.Context, accountID, userID uuid.UUID, agentID, mode string) error
}
