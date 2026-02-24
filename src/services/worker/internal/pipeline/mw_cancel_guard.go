package pipeline

import (
	"context"
	"fmt"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
				rc.Emitter.Emit("run.cancelled", map[string]any{}, nil, nil), nil)
		}

		terminalType, err := readLatestEventType(ctx, pool, eventsRepo, run.ID, terminalEventTypes)
		if err != nil {
			return err
		}
		if terminalType != "" {
			return nil
		}

		// LISTEN/NOTIFY 桥接：优先用直连 pool 避免 PgBouncer transaction mode 断联
		listenPool := rc.DirectPool
		if listenPool == nil {
			listenPool = pool
		}
		listenConn, err := listenPool.Acquire(ctx)
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

	if _, ok := TerminalStatuses[ev.Type]; ok && releaseSlot != nil {
		releaseSlot()
	}

	return nil
}

// TerminalStatuses 映射终态事件类型到 runs.status 值。
var TerminalStatuses = map[string]string{
	"run.completed": "completed",
	"run.failed":    "failed",
	"run.cancelled": "cancelled",
}
