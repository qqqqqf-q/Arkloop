package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/api_go/internal/observability"

	"github.com/google/uuid"
)

const (
	RunExecuteJobType = "run.execute"

	JobStatusQueued = "queued"

	JobPayloadVersionV1 = 1
)

type JobRepository struct {
	db Querier
}

func NewJobRepository(db Querier) (*JobRepository, error) {
	if db == nil {
		return nil, errors.New("db 不能为空")
	}
	return &JobRepository{db: db}, nil
}

func (r *JobRepository) EnqueueRun(
	ctx context.Context,
	orgID uuid.UUID,
	runID uuid.UUID,
	traceID string,
	queueJobType string,
	payload map[string]any,
	availableAt *time.Time,
) (uuid.UUID, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("org_id 不能为空")
	}
	if runID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("run_id 不能为空")
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
	return jobID, nil
}
