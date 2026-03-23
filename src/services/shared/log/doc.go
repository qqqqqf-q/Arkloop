// Package log 提供统一的日志接口，支持彩色终端输出和 JSON 格式。
//
// 使用方式：
//
//	logger := log.New(log.Config{Component: "api", Level: slog.LevelDebug})
//	logger.Info("http request", "method", "GET", "path", "/healthz")
//
// 环境变量：
//   - ARKLOOP_LOG_MODE: pretty（默认）| json
//   - ARKLOOP_LOG_LEVEL: debug | info | warn | error
package log
