//go:build desktop

package desktoprun

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"arkloop/services/shared/eventbus"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	desktopRunTimeoutEnv            = "ARKLOOP_RUN_TIMEOUT_MINUTES"
	defaultDesktopRunTimeoutMinutes = 5
	desktopReaperInterval           = time.Minute
	desktopRecoveryGrace            = 3 * time.Second
	desktopStaleCancelGrace         = 30 * time.Second
)

var desktopTerminalEventStatus = map[string]string{
	"run.completed":   "completed",
	"run.failed":      "failed",
	"run.interrupted": "interrupted",
	"run.cancelled":   "cancelled",
}

type lifecycleManager struct {
	db      data.DesktopDB
	queue   queue.JobQueue
	bus     eventbus.EventBus
	logger  *slog.Logger
	timeout time.Duration
}

type desktopRunSnapshot struct {
	RunID               uuid.UUID
	AccountID           uuid.UUID
	LastEventType       string
	LastTraceID         string
	LastActivity        time.Time
	LastCancelRequested sql.NullTime
}

func newLifecycleManager(db data.DesktopDB, q queue.JobQueue, bus eventbus.EventBus, logger *slog.Logger) *lifecycleManager {
	timeoutMinutes := defaultDesktopRunTimeoutMinutes
	if raw := strings.TrimSpace(os.Getenv(desktopRunTimeoutEnv)); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			timeoutMinutes = value
		}
	}
	return &lifecycleManager{
		db:      db,
		queue:   q,
		bus:     bus,
		logger:  logger,
		timeout: time.Duration(timeoutMinutes) * time.Minute,
	}
}

func (m *lifecycleManager) Bootstrap(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if err := m.markLegacyRunJobsDead(ctx); err != nil {
		return err
	}
	if err := m.reapOnce(ctx); err != nil {
		return err
	}
	return m.recoverRuns(ctx)
}

func (m *lifecycleManager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	go m.reaperLoop(ctx)
	go startDesktopLLMHeartbeatScheduler(ctx, m.db, m.queue, m.bus)
}

func (m *lifecycleManager) reaperLoop(ctx context.Context) {
	ticker := time.NewTicker(desktopReaperInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.reapOnce(ctx); err != nil && m.logger != nil {
				m.logger.Error("desktop stale run reap failed", "error", err.Error())
			}
		}
	}
}

func (m *lifecycleManager) markLegacyRunJobsDead(ctx context.Context) error {
	if m.db == nil {
		return nil
	}
	tag, err := m.db.Exec(ctx,
		`UPDATE jobs
		    SET status = $1,
		        leased_until = NULL,
		        lease_token = NULL,
		        updated_at = datetime('now')
		  WHERE job_type = $2
		    AND status IN ($3, $4)`,
		queue.JobStatusDead,
		queue.RunExecuteJobType,
		queue.JobStatusQueued,
		queue.JobStatusLeased,
	)
	if err != nil {
		return fmt.Errorf("mark legacy desktop run jobs dead: %w", err)
	}
	if m.logger != nil && tag.RowsAffected() > 0 {
		m.logger.Info("desktop legacy run jobs abandoned", "rows", tag.RowsAffected())
	}
	return nil
}

