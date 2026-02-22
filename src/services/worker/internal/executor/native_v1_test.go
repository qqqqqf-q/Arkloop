package executor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNativeRunEngineV1HandlerWritesEventsAndMessage(t *testing.T) {
	t.Setenv("ARKLOOP_STUB_AGENT_DELTA_COUNT", "2")
	t.Setenv("ARKLOOP_STUB_AGENT_DELTA_INTERVAL_SECONDS", "0")

	db := testutil.SetupPostgresDatabase(t, "arkloop_wg07")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	jobID := uuid.New()
	traceID := "0123456789abcdef0123456789abcdef"

	if err := seedRunStarted(t, pool, orgID, threadID, runID, map[string]any{"route_id": "default"}); err != nil {
		t.Fatalf("seed run failed: %v", err)
	}

	handler, err := NewNativeRunEngineV1Handler(pool, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewNativeRunEngineV1Handler failed: %v", err)
	}

	lease := queue.JobLease{
		JobID:       jobID,
		JobType:     queue.RunExecuteJobType,
		PayloadJSON: workerPayloadJSON(jobID, orgID, runID, traceID),
		Attempts:    1,
		LeasedUntil: time.Now().Add(time.Minute),
		LeaseToken:  uuid.New(),
	}

	if err := handler.Handle(context.Background(), lease); err != nil {
		t.Fatalf("handler.Handle failed: %v", err)
	}

	seqTypes := readSeqTypes(t, pool, runID)
	expected := []seqType{
		{Seq: 1, Type: "run.started"},
		{Seq: 2, Type: "worker.job.received"},
		{Seq: 3, Type: "run.route.selected"},
		{Seq: 4, Type: "message.delta"},
		{Seq: 5, Type: "message.delta"},
		{Seq: 6, Type: "run.completed"},
	}
	if len(seqTypes) != len(expected) {
		t.Fatalf("unexpected events count: %d", len(seqTypes))
	}
	for i := range expected {
		if seqTypes[i] != expected[i] {
			t.Fatalf("unexpected event[%d]: %+v", i, seqTypes[i])
		}
	}

	routeData := readEventData(t, pool, runID, "run.route.selected")
	if routeData["route_id"] != "default" {
		t.Fatalf("unexpected route_id: %v", routeData["route_id"])
	}
	if routeData["model"] != "stub" {
		t.Fatalf("unexpected model: %v", routeData["model"])
	}
	if routeData["provider_kind"] != "stub" {
		t.Fatalf("unexpected provider_kind: %v", routeData["provider_kind"])
	}

	content := readAssistantMessage(t, pool, threadID)
	if content != "stub delta 1stub delta 2" {
		t.Fatalf("unexpected assistant message: %q", content)
	}
}

func TestNativeRunEngineV1HandlerCancelsWhenRequested(t *testing.T) {
	t.Setenv("ARKLOOP_STUB_AGENT_DELTA_COUNT", "2")
	t.Setenv("ARKLOOP_STUB_AGENT_DELTA_INTERVAL_SECONDS", "0")

	db := testutil.SetupPostgresDatabase(t, "arkloop_wg07_cancel")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	jobID := uuid.New()
	traceID := "0123456789abcdef0123456789abcdef"

	if err := seedRunStarted(t, pool, orgID, threadID, runID, map[string]any{}); err != nil {
		t.Fatalf("seed run failed: %v", err)
	}
	if err := seedRunCancelRequested(t, pool, runID); err != nil {
		t.Fatalf("seed cancel_requested failed: %v", err)
	}

	handler, err := NewNativeRunEngineV1Handler(pool, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewNativeRunEngineV1Handler failed: %v", err)
	}

	lease := queue.JobLease{
		JobID:       jobID,
		JobType:     queue.RunExecuteJobType,
		PayloadJSON: workerPayloadJSON(jobID, orgID, runID, traceID),
		Attempts:    1,
		LeasedUntil: time.Now().Add(time.Minute),
		LeaseToken:  uuid.New(),
	}

	if err := handler.Handle(context.Background(), lease); err != nil {
		t.Fatalf("handler.Handle failed: %v", err)
	}

	seqTypes := readSeqTypes(t, pool, runID)
	expected := []seqType{
		{Seq: 1, Type: "run.started"},
		{Seq: 2, Type: "run.cancel_requested"},
		{Seq: 3, Type: "worker.job.received"},
		{Seq: 4, Type: "run.cancelled"},
	}
	if len(seqTypes) != len(expected) {
		t.Fatalf("unexpected events count: %d", len(seqTypes))
	}
	for i := range expected {
		if seqTypes[i] != expected[i] {
			t.Fatalf("unexpected event[%d]: %+v", i, seqTypes[i])
		}
	}

	if hasMessages(t, pool, threadID) {
		t.Fatalf("expected no messages inserted")
	}
}

