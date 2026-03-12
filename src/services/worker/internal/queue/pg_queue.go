//go:build !desktop

package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgQueue struct {
	pool         *pgxpool.Pool
	maxAttempts  int
	capabilities []string
}

func NewPgQueue(pool *pgxpool.Pool, maxAttempts int, capabilities []string) (*PgQueue, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if maxAttempts <= 0 {
		return nil, fmt.Errorf("max_attempts must be positive")
	}
	caps := capabilities
	if caps == nil {
		caps = []string{}
	}
	return &PgQueue{pool: pool, maxAttempts: maxAttempts, capabilities: caps}, nil
}

func (q *PgQueue) EnqueueRun(
	ctx context.Context,
	orgID uuid.UUID,
	runID uuid.UUID,
	traceID string,
	queueJobType string,
	payload map[string]any,
	availableAt *time.Time,
) (uuid.UUID, error) {
	jobID := uuid.New()
	chosenTraceID := normalizeTraceID(traceID)
	if chosenTraceID == "" {
		chosenTraceID = uuid.New().String()
		chosenTraceID = strings.ReplaceAll(chosenTraceID, "-", "")
	}

	payloadCopy := map[string]any{}
	for key, value := range payload {
		payloadCopy[key] = value
	}

	payloadJSON := map[string]any{
		"v":        JobPayloadVersionV1,
		"job_id":   jobID.String(),
		"type":     RunExecuteJobType,
		"trace_id": chosenTraceID,
		"org_id":   orgID.String(),
		"run_id":   runID.String(),
		"payload":  payloadCopy,
	}

	encoded, err := json.Marshal(payloadJSON)
	if err != nil {
		return uuid.Nil, err
	}

	chosenJobType := strings.TrimSpace(queueJobType)
	if chosenJobType == "" {
		chosenJobType = RunExecuteJobType
	}

	_, err = q.pool.Exec(
		ctx,
		`INSERT INTO jobs (
			id, job_type, payload_json, status, available_at,
			leased_until, lease_token, attempts, created_at, updated_at
		) VALUES (
			$1, $2, $3::jsonb, $4, COALESCE($5, now()),
			NULL, NULL, 0, now(), now()
		)`,
		jobID,
		chosenJobType,
		string(encoded),
		JobStatusQueued,
		availableAt,
	)
	if err != nil {
		return uuid.Nil, err
	}

	_, _ = q.pool.Exec(ctx, `SELECT pg_notify('arkloop:jobs', '')`)

	return jobID, nil
}

func (q *PgQueue) Lease(ctx context.Context, leaseSeconds int, jobTypes []string) (*JobLease, error) {
	if leaseSeconds <= 0 {
		return nil, fmt.Errorf("lease_seconds must be positive")
	}

	chosenJobTypes := normalizeJobTypes(jobTypes)
	if len(chosenJobTypes) == 0 {
		return nil, nil
	}

	for i := 0; i < leaseAttemptsReapLimit; i++ {
		lease, err := q.tryLeaseOne(ctx, leaseSeconds, chosenJobTypes)
		if err != nil {
			return nil, err
		}
		if lease != nil {
			return lease, nil
		}

		marked, err := q.tryMarkDeadOne(ctx, chosenJobTypes)
		if err != nil {
			return nil, err
		}
		if !marked {
			return nil, nil
		}
	}

	return nil, nil
}

func (q *PgQueue) Heartbeat(ctx context.Context, lease JobLease, leaseSeconds int) error {
	if leaseSeconds <= 0 {
		return fmt.Errorf("lease_seconds must be positive")
	}

	result, err := q.pool.Exec(
		ctx,
		`UPDATE jobs
		 SET leased_until = now() + ($1 * interval '1 second'),
		     updated_at = now()
		 WHERE id = $2
		   AND status = $3
		   AND lease_token = $4`,
		leaseSeconds,
		lease.JobID,
		JobStatusLeased,
		lease.LeaseToken,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return &LeaseLostError{JobID: lease.JobID}
	}
	return nil
}

func (q *PgQueue) Ack(ctx context.Context, lease JobLease) error {
	result, err := q.pool.Exec(
		ctx,
		`UPDATE jobs
		 SET status = $1,
		     leased_until = NULL,
		     lease_token = NULL,
		     updated_at = now()
		 WHERE id = $2
		   AND status = $3
		   AND lease_token = $4`,
		JobStatusDone,
		lease.JobID,
		JobStatusLeased,
		lease.LeaseToken,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return &LeaseLostError{JobID: lease.JobID}
	}
	return nil
}

