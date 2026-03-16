//go:build desktop

package data

import (
	"context"
	"time"

	"arkloop/services/shared/desktop"

	"github.com/google/uuid"
)

func init() {
	jobEnqueueNotify = func(ctx context.Context, accountID, runID uuid.UUID, traceID, jobType string, payload map[string]any, availableAt *time.Time) {
		if enq := desktop.GetJobEnqueuer(); enq != nil {
			_, _ = enq.EnqueueRun(ctx, accountID, runID, traceID, jobType, payload, availableAt)
		}
	}
}
