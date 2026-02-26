package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
				rc.Emitter.Emit("run.cancelled", map[string]any{}, nil, nil), nil, rc.BroadcastRDB)
		}

		terminalType, err := readLatestEventType(ctx, pool, eventsRepo, run.ID, terminalEventTypes)
		if err != nil {
			return err
		}
		if terminalType != "" {
			return nil
		}

		// LISTEN/NOTIFY 桥接：直连 pool 由 Execute 保证非 nil
		listenConn, err := rc.DirectPool.Acquire(ctx)
		if err != nil {
			return err
		}
		channel := fmt.Sprintf(`"run_cancel_%s"`, run.ID.String())
		if _, err := listenConn.Exec(ctx, "LISTEN "+channel); err != nil {
			listenConn.Release()
			return err
		}
		execCtx, cancelExec := context.WithCancel(ctx)
		listenDone := make(chan struct{})
		go func() {
			defer func() {
				listenConn.Release()
				close(listenDone)
			}()
			if err := listenConn.Conn().PgConn().WaitForNotification(execCtx); err == nil {
				cancelExec()
			}
		}()

		rc.CancelFunc = cancelExec
		rc.ListenDone = listenDone

		// 设置 WaitForInput：LISTEN run_input_{runID}，收到通知后查 DB 拿内容
		inputCh := make(chan string, 1)
		if inputConn, err := rc.DirectPool.Acquire(execCtx); err == nil {
			inputChannel := fmt.Sprintf(`"run_input_%s"`, run.ID.String())
			if _, err := inputConn.Exec(execCtx, "LISTEN "+inputChannel); err != nil {
				inputConn.Release()
			} else {
				go func() {
					defer inputConn.Release()
					var lastSeq int64
					for {
						if err := inputConn.Conn().PgConn().WaitForNotification(execCtx); err != nil {
							return
						}
						content, seq, ok := fetchLatestInput(execCtx, pool, run.ID, lastSeq)
						if !ok {
							continue
						}
						lastSeq = seq
						select {
						case inputCh <- content:
						case <-execCtx.Done():
							return
						}
					}
				}()
			}
		}

		rc.WaitForInput = func(ctx context.Context) (string, bool) {
			select {
			case text := <-inputCh:
				return text, true
			case <-ctx.Done():
				return "", false
			}
		}

		// 确保 Pipeline 结束后释放 LISTEN 连接
		defer func() {
			cancelExec()
			<-listenDone
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
) error {
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
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	channel := fmt.Sprintf("run_events:%s", run.ID.String())
	_, _ = pool.Exec(ctx, "SELECT pg_notify($1, '')", channel)

	if rdb != nil {
		redisChannel := fmt.Sprintf("arkloop:sse:run_events:%s", run.ID.String())
		_, _ = rdb.Publish(ctx, redisChannel, "").Result()
	}

	if _, ok := TerminalStatuses[ev.Type]; ok && releaseSlot != nil {
		releaseSlot()
	}

	if rdb != nil {
		if termStatus, ok := TerminalStatuses[ev.Type]; ok {
			ch := fmt.Sprintf("run.child.%s.done", run.ID.String())
			_, _ = rdb.Publish(ctx, ch, termStatus+"\n").Result()
		}
	}

	return nil
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
