//go:build desktop

// Package desktop 提供桌面模式下跨模块共享资源的进程级全局状态。
// API 和 Worker 在同一进程内运行时，通过此包共享 JobQueue 和 EventBus。
package desktop

import (
	"context"
	"strings"
	"sync"
	"time"

	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
)

// JobEnqueuer 定义将作业投递到 Worker 内存队列的最小接口。
// worker/internal/queue.ChannelJobQueue 隐式满足此接口。
type JobEnqueuer interface {
	EnqueueRun(
		ctx context.Context,
		accountID uuid.UUID,
		runID uuid.UUID,
		traceID string,
		queueJobType string,
		payload map[string]any,
		availableAt *time.Time,
	) (uuid.UUID, error)
	// EnqueueRunWithID 与 EnqueueRun 相同，但允许调用方指定 jobID，
	// 以便在异步提交后调用方与队列持有相同的 ID。
	EnqueueRunWithID(
		ctx context.Context,
		jobID uuid.UUID,
		accountID uuid.UUID,
		runID uuid.UUID,
		traceID string,
		queueJobType string,
		payload map[string]any,
		availableAt *time.Time,
	) (uuid.UUID, error)
	HasActiveRun(ctx context.Context, runID uuid.UUID, queueJobType string) (bool, error)
}

var (
	mu            sync.Mutex
	jobEnqueuer   JobEnqueuer
	eventBus      any
	workNotifier  any
	sandboxAddr   string
	executionMode string
	memoryRuntime string
	ready         chan struct{}
	apiReady      chan struct{}

	sharedSQLitePool *sqlitepgx.Pool
)

func init() {
	ready = make(chan struct{})
	apiReady = make(chan struct{})
}

func SetJobEnqueuer(q JobEnqueuer) { mu.Lock(); jobEnqueuer = q; mu.Unlock() }
func GetJobEnqueuer() JobEnqueuer  { mu.Lock(); defer mu.Unlock(); return jobEnqueuer }

func SetEventBus(b any) { mu.Lock(); eventBus = b; mu.Unlock() }
func GetEventBus() any  { mu.Lock(); defer mu.Unlock(); return eventBus }

func SetWorkNotifier(n any) { mu.Lock(); workNotifier = n; mu.Unlock() }
func GetWorkNotifier() any  { mu.Lock(); defer mu.Unlock(); return workNotifier }

func SetSandboxAddr(addr string) { mu.Lock(); sandboxAddr = addr; mu.Unlock() }
func GetSandboxAddr() string     { mu.Lock(); defer mu.Unlock(); return sandboxAddr }

func SetExecutionMode(mode string) { mu.Lock(); executionMode = strings.TrimSpace(mode); mu.Unlock() }
func GetExecutionMode() string     { mu.Lock(); defer mu.Unlock(); return strings.TrimSpace(executionMode) }

func SetMemoryRuntime(mode string) { mu.Lock(); memoryRuntime = strings.TrimSpace(mode); mu.Unlock() }
func GetMemoryRuntime() string     { mu.Lock(); defer mu.Unlock(); return strings.TrimSpace(memoryRuntime) }

// MarkReady 由 Worker 在共享资源初始化完成后调用，通知等待方可以继续。
func MarkReady() {
	select {
	case <-ready:
	default:
		close(ready)
	}
}

// WaitReady 阻塞直到 MarkReady 被调用或 ctx 超时。
func WaitReady(ctx context.Context) error {
	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// MarkAPIReady 由 API 在 migration + seed + HTTP listener 就绪后调用。
func MarkAPIReady() {
	select {
	case <-apiReady:
	default:
		close(apiReady)
	}
}

// WaitAPIReady 阻塞直到 MarkAPIReady 被调用或 ctx 超时。
func WaitAPIReady(ctx context.Context) error {
	select {
	case <-apiReady:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SetSharedSQLitePool 由侧car API 在 migration 后注入，Worker 与同进程共用同一 *sql.DB。
// 仅 API RunDesktop 的 defer 可触发最终 Close 底层连接。
func SetSharedSQLitePool(p *sqlitepgx.Pool) {
	mu.Lock()
	defer mu.Unlock()
	sharedSQLitePool = p
}

func GetSharedSQLitePool() *sqlitepgx.Pool {
	mu.Lock()
	defer mu.Unlock()
	return sharedSQLitePool
}

func ClearSharedSQLitePool() {
	mu.Lock()
	defer mu.Unlock()
	sharedSQLitePool = nil
}

// SetSharedSQLiteWriteExecutor 设置 desktop 全局 SQLite 单写执行器。
// 传入 nil 时恢复 sqlitepgx 默认串行执行器。
func SetSharedSQLiteWriteExecutor(executor sqlitepgx.WriteExecutor) {
	sqlitepgx.SetGlobalWriteExecutor(executor)
}

// GetSharedSQLiteWriteExecutor 返回当前 desktop 全局 SQLite 写执行器。
func GetSharedSQLiteWriteExecutor() sqlitepgx.WriteExecutor {
	return sqlitepgx.GetGlobalWriteExecutor()
}
