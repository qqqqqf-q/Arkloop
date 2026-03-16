//go:build desktop

package worker

import (
	"context"

	"arkloop/services/shared/desktop"
	"arkloop/services/shared/eventbus"
	"arkloop/services/worker/internal/consumer"
	"arkloop/services/worker/internal/desktoprun"
	"arkloop/services/worker/internal/queue"
)

// InitDesktopInfra 创建共享的 job queue 和 event bus，注册到全局状态。
// 在 API 和 Worker 启动之前调用，避免 SQLite 锁竞争。
func InitDesktopInfra() error {
	bus := eventbus.NewLocalEventBus()
	desktop.SetEventBus(bus)

	localNotifier := consumer.NewLocalNotifier()
	cq, err := queue.NewChannelJobQueue(25, localNotifier.Notify)
	if err != nil {
		return err
	}
	desktop.SetJobEnqueuer(cq)
	desktop.MarkReady()

	return nil
}

// StartDesktop 启动桌面模式 Worker 消费循环。阻塞直到 ctx 取消或出错。
func StartDesktop(ctx context.Context) error {
	return desktoprun.RunDesktop(ctx)
}
