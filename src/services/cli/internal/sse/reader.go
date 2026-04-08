package sse

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Event 是一个已解析的 SSE 事件。
type Event struct {
	Seq      int64
	Type     string
	Data     map[string]any
	ToolName string
}

// Reader 从 io.Reader 逐行解析 SSE 事件流。
type Reader struct {
	scanner *bufio.Scanner
}

// NewReader 构造一个 Reader，包装给定的 io.Reader。
func NewReader(r io.Reader) *Reader {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	return &Reader{scanner: s}
}

// Next 阻塞读取直到下一个完整事件，遇到流结束返回 io.EOF。
func (r *Reader) Next() (Event, error) {
	var (
		seq      int64
		eventTyp string
		rawData  string
		hasEvent bool
	)

	for r.scanner.Scan() {
		line := r.scanner.Text()

		switch {
		case line == "":
			// 空行：如果已累积到事件字段则输出事件
			if !hasEvent {
				continue
			}
			var data map[string]any
			if rawData != "" {
				if err := json.Unmarshal([]byte(rawData), &data); err != nil {
					return Event{}, fmt.Errorf("sse: unmarshal data: %w", err)
				}
			}
			ev := Event{
				Seq:  seq,
				Type: eventTyp,
				Data: data,
			}
			if name, ok := data["tool_name"].(string); ok {
				ev.ToolName = name
			}
			return ev, nil

		case strings.HasPrefix(line, ":"):
			// 注释行，跳过
			continue

		case strings.HasPrefix(line, "id:"):
			val := strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return Event{}, fmt.Errorf("sse: parse id %q: %w", val, err)
			}
			seq = n
			hasEvent = true

		case strings.HasPrefix(line, "event:"):
			eventTyp = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			hasEvent = true

		case strings.HasPrefix(line, "data:"):
			chunk := strings.TrimPrefix(line, "data:")
			if len(chunk) > 0 && chunk[0] == ' ' {
				chunk = chunk[1:]
			}
			if rawData != "" {
				rawData += "\n" + chunk
			} else {
				rawData = chunk
			}
			hasEvent = true
		}
		// 未知前缀行直接忽略
	}

	if err := r.scanner.Err(); err != nil {
		return Event{}, fmt.Errorf("sse: scan: %w", err)
	}
	return Event{}, io.EOF
}

// IsTerminal 报告该事件类型是否表示运行终止。
func IsTerminal(eventType string) bool {
	switch eventType {
	case "run.completed", "run.failed", "run.cancelled":
		return true
	}
	return false
}