type seqType struct {
	Seq  int64
	Type string
}

func seedRunStarted(
	t *testing.T,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	threadID uuid.UUID,
	runID uuid.UUID,
	startedData map[string]any,
) error {
	t.Helper()

	encoded, err := json.Marshal(startedData)
	if err != nil {
		return err
	}

	_, err = pool.Exec(
		context.Background(),
		`INSERT INTO runs (id, org_id, thread_id, created_by_user_id, status)
		 VALUES ($1, $2, $3, NULL, 'running')`,
		runID,
		orgID,
		threadID,
	)
	if err != nil {
		return err
	}

	_, err = pool.Exec(
		context.Background(),
		`INSERT INTO run_events (run_id, type, data_json)
		 VALUES ($1, 'run.started', $2::jsonb)`,
		runID,
		string(encoded),
	)
	return err
}

func seedRunCancelRequested(t *testing.T, pool *pgxpool.Pool, runID uuid.UUID) error {
	t.Helper()
	_, err := pool.Exec(
		context.Background(),
		`INSERT INTO run_events (run_id, type, data_json)
		 VALUES ($1, 'run.cancel_requested', '{}'::jsonb)`,
		runID,
	)
	return err
}

func workerPayloadJSON(jobID uuid.UUID, orgID uuid.UUID, runID uuid.UUID, traceID string) map[string]any {
	return map[string]any{
		"v":        float64(queue.JobPayloadVersionV1),
		"job_id":   jobID.String(),
		"type":     queue.RunExecuteJobType,
		"trace_id": traceID,
		"org_id":   orgID.String(),
		"run_id":   runID.String(),
		"payload":  map[string]any{"source": "test"},
	}
}

func readSeqTypes(t *testing.T, pool *pgxpool.Pool, runID uuid.UUID) []seqType {
	t.Helper()

	rows, err := pool.Query(
		context.Background(),
		`SELECT seq, type
		 FROM run_events
		 WHERE run_id = $1
		 ORDER BY seq ASC`,
		runID,
	)
	if err != nil {
		t.Fatalf("query run_events failed: %v", err)
	}
	defer rows.Close()

	var out []seqType
	for rows.Next() {
		var item seqType
		if err := rows.Scan(&item.Seq, &item.Type); err != nil {
			t.Fatalf("scan run_events failed: %v", err)
		}
		out = append(out, item)
	}
	return out
}

func readEventData(t *testing.T, pool *pgxpool.Pool, runID uuid.UUID, eventType string) map[string]any {
	t.Helper()

	var raw []byte
	err := pool.QueryRow(
		context.Background(),
		`SELECT data_json
		 FROM run_events
		 WHERE run_id = $1
		   AND type = $2
		 ORDER BY seq ASC
		 LIMIT 1`,
		runID,
		eventType,
	).Scan(&raw)
	if err != nil {
		t.Fatalf("query event data failed: %v", err)
	}

	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal event data failed: %v", err)
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		t.Fatalf("event data is not object")
	}
	return obj
}

func readAssistantMessage(t *testing.T, pool *pgxpool.Pool, threadID uuid.UUID) string {
	t.Helper()

	var content string
	err := pool.QueryRow(
		context.Background(),
		`SELECT content
		 FROM messages
		 WHERE thread_id = $1
		   AND role = 'assistant'
		 ORDER BY created_at ASC
		 LIMIT 1`,
		threadID,
	).Scan(&content)
	if err != nil {
		t.Fatalf("query messages failed: %v", err)
	}
	return content
}

func hasMessages(t *testing.T, pool *pgxpool.Pool, threadID uuid.UUID) bool {
	t.Helper()

	var count int
	err := pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*)
		 FROM messages
		 WHERE thread_id = $1`,
		threadID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count messages failed: %v", err)
	}
	return count > 0
}
