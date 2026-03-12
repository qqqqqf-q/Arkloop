package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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

type AuditLog struct {
	ID            uuid.UUID
	AccountID         *uuid.UUID
	ActorUserID   *uuid.UUID
	Action        string
	TargetType    *string
	TargetID      *string
	TraceID       string
	MetadataJSON  map[string]any
	IPAddress     *string
	UserAgent     *string
	CreatedAt     time.Time

	// 仅当 IncludeState=true 时填充
	BeforeStateJSON *string
	AfterStateJSON  *string
}

type AuditLogListParams struct {
	AccountID       *uuid.UUID
	Action      *string
	ActorUserID *uuid.UUID
	TargetType  *string
	Since       *time.Time
	Until       *time.Time
	Limit       int
	Offset      int
	IncludeState bool
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

// List 按过滤条件分页查询审计日志，同时返回满足条件的总条数。
func (r *AuditLogRepository) List(ctx context.Context, params AuditLogListParams) ([]AuditLog, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	args := []any{}
	conds := []string{}

	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if params.AccountID != nil {
		conds = append(conds, "account_id = "+addArg(*params.AccountID))
	}
	if params.Action != nil {
		conds = append(conds, "action = "+addArg(*params.Action))
	}
	if params.ActorUserID != nil {
		conds = append(conds, "actor_user_id = "+addArg(*params.ActorUserID))
	}
	if params.TargetType != nil {
		conds = append(conds, "target_type = "+addArg(*params.TargetType))
	}
	if params.Since != nil {
		conds = append(conds, "ts >= "+addArg(*params.Since))
	}
	if params.Until != nil {
		conds = append(conds, "ts <= "+addArg(*params.Until))
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	// 先查总数
	var total int64
	if err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit_logs: %w", err)
	}

	stateColumns := "NULL::text, NULL::text"
	if params.IncludeState {
		stateColumns = "before_state_json::text, after_state_json::text"
	}

	query := fmt.Sprintf(
		`SELECT id, account_id, actor_user_id, action, target_type, target_id,
		        trace_id, metadata_json::text, ip_address::text, user_agent, ts,
		        %s
		 FROM audit_logs%s
		 ORDER BY ts DESC, id DESC
		 LIMIT %s OFFSET %s`,
		stateColumns,
		where,
		addArg(limit),
		addArg(offset),
	)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit_logs: %w", err)
	}
	defer rows.Close()

	logs := []AuditLog{}
	for rows.Next() {
		var (
			l            AuditLog
			metaRaw      string
			beforeRaw    *string
			afterRaw     *string
		)
		if err := rows.Scan(
			&l.ID, &l.AccountID, &l.ActorUserID, &l.Action,
			&l.TargetType, &l.TargetID, &l.TraceID,
			&metaRaw, &l.IPAddress, &l.UserAgent, &l.CreatedAt,
			&beforeRaw, &afterRaw,
		); err != nil {
			return nil, 0, fmt.Errorf("scan audit_log: %w", err)
		}

		if err := json.Unmarshal([]byte(metaRaw), &l.MetadataJSON); err != nil {
			l.MetadataJSON = map[string]any{}
		}
		l.BeforeStateJSON = beforeRaw
		l.AfterStateJSON = afterRaw

		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows audit_logs: %w", err)
	}

	return logs, total, nil
}

