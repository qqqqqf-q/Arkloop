//go:build desktop

package data

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"arkloop/services/shared/desktop"

	"github.com/google/uuid"
)

type afterRollbacker interface {
	AfterRollback(fn func())
}

var pendingRunExecuteMu sync.Mutex
var pendingRunExecute = map[uuid.UUID]struct{}{}

func reservePendingRunExecute(runID uuid.UUID) bool {
	if runID == uuid.Nil {
		return false
	}
	pendingRunExecuteMu.Lock()
	defer pendingRunExecuteMu.Unlock()
	if _, exists := pendingRunExecute[runID]; exists {
		return false
	}
	pendingRunExecute[runID] = struct{}{}
	return true
}

func releasePendingRunExecute(runID uuid.UUID) {
	if runID == uuid.Nil {
		return
	}
	pendingRunExecuteMu.Lock()
	delete(pendingRunExecute, runID)
	pendingRunExecuteMu.Unlock()
}

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
		if !reservePendingRunExecute(runID) {
			return uuid.Nil, true, fmt.Errorf("%w: run_id=%s", ErrRunExecuteAlreadyQueued, runID)
		}
		releaseReservation := func() {
			releasePendingRunExecute(runID)
		}
		active, err := enq.HasActiveRun(ctx, runID, jobType)
		if err != nil {
			releaseReservation()
			return uuid.Nil, true, err
		}
		if active {
			releaseReservation()
			return uuid.Nil, true, fmt.Errorf("%w: run_id=%s", ErrRunExecuteAlreadyQueued, runID)
		}
		if ac, ok := db.(afterCommitter); ok {
			if ar, ok := db.(afterRollbacker); ok {
				ar.AfterRollback(releaseReservation)
			} else {
				releaseReservation()
				return uuid.Nil, true, fmt.Errorf("desktop transaction missing rollback hook")
			}
			jobID := uuid.New()
			ac.AfterCommit(func() {
				defer releaseReservation()
				if _, err := enq.EnqueueRunWithID(ctx, jobID, accountID, runID, traceID, jobType, payload, availableAt); err != nil {
					slog.Error("desktop_job_enqueue_after_commit",
						"run_id", runID.String(),
						"error", err.Error(),
					)
				}
			})
			return jobID, true, nil
		}
		defer releaseReservation()
		jobID, err := enq.EnqueueRun(ctx, accountID, runID, traceID, jobType, payload, availableAt)
		return jobID, true, err
	}

	jobEnqueueNotify = func(ctx context.Context, accountID, runID uuid.UUID, traceID, jobType string, payload map[string]any, availableAt *time.Time) {
		if enq := desktop.GetJobEnqueuer(); enq != nil {
			if _, err := enq.EnqueueRun(ctx, accountID, runID, traceID, jobType, payload, availableAt); err != nil {
				slog.Error("desktop_job_enqueue_notify",
					"run_id", runID.String(),
					"error", err.Error(),
				)
			}
		}
	}
}
