package observability

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

type LogFields struct {
	TraceID *string
	AccountID   *string
	RunID   *string
}

type JSONLogger struct {
	component string
	writer    io.Writer
	now       func() time.Time
	mu        sync.Mutex
}

func NewJSONLogger(component string, writer io.Writer) *JSONLogger {
	if writer == nil {
		writer = io.Discard
	}
	if component == "" {
		component = "api"
	}
	return &JSONLogger{
		component: component,
		writer:    writer,
		now:       time.Now,
	}
}

func (l *JSONLogger) Info(msg string, fields LogFields, extra map[string]any) {
	l.log("info", msg, fields, extra)
}

func (l *JSONLogger) Warn(msg string, fields LogFields, extra map[string]any) {
	l.log("warn", msg, fields, extra)
}

func (l *JSONLogger) Error(msg string, fields LogFields, extra map[string]any) {
	l.log("error", msg, fields, extra)
}

func (l *JSONLogger) log(level string, msg string, fields LogFields, extra map[string]any) {
	record := map[string]any{
		"ts":        formatTimestamp(l.now()),
		"level":     level,
		"msg":       msg,
		"component": l.component,
		"trace_id":  pointerString(fields.TraceID),
		"account_id":    pointerString(fields.AccountID),
		"run_id":    pointerString(fields.RunID),
	}

	for key, value := range extra {
		record[key] = value
	}

	payload, err := json.Marshal(record)
	if err != nil {
		payload = []byte(fmt.Sprintf(`{"level":"error","msg":"marshal log record failed","component":"%s"}`, l.component))
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.writer.Write(payload)
	_, _ = l.writer.Write([]byte("\n"))
}

func formatTimestamp(now time.Time) string {
	utc := now.UTC().Format("2006-01-02T15:04:05.000Z07:00")
	if len(utc) >= 6 && utc[len(utc)-6:] == "+00:00" {
		return utc[:len(utc)-6] + "Z"
	}
	return utc
}

func pointerString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
