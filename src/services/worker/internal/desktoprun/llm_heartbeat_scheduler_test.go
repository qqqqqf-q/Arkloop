//go:build desktop

package desktoprun

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/shared/schedulekind"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
)

func TestDesktopFireJobDoesNotReenqueueOneShotAfterFinalizeFailure(t *testing.T) {
	cases := []struct {
		name           string
		deleteAfterRun bool
		blockerName    string
		blockerSQL     string
		assertFinal    func(t *testing.T, ctx context.Context, db data.DesktopDB, jobID uuid.UUID, triggerID uuid.UUID)
	}{
		{
			name:           "disable job",
			deleteAfterRun: false,
			blockerName:    "block_one_shot_trigger_delete",
			blockerSQL: `
CREATE TRIGGER block_one_shot_trigger_delete
BEFORE DELETE ON scheduled_triggers
BEGIN
	SELECT RAISE(ABORT, 'block trigger delete');
END;`,
			assertFinal: func(t *testing.T, ctx context.Context, db data.DesktopDB, jobID uuid.UUID, triggerID uuid.UUID) {
				t.Helper()

				var enabled bool
				if err := db.QueryRow(ctx, `SELECT enabled FROM scheduled_jobs WHERE id = $1`, jobID.String()).Scan(&enabled); err != nil {
					t.Fatalf("query scheduled_jobs.enabled: %v", err)
				}
				if enabled {
					t.Fatal("expected one-shot job to be disabled after finalize retry")
				}
				assertTriggerRemoved(t, ctx, db, triggerID)
			},
		},
		{
			name:           "delete job",
			deleteAfterRun: true,
			blockerName:    "block_one_shot_job_delete",
			blockerSQL: `
CREATE TRIGGER block_one_shot_job_delete
BEFORE DELETE ON scheduled_jobs
BEGIN
	SELECT RAISE(ABORT, 'block job delete');
END;`,
			assertFinal: func(t *testing.T, ctx context.Context, db data.DesktopDB, jobID uuid.UUID, triggerID uuid.UUID) {
				t.Helper()

				var count int
				if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM scheduled_jobs WHERE id = $1`, jobID.String()).Scan(&count); err != nil {
					t.Fatalf("count scheduled_jobs: %v", err)
				}
				if count != 0 {
					t.Fatal("expected delete_after_run one-shot job to be removed after finalize retry")
				}
				assertTriggerRemoved(t, ctx, db, triggerID)
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, cleanup := openHeartbeatSchedulerTestDB(t, ctx)
			defer cleanup()

			row, jobID := seedDesktopOneShotJob(t, ctx, db, tc.deleteAfterRun)
			q := &heartbeatSchedulerQueueStub{}

			if _, err := db.Exec(ctx, tc.blockerSQL); err != nil {
				t.Fatalf("create finalize blocker: %v", err)
			}

			desktopFireJob(ctx, db, q, row)

			if len(q.calls) != 1 {
				t.Fatalf("expected first tick to enqueue once, got %d", len(q.calls))
			}

			if _, err := db.Exec(ctx, `DROP TRIGGER `+tc.blockerName); err != nil {
				t.Fatalf("drop finalize blocker: %v", err)
			}

			desktopFireJob(ctx, db, q, row)

			if len(q.calls) != 1 {
				t.Fatalf("expected finalize retry not to enqueue again, got %d", len(q.calls))
			}

			tc.assertFinal(t, ctx, db, jobID, row.ID)
		})
	}
}

type heartbeatSchedulerQueueStub struct {
	calls []heartbeatSchedulerQueueCall
	err   error
}

type heartbeatSchedulerQueueCall struct {
	accountID uuid.UUID
	runID     uuid.UUID
	traceID   string
	jobType   string
	payload   map[string]any
}

func (s *heartbeatSchedulerQueueStub) EnqueueRun(
	_ context.Context,
	accountID uuid.UUID,
	runID uuid.UUID,
	traceID string,
	queueJobType string,
	payload map[string]any,
	_ *time.Time,
) (uuid.UUID, error) {
	s.calls = append(s.calls, heartbeatSchedulerQueueCall{
		accountID: accountID,
		runID:     runID,
		traceID:   traceID,
		jobType:   queueJobType,
		payload:   payload,
	})
	if s.err != nil {
		return uuid.Nil, s.err
	}
	return uuid.New(), nil
}

func (s *heartbeatSchedulerQueueStub) Lease(context.Context, int, []string) (*queue.JobLease, error) {
	return nil, nil
}

func (s *heartbeatSchedulerQueueStub) Heartbeat(context.Context, queue.JobLease, int) error {
	return nil
}

func (s *heartbeatSchedulerQueueStub) Ack(context.Context, queue.JobLease) error {
	return nil
}

func (s *heartbeatSchedulerQueueStub) Nack(context.Context, queue.JobLease, *int) error {
	return nil
}

func (s *heartbeatSchedulerQueueStub) QueueDepth(context.Context, []string) (int, error) {
	return 0, nil
}

func openHeartbeatSchedulerTestDB(t *testing.T, ctx context.Context) (data.DesktopDB, func()) {
	t.Helper()

	dataDir := filepath.Join(t.TempDir(), "desktop-data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	t.Setenv("ARKLOOP_DATA_DIR", dataDir)

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	cleanup := func() {
		_ = sqlitePool.Close()
	}
	return sqlitepgx.New(sqlitePool.Unwrap()), cleanup
}

func seedDesktopOneShotJob(t *testing.T, ctx context.Context, db data.DesktopDB, deleteAfterRun bool) (data.ScheduledTriggerRow, uuid.UUID) {
	t.Helper()

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	jobID := uuid.New()
	triggerID := uuid.New()
	now := time.Now().UTC()
	fireAt := now.Add(-time.Minute)

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{
			sql:  `INSERT INTO accounts (id, slug, name, type, status) VALUES ($1, $2, $3, 'personal', 'active')`,
			args: []any{accountID.String(), "desktop-one-shot-" + accountID.String(), "Desktop One Shot"},
		},
		{
			sql:  `INSERT INTO projects (id, account_id, name, visibility) VALUES ($1, $2, $3, 'private')`,
			args: []any{projectID.String(), accountID.String(), "One Shot Project"},
		},
		{
			sql:  `INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`,
			args: []any{threadID.String(), accountID.String(), projectID.String()},
		},
		{
			sql: `INSERT INTO scheduled_jobs
				(id, account_id, name, description, persona_key, prompt, model, workspace_ref, work_dir, thread_id,
				 schedule_kind, interval_min, daily_time, monthly_day, monthly_time, weekly_day, timezone, enabled,
				 created_by_user_id, fire_at, cron_expr, delete_after_run, reasoning_mode, timeout_seconds, created_at, updated_at)
				VALUES ($1, $2, $3, '', '', '', '', '', '', $4, $5, NULL, '', NULL, '', NULL, 'UTC', 1, NULL, $6, '', $7, '', 0, $8, $8)`,
			args: []any{jobID.String(), accountID.String(), "One Shot Job", threadID.String(), schedulekind.At, fireAt.Format(time.RFC3339Nano), deleteAfterRun, now.Format(time.RFC3339Nano)},
		},
		{
			sql: `INSERT INTO scheduled_triggers
				(id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at,
				 created_at, updated_at, trigger_kind, job_id)
				VALUES ($1, $2, $3, '', $4, '', 1, $5, $6, $6, 'job', $7)`,
			args: []any{triggerID.String(), uuid.NewString(), uuid.NewString(), accountID.String(), fireAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), jobID.String()},
		},
	} {
		if _, err := db.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed one-shot job: %v", err)
		}
	}

	return data.ScheduledTriggerRow{
		ID:          triggerID,
		AccountID:   accountID,
		TriggerKind: "job",
		JobID:       jobID,
	}, jobID
}

func assertTriggerRemoved(t *testing.T, ctx context.Context, db data.DesktopDB, triggerID uuid.UUID) {
	t.Helper()

	var count int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM scheduled_triggers WHERE id = $1`, triggerID.String()).Scan(&count); err != nil {
		t.Fatalf("count scheduled_triggers: %v", err)
	}
	if count != 0 {
		t.Fatal("expected one-shot trigger to be removed after finalize retry")
	}
}
