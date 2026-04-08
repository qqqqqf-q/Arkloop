package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"arkloop/services/cli/internal/apiclient"
	"arkloop/services/cli/internal/sse"
)

// RunResult 保存一次 agent run 的完整结果。
type RunResult struct {
	ThreadID   string `json:"thread_id"`
	RunID      string `json:"run_id"`
	Status     string `json:"status"`
	Output     string `json:"result"`
	DurationMs int64  `json:"duration_ms"`
	ToolCalls  int    `json:"tool_calls"`
	Error      string `json:"error,omitempty"`
	IsError    bool   `json:"is_error"`
}

// Execute 在指定 thread 中发送 prompt 并执行一次完整的 agent run。
// threadID 为空时自动创建新 thread。onEvent 为 nil 时跳过回调。
func Execute(ctx context.Context, c *apiclient.Client, threadID string, prompt string, params apiclient.RunParams, onEvent func(sse.Event)) (RunResult, error) {
	var err error

	if threadID == "" {
		threadID, err = c.CreateThread(ctx, "")
		if err != nil {
			return RunResult{}, fmt.Errorf("runner: create thread: %w", err)
		}
	}

	if err = c.AddMessage(ctx, threadID, prompt); err != nil {
		return RunResult{}, fmt.Errorf("runner: add message: %w", err)
	}

	runID, err := c.StartRun(ctx, threadID, params)
	if err != nil {
		return RunResult{}, fmt.Errorf("runner: start run: %w", err)
	}

	start := time.Now()

	body, err := c.OpenEventStream(ctx, runID, 0)
	if err != nil {
		return RunResult{}, fmt.Errorf("runner: open event stream: %w", err)
	}
	defer body.Close()

	var (
		output    strings.Builder
		toolCalls int
		status    string
		runErr    string
		streamErr error
	)

	reader := sse.NewReader(body)
	for {
		ev, readErr := reader.Next()
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				streamErr = readErr
			}
			break
		}

		if onEvent != nil {
			onEvent(ev)
		}

		switch ev.Type {
		case "message.delta":
			if delta, ok := ev.Data["content_delta"].(string); ok {
				output.WriteString(delta)
			}
		case "tool.call":
			toolCalls++
		}

		if sse.IsTerminal(ev.Type) {
			status = terminalStatus(ev.Type)
			if ev.Type == "run.failed" {
				if msg, ok := ev.Data["error"].(string); ok {
					runErr = msg
				}
			}
			break
		}
	}

	durationMs := time.Since(start).Milliseconds()

	// terminal event 未收到，说明被中断或意外断流
	if status == "" {
		switch ctx.Err() {
		case context.DeadlineExceeded:
			status = "timeout"
		case context.Canceled:
			status = "cancelled"
		default:
			status = "error"
			if streamErr != nil {
				runErr = streamErr.Error()
			}
		}
	}

	return RunResult{
		ThreadID:   threadID,
		RunID:      runID,
		Status:     status,
		Output:     output.String(),
		DurationMs: durationMs,
		ToolCalls:  toolCalls,
		Error:      runErr,
		IsError:    status != "completed",
	}, nil
}

func terminalStatus(eventType string) string {
	switch eventType {
	case "run.completed":
		return "completed"
	case "run.failed":
		return "failed"
	case "run.cancelled":
		return "cancelled"
	default:
		return eventType
	}
}
