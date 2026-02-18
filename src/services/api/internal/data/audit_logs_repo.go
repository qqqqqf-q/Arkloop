package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type AuditLogCreateParams struct {
	OrgID       *uuid.UUID
	ActorUserID *uuid.UUID
	Action      string
	TargetType  *string
	TargetID    *string
	TraceID     string
	Metadata    any
}

type AuditLogRepository struct {
	db Querier
}

func NewAuditLogRepository(db Querier) (*AuditLogRepository, error) {
	if db == nil {
		return nil, errors.New("db 不能为空")
	}
	return &AuditLogRepository{db: db}, nil
}

func (r *AuditLogRepository) Create(ctx context.Context, params AuditLogCreateParams) error {
	if ctx == nil {
		ctx = context.Background()
	}

	action := strings.TrimSpace(params.Action)
	if action == "" {
		return fmt.Errorf("action 不能为空")
	}
	traceID := strings.TrimSpace(params.TraceID)
	if traceID == "" {
		return fmt.Errorf("trace_id 不能为空")
	}

	metadata := params.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	rawJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	_, err = r.db.Exec(
		ctx,
		`INSERT INTO audit_logs (
		   org_id,
		   actor_user_id,
		   action,
		   target_type,
		   target_id,
		   trace_id,
		   metadata_json
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`,
		params.OrgID,
		params.ActorUserID,
		action,
		params.TargetType,
		params.TargetID,
		traceID,
		string(rawJSON),
	)
	return err
}
