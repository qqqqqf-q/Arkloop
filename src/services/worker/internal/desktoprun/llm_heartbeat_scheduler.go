//go:build desktop

package desktoprun

import (
	"context"
	"time"

	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/app"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"
)

const desktopHeartbeatTickInterval = 30 * time.Second

func startDesktopLLMHeartbeatScheduler(
	ctx context.Context,
	db data.DesktopDB,
	q queue.JobQueue,
	logger *app.JSONLogger,
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
			desktopHeartbeatTick(ctx, db, q, logger)
		}
	}
}

func desktopHeartbeatTick(
	ctx context.Context,
	db data.DesktopDB,
	q queue.JobQueue,
	logger *app.JSONLogger,
) {
	repo := data.ScheduledTriggersRepository{}
	rows, err := repo.ClaimDueHeartbeats(ctx, db, 8)
	if err != nil {
		if logger != nil {
			logger.Error("desktop_heartbeat_claim_failed", app.LogFields{}, map[string]any{"error": err.Error()})
		}
		return
	}
	if logger != nil && len(rows) > 0 {
		logger.Info("desktop_heartbeat_claimed", app.LogFields{}, map[string]any{"count": len(rows)})
	}
	for _, row := range rows {
		runID, err := data.DesktopCreateHeartbeatRun(ctx, db, row, row.Model)
		if err != nil {
			if logger != nil {
				logger.Error("desktop_heartbeat_create_run_failed", app.LogFields{}, map[string]any{
					"channel_identity_id": row.ChannelIdentityID.String(),
					"error":               err.Error(),
				})
			}
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
			if logger != nil {
				logger.Error("desktop_heartbeat_enqueue_failed", app.LogFields{}, map[string]any{
					"run_id": runID.String(),
					"error":  err.Error(),
				})
			}
			_ = repo.PostponeHeartbeat(ctx, db, row.ID, 2*time.Minute)
		}
	}
}
