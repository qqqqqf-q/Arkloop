package activityrecorderfinish

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestExecuteRecordsFinishOutcome(t *testing.T) {
	db := &finishTestDB{rowsAffected: 1}
	exec := NewToolExecutor(db)
	accountID := uuid.New()
	userID := uuid.New()
	runID := uuid.New()

	result := exec.Execute(context.Background(), AgentSpec.Name, map[string]any{
		"status":              "no_durable_memory",
		"reason":              "checked sources contained only low-value app activity",
		"sources_checked":     []any{"ActivityWatch", "Screen Time"},
		"sources_unavailable": []any{"AIContext"},
		"memory_write_count":  float64(0),
	}, tools.ExecutionContext{
		RunID:        runID,
		AccountID:    &accountID,
		UserID:       &userID,
		PersonaID:    "activity-recorder-builder",
		ProfileRef:   "profile",
		WorkspaceRef: "workspace",
	}, "call_1")

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if db.execCount != 1 {
		t.Fatalf("expected one update, got %d", db.execCount)
	}
	if !strings.Contains(db.sql, "last_finish_status") {
		t.Fatalf("expected finish fields to be updated, sql=%s", db.sql)
	}
	if result.ResultJSON["status"] != "no_durable_memory" {
		t.Fatalf("unexpected status: %#v", result.ResultJSON["status"])
	}
}

func TestExecuteRejectsMissingReason(t *testing.T) {
	exec := NewToolExecutor(&finishTestDB{rowsAffected: 1})
	accountID := uuid.New()
	userID := uuid.New()

	result := exec.Execute(context.Background(), AgentSpec.Name, map[string]any{
		"status":              "no_durable_memory",
		"sources_checked":     []any{},
		"sources_unavailable": []any{},
		"memory_write_count":  float64(0),
	}, tools.ExecutionContext{
		RunID:        uuid.New(),
		AccountID:    &accountID,
		UserID:       &userID,
		PersonaID:    "activity-recorder-builder",
		ProfileRef:   "profile",
		WorkspaceRef: "workspace",
	}, "call_1")

	if result.Error == nil || result.Error.ErrorClass != "tool.args_invalid" {
		t.Fatalf("expected args_invalid, got %#v", result.Error)
	}
}

type finishTestDB struct {
	execCount    int
	sql          string
	rowsAffected int64
}

func (db *finishTestDB) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	db.execCount++
	db.sql = sql
	return pgconn.NewCommandTag("UPDATE " + string(rune('0'+db.rowsAffected))), nil
}

func (db *finishTestDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("not used")
}

func (db *finishTestDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("not used")
}

func (db *finishTestDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	panic("not used")
}
