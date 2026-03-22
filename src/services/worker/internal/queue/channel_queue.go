package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type channelJob struct {
	id          uuid.UUID
	jobType     string
	payloadJSON map[string]any
	status      string
	availableAt time.Time
	leasedUntil time.Time
	leaseToken  uuid.UUID
	attempts    int
	createdAt   time.Time
}

// ChannelJobQueue is a pure in-memory JobQueue backed by a mutex-protected
// map. It is intended for development, testing, and single-process deployments
// where a PostgreSQL dependency is undesirable.
type ChannelJobQueue struct {
	mu          sync.Mutex
	jobs        map[uuid.UUID]*channelJob
	order       []uuid.UUID // insertion order for deterministic iteration
	maxAttempts int
	onEnqueue   func()
}

func NewChannelJobQueue(maxAttempts int, onEnqueue func()) (*ChannelJobQueue, error) {
	if maxAttempts <= 0 {
		return nil, fmt.Errorf("max_attempts must be positive")
	}
	return &ChannelJobQueue{
		jobs:        make(map[uuid.UUID]*channelJob),
		maxAttempts: maxAttempts,
		onEnqueue:   onEnqueue,
	}, nil
}

func (q *ChannelJobQueue) EnqueueRun(
	ctx context.Context,
	accountID uuid.UUID,
	runID uuid.UUID,
	traceID string,
	queueJobType string,
	payload map[string]any,
	availableAt *time.Time,
) (uuid.UUID, error) {
	jobID := uuid.New()

	chosenTraceID := normalizeTraceID(traceID)
	if chosenTraceID == "" {
		chosenTraceID = strings.ReplaceAll(uuid.New().String(), "-", "")
	}

	payloadCopy := map[string]any{}
	for key, value := range payload {
		payloadCopy[key] = value
	}

	payloadJSON := map[string]any{
		"v":          JobPayloadVersionV1,
		"job_id":     jobID.String(),
		"type":       RunExecuteJobType,
		"trace_id":   chosenTraceID,
		"account_id": accountID.String(),
		"run_id":     runID.String(),
		"payload":    payloadCopy,
	}

	// Round-trip through JSON so that numeric types match PgQueue behaviour
	// (e.g. integers become float64 after JSON unmarshal).
	encoded, err := json.Marshal(payloadJSON)
	if err != nil {
		return uuid.Nil, err
	}
	var roundTripped map[string]any
	if err := json.Unmarshal(encoded, &roundTripped); err != nil {
		return uuid.Nil, err
	}

	chosenJobType := strings.TrimSpace(queueJobType)
	if chosenJobType == "" {
		chosenJobType = RunExecuteJobType
	}

	now := time.Now()
	chosenAvailableAt := now
	if availableAt != nil {
		chosenAvailableAt = *availableAt
	}

	job := &channelJob{
		id:          jobID,
		jobType:     chosenJobType,
		payloadJSON: roundTripped,
		status:      JobStatusQueued,
		availableAt: chosenAvailableAt,
		attempts:    0,
		createdAt:   now,
	}

	q.mu.Lock()
	q.jobs[jobID] = job
	q.order = append(q.order, jobID)
	if len(q.jobs) > defaultPruneThreshold {
		q.pruneTerminalJobsLocked()
	}
	q.mu.Unlock()

	if q.onEnqueue != nil {
		q.onEnqueue()
	}

	return jobID, nil
}

func (q *ChannelJobQueue) Lease(ctx context.Context, leaseSeconds int, jobTypes []string) (*JobLease, error) {
	if leaseSeconds <= 0 {
		return nil, fmt.Errorf("lease_seconds must be positive")
	}

	chosenJobTypes := normalizeJobTypes(jobTypes)
	if len(chosenJobTypes) == 0 {
		return nil, nil
	}

	for i := 0; i < leaseAttemptsReapLimit; i++ {
		lease := q.tryLeaseOne(leaseSeconds, chosenJobTypes)
		if lease != nil {
			return lease, nil
		}

		marked := q.tryMarkDeadOne(chosenJobTypes)
		if !marked {
			return nil, nil
		}
	}

	return nil, nil
}

func (q *ChannelJobQueue) Heartbeat(ctx context.Context, lease JobLease, leaseSeconds int) error {
	if leaseSeconds <= 0 {
		return fmt.Errorf("lease_seconds must be positive")
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	job, ok := q.jobs[lease.JobID]
	if !ok || job.status != JobStatusLeased || job.leaseToken != lease.LeaseToken {
		return &LeaseLostError{JobID: lease.JobID}
	}

	job.leasedUntil = time.Now().Add(time.Duration(leaseSeconds) * time.Second)
	return nil
}

func (q *ChannelJobQueue) Ack(ctx context.Context, lease JobLease) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, ok := q.jobs[lease.JobID]
	if !ok || job.status != JobStatusLeased || job.leaseToken != lease.LeaseToken {
		return &LeaseLostError{JobID: lease.JobID}
	}

	job.status = JobStatusDone
	job.leasedUntil = time.Time{}
	job.leaseToken = uuid.Nil
	return nil
}

