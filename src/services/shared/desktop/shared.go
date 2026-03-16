//go:build desktop

// Package desktop 提供桌面模式下跨模块共享资源的进程级全局状态。
// API 和 Worker 在同一进程内运行时，通过此包共享 JobQueue 和 EventBus。
package desktop

import (
	"context"
	"sync"
	"time"

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
}

var (
	mu          sync.Mutex
	jobEnqueuer JobEnqueuer
	eventBus    any
	sandboxAddr string
	ready       chan struct{}
	apiReady    chan struct{}
)

func init() {
	ready = make(chan struct{})
	apiReady = make(chan struct{})
}

func SetJobEnqueuer(q JobEnqueuer) { mu.Lock(); jobEnqueuer = q; mu.Unlock() }
func GetJobEnqueuer() JobEnqueuer  { mu.Lock(); defer mu.Unlock(); return jobEnqueuer }

func SetEventBus(b any) { mu.Lock(); eventBus = b; mu.Unlock() }
func GetEventBus() any  { mu.Lock(); defer mu.Unlock(); return eventBus }

func SetSandboxAddr(addr string) { mu.Lock(); sandboxAddr = addr; mu.Unlock() }
func GetSandboxAddr() string     { mu.Lock(); defer mu.Unlock(); return sandboxAddr }

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
