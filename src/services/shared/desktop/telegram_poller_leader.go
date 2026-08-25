package desktop

import "sync/atomic"

var telegramDesktopPollLeader uint32

// TryAcquireTelegramDesktopPollLeader 进程内只允许一处 getUpdates 长轮询（API 与 Worker 互斥）。
func TryAcquireTelegramDesktopPollLeader() bool {
	return atomic.CompareAndSwapUint32(&telegramDesktopPollLeader, 0, 1)
}