func (q *ChannelJobQueue) Nack(ctx context.Context, lease JobLease, delaySeconds *int) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, ok := q.jobs[lease.JobID]
	if !ok || job.status != JobStatusLeased || job.leaseToken != lease.LeaseToken {
		return &LeaseLostError{JobID: lease.JobID}
	}

	if lease.Attempts >= q.maxAttempts {
		job.status = JobStatusDead
		job.leasedUntil = time.Time{}
		job.leaseToken = uuid.Nil
		return nil
	}

	chosenDelay := DefaultRetryDelaySeconds(lease.Attempts)
	if delaySeconds != nil {
		chosenDelay = *delaySeconds
	}
	if chosenDelay < 0 {
		return fmt.Errorf("delay_seconds must not be negative")
	}

	job.status = JobStatusQueued
	job.leasedUntil = time.Time{}
	job.leaseToken = uuid.Nil
	job.availableAt = time.Now().Add(time.Duration(chosenDelay) * time.Second)
	return nil
}

// tryLeaseOne finds the first leasable job that matches the requested types,
// atomically transitions it to leased status, and returns the lease. Returns
// nil if no candidate is found.
func (q *ChannelJobQueue) tryLeaseOne(leaseSeconds int, jobTypes []string) *JobLease {
	q.mu.Lock()
	defer q.mu.Unlock()

	typeSet := make(map[string]struct{}, len(jobTypes))
	for _, jt := range jobTypes {
		typeSet[jt] = struct{}{}
	}

	now := time.Now()
	var best *channelJob
	for _, id := range q.order {
		job := q.jobs[id]
		if _, ok := typeSet[job.jobType]; !ok {
			continue
		}
		if job.attempts >= q.maxAttempts {
			continue
		}

		eligible := false
		if job.status == JobStatusQueued && !job.availableAt.After(now) {
			eligible = true
		}
		if job.status == JobStatusLeased && !job.leasedUntil.IsZero() && !job.leasedUntil.After(now) {
			eligible = true
		}
		if !eligible {
			continue
		}

		if best == nil || jobBefore(job, best) {
			best = job
		}
	}

	if best == nil {
		return nil
	}

	leaseToken := uuid.New()
	best.status = JobStatusLeased
	best.attempts++
	best.leaseToken = leaseToken
	best.leasedUntil = now.Add(time.Duration(leaseSeconds) * time.Second)

	return &JobLease{
		JobID:       best.id,
		JobType:     best.jobType,
		PayloadJSON: best.payloadJSON,
		Attempts:    best.attempts,
		LeasedUntil: best.leasedUntil,
		LeaseToken:  leaseToken,
	}
}

// tryMarkDeadOne finds one dead-letterable job (attempts >= maxAttempts) and
// transitions it to "dead". Returns true if a job was marked.
func (q *ChannelJobQueue) tryMarkDeadOne(jobTypes []string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	typeSet := make(map[string]struct{}, len(jobTypes))
	for _, jt := range jobTypes {
		typeSet[jt] = struct{}{}
	}

	now := time.Now()
	for _, id := range q.order {
		job := q.jobs[id]
		if _, ok := typeSet[job.jobType]; !ok {
			continue
		}
		if job.attempts < q.maxAttempts {
			continue
		}

		eligible := false
		if job.status == JobStatusQueued && !job.availableAt.After(now) {
			eligible = true
		}
		if job.status == JobStatusLeased && !job.leasedUntil.IsZero() && !job.leasedUntil.After(now) {
			eligible = true
		}
		if !eligible {
			continue
		}

		job.status = JobStatusDead
		job.leasedUntil = time.Time{}
		job.leaseToken = uuid.Nil
		return true
	}

	return false
}

func (q *ChannelJobQueue) QueueDepth(_ context.Context, jobTypes []string) (int, error) {
	typeSet := make(map[string]struct{}, len(jobTypes))
	for _, jt := range normalizeJobTypes(jobTypes) {
		typeSet[jt] = struct{}{}
	}

	now := time.Now()
	q.mu.Lock()
	defer q.mu.Unlock()

	count := 0
	for _, id := range q.order {
		job := q.jobs[id]
		if _, ok := typeSet[job.jobType]; !ok {
			continue
		}
		if job.status == JobStatusQueued && !job.availableAt.After(now) {
			count++
		}
	}
	return count, nil
}

// pruneTerminalJobsLocked removes all done/dead jobs from the order slice and
// jobs map. Must be called while q.mu is held.
func (q *ChannelJobQueue) pruneTerminalJobsLocked() {
	alive := make([]uuid.UUID, 0, len(q.order))
	for _, id := range q.order {
		job := q.jobs[id]
		if job.status == JobStatusDone || job.status == JobStatusDead {
			delete(q.jobs, id)
			continue
		}
		alive = append(alive, id)
	}
	q.order = alive
}

// jobBefore returns true when a should be leased before b (available_at ASC,
// created_at ASC, id ASC).
func jobBefore(a, b *channelJob) bool {
	if !a.availableAt.Equal(b.availableAt) {
		return a.availableAt.Before(b.availableAt)
	}
	if !a.createdAt.Equal(b.createdAt) {
		return a.createdAt.Before(b.createdAt)
	}
	return a.id.String() < b.id.String()
}
