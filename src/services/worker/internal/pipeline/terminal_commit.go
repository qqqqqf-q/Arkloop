package pipeline

import (
	"context"
	"fmt"
	"strings"

	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/threadrunstate"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// appendAndCommitSingle 写入单个事件并提交,用于短路场景。
func appendAndCommitSingle(
	ctx context.Context,
	pool *pgxpool.Pool,
	run data.Run,
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	ev events.RunEvent,
	releaseSlot func(),
	rdb *redis.Client,
	bus eventbus.EventBus,
) error {
	// For terminal events, guarantee slot release on all exit paths (including errors).
	if _, ok := TerminalStatuses[ev.Type]; ok && releaseSlot != nil {
		defer func() {
			if releaseSlot != nil {
				releaseSlot()
			}
		}()
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := eventsRepo.AppendRunEvent(ctx, tx, run.ID, ev); err != nil {
		return err
	}

	if status, ok := TerminalStatuses[ev.Type]; ok {
		if err := runsRepo.UpdateRunTerminalStatus(ctx, tx, run.ID, data.TerminalStatusUpdate{
			Status: status,
		}); err != nil {
			return err
		}

		// 同步 sub_agents 终态,避免 wait_agent 永久轮询
		subAgent, err := (data.SubAgentRepository{}).GetByCurrentRunID(ctx, tx, run.ID)
		if err != nil {
			return err
		}
		if subAgent != nil {
			var lastError *string
			if msg := TerminalStatusMessage(ev.DataJSON); msg != "" {
				lastError = &msg
			}
			if err := (data.SubAgentRepository{}).TransitionToTerminal(ctx, tx, run.ID, status, lastError); err != nil {
				return err
			}
			eventType, err := data.SubAgentTerminalEventType(status)
			if err != nil {
				return err
			}
			if _, err := (data.SubAgentEventAppender{}).Append(ctx, tx, subAgent.ID, &run.ID, eventType, ev.DataJSON, ev.ErrorClass); err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if bus != nil {
		channel := fmt.Sprintf("run_events:%s", run.ID.String())
		_ = bus.Publish(ctx, channel, "")
	}

	if rdb != nil {
		redisChannel := fmt.Sprintf("arkloop:sse:run_events:%s", run.ID.String())
		_, _ = rdb.Publish(ctx, redisChannel, "").Result()
	}

	if _, ok := TerminalStatuses[ev.Type]; ok {
		threadrunstate.Publish(ctx, bus, run.AccountID, run.ThreadID)
	}

	// Success path: release now and nil out so defer does not double-call.
	if _, ok := TerminalStatuses[ev.Type]; ok && releaseSlot != nil {
		releaseSlot()
		releaseSlot = nil
	}

	if rdb != nil {
		if termStatus, ok := TerminalStatuses[ev.Type]; ok {
			payload := truncateChildRunPayload(TerminalStatusMessage(ev.DataJSON))
			ch := fmt.Sprintf("run.child.%s.done", run.ID.String())
			_, _ = rdb.Publish(ctx, ch, termStatus+"\n"+payload).Result()
		}
	}

	return nil
}

// TerminalStatusMessage 从终态事件 data_json 提取对用户可读的摘要(Channel、子 run 等共用)。
func TerminalStatusMessage(dataJSON map[string]any) string {
	if dataJSON == nil {
		return ""
	}
	details, _ := dataJSON["details"].(map[string]any)
	main := ""
	if details != nil {
		if pm, _ := details["provider_message"].(string); strings.TrimSpace(pm) != "" {
			main = strings.TrimSpace(pm)
		}
	}
	if main == "" {
		if msg, _ := dataJSON["message"].(string); strings.TrimSpace(msg) != "" {
			main = strings.TrimSpace(msg)
		}
	}
	if main == "" {
		return ""
	}
	if details != nil {
		if t, _ := details["type"].(string); strings.TrimSpace(t) != "" {
			t = strings.TrimSpace(t)
			if !strings.Contains(strings.ToLower(main), strings.ToLower(t)) {
				main = main + " (" + t + ")"
			}
		}
	}
	return main
}

func truncateChildRunPayload(raw string) string {
	if len(raw) <= maxChildRunOutputBytes {
		return raw
	}
	return raw[:maxChildRunOutputBytes]
}

// TerminalStatuses 映射终态事件类型到 runs.status 值。
var TerminalStatuses = map[string]string{
	"run.completed":   "completed",
	"run.failed":      "failed",
	"run.interrupted": "interrupted",
	"run.cancelled":   "cancelled",
}
