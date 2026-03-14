package plugin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type mockDBExecutor struct {
	execFn func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func (m *mockDBExecutor) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return m.execFn(ctx, sql, arguments...)
}

func TestNewDBSink_NilDB(t *testing.T) {
	_, err := NewDBSink(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestDBSink_Name(t *testing.T) {
	sink, err := NewDBSink(&mockDBExecutor{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sink.Name() != "db" {
		t.Fatalf("expected name 'db', got %q", sink.Name())
	}
}

func TestDBSink_Emit_Success(t *testing.T) {
	accountID := uuid.New()
	actorID := uuid.New()
	ts := time.Now().UTC()

	var captured []any
	sink, _ := NewDBSink(&mockDBExecutor{
		execFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
			captured = args
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	})

	err := sink.Emit(context.Background(), AuditEvent{
		Timestamp:  ts,
		ActorID:    actorID,
		AccountID:  accountID,
		Action:     "user.login",
		Resource:   "session",
		ResourceID: "sess-123",
		Detail:     map[string]any{"method": "password"},
		IP:         "10.0.0.1",
		UserAgent:  "curl/8.0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(captured) != 10 {
		t.Fatalf("expected 10 arguments, got %d", len(captured))
	}

	// accountID
	if v, ok := captured[0].(*uuid.UUID); !ok || *v != accountID {
		t.Errorf("arg[0] accountID mismatch")
	}
	// actorID
	if v, ok := captured[1].(*uuid.UUID); !ok || *v != actorID {
		t.Errorf("arg[1] actorID mismatch")
	}
	// action
	if captured[2] != "user.login" {
		t.Errorf("arg[2] action = %v", captured[2])
	}
	// target_type
	if v, ok := captured[3].(*string); !ok || *v != "session" {
		t.Errorf("arg[3] target_type mismatch")
	}
	// target_id
	if v, ok := captured[4].(*string); !ok || *v != "sess-123" {
		t.Errorf("arg[4] target_id mismatch")
	}
	// trace_id 非空
	if v, ok := captured[5].(string); !ok || v == "" {
		t.Errorf("arg[5] trace_id should be non-empty string")
	}
	// metadata_json 包含 method
	if v, ok := captured[6].(string); !ok || v == "" {
		t.Errorf("arg[6] metadata_json should be non-empty")
	}
	// ip_address
	if v, ok := captured[7].(*string); !ok || *v != "10.0.0.1" {
		t.Errorf("arg[7] ip_address mismatch")
	}
	// user_agent
	if v, ok := captured[8].(*string); !ok || *v != "curl/8.0" {
		t.Errorf("arg[8] user_agent mismatch")
	}
	// timestamp
	if v, ok := captured[9].(time.Time); !ok || !v.Equal(ts) {
		t.Errorf("arg[9] timestamp mismatch")
	}
}

func TestDBSink_Emit_NilUUIDs(t *testing.T) {
	var captured []any
	sink, _ := NewDBSink(&mockDBExecutor{
		execFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
			captured = args
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	})

	err := sink.Emit(context.Background(), AuditEvent{
		Timestamp: time.Now(),
		Action:    "system.ping",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// AccountID = uuid.Nil -> typed nil *uuid.UUID
	if v, ok := captured[0].(*uuid.UUID); !ok || v != nil {
		t.Errorf("arg[0] accountID should be nil for uuid.Nil")
	}
	// ActorID = uuid.Nil -> typed nil *uuid.UUID
	if v, ok := captured[1].(*uuid.UUID); !ok || v != nil {
		t.Errorf("arg[1] actorID should be nil for uuid.Nil")
	}
}

func TestDBSink_Emit_EmptyStrings(t *testing.T) {
	var captured []any
	sink, _ := NewDBSink(&mockDBExecutor{
		execFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
			captured = args
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	})

	err := sink.Emit(context.Background(), AuditEvent{
		Timestamp: time.Now(),
		Action:    "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Resource -> typed nil *string
	if v, ok := captured[3].(*string); !ok || v != nil {
		t.Errorf("arg[3] target_type should be nil for empty Resource")
	}
	// ResourceID -> typed nil *string
	if v, ok := captured[4].(*string); !ok || v != nil {
		t.Errorf("arg[4] target_id should be nil for empty ResourceID")
	}
	// IP -> typed nil *string
	if v, ok := captured[7].(*string); !ok || v != nil {
		t.Errorf("arg[7] ip_address should be nil for empty IP")
	}
	// UserAgent -> typed nil *string
	if v, ok := captured[8].(*string); !ok || v != nil {
		t.Errorf("arg[8] user_agent should be nil for empty UserAgent")
	}
}

func TestDBSink_Emit_DBError(t *testing.T) {
	dbErr := errors.New("connection refused")
	sink, _ := NewDBSink(&mockDBExecutor{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), dbErr
		},
	})

	err := sink.Emit(context.Background(), AuditEvent{
		Timestamp: time.Now(),
		Action:    "test",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("expected wrapped db error, got: %v", err)
	}
}

func TestDBSink_Emit_DetailMarshalNil(t *testing.T) {
	var captured []any
	sink, _ := NewDBSink(&mockDBExecutor{
		execFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
			captured = args
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	})

	err := sink.Emit(context.Background(), AuditEvent{
		Timestamp: time.Now(),
		Action:    "test",
		Detail:    nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v, ok := captured[6].(string); !ok || v != "null" {
		t.Errorf("arg[6] expected 'null' for nil Detail, got %q", captured[6])
	}
}

func TestDBSink_ImplementsInterface(t *testing.T) {
	var _ AuditSink = (*DBSink)(nil)
}
