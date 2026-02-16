package app

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestJSONLoggerWritesNullForMissingContextFields(t *testing.T) {
	var buffer bytes.Buffer
	logger := NewJSONLogger("worker_go", &buffer)
	logger.now = func() time.Time {
		return time.Date(2026, 2, 16, 9, 30, 0, 0, time.UTC)
	}

	logger.Info("hello", LogFields{}, map[string]any{"foo": "bar"})

	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buffer.Bytes()), &payload); err != nil {
		t.Fatalf("unmarshal log json failed: %v", err)
	}

	if payload["component"] != "worker_go" {
		t.Fatalf("unexpected component: %v", payload["component"])
	}
	if payload["trace_id"] != nil {
		t.Fatalf("trace_id should be null, got %v", payload["trace_id"])
	}
	if payload["org_id"] != nil {
		t.Fatalf("org_id should be null, got %v", payload["org_id"])
	}
	if payload["run_id"] != nil {
		t.Fatalf("run_id should be null, got %v", payload["run_id"])
	}
	if payload["job_id"] != nil {
		t.Fatalf("job_id should be null, got %v", payload["job_id"])
	}
}

func TestJSONLoggerWritesContextFields(t *testing.T) {
	var buffer bytes.Buffer
	logger := NewJSONLogger("worker_go", &buffer)

	traceID := "trace"
	orgID := "org"
	runID := "run"
	jobID := "job"

	logger.Info("with context", LogFields{
		TraceID: &traceID,
		OrgID:   &orgID,
		RunID:   &runID,
		JobID:   &jobID,
	}, nil)

	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buffer.Bytes()), &payload); err != nil {
		t.Fatalf("unmarshal log json failed: %v", err)
	}

	if payload["trace_id"] != traceID {
		t.Fatalf("unexpected trace_id: %v", payload["trace_id"])
	}
	if payload["org_id"] != orgID {
		t.Fatalf("unexpected org_id: %v", payload["org_id"])
	}
	if payload["run_id"] != runID {
		t.Fatalf("unexpected run_id: %v", payload["run_id"])
	}
	if payload["job_id"] != jobID {
		t.Fatalf("unexpected job_id: %v", payload["job_id"])
	}
}