func (m *lifecycleManager) reapOnce(ctx context.Context) error {
	if m.db == nil || m.timeout <= 0 {
		return nil
	}
	now := time.Now().UTC()
	staleBefore := now.Add(-m.timeout)
	cancelGraceBefore := now.Add(-desktopStaleCancelGrace)
	runs, err := listRunningRuns(ctx, m.db)
	if err != nil {
		return err
	}
	for _, snapshot := range runs {
		if snapshot.LastEventType != "" {
			if terminalStatus, ok := desktopTerminalEventStatus[snapshot.LastEventType]; ok {
				if err := syncRunStatusFromTerminalEvent(ctx, m.db, snapshot.RunID, terminalStatus); err != nil {
					return err
				}
				continue
			}
		}
		if snapshot.LastCancelRequested.Valid {
			if snapshot.LastCancelRequested.Time.Before(cancelGraceBefore) {
				reaped, err := forceFailDesktopRun(ctx, m.db, snapshot.RunID)
				if err != nil {
					return err
				}
				if reaped && m.logger != nil {
					runID := snapshot.RunID.String()
					accountID := snapshot.AccountID.String()
					m.logger.Info("desktop stale run reaped", "run_id", runID, "account_id", accountID)
				}
			}
			continue
		}
		if snapshot.LastActivity.After(staleBefore) {
			continue
		}
		requested, err := requestCancelDesktopRun(ctx, m.db, snapshot.RunID, snapshot.LastTraceID)
		if err != nil {
			return err
		}
		if requested && m.logger != nil {
			runID := snapshot.RunID.String()
			accountID := snapshot.AccountID.String()
			m.logger.Info("desktop stale run cancel requested", "run_id", runID, "account_id", accountID)
		}
	}
	return nil
}

func (m *lifecycleManager) recoverRuns(ctx context.Context) error {
	if m.db == nil || m.queue == nil {
		return nil
	}
	recoverBefore := time.Now().UTC().Add(-desktopRecoveryGrace)
	staleBefore := time.Now().UTC().Add(-m.timeout)
	runs, err := listRunningRuns(ctx, m.db)
	if err != nil {
		return err
	}
	for _, snapshot := range runs {
		if snapshot.LastActivity.After(recoverBefore) || !snapshot.LastActivity.After(staleBefore) {
			continue
		}
		if _, ok := desktopTerminalEventStatus[snapshot.LastEventType]; ok {
			continue
		}
		if snapshot.LastCancelRequested.Valid {
			continue
		}
		if snapshot.LastEventType == "run.input_requested" || snapshot.LastEventType == "run.cancel_requested" {
			continue
		}
		if _, err := m.queue.EnqueueRun(ctx, snapshot.AccountID, snapshot.RunID, snapshot.LastTraceID, queue.RunExecuteJobType, map[string]any{
			"source": "desktop_recovery",
		}, nil); err != nil {
			return fmt.Errorf("recover desktop run %s: %w", snapshot.RunID, err)
		}
		if m.logger != nil {
			runID := snapshot.RunID.String()
			accountID := snapshot.AccountID.String()
			m.logger.Info("desktop run recovered", "run_id", runID, "account_id", accountID,
				"last_event_type", snapshot.LastEventType,
			)
		}
	}
	return nil
}

