package pipeline

import (
	"context"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
)

type desktopMemorySnapshotStore struct {
	db data.DesktopDB
}

// NewDesktopMemorySnapshotStore 基于桌面 SQLite；db 为 nil 时 Get 恒未命中、Upsert 无操作。
func NewDesktopMemorySnapshotStore(db data.DesktopDB) MemorySnapshotStore {
	return desktopMemorySnapshotStore{db: db}
}

func (s desktopMemorySnapshotStore) Get(ctx context.Context, accountID, userID uuid.UUID, agentID string) (string, bool, error) {
	if s.db == nil {
		return "", false, nil
	}
	return data.MemorySnapshotRepository{}.Get(ctx, s.db, accountID, userID, agentID)
}

func (s desktopMemorySnapshotStore) UpsertWithHits(ctx context.Context, accountID, userID uuid.UUID, agentID, memoryBlock string, hits []data.MemoryHitCache) error {
	if s.db == nil {
		return nil
	}
	return data.MemorySnapshotRepository{}.UpsertWithHits(ctx, s.db, accountID, userID, agentID, memoryBlock, hits)
}
