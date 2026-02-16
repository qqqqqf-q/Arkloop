package executor

import (
	"context"

	"arkloop/services/worker/internal/queue"
)

type NoopHandler struct{}

func (NoopHandler) Handle(ctx context.Context, lease queue.JobLease) error {
	_ = lease
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
