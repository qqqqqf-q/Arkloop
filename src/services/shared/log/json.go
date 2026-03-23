package log

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"time"
)

// JSONHandler JSON 格式输出 handler
type JSONHandler struct {
	mu        sync.Mutex
	w         io.Writer
	component string
	level     slog.Level
}

// newJSONHandler 创建 JSONHandler
func newJSONHandler(w io.Writer, component string, level slog.Level) *JSONHandler {
	return &JSONHandler{
		w:         w,
		component: component,
		level:     level,
	}
}

func (h *JSONHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *JSONHandler) Handle(ctx context.Context, r slog.Record) error {
	record := map[string]any{
		"ts":        formatTimestamp(r.Time),
		"level":     r.Level.String(),
		"msg":       r.Message,
		"component": h.component,
	}

	// 添加属性
	attrs := collectAttrs(&r)
	for _, attr := range attrs {
		record[attr.Key] = attr.Value.Any()
	}

	payload, err := json.Marshal(record)
	if err != nil {
		payload = []byte(`{"level":"error","msg":"marshal log record failed","component":"` + h.component + `"}`)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err = h.w.Write(append(payload, '\n'))
	return err
}

func (h *JSONHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &JSONHandler{
		w:         h.w,
		component: h.component,
		level:     h.level,
	}
}

func (h *JSONHandler) WithGroup(name string) slog.Handler {
	return h
}

func formatTimestamp(t time.Time) string {
	utc := t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
	if len(utc) >= 6 && utc[len(utc)-6:] == "+00:00" {
		return utc[:len(utc)-6] + "Z"
	}
	return utc
}
