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
)

const (
	RunExecuteJobType  = "run.execute"
	EmailSendJobType   = "email.send"

	JobStatusQueued = "queued"

	JobPayloadVersionV1 = 1
)

// afterCommitter is satisfied by sqlitepgx.Tx and allows deferring
// side-effects (like worker notification) until the transaction commits.
type afterCommitter interface {
	AfterCommit(fn func())
}

// jobEnqueueNotify 在 job INSERT 后调用，桌面合并模式下将作业转发到 Worker 内存队列。
// 由 jobs_repo_desktop.go 的 init 设置，非桌面构建保持 nil。
var jobEnqueueNotify func(ctx context.Context, accountID, runID uuid.UUID, traceID, jobType string, payload map[string]any, availableAt *time.Time)

type JobRepository struct {
	db Querier
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

	payloadCopy := map[string]any{}
	for key, value := range payload {
		payloadCopy[key] = value
	}

	payloadJSON := map[string]any{
		"v":        JobPayloadVersionV1,
		"job_id":   jobID.String(),
		"type":     RunExecuteJobType,
		"trace_id": chosenTraceID,
		"account_id":   accountID.String(),
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