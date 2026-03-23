package log

import (
	"log/slog"
)

// collectAttrs 从 slog.Record 中提取属性
func collectAttrs(r *slog.Record) []slog.Attr {
	var attrs []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	return attrs
}
