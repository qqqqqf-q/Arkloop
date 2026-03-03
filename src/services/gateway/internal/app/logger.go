package app

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type LogFields struct {
	TraceID *string
}

type JSONLogger struct {
	component string
	writer    io.Writer
	now       func() time.Time
	ch        chan []byte
}

func NewJSONLogger(component string, writer io.Writer) *JSONLogger {
	if writer == nil {
		writer = io.Discard
	}
	if component == "" {
		component = "gateway"
	}
	l := &JSONLogger{
		component: component,
		writer:    writer,
		now:       time.Now,
		ch:        make(chan []byte, 4096),
	}
	go l.loop()
	return l
}

func (l *JSONLogger) Info(msg string, fields LogFields, extra map[string]any) {
	l.log("info", msg, fields, extra)
}

func (l *JSONLogger) Error(msg string, fields LogFields, extra map[string]any) {
	l.log("error", msg, fields, extra)
}

func (l *JSONLogger) log(level string, msg string, fields LogFields, extra map[string]any) {
	if l == nil {
		return
	}
	record := map[string]any{
		"ts":        formatTimestamp(l.now()),
		"level":     level,
		"msg":       msg,
		"component": l.component,
		"trace_id":  pointerString(fields.TraceID),
	}
	for key, value := range extra {
		record[key] = value
	}

	payload, err := json.Marshal(record)
	if err != nil {
		payload = []byte(fmt.Sprintf(`{"level":"error","msg":"marshal log record failed","component":"%s"}`, l.component))
	}

	buf := append(payload, '\n')
	select {
	case l.ch <- buf:
	default:
	}
}

func (l *JSONLogger) loop() {
	for buf := range l.ch {
		_, _ = l.writer.Write(buf)
	}
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
