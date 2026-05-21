//go:build desktop

package desktoprun

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	activityRecorderPluginID            = "arkloop.plugins.activity-recorder"
	activityRecorderBuilderPersonaID    = "activity-recorder-builder"
	activityRecorderDefaultIntervalMin  = 300
	activityRecorderMinIntervalMin      = 5
	activityRecorderInitialLookback     = 24 * time.Hour
	activityRecorderSchedulerInterval   = 10 * time.Second
	activityRecorderRunningStaleTimeout = 2 * time.Hour
)

type activityRecorderEnablement struct {
	AccountID    uuid.UUID
	UserID       uuid.UUID
	ProfileRef   string
	WorkspaceRef string
	IntervalMin  int
}

type activityRecorderDueRow struct {
	AccountID       uuid.UUID
	UserID          uuid.UUID
	ProfileRef      string
	WorkspaceRef    string
	IntervalMin     int
	LastWindowEndAt *time.Time
}

func startActivityRecorderBuilderScheduler(ctx context.Context, db data.DesktopDB, q queue.JobQueue) {
	if db == nil || q == nil {
		return
	}
	ticker := time.NewTicker(activityRecorderSchedulerInterval)
	defer ticker.Stop()

	for {
		activityRecorderBuilderTick(ctx, db, q)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func activityRecorderBuilderTick(ctx context.Context, db data.DesktopDB, q queue.JobQueue) {
	if err := syncActivityRecorderBuilderState(ctx, db); err != nil {
		slog.WarnContext(ctx, "activity_recorder_builder_sync_failed", "error", err.Error())
		return
	}
	if err := clearFinishedActivityRecorderRuns(ctx, db); err != nil {
		slog.WarnContext(ctx, "activity_recorder_builder_clear_finished_failed", "error", err.Error())
	}
	rows, err := claimDueActivityRecorderBuilders(ctx, db, 2)
	if err != nil {
		slog.WarnContext(ctx, "activity_recorder_builder_claim_failed", "error", err.Error())
		return
	}
	for _, row := range rows {
		fireActivityRecorderBuilder(ctx, db, q, row)
	}
}

func syncActivityRecorderBuilderState(ctx context.Context, db data.DesktopDB) error {
	rows, err := db.Query(ctx, `
		SELECT pe.account_id, pe.enabled_by_user_id, pe.profile_ref, pe.workspace_ref, pe.settings_json
		  FROM plugin_enablements pe
		  JOIN plugin_runtime_state prs
		    ON prs.account_id = pe.account_id
		   AND prs.package_id = pe.package_id
		   AND prs.profile_ref = pe.profile_ref
		   AND prs.workspace_ref = pe.workspace_ref
		 WHERE pe.plugin_id = $1
		   AND pe.desired_enabled = 1
		   AND prs.status = 'installed'`,
		activityRecorderPluginID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var enabled []activityRecorderEnablement
	for rows.Next() {
		var accountIDRaw, userIDRaw, settingsRaw string
		var item activityRecorderEnablement
		if err := rows.Scan(&accountIDRaw, &userIDRaw, &item.ProfileRef, &item.WorkspaceRef, &settingsRaw); err != nil {
			return err
		}
		accountID, err := uuid.Parse(accountIDRaw)
		if err != nil {
			return err
		}
		userID, err := uuid.Parse(userIDRaw)
		if err != nil {
			return err
		}
		item.AccountID = accountID
		item.UserID = userID
		item.IntervalMin = activityRecorderIntervalFromSettings(settingsRaw)
		enabled = append(enabled, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if _, err := db.Exec(ctx, `
		UPDATE activity_recorder_builder_state
		   SET enabled = 0,
		       updated_at = $1
		 WHERE NOT EXISTS (
		       SELECT 1
		         FROM plugin_enablements pe
		         JOIN plugin_runtime_state prs
		           ON prs.account_id = pe.account_id
		          AND prs.package_id = pe.package_id
		          AND prs.profile_ref = pe.profile_ref
		          AND prs.workspace_ref = pe.workspace_ref
		        WHERE pe.plugin_id = $2
		          AND pe.desired_enabled = 1
		          AND prs.status = 'installed'
		          AND pe.account_id = activity_recorder_builder_state.account_id
		          AND pe.enabled_by_user_id = activity_recorder_builder_state.user_id
		          AND pe.profile_ref = activity_recorder_builder_state.profile_ref
		          AND pe.workspace_ref = activity_recorder_builder_state.workspace_ref
		   )`,
		formatActivityRecorderSQLiteTimestamp(time.Now().UTC()),
		activityRecorderPluginID,
	); err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, item := range enabled {
		if _, err := db.Exec(ctx, `
			INSERT INTO activity_recorder_builder_state
			    (account_id, user_id, profile_ref, workspace_ref, enabled, interval_min, next_run_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 1, $5, $6, $6, $6)
			ON CONFLICT (account_id, user_id, profile_ref, workspace_ref) DO UPDATE
			    SET enabled      = 1,
			        interval_min = excluded.interval_min,
			        next_run_at  = CASE
			            WHEN activity_recorder_builder_state.enabled = 0 THEN excluded.next_run_at
			            ELSE activity_recorder_builder_state.next_run_at
			        END,
			        updated_at   = excluded.updated_at`,
			item.AccountID.String(),
			item.UserID.String(),
			item.ProfileRef,
			item.WorkspaceRef,
			item.IntervalMin,
			formatActivityRecorderSQLiteTimestamp(now),
		); err != nil {
			return err
		}
	}
	return nil
}

func activityRecorderIntervalFromSettings(raw string) int {
	var settings map[string]any
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return activityRecorderDefaultIntervalMin
	}
	value := activityRecorderDefaultIntervalMin
	switch typed := settings["builder_interval_min"].(type) {
	case float64:
		value = int(typed)
	case string:
		var parsed int
		if _, err := fmt.Sscanf(typed, "%d", &parsed); err == nil {
			value = parsed
		}
	}
	if value < activityRecorderMinIntervalMin {
		return activityRecorderMinIntervalMin
	}
	return value
}

func clearFinishedActivityRecorderRuns(ctx context.Context, db data.DesktopDB) error {
	_, err := db.Exec(ctx, `
		UPDATE activity_recorder_builder_state
		   SET running_run_id = NULL,
		       running_started_at = NULL,
		       last_run_status = COALESCE((SELECT status FROM runs WHERE id = activity_recorder_builder_state.running_run_id), last_run_status),
		       updated_at = $1
		 WHERE running_run_id IS NOT NULL
		   AND EXISTS (
		       SELECT 1
		         FROM runs
		        WHERE runs.id = activity_recorder_builder_state.running_run_id
		          AND runs.status IN ('completed', 'failed', 'cancelled', 'interrupted')
		   )`,
		formatActivityRecorderSQLiteTimestamp(time.Now().UTC()),
	)
	return err
}

func claimDueActivityRecorderBuilders(ctx context.Context, db data.DesktopDB, limit int) ([]activityRecorderDueRow, error) {
	if limit <= 0 {
		limit = 2
	}
	now := time.Now().UTC()
	staleBefore := now.Add(-activityRecorderRunningStaleTimeout)
	rows, err := db.Query(ctx, `
		SELECT account_id, user_id, profile_ref, workspace_ref, interval_min, last_window_end_at
		  FROM activity_recorder_builder_state
		 WHERE enabled = 1
		   AND datetime(next_run_at) <= datetime($1)
		   AND (
		       running_run_id IS NULL
		       OR running_started_at IS NULL
		       OR datetime(running_started_at) <= datetime($2)
		   )
		 ORDER BY next_run_at ASC
		 LIMIT $3`,
		formatActivityRecorderSQLiteTimestamp(now),
		formatActivityRecorderSQLiteTimestamp(staleBefore),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []activityRecorderDueRow
	for rows.Next() {
		var accountRaw, userRaw string
		var lastRaw sql.NullString
		var row activityRecorderDueRow
		if err := rows.Scan(&accountRaw, &userRaw, &row.ProfileRef, &row.WorkspaceRef, &row.IntervalMin, &lastRaw); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, err
		}
		accountID, err := uuid.Parse(accountRaw)
		if err != nil {
			return nil, err
		}
		userID, err := uuid.Parse(userRaw)
		if err != nil {
			return nil, err
		}
		row.AccountID = accountID
		row.UserID = userID
		if lastRaw.Valid {
			parsed, ok := parseActivityRecorderTime(lastRaw.String)
			if ok {
				row.LastWindowEndAt = &parsed
			}
		}
		if row.IntervalMin < activityRecorderMinIntervalMin {
			row.IntervalMin = activityRecorderMinIntervalMin
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func fireActivityRecorderBuilder(ctx context.Context, db data.DesktopDB, q queue.JobQueue, row activityRecorderDueRow) {
	now := time.Now().UTC()
	windowStart := now.Add(-activityRecorderInitialLookback)
	if row.LastWindowEndAt != nil && !row.LastWindowEndAt.IsZero() {
		windowStart = *row.LastWindowEndAt
	}
	windowEnd := now
	threadID := uuid.New()
	runID := uuid.New()
	traceID := uuid.NewString()

	var projectID uuid.UUID
	if err := db.QueryRow(ctx,
		`SELECT id FROM projects WHERE account_id = $1 AND deleted_at IS NULL ORDER BY created_at ASC LIMIT 1`,
		row.AccountID.String(),
	).Scan(&projectID); err != nil {
		slog.WarnContext(ctx, "activity_recorder_builder_project_lookup_failed", "error", err.Error(), "account_id", row.AccountID.String())
		postponeActivityRecorderBuilder(ctx, db, row, 2*time.Minute)
		return
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.WarnContext(ctx, "activity_recorder_builder_tx_begin_failed", "error", err.Error())
		postponeActivityRecorderBuilder(ctx, db, row, 2*time.Minute)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	if _, err := tx.Exec(ctx,
		`INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`,
		threadID.String(),
		row.AccountID.String(),
		projectID.String(),
	); err != nil {
		slog.WarnContext(ctx, "activity_recorder_builder_thread_create_failed", "error", err.Error())
		_ = tx.Rollback(ctx)
		postponeActivityRecorderBuilder(ctx, db, row, 2*time.Minute)
		return
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO runs (id, account_id, thread_id, status, created_by_user_id, profile_ref, workspace_ref)
		 VALUES ($1, $2, $3, 'running', $4, $5, $6)`,
		runID.String(),
		row.AccountID.String(),
		threadID.String(),
		row.UserID.String(),
		row.ProfileRef,
		row.WorkspaceRef,
	); err != nil {
		slog.WarnContext(ctx, "activity_recorder_builder_run_create_failed", "error", err.Error())
		_ = tx.Rollback(ctx)
		postponeActivityRecorderBuilder(ctx, db, row, 2*time.Minute)
		return
	}

	startedData := activityRecorderRunData(row, windowStart, windowEnd)
	if _, err := (data.DesktopRunEventsRepository{}).AppendEvent(ctx, tx, runID, "run.started", startedData, nil, nil); err != nil {
		slog.WarnContext(ctx, "activity_recorder_builder_started_event_failed", "error", err.Error())
		_ = tx.Rollback(ctx)
		postponeActivityRecorderBuilder(ctx, db, row, 2*time.Minute)
		return
	}

	nextRunAt := now.Add(time.Duration(row.IntervalMin) * time.Minute)
	if _, err := tx.Exec(ctx, `
		UPDATE activity_recorder_builder_state
		   SET running_run_id = $1,
		       running_started_at = $2,
		       last_run_id = $1,
		       last_run_status = 'running',
		       last_error = '',
		       last_finish_status = '',
		       last_finish_reason = '',
		       last_sources_checked = '[]',
		       last_sources_unavailable = '[]',
		       last_memory_write_count = 0,
		       last_finished_at = NULL,
		       next_run_at = $3,
		       updated_at = $2
		 WHERE account_id = $4
		   AND user_id = $5
		   AND profile_ref = $6
		   AND workspace_ref = $7`,
		runID.String(),
		formatActivityRecorderSQLiteTimestamp(now),
		formatActivityRecorderSQLiteTimestamp(nextRunAt),
		row.AccountID.String(),
		row.UserID.String(),
		row.ProfileRef,
		row.WorkspaceRef,
	); err != nil {
		slog.WarnContext(ctx, "activity_recorder_builder_state_update_failed", "error", err.Error())
		_ = tx.Rollback(ctx)
		postponeActivityRecorderBuilder(ctx, db, row, 2*time.Minute)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		slog.WarnContext(ctx, "activity_recorder_builder_commit_failed", "error", err.Error())
		postponeActivityRecorderBuilder(ctx, db, row, 2*time.Minute)
		return
	}

	payload := activityRecorderRunData(row, windowStart, windowEnd)
	payload["source"] = "activity_recorder_builder_scheduler"
	if _, err := q.EnqueueRun(ctx, row.AccountID, runID, traceID, queue.RunExecuteJobType, payload, nil); err != nil {
		slog.WarnContext(ctx, "activity_recorder_builder_enqueue_failed", "run_id", runID.String(), "error", err.Error())
		if markErr := markDesktopRunFailed(ctx, db, runID, "worker.enqueue_failed", "failed to enqueue activity recorder builder run", err); markErr != nil {
			slog.WarnContext(ctx, "activity_recorder_builder_mark_failed_failed", "run_id", runID.String(), "error", markErr.Error())
		}
		postponeActivityRecorderBuilder(ctx, db, row, 2*time.Minute)
		return
	}

	slog.InfoContext(ctx, "activity_recorder_builder_triggered",
		"account_id", row.AccountID.String(),
		"user_id", row.UserID.String(),
		"run_id", runID.String(),
		"window_start", windowStart.Format(time.RFC3339),
		"window_end", windowEnd.Format(time.RFC3339),
	)
}

func activityRecorderRunData(row activityRecorderDueRow, windowStart, windowEnd time.Time) map[string]any {
	windowStartText := windowStart.UTC().Format(time.RFC3339)
	windowEndText := windowEnd.UTC().Format(time.RFC3339)
	return map[string]any{
		"run_kind":      runkind.ActivityRecorder,
		"persona_id":    activityRecorderBuilderPersonaID,
		"profile_ref":   row.ProfileRef,
		"workspace_ref": row.WorkspaceRef,
		"window_start":  windowStartText,
		"window_end":    windowEndText,
		"instruction": strings.TrimSpace(`执行 Activity Recorder 后台扫描。
window_start: ` + windowStartText + `
window_end: ` + windowEndText + `

加载 Activity Recorder 相关 skill 和可用 MCP 工具，读取该时间窗口内的本地活动事件。如果社交搜索工具可用且已有上下文能确定 owner 的公开账号，也可以搜索 owner 相关公开动态作为补充信号。只把长期有价值的事实、偏好、项目上下文或重要事件写入 memory_write。完成前必须调用 activity_recorder_finish 记录本轮状态。不要写 Notebook，不要输出用户可见说明。`),
	}
}

func postponeActivityRecorderBuilder(ctx context.Context, db data.DesktopDB, row activityRecorderDueRow, delay time.Duration) {
	nextRunAt := time.Now().UTC().Add(delay)
	if _, err := db.Exec(ctx, `
		UPDATE activity_recorder_builder_state
		   SET running_run_id = NULL,
		       running_started_at = NULL,
		       next_run_at = $1,
		       last_run_status = 'error',
		       updated_at = $1
		 WHERE account_id = $2
		   AND user_id = $3
		   AND profile_ref = $4
		   AND workspace_ref = $5`,
		formatActivityRecorderSQLiteTimestamp(nextRunAt),
		row.AccountID.String(),
		row.UserID.String(),
		row.ProfileRef,
		row.WorkspaceRef,
	); err != nil {
		slog.WarnContext(ctx, "activity_recorder_builder_postpone_failed", "error", err.Error())
	}
}

func parseActivityRecorderTime(raw string) (time.Time, bool) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, clean); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func formatActivityRecorderSQLiteTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.999999999")
}
