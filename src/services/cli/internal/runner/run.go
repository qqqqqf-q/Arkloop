package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"arkloop/services/cli/internal/apiclient"
	"arkloop/services/cli/internal/sse"
)

var (
	sseReconnectMaxAttempts = 5
	sseReconnectBaseDelay   = 500 * time.Millisecond
	sseReconnectMaxDelay    = 3 * time.Second
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

	sseTrace("cli_sse_run_started", "run_id", runID, "thread_id", threadID)

	start := time.Now()

	body, err := c.OpenEventStream(ctx, runID, 0)
	if err != nil {
		sseTrace("cli_sse_open_failed", "run_id", runID, "after_seq", int64(0), "err", err.Error())
		return RunResult{}, fmt.Errorf("runner: open event stream: %w", err)
	}
	sseTrace("cli_sse_open_ok", "run_id", runID, "after_seq", int64(0))
	defer func() {
		if body != nil {
			_ = body.Close()
		}
	}()

	var (
		output     strings.Builder
		toolCalls  int
		status     string
		runErr     string
		lastSeq    int64
		seenSeqs   = make(map[int64]struct{})
		reconnects int
	)

	for status == "" {
		reader := sse.NewReader(body)
		streamErr := consumeStream(reader, seenSeqs, onEvent, &output, &toolCalls, &lastSeq, &status, &runErr)
		sseTrace("cli_sse_read_stop",
			"run_id", runID,
			"last_seq", lastSeq,
			"tool_calls", toolCalls,
			"terminal_status", status,
			"stream_err", errOrEmpty(streamErr),
			"is_eof", streamErr != nil && errors.Is(streamErr, io.EOF),
		)
		if status != "" {
			break
		}
		if ctx.Err() != nil {
			sseTrace("cli_sse_runner_ctx_done", "run_id", runID, "err", ctx.Err().Error())
			break
		}

		nextStatus, nextErr := c.GetRun(ctx, runID)
		if nextErr != nil {
			status = "error"
			runErr = fmt.Sprintf("runner: get run: %v", nextErr)
			sseTrace("cli_sse_get_run_failed", "run_id", runID, "err", nextErr.Error())
			break
		}
		if !isActiveRunStatus(nextStatus.Status) {
			status = nextStatus.Status
			sseTrace("cli_sse_run_inactive_via_api", "run_id", runID, "api_status", nextStatus.Status)
			break
		}

		reconnects++
		sseTrace("cli_sse_reconnect_scheduled",
			"run_id", runID,
			"attempt", reconnects,
			"last_seq", lastSeq,
			"prev_stream_err", errOrEmpty(streamErr),
		)
		if reconnects > sseReconnectMaxAttempts {
			status = "error"
			runErr = reconnectError(streamErr, reconnects-1)
			sseTrace("cli_sse_reconnect_exhausted", "run_id", runID, "attempts", reconnects-1, "last_seq", lastSeq)
			break
		}

		if closeErr := body.Close(); closeErr != nil && runErr == "" {
			runErr = closeErr.Error()
			sseTrace("cli_sse_body_close_err", "run_id", runID, "err", closeErr.Error())
		}
		body = nil
		if err = sleepBeforeReconnect(ctx, reconnects); err != nil {
			sseTrace("cli_sse_reconnect_sleep_abort", "run_id", runID, "err", err.Error())
			break
		}
		body, err = c.OpenEventStream(ctx, runID, lastSeq)
		if err != nil {
			status = "error"
			runErr = fmt.Sprintf("runner: reopen event stream: %v", err)
			sseTrace("cli_sse_reopen_failed", "run_id", runID, "after_seq", lastSeq, "err", err.Error())
			break
		}
		sseTrace("cli_sse_reopen_ok", "run_id", runID, "after_seq", lastSeq)
	}

	durationMs := time.Since(start).Milliseconds()

	if status == "" {
		switch ctx.Err() {
		case context.DeadlineExceeded:
			status = "timeout"
		case context.Canceled:
			status = "cancelled"
		default:
			status = "error"
			runErr = "run ended without terminal status"
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
	case "run.interrupted":
		return "interrupted"
	default:
		return eventType
	}
}

func consumeStream(
	reader *sse.Reader,
	seenSeqs map[int64]struct{},
	onEvent func(sse.Event),
	output *strings.Builder,
	toolCalls *int,
	lastSeq *int64,
	status *string,
	runErr *string,
) error {
	for {
		ev, err := reader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return io.EOF
			}
			return err
		}

		if ev.Seq > 0 {
			if _, exists := seenSeqs[ev.Seq]; exists {
				continue
			}
			seenSeqs[ev.Seq] = struct{}{}
			*lastSeq = ev.Seq
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
			*toolCalls = *toolCalls + 1
		}

		if sse.IsTerminal(ev.Type) {
			*status = terminalStatus(ev.Type)
			if ev.Type == "run.failed" {
				if msg, ok := ev.Data["error"].(string); ok {
					*runErr = msg
				}
			}
			return nil
		}
	}
}

func isActiveRunStatus(status string) bool {
	switch status {
	case "running", "cancelling":
		return true
	default:
		return false
	}
}

func sleepBeforeReconnect(ctx context.Context, attempt int) error {
	delay := sseReconnectBaseDelay << (attempt - 1)
	if delay > sseReconnectMaxDelay {
		delay = sseReconnectMaxDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func reconnectError(streamErr error, attempts int) string {
	if streamErr == nil {
		return fmt.Sprintf("sse reconnect exhausted after %d attempts", attempts)
	}
	return fmt.Sprintf("%s (reconnect exhausted after %d attempts)", streamErr.Error(), attempts)
}

func sseTraceEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("ARKLOOP_DEBUG_SSE")))
	return v == "1" || v == "true" || v == "yes"
}

// stderr JSON，Harbor 子进程日志里可 grep cli_sse_
var sseTraceLog = slog.New(slog.NewJSONHandler(os.Stderr, nil))

func sseTrace(msg string, args ...any) {
	if !sseTraceEnabled() {
		return
	}
	sseTraceLog.Info(msg, args...)
}

func errOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
