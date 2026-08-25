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
	AccountID       *uuid.UUID
	ActorUserID *uuid.UUID
	Action      string
	TargetType  *string
	TargetID    *string
	TraceID     string
	Metadata    any

	// 请求来源信息，全部 nullable
	IPAddress      *string
	UserAgent      *string
	APIKeyID       *uuid.UUID
	BeforeStateJSON any
	AfterStateJSON  any
}

type AuditLogRepository struct {
	db Querier
}

func NewAuditLogRepository(db Querier) (*AuditLogRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &AuditLogRepository{db: db}, nil
}

func (r *AuditLogRepository) Create(ctx context.Context, params AuditLogCreateParams) error {
	if ctx == nil {
		ctx = context.Background()
	}

	action := strings.TrimSpace(params.Action)
	if action == "" {
		return fmt.Errorf("action must not be empty")
	}
	traceID := strings.TrimSpace(params.TraceID)
	if traceID == "" {
		return fmt.Errorf("trace_id must not be empty")
	}

	metadata := params.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	rawJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	var beforeJSON, afterJSON *string
	if params.BeforeStateJSON != nil {
		b, err := json.Marshal(params.BeforeStateJSON)
		if err != nil {
			return fmt.Errorf("marshal before_state_json: %w", err)
		}
		s := string(b)
		beforeJSON = &s
	}
	if params.AfterStateJSON != nil {
		b, err := json.Marshal(params.AfterStateJSON)
		if err != nil {
			return fmt.Errorf("marshal after_state_json: %w", err)
		}
		s := string(b)
		afterJSON = &s
	}

	_, err = r.db.Exec(
		ctx,
		`INSERT INTO audit_logs (
		   account_id,
		   actor_user_id,
		   action,
		   target_type,
		   target_id,
		   trace_id,
		   metadata_json,
		   ip_address,
		   user_agent,
		   api_key_id,
		   before_state_json,
		   after_state_json
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::inet, $9, $10, $11::jsonb, $12::jsonb)`,
		params.AccountID,
		params.ActorUserID,
		action,
		params.TargetType,
		params.TargetID,
		traceID,
		string(rawJSON),
		params.IPAddress,
		params.UserAgent,
		params.APIKeyID,
		beforeJSON,
		afterJSON,
	)
	return err
}

