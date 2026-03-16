//go:build desktop

package api

import (
	"context"

	"arkloop/services/api/internal/app"
)

// StartDesktop 启动桌面模式 API 服务。阻塞直到 ctx 取消或出错。
func StartDesktop(ctx context.Context) error {
	return app.RunDesktop(ctx)
}
