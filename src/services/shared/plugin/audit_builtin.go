package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBExecutor DB 执行抽象，兼容 pgxpool.Pool 和 pgx.Tx。
type DBExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// DBSink OSS 默认审计实现，将审计事件写入 audit_logs 表。
type DBSink struct {
	db DBExecutor
}

// NewDBSink 创建 DBSink。db 必须非 nil。
func NewDBSink(db DBExecutor) (*DBSink, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	return &DBSink{db: db}, nil
}

func (s *DBSink) Name() string {
	return "db"
}

func (s *DBSink) Emit(ctx context.Context, event AuditEvent) error {
	detail, err := json.Marshal(event.Detail)
	if err != nil {
		detail = []byte("{}")
	}

	var orgID *uuid.UUID
	if event.OrgID != uuid.Nil {
		orgID = &event.OrgID
	}

	var actorID *uuid.UUID
	if event.ActorID != uuid.Nil {
		actorID = &event.ActorID
	}

	var ipAddr *string
	if event.IP != "" {
		ipAddr = &event.IP
	}

	var ua *string
	if event.UserAgent != "" {
		ua = &event.UserAgent
	}

	traceID := uuid.New().String()

	_, err = s.db.Exec(ctx,
		`INSERT INTO audit_logs (account_id, actor_user_id, action, target_type, target_id, trace_id, metadata_json, ip_address, user_agent, ts)
		 VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::inet, $9, $10)`,
		orgID,
		actorID,
		event.Action,
		nilStr(event.Resource),
		nilStr(event.ResourceID),
		traceID,
		string(detail),
		ipAddr,
		ua,
		event.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("audit db sink: %w", err)
	}
	return nil
}

func nilStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
