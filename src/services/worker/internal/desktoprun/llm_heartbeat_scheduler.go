//go:build desktop

package desktoprun

import (
	"context"
	"log/slog"
	"time"

	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"
)

const desktopHeartbeatTickInterval = 30 * time.Second

func startDesktopLLMHeartbeatScheduler(
	ctx context.Context,
	db data.DesktopDB,
	q queue.JobQueue,
) {
	if db == nil || q == nil {
		return
	}
	ticker := time.NewTicker(desktopHeartbeatTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			desktopHeartbeatTick(ctx, db, q)
		}
	}
}

func desktopHeartbeatTick(
	ctx context.Context,
	db data.DesktopDB,
	q queue.JobQueue,
) {
	repo := data.ScheduledTriggersRepository{}
	rows, err := repo.ClaimDueHeartbeats(ctx, db, 8)
	if err != nil {
		slog.ErrorContext(ctx, "desktop_heartbeat_claim_failed", "error", err)
		return
	}
	if len(rows) > 0 {
		slog.InfoContext(ctx, "desktop_heartbeat_claimed", "count", len(rows))
	}
	for _, row := range rows {
		runID, err := data.DesktopCreateHeartbeatRun(ctx, db, row, row.Model)
		if err != nil {
			slog.ErrorContext(ctx, "desktop_heartbeat_create_run_failed",
				"channel_identity_id", row.ChannelIdentityID.String(),
				"error", err,
			)
			_ = repo.PostponeHeartbeat(ctx, db, row.ID, 2*time.Minute)
			continue
		}

		payload := map[string]any{
			"source":                     "llm_heartbeat_scheduler",
			"run_kind":                   runkind.Heartbeat,
			"heartbeat_interval_minutes": row.IntervalMin,
			"heartbeat_reason":           "interval",
			"persona_key":                row.PersonaKey,
			"model":                      row.Model,
		}
		if _, err := q.EnqueueRun(ctx, row.AccountID, runID, "", queue.RunExecuteJobType, payload, nil); err != nil {
			slog.ErrorContext(ctx, "desktop_heartbeat_enqueue_failed",
				"run_id", runID.String(),
				"error", err,
			)
			_ = repo.PostponeHeartbeat(ctx, db, row.ID, 2*time.Minute)
		}
	}
}
