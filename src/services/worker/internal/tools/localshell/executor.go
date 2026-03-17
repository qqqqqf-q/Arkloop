//go:build desktop

package localshell

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"arkloop/services/worker/internal/tools"
)

const (
	errorArgsInvalid  = "tool.args_invalid"
	errorShellError   = "tool.local_shell_error"
	errorDisabled     = "tool.local_shell_disabled"
	maxOutputBytes    = 32 * 1024
	envLocalShellWork = "ARKLOOP_LOCAL_SHELL_WORKSPACE"
)

// Executor implements tools.Executor for local trusted shell execution.
type Executor struct {
	mu          sync.Mutex
	controllers map[string]*shellController
	workDir     string
}

func NewExecutor() *Executor {
	workDir := os.Getenv(envLocalShellWork)
	if workDir == "" {
		if wd, err := os.Getwd(); err == nil {
			workDir = wd
		} else {
			workDir = os.TempDir()
		}
	}
	return &Executor{
		controllers: make(map[string]*shellController),
		workDir:     workDir,
	}
}

func (e *Executor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	started := time.Now()

	switch toolName {
	case "exec_command":
		return e.executeExecCommand(ctx, args, execCtx, started)
	case "write_stdin":
		return e.executeWriteStdin(ctx, args, execCtx, started)
	default:
		return errResult(errorArgsInvalid, fmt.Sprintf("unknown local shell tool: %s", toolName), started)
	}
}

func (e *Executor) executeExecCommand(
	_ context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	command := readStringArg(args, "command")
	if strings.TrimSpace(command) == "" {
		return errResult(errorArgsInvalid, "parameter command is required", started)
	}

	cwd := readStringArg(args, "cwd")
	timeoutMs := readIntArg(args, "timeout_ms")

	controller := e.getOrCreateController(execCtx.RunID.String())

	slog.Info("local_shell: exec_command",
		"run_id", execCtx.RunID.String(),
		"command", truncateForLog(command, 200),
		"cwd", cwd,
	)

	resp, err := controller.execCommand(command, cwd, timeoutMs)
	if err != nil {
		return errResult(errorShellError, err.Error(), started)
	}

	return buildResult(resp, execCtx.RunID.String(), started)
}

func (e *Executor) executeWriteStdin(
	_ context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	chars := readStringArg(args, "chars")
	yieldTimeMs := readIntArg(args, "yield_time_ms")

	controller := e.getOrCreateController(execCtx.RunID.String())

	if chars != "" {
		slog.Info("local_shell: write_stdin",
			"run_id", execCtx.RunID.String(),
			"chars_len", len(chars),
		)
	}

	resp, err := controller.writeStdin(chars, yieldTimeMs)
	if err != nil {
		return errResult(errorShellError, err.Error(), started)
	}

	return buildResult(resp, execCtx.RunID.String(), started)
}

func (e *Executor) getOrCreateController(runID string) *shellController {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ctrl, ok := e.controllers[runID]; ok {
		return ctrl
	}
	ctrl := newShellController(e.workDir)
	e.controllers[runID] = ctrl
	return ctrl
}

// CloseSession terminates the shell session for a given run.
func (e *Executor) CloseSession(runID string) {
	e.mu.Lock()
	ctrl, ok := e.controllers[runID]
	if ok {
		delete(e.controllers, runID)
	}
	e.mu.Unlock()
	if ctrl != nil {
		ctrl.close()
	}
}

func buildResult(resp *shellResponse, runID string, started time.Time) tools.ExecutionResult {
	output := sanitizeOutput(resp.Output)
	truncated := false
	if len(output) > maxOutputBytes {
		marker := fmt.Sprintf("\n...[truncated %d bytes]", len(output)-maxOutputBytes)
		allowed := maxOutputBytes - len(marker)
		if allowed < 0 {
			allowed = 0
		}
		output = output[:allowed] + marker
		truncated = true
	}

	resultJSON := map[string]any{
		"status":    resp.Status,
		"cwd":       resp.Cwd,
		"output":    output,
		"running":   resp.Running,
		"timed_out": resp.TimedOut,
		"truncated": truncated,
	}
	if resp.ExitCode != nil {
		resultJSON["exit_code"] = *resp.ExitCode
	}
	return tools.ExecutionResult{
		ResultJSON: resultJSON,
		DurationMs: durationMs(started),
	}
}

func errResult(errorClass, message string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error:      &tools.ExecutionError{ErrorClass: errorClass, Message: message},
		DurationMs: durationMs(started),
	}
}

func durationMs(started time.Time) int {
	ms := int(time.Since(started) / time.Millisecond)
	if ms < 0 {
		return 0
	}
	return ms
}

func readStringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func readIntArg(args map[string]any, key string) int {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		parsed, err := n.Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return 0
}

// sanitizeOutput strips ANSI escape codes and collapses carriage-return overwrites.
func sanitizeOutput(s string) string {
	// Collapse \r overwrites (progress bars, spinners).
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		parts := strings.Split(line, "\r")
		lines[i] = parts[len(parts)-1]
	}
	s = strings.Join(lines, "\n")
	// Strip ANSI escape sequences.
	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' && s[i] != 'J' && s[i] != 'K' && s[i] != 'H' && s[i] != 'A' && s[i] != 'B' && s[i] != 'C' && s[i] != 'D' {
				i++
			}
			i++ // skip terminator
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
