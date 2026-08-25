package sqlitepgx

import (
	"context"
	"sync"
	"time"
)

// WriteGuard 表示一次写执行租约；调用方必须在完成后 Release。
type WriteGuard interface {
	Release()
}

// WriteExecutor 负责在 desktop 进程内协调 SQLite 写入并发。
type WriteExecutor interface {
	AcquireWrite(ctx context.Context) (WriteGuard, error)
}

type serialWriteGuard struct {
	once    sync.Once
	release func()
}

func (g *serialWriteGuard) Release() {
	if g == nil {
		return
	}
	g.once.Do(func() {
		if g.release != nil {
			g.release()
		}
	})
}

// SerialWriteExecutor 提供进程级单写执行能力。
type SerialWriteExecutor struct {
	token chan struct{}

	mu        sync.Mutex
	heldSince time.Time
	heldSeq   uint64
}

func NewSerialWriteExecutor() *SerialWriteExecutor {
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	return &SerialWriteExecutor{token: ch}
}

func (e *SerialWriteExecutor) AcquireWrite(ctx context.Context) (WriteGuard, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.token:
		e.mu.Lock()
		e.heldSeq++
		e.heldSince = time.Now()
		e.mu.Unlock()
		return &serialWriteGuard{
			release: func() {
				e.mu.Lock()
				e.heldSince = time.Time{}
				e.mu.Unlock()
				e.token <- struct{}{}
			},
		}, nil
	}
}

// HoldStatus 返回写令牌当前是否被持有、本次持有序号与已持有时长，供诊断监控定位长时间持锁者。
func (e *SerialWriteExecutor) HoldStatus() (held bool, seq uint64, dur time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.heldSince.IsZero() {
		return false, e.heldSeq, 0
	}
	return true, e.heldSeq, time.Since(e.heldSince)
}

type noopWriteGuard struct{}

func (noopWriteGuard) Release() {}

var (
	globalWriteExecutorMu sync.RWMutex
	globalWriteExecutor   WriteExecutor = NewSerialWriteExecutor()
)

// SetGlobalWriteExecutor 设置 desktop 全局写执行器；传入 nil 时恢复默认串行执行器。
func SetGlobalWriteExecutor(executor WriteExecutor) {
	if executor == nil {
		executor = NewSerialWriteExecutor()
	}
	globalWriteExecutorMu.Lock()
	globalWriteExecutor = executor
	globalWriteExecutorMu.Unlock()
}

// GetGlobalWriteExecutor 返回当前 desktop 全局写执行器。
func GetGlobalWriteExecutor() WriteExecutor {
	globalWriteExecutorMu.RLock()
	defer globalWriteExecutorMu.RUnlock()
	return globalWriteExecutor
}
