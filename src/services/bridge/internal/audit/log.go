package audit

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

const (
	maxParamValueBytes = 512
	maxParamsEntries   = 24
)

type Entry struct {
	Timestamp string            `json:"timestamp"`
	Action    string            `json:"action"`
	ModuleID  string            `json:"module_id"`
	Params    map[string]string `json:"params,omitempty"`
	Result    string            `json:"result"`
	Duration  string            `json:"duration,omitempty"`
	Error     string            `json:"error,omitempty"`
}

type Logger struct {
	mu     sync.Mutex
	writer io.Writer
	now    func() time.Time
}

func NewLogger(writer io.Writer) *Logger {
	return &Logger{
		writer: writer,
		now:    time.Now,
	}
}

// LogAction logs a module action. Call with result="started" at beginning,
// then LogActionComplete with final result.
func (l *Logger) LogAction(action, moduleID string, params map[string]string) {
	l.write(Entry{
		Timestamp: l.now().UTC().Format(time.RFC3339Nano),
		Action:    action,
		ModuleID:  moduleID,
		Params:    sanitizeParams(params),
		Result:    "started",
	})
}

func (l *Logger) LogActionComplete(action, moduleID string, duration time.Duration, err error) {
	entry := Entry{
		Timestamp: l.now().UTC().Format(time.RFC3339Nano),
		Action:    action,
		ModuleID:  moduleID,
		Result:    "completed",
		Duration:  duration.String(),
	}
	if err != nil {
		entry.Result = "failed"
		entry.Error = err.Error()
	}
	l.write(entry)
}

func (l *Logger) write(entry Entry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.writer.Write(append(data, '\n'))
}

func sanitizeParams(params map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}
	clean := make(map[string]string, minInt(len(params), maxParamsEntries))
	count := 0
	for key, value := range params {
		if count >= maxParamsEntries {
			clean["_truncated"] = "true"
			break
		}
		clean[key] = truncateString(value, maxParamValueBytes)
		count++
	}
	return clean
}

func truncateString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
