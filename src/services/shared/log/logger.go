package log

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Config 日志配置
type Config struct {
	Component string      // 组件名：api, worker, gateway, bridge, sandbox
	Level     slog.Level // 日志级别，默认 slog.LevelInfo
	Output    io.Writer  // 输出目标，默认 os.Stderr
	Async     bool        // 是否异步写
	BufSize   int         // 异步缓冲大小，默认 4096
}

var (
	defaultLevel = slog.LevelWarn
	logMode     = "pretty"
)

func init() {
	if level := os.Getenv("ARKLOOP_LOG_LEVEL"); level != "" {
		switch strings.ToLower(level) {
		case "debug":
			defaultLevel = slog.LevelDebug
		case "warn", "warning":
			defaultLevel = slog.LevelWarn
		case "error":
			defaultLevel = slog.LevelError
		}
	}
	if mode := os.Getenv("ARKLOOP_LOG_MODE"); mode != "" {
		logMode = strings.ToLower(mode)
	}
}

// Logger 封装 slog.Logger，提供组件化日志能力
type Logger struct {
	*slog.Logger
	component string
	writer    io.Writer
}

// New 创建 Logger 实例，返回 *slog.Logger 以便于兼容
func New(cfg Config) *slog.Logger {
	if cfg.Level == 0 {
		cfg.Level = defaultLevel
	}
	if cfg.Output == nil {
		cfg.Output = os.Stderr
	}
	if cfg.BufSize == 0 {
		cfg.BufSize = 4096
	}
	if cfg.Component == "" {
		cfg.Component = "app"
	}

	var handler slog.Handler
	if logMode == "json" {
		handler = newJSONHandler(cfg.Output, cfg.Component, cfg.Level)
	} else {
		handler = newPrettyHandler(cfg.Output, cfg.Component, cfg.Level)
	}

	if cfg.Async {
		ch := make(chan []byte, cfg.BufSize)
		w := cfg.Output
		go func() {
			for b := range ch {
				w.Write(b)
			}
		}()
		return slog.New(&asyncHandler{handler: handler, ch: ch})
	}

	return slog.New(handler)
}

// With 创建带额外字段的 Logger
func (l *Logger) With(fields ...any) *Logger {
	return &Logger{
		Logger:    l.Logger.With(fields...),
		component: l.component,
		writer:    l.writer,
	}
}

// asyncHandler 异步写入封装
type asyncHandler struct {
	handler slog.Handler
	ch      chan []byte
}

func (h *asyncHandler) Handle(ctx context.Context, r slog.Record) error {
	h.handler.Handle(ctx, r)
	return nil
}

func (h *asyncHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *asyncHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &asyncHandler{handler: h.handler.WithAttrs(attrs), ch: h.ch}
}

func (h *asyncHandler) WithGroup(name string) slog.Handler {
	return &asyncHandler{handler: h.handler.WithGroup(name), ch: h.ch}
}

// componentColors 组件颜色映射
var componentColors = map[string]int{
	"api":       36, // cyan
	"worker":    34, // blue
	"worker_go": 34,
	"gateway":   32, // green
	"bridge":    35, // magenta
	"sandbox":   36, // cyan
	"desktop":   33, // yellow
}

func colorForComponent(comp string) int {
	if c, ok := componentColors[comp]; ok {
		return c
	}
	return 37 // white
}

// levelColors 日志级别颜色
var levelColors = map[slog.Level]int{
	slog.LevelError: 31, // red
	slog.LevelWarn:  33, // yellow
	slog.LevelInfo:  32, // green
	slog.LevelDebug: 90, // bright black (gray)
}

func colorForLevel(level slog.Level) int {
	if c, ok := levelColors[level]; ok {
		return c
	}
	return 37
}

// formatTime 格式化时间
func formatTime(t time.Time) string {
	return t.Format("06-01-02 15:04:05")
}
