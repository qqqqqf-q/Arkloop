//go:build !desktop

package http

import (
	"context"
	"encoding/json"
	"testing"

	"arkloop/services/api/internal/data"
)

type storedAuditLog struct {
	Action      string
	TargetType  *string
	TargetID    *string
	Metadata    map[string]any
	BeforeState map[string]any
	AfterState  map[string]any
}

func latestAuditLogByAction(t *testing.T, ctx context.Context, db data.Querier, action string) storedAuditLog {
	t.Helper()
	if ctx == nil {
		ctx = context.Background()
	}

	var (
		targetType *string
		targetID   *string
		metadata   string
		beforeRaw  *string
		afterRaw   *string
	)
	if err := db.QueryRow(ctx, `
		SELECT target_type, target_id, metadata_json::text, before_state_json::text, after_state_json::text
		FROM audit_logs
		WHERE action = $1
		ORDER BY ts DESC, id DESC
		LIMIT 1`, action,
	).Scan(&targetType, &targetID, &metadata, &beforeRaw, &afterRaw); err != nil {
		t.Fatalf("query audit log %s: %v", action, err)
	}

	return storedAuditLog{
		Action:      action,
		TargetType:  targetType,
		TargetID:    targetID,
		Metadata:    decodeAuditObject(t, &metadata),
		BeforeState: decodeAuditObject(t, beforeRaw),
		AfterState:  decodeAuditObject(t, afterRaw),
	}
}

func countAuditLogByAction(t *testing.T, ctx context.Context, db data.Querier, action string) int {
	t.Helper()
	if ctx == nil {
		ctx = context.Background()
	}

	var count int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action = $1`, action).Scan(&count); err != nil {
		t.Fatalf("count audit log %s: %v", action, err)
	}
	return count
}

func decodeAuditObject(t *testing.T, raw *string) map[string]any {
	t.Helper()
	if raw == nil || *raw == "" || *raw == "null" {
		return nil
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(*raw), &out); err != nil {
		t.Fatalf("decode audit json: %v raw=%s", err, *raw)
	}
	return out
}
