package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/eventbus"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

var (
	cancelEventTypes   = []string{"run.cancel_requested", "run.cancelled"}
	terminalEventTypes = []string{"run.completed", "run.failed", "run.cancelled"}
)

// NewCancelGuardMiddleware 检查 run 是否已取消或终态，
// 并设置 LISTEN/NOTIFY 取消信号桥接到 context。
func NewCancelGuardMiddleware(
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	hub *RunControlHub,
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		pool := rc.Pool
		run := rc.Run

		cancelType, err := readLatestEventType(ctx, pool, eventsRepo, run.ID, cancelEventTypes)
		if err != nil {
			return err
		}
		if cancelType == "run.cancelled" {
			return nil
		}
		if cancelType == "run.cancel_requested" {
			return appendAndCommitSingle(ctx, pool, run, runsRepo, eventsRepo,
				rc.Emitter.Emit("run.cancelled", map[string]any{}, nil, nil), rc.ReleaseSlot, rc.BroadcastRDB, rc.EventBus)
		}

		terminalType, err := readLatestEventType(ctx, pool, eventsRepo, run.ID, terminalEventTypes)
		if err != nil {
			return err
		}
		if terminalType != "" {
			return nil
		}

		execCtx, cancelExec := context.WithCancel(ctx)

		cancelWake := (<-chan struct{})(nil)
		inputWake := (<-chan struct{})(nil)
		unsubscribe := func() {}
		if hub != nil {
			var unsubscribeFn func()
			cancelWake, inputWake, unsubscribeFn = hub.Subscribe(run.ID)
			unsubscribe = unsubscribeFn
		}

		listenDone := make(chan struct{})
		go func() {
			defer close(listenDone)
			select {
			case <-cancelWake:
				cancelExec()
			case <-execCtx.Done():
			}
		}()

		rc.CancelFunc = cancelExec
		rc.ListenDone = listenDone

		var lastSeq int64
		rc.WaitForInput = func(ctx context.Context) (string, bool) {
			for {
				content, seq, ok := fetchLatestInput(ctx, pool, run.ID, lastSeq)
				if ok {
					lastSeq = seq
					return content, true
				}

				timer := time.NewTimer(2 * time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return "", false
				case <-inputWake:
					timer.Stop()
				case <-timer.C:
				}
			}
		}

		// 确保 Pipeline 结束后释放 LISTEN 连接
		defer func() {
			cancelExec()
			<-listenDone
			unsubscribe()
		}()

		return next(execCtx, rc)
	}
}

func readLatestEventType(
	ctx context.Context,
	pool *pgxpool.Pool,
	eventsRepo data.RunEventsRepository,
	runID uuid.UUID,
	types []string,
) (string, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	return eventsRepo.GetLatestEventType(ctx, tx, runID, types)
}

// appendAndCommitSingle 写入单个事件并提交，用于短路场景。
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
	defer tx.Rollback(ctx)

	if _, err := eventsRepo.AppendEvent(ctx, tx, run.ID, ev.Type, ev.DataJSON, ev.ToolName, ev.ErrorClass); err != nil {
		return err
	}

	if status, ok := TerminalStatuses[ev.Type]; ok {
		if err := runsRepo.UpdateRunTerminalStatus(ctx, tx, run.ID, data.TerminalStatusUpdate{
			Status: status,
		}); err != nil {
			return err
		}

		// 同步 sub_agents 终态，避免 wait_agent 永久轮询
		subAgent, err := (data.SubAgentRepository{}).GetByCurrentRunID(ctx, tx, run.ID)
		if err != nil {
			return err
		}
		if subAgent != nil {
			var lastError *string
			if msg := terminalStatusMessage(ev.DataJSON); msg != "" {
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

	channel := fmt.Sprintf("run_events:%s", run.ID.String())
	if bus != nil {
		_ = bus.Publish(ctx, channel, "")
	} else {
		_, _ = pool.Exec(ctx, "SELECT pg_notify($1, '')", channel)
	}

	if rdb != nil {
		redisChannel := fmt.Sprintf("arkloop:sse:run_events:%s", run.ID.String())
		_, _ = rdb.Publish(ctx, redisChannel, "").Result()
	}

	// Success path: release now and nil out so defer does not double-call.
	if _, ok := TerminalStatuses[ev.Type]; ok && releaseSlot != nil {
		releaseSlot()
		releaseSlot = nil
	}

	if rdb != nil {
		if termStatus, ok := TerminalStatuses[ev.Type]; ok {
			payload := truncateChildRunPayload(terminalStatusMessage(ev.DataJSON))
			ch := fmt.Sprintf("run.child.%s.done", run.ID.String())
			_, _ = rdb.Publish(ctx, ch, termStatus+"\n"+payload).Result()
		}
	}

	return nil
}

func terminalStatusMessage(dataJSON map[string]any) string {
	if dataJSON == nil {
		return ""
	}
	message, _ := dataJSON["message"].(string)
	return strings.TrimSpace(message)
}

func truncateChildRunPayload(raw string) string {
	if len(raw) <= maxChildRunOutputBytes {
		return raw
	}
	return raw[:maxChildRunOutputBytes]
}

// TerminalStatuses 映射终态事件类型到 runs.status 值。
var TerminalStatuses = map[string]string{
	"run.completed": "completed",
	"run.failed":    "failed",
	"run.cancelled": "cancelled",
}

// fetchLatestInput 查询 run_events 中 seq > sinceSeq 的最新 run.input_provided 事件。
// 返回 (content, seq, true) 或 ("", 0, false)。
func fetchLatestInput(ctx context.Context, pool *pgxpool.Pool, runID uuid.UUID, sinceSeq int64) (string, int64, bool) {
	var rawJSON []byte
	var seq int64
	err := pool.QueryRow(
		ctx,
		`SELECT data_json, seq
		 FROM run_events
		 WHERE run_id = $1
		   AND type = $2
		   AND seq > $3
		 ORDER BY seq ASC
		 LIMIT 1`,
		runID,
		EventTypeInputProvided,
		sinceSeq,
	).Scan(&rawJSON, &seq)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", 0, false
		}
		return "", 0, false
	}

	var payload map[string]any
	if err := json.Unmarshal(rawJSON, &payload); err != nil {
		return "", 0, false
	}
	content, _ := payload["content"].(string)
	return content, seq, true
}
