package api

import (
	"context"

	"arkloop/services/api/internal/app"
)

// StartDesktop 启动桌面模式 API 服务。阻塞直到 ctx 取消或出错。
func StartDesktop(ctx context.Context) error {
	return app.RunDesktop(ctx)
}

// StartDesktopTelegramPollWorker 尝试启动 Telegram getUpdates；pool 为 nil 时使用 GetSharedSQLitePool。与 API 进程互斥。
func StartDesktopTelegramPollWorker(ctx context.Context, pool any) error {
	return app.StartTelegramDesktopPollWorker(ctx, pool)
}