func (q *PgQueue) Nack(ctx context.Context, lease JobLease, delaySeconds *int) error {
	if lease.Attempts >= q.maxAttempts {
		return q.deadLetter(ctx, lease)
	}

	chosenDelay := DefaultRetryDelaySeconds(lease.Attempts)
	if delaySeconds != nil {
		chosenDelay = *delaySeconds
	}
	if chosenDelay < 0 {
		return fmt.Errorf("delay_seconds must not be negative")
	}

	result, err := q.pool.Exec(
		ctx,
		`UPDATE jobs
		 SET status = $1,
		     leased_until = NULL,
		     lease_token = NULL,
		     available_at = now() + ($2 * interval '1 second'),
		     updated_at = now()
		 WHERE id = $3
		   AND status = $4
		   AND lease_token = $5`,
		JobStatusQueued,
		chosenDelay,
		lease.JobID,
		JobStatusLeased,
		lease.LeaseToken,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return &LeaseLostError{JobID: lease.JobID}
	}
	return nil
}

func (q *PgQueue) tryLeaseOne(ctx context.Context, leaseSeconds int, jobTypes []string) (*JobLease, error) {
	tx, err := q.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var (
		jobID       uuid.UUID
		jobType     string
		payloadRaw  []byte
		currentTrys int
	)

	err = tx.QueryRow(
		ctx,
		`SELECT id, job_type, payload_json, attempts
		 FROM jobs
		 WHERE (
		   (status = $1 AND available_at <= now()) OR
		   (status = $2 AND leased_until IS NOT NULL AND leased_until <= now())
		 )
		 AND attempts < $3
		 AND job_type = ANY($4)
		 AND worker_tags <@ $5
		 ORDER BY available_at ASC, created_at ASC, id ASC
		 FOR UPDATE SKIP LOCKED
		 LIMIT 1`,
		JobStatusQueued,
		JobStatusLeased,
		q.maxAttempts,
		jobTypes,
		q.capabilities,
	).Scan(&jobID, &jobType, &payloadRaw, &currentTrys)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	leaseToken := uuid.New()
	var (
		attempts    int
		leasedUntil time.Time
	)
	err = tx.QueryRow(
		ctx,
		`UPDATE jobs
		 SET status = $1,
		     leased_until = now() + ($2 * interval '1 second'),
		     lease_token = $3,
		     attempts = attempts + 1,
		     updated_at = now()
		 WHERE id = $4
		 RETURNING attempts, leased_until`,
		JobStatusLeased,
		leaseSeconds,
		leaseToken,
		jobID,
	).Scan(&attempts, &leasedUntil)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	payloadJSON := map[string]any{}
	if len(payloadRaw) > 0 {
		if err := json.Unmarshal(payloadRaw, &payloadJSON); err != nil {
			return nil, err
		}
	}

	return &JobLease{
		JobID:       jobID,
		JobType:     jobType,
		PayloadJSON: payloadJSON,
		Attempts:    attempts,
		LeasedUntil: leasedUntil,
		LeaseToken:  leaseToken,
	}, nil
}

func (q *PgQueue) tryMarkDeadOne(ctx context.Context, jobTypes []string) (bool, error) {
	tx, err := q.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var jobID uuid.UUID
	err = tx.QueryRow(
		ctx,
		`SELECT id
		 FROM jobs
		 WHERE (
		   (status = $1 AND available_at <= now()) OR
		   (status = $2 AND leased_until IS NOT NULL AND leased_until <= now())
		 )
		 AND attempts >= $3
		 AND job_type = ANY($4)
		 AND worker_tags <@ $5
		 ORDER BY available_at ASC, created_at ASC, id ASC
		 FOR UPDATE SKIP LOCKED
		 LIMIT 1`,
		JobStatusQueued,
		JobStatusLeased,
		q.maxAttempts,
		jobTypes,
		q.capabilities,
	).Scan(&jobID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE jobs
		 SET status = $1,
		     leased_until = NULL,
		     lease_token = NULL,
		     updated_at = now()
		 WHERE id = $2`,
		JobStatusDead,
		jobID,
	); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (q *PgQueue) deadLetter(ctx context.Context, lease JobLease) error {
	result, err := q.pool.Exec(
		ctx,
		`UPDATE jobs
		 SET status = $1,
		     leased_until = NULL,
		     lease_token = NULL,
		     updated_at = now()
		 WHERE id = $2
		   AND status = $3
		   AND lease_token = $4`,
		JobStatusDead,
		lease.JobID,
		JobStatusLeased,
		lease.LeaseToken,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return &LeaseLostError{JobID: lease.JobID}
	}
	return nil
}
