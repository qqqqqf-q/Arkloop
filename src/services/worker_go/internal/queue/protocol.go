package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	RunExecuteJobType = "run.execute"
	RunExecuteQueueJobTypeGoBridge = "run.execute.go_bridge"

	JobStatusQueued = "queued"
	JobStatusLeased = "leased"
	JobStatusDone   = "done"
	JobStatusDead   = "dead"

	JobPayloadVersionV1 = 1
)

type JobLease struct {
	JobID       uuid.UUID
	JobType     string
	PayloadJSON map[string]any
	Attempts    int
	LeasedUntil time.Time
	LeaseToken  uuid.UUID
}

type LeaseLostError struct {
	JobID uuid.UUID
}

func (e *LeaseLostError) Error() string {
	return fmt.Sprintf("job lease 已丢失: %s", e.JobID.String())
}

type JobQueue interface {
	EnqueueRun(
		ctx context.Context,
		orgID uuid.UUID,
		runID uuid.UUID,
		traceID string,
		queueJobType string,
		payload map[string]any,
		availableAt *time.Time,
	) (uuid.UUID, error)

	Lease(ctx context.Context, leaseSeconds int, jobTypes []string) (*JobLease, error)
	Heartbeat(ctx context.Context, lease JobLease, leaseSeconds int) error
	Ack(ctx context.Context, lease JobLease) error
	Nack(ctx context.Context, lease JobLease, delaySeconds *int) error
}
