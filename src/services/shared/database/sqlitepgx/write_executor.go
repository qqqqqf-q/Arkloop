//go:build desktop

package sqlitepgx

import (
	"context"
	"sync"
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
		return &serialWriteGuard{
			release: func() {
				e.token <- struct{}{}
			},
		}, nil
	}
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
