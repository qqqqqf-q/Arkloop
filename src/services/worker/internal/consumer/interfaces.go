package consumer

import (
	"context"

	"github.com/google/uuid"
)

// UnlockFunc releases an advisory lock.
type UnlockFunc func(ctx context.Context) error

// RunLocker provides advisory locking per run ID.
type RunLocker interface {
	TryAcquire(ctx context.Context, runID uuid.UUID) (UnlockFunc, bool, error)
}

// WorkNotifier signals idle consumer goroutines when new work might be available.
type WorkNotifier interface {
	Wake() <-chan struct{}
}
