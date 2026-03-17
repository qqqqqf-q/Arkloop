//go:build desktop

package consumer

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

type LocalRunLocker struct {
	mu     sync.Mutex
	locked map[uuid.UUID]struct{}
}

func NewLocalRunLocker() *LocalRunLocker {
	return &LocalRunLocker{locked: make(map[uuid.UUID]struct{})}
}

func (l *LocalRunLocker) TryAcquire(_ context.Context, runID uuid.UUID) (UnlockFunc, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, ok := l.locked[runID]; ok {
		return nil, false, nil
	}
	l.locked[runID] = struct{}{}

	var once sync.Once
	unlock := func(_ context.Context) error {
		once.Do(func() {
			l.mu.Lock()
			delete(l.locked, runID)
			l.mu.Unlock()
		})
		return nil
	}
	return unlock, true, nil
}