func listRunningRuns(ctx context.Context, db data.DesktopDB) ([]desktopRunSnapshot, error) {
	rows, err := db.Query(ctx,
		`SELECT r.id,
		        r.account_id,
		        COALESCE((
		            SELECT type
		              FROM run_events re
		             WHERE re.run_id = r.id
		             ORDER BY seq DESC
		             LIMIT 1
		        ), ''),
		        COALESCE((
		            SELECT json_extract(re.data_json, '$.trace_id')
		              FROM run_events re
		             WHERE re.run_id = r.id
		               AND json_extract(re.data_json, '$.trace_id') IS NOT NULL
		             ORDER BY seq DESC
		             LIMIT 1
		        ), ''),
		        COALESCE((
		            SELECT MAX(ts)
		              FROM run_events re
		             WHERE re.run_id = r.id
		        ), r.created_at),
		        (SELECT MAX(ts)
		              FROM run_events re
		             WHERE re.run_id = r.id
		               AND re.type = 'run.cancel_requested')
		   FROM runs r
		  WHERE r.status = 'running'
		  ORDER BY r.created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list running desktop runs: %w", err)
	}
	defer rows.Close()

	snapshots := make([]desktopRunSnapshot, 0)
	for rows.Next() {
		var snapshot desktopRunSnapshot
		var rawCancel sql.NullString
		if err := rows.Scan(
			&snapshot.RunID,
			&snapshot.AccountID,
			&snapshot.LastEventType,
			&snapshot.LastTraceID,
			&snapshot.LastActivity,
			&rawCancel,
		); err != nil {
			return nil, fmt.Errorf("scan running desktop run: %w", err)
		}
		if rawCancel.Valid {
			parsed, err := parseDesktopEventTimestamp(rawCancel.String)
			if err != nil {
				return nil, fmt.Errorf("parse cancel_requested ts: %w", err)
			}
			snapshot.LastCancelRequested = sql.NullTime{Time: parsed, Valid: true}
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate running desktop runs: %w", err)
	}
	return snapshots, nil
}

func parseDesktopEventTimestamp(raw string) (time.Time, error) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999 -0700",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, cleaned); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp format: %s", cleaned)
}

func syncRunStatusFromTerminalEvent(ctx context.Context, db data.DesktopDB, runID uuid.UUID, status string) error {
	if runID == uuid.Nil || strings.TrimSpace(status) == "" {
		return nil
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin desktop terminal sync tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx,
		`UPDATE runs
		    SET status = $2,
		        status_updated_at = datetime('now'),
		        completed_at = CASE
		            WHEN $2 = 'completed' THEN COALESCE(completed_at, datetime('now'))
		            ELSE completed_at
		        END,
		        failed_at = CASE
		            WHEN $2 = 'failed' THEN COALESCE(failed_at, datetime('now'))
		            ELSE failed_at
		        END,
		        duration_ms = COALESCE(duration_ms, CAST((julianday('now') - julianday(created_at)) * 86400000 AS INTEGER))
		  WHERE id = $1
		    AND status = 'running'`,
		runID,
		status,
	)
	if err != nil {
		return fmt.Errorf("sync desktop terminal status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit desktop terminal sync: %w", err)
	}
	return nil
}

func forceFailDesktopRun(ctx context.Context, db data.DesktopDB, runID uuid.UUID) (bool, error) {
	if runID == uuid.Nil {
		return false, fmt.Errorf("run_id must not be empty")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin desktop force-fail tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx,
		`UPDATE runs
		    SET status = 'failed',
		        failed_at = datetime('now'),
		        status_updated_at = datetime('now')
		  WHERE id = $1
		    AND status = 'running'`,
		runID,
	)
	if err != nil {
		return false, fmt.Errorf("update desktop run failed status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	repo := data.DesktopRunEventsRepository{}
	if _, err := repo.AppendEvent(ctx, tx, runID,
		"run.failed",
		map[string]any{"reason": "stale run reaped by system"},
		nil,
		stringPtr("worker.timeout"),
	); err != nil {
		return false, fmt.Errorf("append desktop stale run failed event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit desktop force-fail tx: %w", err)
	}
	return true, nil
}

func requestCancelDesktopRun(ctx context.Context, db data.DesktopDB, runID uuid.UUID, traceID string) (bool, error) {
	if runID == uuid.Nil {
		return false, fmt.Errorf("run_id must not be empty")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin desktop cancel tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1 FOR UPDATE`, runID).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("lock desktop run row: %w", err)
	}
	if status != "running" {
		return false, nil
	}

	repo := data.DesktopRunEventsRepository{}
	terminalType, err := repo.GetLatestEventType(ctx, tx, runID, []string{"run.cancel_requested", "run.cancelled"})
	if err != nil {
		return false, fmt.Errorf("fetch latest terminal event: %w", err)
	}
	if terminalType == "run.cancel_requested" || terminalType == "run.cancelled" {
		return false, nil
	}

	emitter := events.NewEmitter(strings.TrimSpace(traceID))
	event := emitter.Emit("run.cancel_requested", map[string]any{"reason": "stale run timeout"}, nil, nil)
	if _, err := repo.AppendRunEvent(ctx, tx, runID, event); err != nil {
		return false, fmt.Errorf("append cancel request event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit desktop cancel tx: %w", err)
	}
	return true, nil
}

func stringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
