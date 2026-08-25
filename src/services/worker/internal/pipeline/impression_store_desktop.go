package pipeline

import (
	"context"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
)

type desktopImpressionStore struct {
	db data.DesktopDB
}

func NewDesktopImpressionStore(db data.DesktopDB) ImpressionStore {
	return desktopImpressionStore{db: db}
}

func (s desktopImpressionStore) Get(ctx context.Context, accountID, userID uuid.UUID, agentID string) (string, bool, error) {
	if s.db == nil {
		return "", false, nil
	}
	return data.ImpressionRepository{}.Get(ctx, s.db, accountID, userID, agentID)
}

func (s desktopImpressionStore) Upsert(ctx context.Context, accountID, userID uuid.UUID, agentID, impression string) error {
	if s.db == nil {
		return nil
	}
	return data.ImpressionRepository{}.Upsert(ctx, s.db, accountID, userID, agentID, impression)
}

func (s desktopImpressionStore) AddScore(ctx context.Context, accountID, userID uuid.UUID, agentID string, delta int) (int, error) {
	if s.db == nil {
		return 0, nil
	}
	return data.ImpressionRepository{}.AddScore(ctx, s.db, accountID, userID, agentID, delta)
}

func (s desktopImpressionStore) ResetScore(ctx context.Context, accountID, userID uuid.UUID, agentID string) error {
	if s.db == nil {
		return nil
	}
	return data.ImpressionRepository{}.ResetScore(ctx, s.db, accountID, userID, agentID)
}
