//go:build desktop

package desktoprun

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const desktopHeartbeatReconnectDelay = 2 * time.Second

func startDesktopLLMHeartbeatScheduler(
	ctx context.Context,
	db data.DesktopDB,
	q queue.JobQueue,
	bus eventbus.EventBus,
) {
	if db == nil || q == nil {
		slog.WarnContext(ctx, "desktop_heartbeat_scheduler: db or queue is nil, skipping")
		return
	}

	wakeCh := make(chan struct{}, 1)
	if bus != nil {
		go listenDesktopHeartbeatWake(ctx, bus, wakeCh)
	}

	for {
		dueAt, err := data.ScheduledTriggersRepository{}.GetEarliestHeartbeatDue(ctx, db)
		if err != nil {
			slog.ErrorContext(ctx, "desktop_heartbeat_due_lookup_failed", "error", err)
			if !waitForDesktopHeartbeatWake(ctx, wakeCh, desktopHeartbeatReconnectDelay) {
				return
			}
			continue
		}
		if dueAt == nil {
			slog.DebugContext(ctx, "desktop_heartbeat_waiting", "state", "idle")
			select {
			case <-ctx.Done():
				return
			case <-wakeCh:
				continue
			}
		}

		delay := time.Until(dueAt.UTC())
		if delay < 0 {
			delay = 0
		}
		slog.DebugContext(ctx, "desktop_heartbeat_timer_armed", "next_fire_at", dueAt.UTC(), "delay", delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-wakeCh:
			if !timer.Stop() {
				<-timer.C
			}
			continue
		case <-timer.C:
		}

		desktopHeartbeatTick(ctx, db, q)
	}
}

func listenDesktopHeartbeatWake(ctx context.Context, bus eventbus.EventBus, wakeCh chan struct{}) {
	for {
		sub, err := bus.Subscribe(ctx, pgnotify.ChannelHeartbeat)
		if err != nil {
			if !waitForDesktopHeartbeatWake(ctx, wakeCh, desktopHeartbeatReconnectDelay) {
				return
			}
			continue
		}
		for {
			select {
			case <-ctx.Done():
				_ = sub.Close()
				return
			case _, ok := <-sub.Channel():
				if !ok {
					_ = sub.Close()
					if !waitForDesktopHeartbeatWake(ctx, wakeCh, desktopHeartbeatReconnectDelay) {
						return
					}
					goto retry
				}
				signalDesktopHeartbeatWake(wakeCh)
			}
		}
	retry:
	}
}

func signalDesktopHeartbeatWake(wakeCh chan<- struct{}) {
	select {
	case wakeCh <- struct{}{}:
	default:
	}
}

func waitForDesktopHeartbeatWake(ctx context.Context, wakeCh <-chan struct{}, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-wakeCh:
		return true
	case <-timer.C:
		return true
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
		slog.InfoContext(ctx, "desktop_heartbeat_due_claimed", "count", len(rows))
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

		if err := tx.Commit(ctx); err != nil {
			slog.ErrorContext(ctx, "desktop_heartbeat_commit_failed", "error", err)
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
			_ = repo.PostponeHeartbeat(ctx, db, row.ID, 2*time.Minute)
			continue
		}
	}
}
