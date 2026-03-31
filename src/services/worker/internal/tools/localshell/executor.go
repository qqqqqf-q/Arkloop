//go:build desktop

package localshell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
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
	rtkRewriteTimeout = 1500 * time.Millisecond
)

// Executor implements tools.Executor for local trusted shell execution.
type Executor struct {
	mu          sync.Mutex
	controllers map[string]*shellController
	workDir     string
}

var rtkRewriteRunner = func(ctx context.Context, bin string, command string) (string, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, append([]string{"rewrite"}, strings.Fields(command)...)...)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
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
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	reqArgs, argErr := parseExecCommandArgs(args)
	if argErr != nil {
		return errResult(errorArgsInvalid, argErr.Error(), started)
	}

	controller := e.getOrCreateController(execCtx.RunID.String(), execCtx.WorkDir)

	command := reqArgs.Command
	if rewritten := rtkRewrite(ctx, command); rewritten != "" {
		command = rewritten
	}

	slog.Info("local_shell: exec_command",
		"run_id", execCtx.RunID.String(),
		"command_len", len(command),
		"cwd", reqArgs.Cwd,
	)

	resp, err := controller.execCommand(command, reqArgs.Cwd, reqArgs.TimeoutMs, reqArgs.YieldTimeMs, reqArgs.Background, reqArgs.Env)
	if err != nil {
		return errResult(errorShellError, err.Error(), started)
	}

	return buildResult(resp, execCtx.RunID.String(), started)
}

type execCommandArgs struct {
	Cwd         string
	Command     string
	TimeoutMs   int
	YieldTimeMs int
	Background  bool
	Env         map[string]string
}

func parseExecCommandArgs(args map[string]any) (execCommandArgs, error) {
	command := readStringArg(args, "command")
	if strings.TrimSpace(command) == "" {
		return execCommandArgs{}, fmt.Errorf("parameter command is required")
	}
	reqArgs := execCommandArgs{
		Cwd:         readStringArg(args, "cwd"),
		Command:     command,
		TimeoutMs:   readIntArg(args, "timeout_ms"),
		YieldTimeMs: readIntArg(args, "yield_time_ms"),
		Background:  readBoolArg(args, "background"),
		Env:         readMapStringArg(args, "env"),
	}
	if reqArgs.Background {
		reqArgs.YieldTimeMs = 1
	} else if reqArgs.YieldTimeMs <= 0 {
		reqArgs.YieldTimeMs = min(reqArgs.TimeoutMs, 30_000)
		if reqArgs.YieldTimeMs <= 0 {
			reqArgs.YieldTimeMs = 30_000
		}
	}
	return reqArgs, nil
}

func (e *Executor) executeWriteStdin(
	_ context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	chars := readStringArg(args, "chars")
	yieldTimeMs := readIntArg(args, "yield_time_ms")

	controller := e.getOrCreateController(execCtx.RunID.String(), execCtx.WorkDir)

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

func (e *Executor) getOrCreateController(runID string, workDir string) *shellController {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ctrl, ok := e.controllers[runID]; ok {
		return ctrl
	}
	wd := workDir
	if wd == "" {
		wd = e.workDir
	}
	ctrl := newShellController(wd)
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

func readMapStringArg(args map[string]any, key string) map[string]string {
	raw, ok := args[key].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	result := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func readBoolArg(args map[string]any, key string) bool {
	v, _ := args[key].(bool)
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

// rtkRewrite returns the RTK-optimized version of a simple shell command, or
// "" if RTK is unavailable, times out, or the command requires full shell parsing.
func rtkRewrite(ctx context.Context, command string) string {
	if ctx == nil {
		ctx = context.Background()
	}
	bin := resolvedRTKBin()
	if bin == "" {
		return ""
	}
	if !shouldAttemptRTKRewrite(command) {
		return ""
	}
	rewriteCtx, cancel := context.WithTimeout(ctx, rtkRewriteTimeout)
	defer cancel()
	rewritten, err := rtkRewriteRunner(rewriteCtx, bin, command)
	if err != nil {
		// exit 1 / timeout / cancellation 都视为放弃重写，保持原命令
		return ""
	}
	return rewritten
}

func shouldAttemptRTKRewrite(command string) bool {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return false
	}
	if strings.ContainsAny(trimmed, "\r\n") {
		return false
	}
	if strings.ContainsAny(trimmed, "'\"`|;&<>$()") {
		return false
	}
	return true
}

var (
	rtkBinOnce  sync.Once
	rtkBinCache string
)

func resolvedRTKBin() string {
	rtkBinOnce.Do(func() {
		home, _ := os.UserHomeDir()
		arkBin := home + "/.arkloop/bin/rtk"
		if _, err := os.Stat(arkBin); err == nil {
			rtkBinCache = arkBin
			return
		}
		if p, err := exec.LookPath("rtk"); err == nil {
			rtkBinCache = p
		}
	})
	return rtkBinCache
}

func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
