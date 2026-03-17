//go:build desktop

package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/desktop"

	"github.com/google/uuid"
)

func init() {
	jobEnqueueDirect = func(
		ctx context.Context,
		db Querier,
		accountID uuid.UUID,
		runID uuid.UUID,
		traceID string,
		jobType string,
		payload map[string]any,
		availableAt *time.Time,
	) (uuid.UUID, bool, error) {
		if strings.TrimSpace(jobType) != RunExecuteJobType {
			return uuid.Nil, false, nil
		}
		enq := desktop.GetJobEnqueuer()
		if enq == nil {
			return uuid.Nil, true, fmt.Errorf("desktop job queue not initialized")
		}
		if ac, ok := db.(afterCommitter); ok {
			ac.AfterCommit(func() {
				_, _ = enq.EnqueueRun(ctx, accountID, runID, traceID, jobType, payload, availableAt)
			})
			return uuid.New(), true, nil
		}
		jobID, err := enq.EnqueueRun(ctx, accountID, runID, traceID, jobType, payload, availableAt)
		return jobID, true, err
	}

	jobEnqueueNotify = func(ctx context.Context, accountID, runID uuid.UUID, traceID, jobType string, payload map[string]any, availableAt *time.Time) {
		if enq := desktop.GetJobEnqueuer(); enq != nil {
			_, _ = enq.EnqueueRun(ctx, accountID, runID, traceID, jobType, payload, availableAt)
		}
	}
}
