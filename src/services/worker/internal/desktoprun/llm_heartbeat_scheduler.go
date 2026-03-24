//go:build desktop

package desktoprun

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const desktopHeartbeatTickInterval = 30 * time.Second

func startDesktopLLMHeartbeatScheduler(
	ctx context.Context,
	db data.DesktopDB,
	q queue.JobQueue,
) {
	if db == nil || q == nil {
		slog.WarnContext(ctx, "desktop_heartbeat_scheduler: db or queue is nil, skipping")
		return
	}
	slog.InfoContext(ctx, "desktop_heartbeat_scheduler: started", "interval", desktopHeartbeatTickInterval)
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
	slog.DebugContext(ctx, "desktop_heartbeat_tick", "due_count", len(rows))
	if len(rows) > 0 {
		slog.InfoContext(ctx, "desktop_heartbeat_claimed", "count", len(rows))
	}
	for _, row := range rows {
		ctxData, err := repo.ResolveHeartbeatThread(ctx, db, row)
		if err != nil {
			if errors.Is(err, data.ErrHeartbeatIdentityGone) {
				slog.WarnContext(ctx, "desktop_heartbeat_stale_trigger_removed",
					"channel_identity_id", row.ChannelIdentityID.String(),
				)
				_ = repo.DeleteHeartbeat(ctx, db, row.ChannelIdentityID)
			} else {
				slog.ErrorContext(ctx, "desktop_heartbeat_thread_resolution_failed",
					"channel_identity_id", row.ChannelIdentityID.String(),
					"error", err,
				)
				_ = repo.PostponeHeartbeat(ctx, db, row.ID, 2*time.Minute)
			}
			continue
		}

		tx, err := db.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			slog.ErrorContext(ctx, "desktop_heartbeat_tx_begin_failed", "error", err)
			_ = repo.PostponeHeartbeat(ctx, db, row.ID, 2*time.Minute)
			continue
		}

		result, err := repo.InsertHeartbeatRunInTx(ctx, tx, row, ctxData, row.Model)
		if err != nil {
			_ = tx.Rollback(ctx)
			if errors.Is(err, data.ErrThreadBusy) {
				delayMin := row.IntervalMin
				if delayMin <= 0 {
					delayMin = runkind.DefaultHeartbeatIntervalMinutes
				}
				_ = repo.PostponeHeartbeat(ctx, db, row.ID, time.Duration(delayMin)*time.Minute)
				continue
			}
			if errors.Is(err, data.ErrHeartbeatIdentityGone) {
				slog.WarnContext(ctx, "desktop_heartbeat_stale_trigger_removed",
					"channel_identity_id", row.ChannelIdentityID.String(),
				)
				_ = repo.DeleteHeartbeat(ctx, db, row.ChannelIdentityID)
				continue
			}
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
			"channel_delivery": map[string]any{
				"channel_id":                 result.ChannelID,
				"channel_type":               result.ChannelType,
				"sender_channel_identity_id": result.IdentityID,
				"conversation_type":          result.ConversationType,
				"conversation_ref": map[string]any{
					"target": result.PlatformChatID,
				},
			},
		}
		traceID := uuid.NewString()
		if _, err := q.EnqueueRun(ctx, row.AccountID, result.RunID, traceID, queue.RunExecuteJobType, payload, nil); err != nil {
			slog.ErrorContext(ctx, "desktop_heartbeat_enqueue_failed",
				"run_id", result.RunID.String(),
				"error", err,
			)
			_ = tx.Rollback(ctx)
			_ = repo.PostponeHeartbeat(ctx, db, row.ID, 2*time.Minute)
			continue
		}

		if err := tx.Commit(ctx); err != nil {
			slog.ErrorContext(ctx, "desktop_heartbeat_commit_failed", "error", err)
			_ = repo.PostponeHeartbeat(ctx, db, row.ID, 2*time.Minute)
			continue
		}
	}
}
