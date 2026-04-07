package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	RunExecuteJobType = "run.execute"
	EmailSendJobType  = "email.send"

	JobStatusQueued = "queued"
	JobStatusLeased = "leased"

	JobPayloadVersionV1 = 1
)

const activeRunExecuteJobIndex = "ux_jobs_run_execute_active_run"

var ErrRunExecuteAlreadyQueued = errors.New("run.execute already queued")

// afterCommitter is satisfied by sqlitepgx.Tx and allows deferring
// side-effects (like worker notification) until the transaction commits.
type afterCommitter interface {
	AfterCommit(fn func())
}

// jobEnqueueNotify 在 job INSERT 后调用，桌面合并模式下将作业转发到 Worker 内存队列。
// 由 jobs_repo_desktop.go 的 init 设置，非桌面构建保持 nil。
var jobEnqueueNotify func(ctx context.Context, accountID, runID uuid.UUID, traceID, jobType string, payload map[string]any, availableAt *time.Time)

// jobEnqueueDirect allows desktop mode to bypass the persistent jobs table for
// run.execute while still preserving after-commit enqueue semantics.
// It returns (jobID, handled, err).
var jobEnqueueDirect func(
	ctx context.Context,
	db Querier,
	accountID uuid.UUID,
	runID uuid.UUID,
	traceID string,
	jobType string,
	payload map[string]any,
	availableAt *time.Time,
) (uuid.UUID, bool, error)

type JobRepository struct {
	db Querier
}

func (r *JobRepository) WithTx(tx pgx.Tx) *JobRepository {
	return &JobRepository{db: tx}
}

func NewJobRepository(db Querier) (*JobRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &JobRepository{db: db}, nil
}

func (r *JobRepository) EnqueueRun(
	ctx context.Context,
	accountID uuid.UUID,
	runID uuid.UUID,
	traceID string,
	queueJobType string,
	payload map[string]any,
	availableAt *time.Time,
) (uuid.UUID, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("account_id must not be empty")
	}
	if runID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("run_id must not be empty")
	}

	jobID := uuid.New()

	chosenTraceID := observability.NormalizeTraceID(traceID)
	if chosenTraceID == "" {
		chosenTraceID = observability.NewTraceID()
	}
	chosenJobType := strings.TrimSpace(queueJobType)
	if chosenJobType == "" {
		chosenJobType = RunExecuteJobType
	}

	payloadCopy := map[string]any{}
	for key, value := range payload {
		payloadCopy[key] = value
	}

	payloadJSON := map[string]any{
		"v":          JobPayloadVersionV1,
		"job_id":     jobID.String(),
		"type":       chosenJobType,
		"trace_id":   chosenTraceID,
		"account_id": accountID.String(),
		"run_id":     runID.String(),
		"payload":    payloadCopy,
	}

	encoded, err := json.Marshal(payloadJSON)
	if err != nil {
		return uuid.Nil, err
	}

	if jobEnqueueDirect != nil {
		if directJobID, handled, err := jobEnqueueDirect(
			ctx,
			r.db,
			accountID,
			runID,
			chosenTraceID,
			chosenJobType,
			payloadCopy,
			availableAt,
		); handled {
			return directJobID, err
		}
	}

	if chosenJobType == RunExecuteJobType {
		existingJobID, err := findActiveRunExecuteJob(ctx, r.db, runID)
		if err != nil {
			return uuid.Nil, err
		}
		if existingJobID != uuid.Nil {
			return uuid.Nil, fmt.Errorf("%w: run_id=%s job_id=%s", ErrRunExecuteAlreadyQueued, runID, existingJobID)
		}
	}

	_, err = r.db.Exec(
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
		if chosenJobType == RunExecuteJobType && isActiveRunExecuteConflict(err) {
			existingJobID, lookupErr := findActiveRunExecuteJob(ctx, r.db, runID)
			if lookupErr == nil && existingJobID != uuid.Nil {
				return uuid.Nil, fmt.Errorf("%w: run_id=%s job_id=%s", ErrRunExecuteAlreadyQueued, runID, existingJobID)
			}
			// 即使找不到（对方 job 瞬间完成），unique 冲突本身已经明确，仍然返回语义错误
			return uuid.Nil, fmt.Errorf("%w: run_id=%s", ErrRunExecuteAlreadyQueued, runID)
		}
		return uuid.Nil, err
	}

	// pg_notify is transaction-safe in PostgreSQL (delivered after commit).
	// Silently ignored on SQLite where pg_notify does not exist.
	_, _ = r.db.Exec(ctx, `SELECT pg_notify('arkloop:jobs', '')`)

	if jobEnqueueNotify != nil {
		notify := func() {
			jobEnqueueNotify(ctx, accountID, runID, chosenTraceID, chosenJobType, payloadCopy, availableAt)
		}
		if ac, ok := r.db.(afterCommitter); ok {
			ac.AfterCommit(notify)
		} else {
			notify()
		}
	}

	return jobID, nil
}

func isActiveRunExecuteConflict(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" && pgErr.ConstraintName == activeRunExecuteJobIndex
	}
	msg := err.Error()
	return (strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "constraint failed: UNIQUE")) &&
		strings.Contains(msg, activeRunExecuteJobIndex)
}

func findActiveRunExecuteJob(ctx context.Context, db Querier, runID uuid.UUID) (uuid.UUID, error) {
	if db == nil || runID == uuid.Nil {
		return uuid.Nil, nil
	}
	var existingJobID uuid.UUID
	err := db.QueryRow(
		ctx,
		`SELECT id
		   FROM jobs
		  WHERE job_type = $1
		    AND payload_json->>'run_id' = $2
		    AND status IN ($3, $4)
		  ORDER BY created_at ASC, id ASC
		  LIMIT 1`,
		RunExecuteJobType,
		runID.String(),
		JobStatusQueued,
		JobStatusLeased,
	).Scan(&existingJobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	return existingJobID, nil
}

// EnqueueEmail 将一封邮件加入异步队列，由 Worker 的 email.send handler 发送。
func (r *JobRepository) EnqueueEmail(ctx context.Context, to, subject, html, text string) (uuid.UUID, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if to == "" {
		return uuid.Nil, fmt.Errorf("to must not be empty")
	}
	if subject == "" {
		return uuid.Nil, fmt.Errorf("subject must not be empty")
	}
	if html == "" && text == "" {
		return uuid.Nil, fmt.Errorf("html or text body is required")
	}

	jobID := uuid.New()
	payloadJSON := map[string]any{
		"v":        JobPayloadVersionV1,
		"job_id":   jobID.String(),
		"type":     EmailSendJobType,
		"trace_id": observability.NewTraceID(),
		"payload": map[string]any{
			"to":      to,
			"subject": subject,
			"html":    html,
			"text":    text,
		},
	}

	encoded, err := json.Marshal(payloadJSON)
	if err != nil {
		return uuid.Nil, err
	}

	_, err = r.db.Exec(
		ctx,
		`INSERT INTO jobs (
		   id, job_type, payload_json, status, available_at,
		   leased_until, lease_token, attempts, created_at, updated_at
		 ) VALUES (
		   $1, $2, $3::jsonb, $4, now(),
		   NULL, NULL, 0, now(), now()
		 )`,
		jobID,
		EmailSendJobType,
		string(encoded),
		JobStatusQueued,
	)
	if err != nil {
		return uuid.Nil, err
	}

	_, _ = r.db.Exec(ctx, `SELECT pg_notify('arkloop:jobs', '')`)

	return jobID, nil
}
