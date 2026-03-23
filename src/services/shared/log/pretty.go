package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"sync"
)

// PrettyHandler 彩色终端输出 handler
type PrettyHandler struct {
	mu        sync.Mutex
	w         io.Writer
	component string
	level     slog.Level
}

// newPrettyHandler 创建 PrettyHandler
func newPrettyHandler(w io.Writer, component string, level slog.Level) *PrettyHandler {
	return &PrettyHandler{
		w:         w,
		component: component,
		level:     level,
	}
}

func (h *PrettyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *PrettyHandler) Handle(ctx context.Context, r slog.Record) error {
	level := r.Level
	levelStr := r.Level.String()
	if len(levelStr) > 4 {
		levelStr = levelStr[:4]
	}
	levelStr = fmt.Sprintf("%-5s", levelStr)

	// 时间
	timeStr := formatTime(r.Time)

	// 组件标签
	compColor := colorForComponent(h.component)
	compLabel := fmt.Sprintf("[%s]", h.component)

	// 级别颜色
	levelColor := colorForLevel(level)

	// 收集属性
	attrs := collectAttrs(&r)

	var buf []byte

	// 时间戳（暗灰）
	buf = append(buf, "\033[90m"...)
	buf = append(buf, timeStr...)
	buf = append(buf, "\033[0m"...)
	buf = append(buf, ' ')

	// 组件（彩色）
	buf = append(buf, "\033["...)
	buf = append(buf, strconv.Itoa(compColor)...)
	buf = append(buf, "m"...)
	buf = append(buf, compLabel...)
	buf = append(buf, "\033[0m"...)
	buf = append(buf, ' ')

	// 级别（按级别着色）
	buf = append(buf, "\033["...)
	buf = append(buf, strconv.Itoa(levelColor)...)
	buf = append(buf, "m"...)
	buf = append(buf, levelStr...)
	buf = append(buf, "\033[0m"...)
	buf = append(buf, ' ')

	// 消息
	buf = append(buf, r.Message...)
	buf = append(buf, ' ')

	// 排序并输出属性
	sortAttrs(attrs)

	for i, attr := range attrs {
		if i > 0 {
			buf = append(buf, ' ')
		}
		key := attr.Key
		// key 白色
		buf = append(buf, "\033[37m"...)
		buf = append(buf, key...)
		buf = append(buf, "\033[0m"...)
		buf = append(buf, '=')

		value := attr.Value.String()
		// 字符串加引号
		if attr.Value.Kind() == slog.KindString {
			buf = append(buf, '"')
			buf = append(buf, value...)
			buf = append(buf, '"')
		} else {
			buf = append(buf, value...)
		}
	}

	buf = append(buf, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf)
	return err
}

// attrs 用于排序
type attrs []slog.Attr

func (a attrs) Len() int           { return len(a) }
func (a attrs) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a attrs) Less(i, j int) bool { return a[i].Key < a[j].Key }

func sortAttrs(a []slog.Attr) {
	sort.Sort(attrs(a))
}

func (h *PrettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &PrettyHandler{
		w:         h.w,
		component: h.component,
		level:     h.level,
	}
}

func (h *PrettyHandler) WithGroup(name string) slog.Handler {
	return h
}
