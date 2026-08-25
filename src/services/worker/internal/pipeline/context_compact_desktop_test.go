package pipeline

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/routing"

	"github.com/google/uuid"
)

func TestExecuteContextCompactMaintenanceJobDesktopEmitsLLMRequestEvent(t *testing.T) {
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "context-compact.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	db := sqlitepgx.New(sqlitePool.Unwrap())
	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	msg1ID := uuid.New()
	msg2ID := uuid.New()
	msg3ID := uuid.New()
	msg4ID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`, []any{accountID, "acc-" + accountID.String(), "acc"}},
		{`INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, 'p', 'private')`, []any{projectID, accountID}},
		{`INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`, []any{threadID, accountID, projectID}},
		{`INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, []any{runID, accountID, threadID}},
	} {
		if _, err := db.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}

	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, msg1ID, 1, "user", longCompactFixtureText(240), "2026-04-15 00:00:01.000000000 +0000"); err != nil {
		t.Fatalf("insert message one: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, msg2ID, 2, "assistant", longCompactFixtureText(160), "2026-04-15 00:00:02.000000000 +0000"); err != nil {
		t.Fatalf("insert message two: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, msg3ID, 3, "user", longCompactFixtureText(220), "2026-04-15 00:00:03.000000000 +0000"); err != nil {
		t.Fatalf("insert message three: %v", err)
	}
	if err := insertDesktopThreadMessage(ctx, db, accountID, threadID, msg4ID, 4, "assistant", longCompactFixtureText(120), "2026-04-15 00:00:04.000000000 +0000"); err != nil {
		t.Fatalf("insert message four: %v", err)
	}

	gateway := &stubCompactGateway{summary: "压缩后的摘要"}
	rc := &RunContext{
		Run:     data.Run{ID: runID, AccountID: accountID, ThreadID: threadID},
		DB:      db,
		Gateway: gateway,
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{ID: "route-1", Model: "stub"},
		},
		ContextWindowTokens: 120,
		ContextCompact: ContextCompactSettings{
			PersistEnabled:              true,
			PersistTriggerContextPct:    80,
			FallbackContextWindowTokens: 120,
			TargetContextPct:            20,
		},
		Emitter: events.NewEmitter("trace-compact-desktop"),
	}

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := ExecuteContextCompactMaintenanceJob(runCtx, rc, nil, data.DesktopRunEventsRepository{}); err != nil {
		t.Fatalf("ExecuteContextCompactMaintenanceJob failed: %v", err)
	}
	if gateway.calls == 0 {
		t.Fatal("expected compact gateway to be called")
	}

	rows, err := db.Query(ctx,
		`SELECT type, json_extract(data_json, '$.event_scope')
		   FROM run_events
		  WHERE run_id = $1
		  ORDER BY seq ASC`,
		runID,
	)
	if err != nil {
		t.Fatalf("query run events: %v", err)
	}
	defer rows.Close()

	foundLLMRequest := false
	for rows.Next() {
		var eventType string
		var scope *string
		if err := rows.Scan(&eventType, &scope); err != nil {
			t.Fatalf("scan run event: %v", err)
		}
		if eventType == "llm.request" && scope != nil && *scope == "context_compact" {
			foundLLMRequest = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate run events: %v", err)
	}
	if !foundLLMRequest {
		t.Fatal("expected context_compact llm.request event in run_events")
	}
}

func longCompactFixtureText(repeats int) string {
	text := "这一段是为了触发 compact 维护任务。"
	out := ""
	for i := 0; i < repeats; i++ {
		out += text
	}
	return out
}
