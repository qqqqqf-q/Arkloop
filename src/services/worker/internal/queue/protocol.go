package queue

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	RunExecuteJobType     = "run.execute"
	WebhookDeliverJobType = "webhook.deliver"
	EmailSendJobType      = "email.send"

	JobStatusQueued = "queued"
	JobStatusLeased = "leased"
	JobStatusDone   = "done"
	JobStatusDead   = "dead"

	JobPayloadVersionV1 = 1

	leaseAttemptsReapLimit = 10

	// defaultPruneThreshold is the number of total jobs above which
	// terminal (done/dead) jobs are pruned from the in-memory queue
	// during EnqueueRun. Keeps memory bounded for long-running Desktop
	// processes.
	defaultPruneThreshold = 1000
)

var traceIDRegex = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

func normalizeTraceID(value string) string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return ""
	}
	if !traceIDRegex.MatchString(cleaned) {
		return ""
	}
	return strings.ToLower(cleaned)
}

func normalizeJobTypes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	deduped := make([]string, 0, len(values))
	for _, item := range values {
		cleaned := strings.TrimSpace(item)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		deduped = append(deduped, cleaned)
	}
	return deduped
}



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
	return fmt.Sprintf("job lease lost: %s", e.JobID.String())
}

type JobQueue interface {
	EnqueueRun(
		ctx context.Context,
		accountID uuid.UUID,
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
