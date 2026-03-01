package runengine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// childThreadTTL 是子 Run 独立临时线程的自动过期时长。
const childThreadTTL = 7 * 24 * time.Hour

func newSpawnChildRunFunc(pool *pgxpool.Pool, rdb *redis.Client, jobQueue queue.JobQueue, parentRun data.Run, traceID string) func(ctx context.Context, personaID string, input string) (string, error) {
	return func(ctx context.Context, personaID string, input string) (string, error) {
		return spawnChildRun(ctx, pool, rdb, jobQueue, parentRun, traceID, personaID, input)
	}
}

func spawnChildRun(
	ctx context.Context,
	pool *pgxpool.Pool,
	rdb *redis.Client,
	jobQueue queue.JobQueue,
	parentRun data.Run,
	traceID string,
	personaID string,
	input string,
) (string, error) {
	childRunID := uuid.New()
	childChannel := fmt.Sprintf("run.child.%s.done", childRunID.String())

	// 先订阅再创建子 Run，确保不会错过完成信号
	pubsub := rdb.Subscribe(ctx, childChannel)
	defer pubsub.Close()

	// 等待 Redis 确认订阅建立后再继续，避免竞态
	if _, err := pubsub.Receive(ctx); err != nil {
		return "", fmt.Errorf("subscribe child run channel: %w", err)
	}

	if err := createAndEnqueueChildRun(ctx, pool, rdb, jobQueue, childRunID, parentRun, traceID, personaID, input); err != nil {
		return "", fmt.Errorf("create child run: %w", err)
	}

	msgCh := pubsub.Channel()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case msg := <-msgCh:
		return parseChildRunResult(msg.Payload)
	}
}

// createAndEnqueueChildRun 在事务中创建独立临时线程、用户消息、子 Run 和启动事件，
// 然后向 job queue 投递执行任务。
func createAndEnqueueChildRun(
	ctx context.Context,
	pool *pgxpool.Pool,
	rdb *redis.Client,
	jobQueue queue.JobQueue,
	childRunID uuid.UUID,
	parentRun data.Run,
	traceID string,
	personaID string,
	input string,
) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// 创建独立临时线程，避免污染父 Run 的 thread 历史
	var childThreadID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO threads (org_id, is_private, expires_at)
		 VALUES ($1, TRUE, now() + make_interval(secs => $2))
		 RETURNING id`,
		parentRun.OrgID,
		int64(childThreadTTL.Seconds()),
	).Scan(&childThreadID); err != nil {
		return fmt.Errorf("create child thread: %w", err)
	}

	// 插入子 Run 的用户输入消息
	if _, err := tx.Exec(ctx,
		`INSERT INTO messages (org_id, thread_id, role, content)
		 VALUES ($1, $2, 'user', $3)`,
		parentRun.OrgID,
		childThreadID,
		input,
	); err != nil {
		return fmt.Errorf("insert child message: %w", err)
	}

	// 创建子 Run（继承父 Run 的 org，指向独立临时线程）
	if _, err := tx.Exec(ctx,
		`INSERT INTO runs (id, org_id, thread_id, parent_run_id, status)
		 VALUES ($1, $2, $3, $4, 'running')`,
		childRunID,
		parentRun.OrgID,
		childThreadID,
		parentRun.ID,
	); err != nil {
		return fmt.Errorf("insert child run: %w", err)
	}

	// 分配 seq 并插入 run.started 事件（携带 persona_id，供 InputLoaderMiddleware 解析）
	var seq int64
	if err := tx.QueryRow(ctx, `SELECT nextval('run_events_seq_global')`).Scan(&seq); err != nil {
		return fmt.Errorf("alloc seq: %w", err)
	}
	eventData, err := json.Marshal(map[string]any{"persona_id": personaID})
	if err != nil {
		return fmt.Errorf("marshal run.started data: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json)
		 VALUES ($1, $2, 'run.started', $3::jsonb)`,
		childRunID, seq, string(eventData),
	); err != nil {
		return fmt.Errorf("insert run.started: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// 事务提交后投递 job（job queue 使用独立连接池，不需要在同一事务中）
	_, enqueueErr := jobQueue.EnqueueRun(ctx, parentRun.OrgID, childRunID, traceID, queue.RunExecuteJobType, map[string]any{}, nil)
	if enqueueErr != nil {
		// 入队失败：子 Run 已持久化但无 worker 处理。
		// best-effort 标记为 failed 并通知父 Run，避免父 Run 永久等待 ctx 超时。
		markChildRunFailed(context.WithoutCancel(ctx), pool, rdb, childRunID)
		return fmt.Errorf("enqueue child run: %w", enqueueErr)
	}
	return nil
}

// markChildRunFailed 在入队失败后 best-effort 将子 Run 标记为 failed 并广播通知。
// 使用独立 context 避免调用方 ctx 已取消时操作失败。
func markChildRunFailed(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, childRunID uuid.UUID) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)

	var seq int64
	if err := tx.QueryRow(ctx, `SELECT nextval('run_events_seq_global')`).Scan(&seq); err != nil {
		return
	}
	errData, _ := json.Marshal(map[string]any{
		"error_class": "worker.enqueue_failed",
		"message":     "failed to enqueue child run job",
	})
	if _, err := tx.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json)
		 VALUES ($1, $2, 'run.failed', $3::jsonb)`,
		childRunID, seq, string(errData),
	); err != nil {
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE runs SET status = 'failed', status_updated_at = now(), failed_at = now()
		 WHERE id = $1`,
		childRunID,
	); err != nil {
		return
	}
	if err := tx.Commit(ctx); err != nil {
		return
	}

	if rdb != nil {
		ch := fmt.Sprintf("run.child.%s.done", childRunID.String())
		_, _ = rdb.Publish(ctx, ch, "failed\n").Result()
	}
}

// parseChildRunResult 解析 Redis 消息格式 "status\noutput"。
func parseChildRunResult(payload string) (string, error) {
	idx := strings.Index(payload, "\n")
	if idx < 0 {
		return "", fmt.Errorf("malformed child run result payload")
	}
	status := payload[:idx]
	output := payload[idx+1:]
	if status != "completed" {
		return "", fmt.Errorf("child run ended with status: %s", status)
	}
	return output, nil
}
