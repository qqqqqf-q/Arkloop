package data

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSubAgentEventsRepository_AppendAndList(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_sub_agent_events_repo")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	seedThread(t, pool, accountID, threadID, uuid.New(), nil)
	seedRun(t, pool, accountID, threadID, runID, nil)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(context.Background())

	repo := SubAgentRepository{}
	record, err := repo.Create(context.Background(), tx, SubAgentCreateParams{
		AccountID:      accountID,
		ParentRunID:    runID,
		ParentThreadID: threadID,
		RootRunID:      runID,
		RootThreadID:   threadID,
		Depth:          1,
		SourceType:     SubAgentSourceTypeThreadSpawn,
		ContextMode:    SubAgentContextModeIsolated,
	})
	if err != nil {
		t.Fatalf("create sub_agent: %v", err)
	}

	eventsRepo := SubAgentEventsRepository{}
	if _, err := eventsRepo.AppendEvent(context.Background(), tx, record.ID, nil, SubAgentEventTypeSpawnRequested, nil, nil); err != nil {
		t.Fatalf("append first event: %v", err)
	}
	if _, err := eventsRepo.AppendEvent(context.Background(), tx, record.ID, &runID, SubAgentEventTypeRunQueued, map[string]any{"run_id": runID.String()}, nil); err != nil {
		t.Fatalf("append second event: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit: %v", err)
	}

	list, err := eventsRepo.ListBySubAgent(context.Background(), pool, record.ID, 0, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 events, got %d", len(list))
	}
	if list[0].Type != SubAgentEventTypeSpawnRequested {
		t.Fatalf("unexpected first type: %s", list[0].Type)
	}
	if list[0].DataJSON == nil || len(list[0].DataJSON) != 0 {
		t.Fatalf("expected empty data_json, got %#v", list[0].DataJSON)
	}
	if list[0].RunID != nil {
		t.Fatalf("expected nil run_id, got %#v", list[0].RunID)
	}
	if list[1].Type != SubAgentEventTypeRunQueued {
		t.Fatalf("unexpected second type: %s", list[1].Type)
	}
	if list[1].RunID == nil || *list[1].RunID != runID {
		t.Fatalf("unexpected run_id: %#v", list[1].RunID)
	}
	if list[1].Seq <= list[0].Seq {
		t.Fatalf("expected increasing seq, got %d then %d", list[0].Seq, list[1].Seq)
	}
}
