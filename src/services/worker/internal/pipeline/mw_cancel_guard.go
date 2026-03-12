package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/database"
	"arkloop/services/shared/eventbus"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"

	"github.com/google/uuid"
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
	bus eventbus.EventBus,
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		db := rc.DB
		run := rc.Run

		cancelType, err := readLatestEventType(ctx, db, eventsRepo, run.ID, cancelEventTypes)
		if err != nil {
			return err
		}
		if cancelType == "run.cancelled" {
			return nil
		}
		if cancelType == "run.cancel_requested" {
			return appendAndCommitSingle(ctx, db, run, runsRepo, eventsRepo,
				rc.Emitter.Emit("run.cancelled", map[string]any{}, nil, nil), nil, bus)
		}

		terminalType, err := readLatestEventType(ctx, db, eventsRepo, run.ID, terminalEventTypes)
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
				content, seq, ok := fetchLatestInput(ctx, db, run.ID, lastSeq)
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
	db database.DB,
	eventsRepo data.RunEventsRepository,
	runID uuid.UUID,
	types []string,
) (string, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	return eventsRepo.GetLatestEventType(ctx, tx, runID, types)
}

// appendAndCommitSingle 写入单个事件并提交，用于短路场景。
func appendAndCommitSingle(
	ctx context.Context,
	db database.DB,
	run data.Run,
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	ev events.RunEvent,
	releaseSlot func(),
	bus eventbus.EventBus,
) error {
	tx, err := db.Begin(ctx)
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
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	channel := fmt.Sprintf("run_events:%s", run.ID.String())
	_, _ = db.Exec(ctx, "SELECT pg_notify($1, '')", channel)

	if bus != nil {
		redisChannel := fmt.Sprintf("arkloop:sse:run_events:%s", run.ID.String())
		_ = bus.Publish(ctx, redisChannel, "")
	}

	if _, ok := TerminalStatuses[ev.Type]; ok && releaseSlot != nil {
		releaseSlot()
	}

	if bus != nil {
		if termStatus, ok := TerminalStatuses[ev.Type]; ok {
			payload := truncateChildRunPayload(terminalStatusMessage(ev.DataJSON))
			ch := fmt.Sprintf("run.child.%s.done", run.ID.String())
			_ = bus.Publish(ctx, ch, termStatus+"\n"+payload)
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
func fetchLatestInput(ctx context.Context, db database.DB, runID uuid.UUID, sinceSeq int64) (string, int64, bool) {
	var rawJSON []byte
	var seq int64
	err := db.QueryRow(
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
		if errors.Is(err, database.ErrNoRows) {
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
