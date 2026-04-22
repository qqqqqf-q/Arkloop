//go:build desktop

package localshell

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"arkloop/services/worker/internal/tools"
)

const (
	errorArgsInvalid  = "tool.args_invalid"
	errorShellError   = "tool.local_shell_error"
	maxOutputBytes    = 32 * 1024
	envLocalShellWork = "ARKLOOP_LOCAL_SHELL_WORKSPACE"
	rtkRewriteTimeout = 1500 * time.Millisecond
)

var localShellAllowedEnvKeys = []string{
	"HOME",
	"LANG",
	"LC_ALL",
	"LOGNAME",
	"PATH",
	"SHELL",
	"TEMP",
	"TMP",
	"TMPDIR",
	"USER",
}

type Executor struct {
	controller *ProcessController
	workDir    string
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
		controller: NewProcessController(),
		workDir:    workDir,
	}
}

func (e *Executor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	_ = toolCallID
	started := time.Now()

	switch toolName {
	case ExecCommandAgentSpec.Name:
		return e.executeExecCommand(ctx, args, execCtx, started)
	case ContinueProcessAgentSpec.Name:
		return e.executeContinueProcess(args, execCtx, started)
	case TerminateProcessAgentSpec.Name:
		return e.executeTerminateProcess(args, execCtx, started)
	case ResizeProcessAgentSpec.Name:
		return e.executeResizeProcess(args, execCtx, started)
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
	reqArgs, err := parseExecCommandArgs(args)
	if err != nil {
		return errResult(errorArgsInvalid, err.Error(), started)
	}

	command := reqArgs.Command
	if rewritten := rtkRewrite(ctx, command); rewritten != "" {
		command = rewritten
	}

	req := ExecCommandRequest{
		RunID:     execCtx.RunID.String(),
		Command:   command,
		Mode:      reqArgs.Mode,
		Cwd:       resolveExecCwd(reqArgs.Cwd, execCtx.WorkDir, e.workDir),
		TimeoutMs: reqArgs.TimeoutMs,
		Size:      reqArgs.Size,
		Env:       sanitizeLocalEnvPatches(reqArgs.Env),
	}

	slog.Info("local_shell: exec_command",
		"run_id", execCtx.RunID.String(),
		"mode", req.Mode,
		"command_len", len(command),
		"cwd", req.Cwd,
	)

	resp, runErr := e.controller.ExecCommand(req)
	if runErr != nil {
		return mapLocalProcessError(runErr, started)
	}
	return buildProcessResult(resp, ExecCommandAgentSpec.Name, execCtx.RunID.String(), execCtx.PerToolSoftLimits, started)
}

func (e *Executor) executeContinueProcess(args map[string]any, execCtx tools.ExecutionContext, started time.Time) tools.ExecutionResult {
	reqArgs, err := parseContinueProcessArgs(args)
	if err != nil {
		return errResult(errorArgsInvalid, err.Error(), started)
	}
	limit := tools.ResolveToolSoftLimit(execCtx.PerToolSoftLimits, ContinueProcessAgentSpec.Name)
	if limit.MaxWaitTimeMs != nil && reqArgs.WaitMs > *limit.MaxWaitTimeMs {
		reqArgs.WaitMs = *limit.MaxWaitTimeMs
	}
	resp, runErr := e.controller.ContinueProcess(ContinueProcessRequest{
		ProcessRef: reqArgs.ProcessRef,
		Cursor:     reqArgs.Cursor,
		WaitMs:     reqArgs.WaitMs,
		StdinText:  reqArgs.StdinText,
		InputSeq:   reqArgs.InputSeq,
		CloseStdin: reqArgs.CloseStdin,
	})
	if runErr != nil {
		return mapLocalProcessError(runErr, started)
	}
	return buildProcessResult(resp, ContinueProcessAgentSpec.Name, execCtx.RunID.String(), execCtx.PerToolSoftLimits, started)
}

func (e *Executor) executeTerminateProcess(args map[string]any, execCtx tools.ExecutionContext, started time.Time) tools.ExecutionResult {
	reqArgs, err := parseTerminateProcessArgs(args)
	if err != nil {
		return errResult(errorArgsInvalid, err.Error(), started)
	}
	resp, runErr := e.controller.TerminateProcess(TerminateProcessRequest{ProcessRef: reqArgs.ProcessRef})
	if runErr != nil {
		return mapLocalProcessError(runErr, started)
	}
	return buildProcessResult(resp, ContinueProcessAgentSpec.Name, execCtx.RunID.String(), execCtx.PerToolSoftLimits, started)
}

func (e *Executor) executeResizeProcess(args map[string]any, execCtx tools.ExecutionContext, started time.Time) tools.ExecutionResult {
	reqArgs, err := parseResizeProcessArgs(args)
	if err != nil {
		return errResult(errorArgsInvalid, err.Error(), started)
	}
	resp, runErr := e.controller.ResizeProcess(ResizeProcessRequest{
		ProcessRef: reqArgs.ProcessRef,
		Rows:       reqArgs.Rows,
		Cols:       reqArgs.Cols,
	})
	if runErr != nil {
		return mapLocalProcessError(runErr, started)
	}
	return buildProcessResult(resp, ContinueProcessAgentSpec.Name, execCtx.RunID.String(), execCtx.PerToolSoftLimits, started)
}

type execCommandArgs struct {
	Command   string
	Mode      string
	Cwd       string
	TimeoutMs int
	Size      *Size
	Env       map[string]*string
}

type continueProcessArgs struct {
	ProcessRef string
	Cursor     string
	WaitMs     int
	StdinText  *string
	InputSeq   *int64
	CloseStdin bool
}

type terminateProcessArgs struct {
	ProcessRef string
}

type resizeProcessArgs struct {
	ProcessRef string
	Rows       int
	Cols       int
}

func parseExecCommandArgs(args map[string]any) (execCommandArgs, error) {
	for key := range args {
		switch key {
		case "command", "mode", "cwd", "timeout_ms", "size", "env":
		case "session_mode", "session_ref", "from_session_ref", "share_scope", "yield_time_ms", "background", "chars":
			return execCommandArgs{}, fmt.Errorf("parameter %s is not supported", key)
		default:
			return execCommandArgs{}, fmt.Errorf("parameter %s is not supported", key)
		}
	}

	req := execCommandArgs{
		Command:   readStringArg(args, "command"),
		Mode:      strings.TrimSpace(readStringArg(args, "mode")),
		Cwd:       readStringArg(args, "cwd"),
		TimeoutMs: readIntArg(args, "timeout_ms"),
		Size:      readProcessSizeArg(args["size"]),
		Env:       readNullableStringMapArg(args["env"]),
	}
	if req.Mode == "" {
		req.Mode = ModeBuffered
	}
	payload := ExecCommandRequest{
		Command:   req.Command,
		Mode:      req.Mode,
		Cwd:       req.Cwd,
		TimeoutMs: req.TimeoutMs,
		Size:      req.Size,
		Env:       req.Env,
	}
	if err := ValidateExecRequest(payload); err != nil {
		return execCommandArgs{}, errors.New(err.Message)
	}
	return req, nil
}

func parseContinueProcessArgs(args map[string]any) (continueProcessArgs, error) {
	for key := range args {
		switch key {
		case "process_ref", "cursor", "wait_ms", "stdin_text", "input_seq", "close_stdin":
		case "session_ref", "chars", "yield_time_ms":
			return continueProcessArgs{}, fmt.Errorf("parameter %s is not supported", key)
		default:
			return continueProcessArgs{}, fmt.Errorf("parameter %s is not supported", key)
		}
	}

	req := continueProcessArgs{
		ProcessRef: strings.TrimSpace(readStringArg(args, "process_ref")),
		Cursor:     strings.TrimSpace(readStringArg(args, "cursor")),
		WaitMs:     readIntArg(args, "wait_ms"),
		CloseStdin: readBoolArg(args, "close_stdin"),
	}
	if raw, ok := args["stdin_text"]; ok {
		if text, ok := raw.(string); ok {
			req.StdinText = &text
		}
	}
	if raw, ok := args["input_seq"]; ok {
		value := int64(readIntArg(map[string]any{"input_seq": raw}, "input_seq"))
		req.InputSeq = &value
	}
	payload := ContinueProcessRequest{
		ProcessRef: req.ProcessRef,
		Cursor:     req.Cursor,
		WaitMs:     req.WaitMs,
		StdinText:  req.StdinText,
		InputSeq:   req.InputSeq,
		CloseStdin: req.CloseStdin,
	}
	if err := ValidateContinueRequest(payload); err != nil {
		return continueProcessArgs{}, errors.New(err.Message)
	}
	return req, nil
}

func parseTerminateProcessArgs(args map[string]any) (terminateProcessArgs, error) {
	if len(args) != 1 {
		for key := range args {
			if key != "process_ref" {
				return terminateProcessArgs{}, fmt.Errorf("parameter %s is not supported", key)
			}
		}
	}
	processRef := strings.TrimSpace(readStringArg(args, "process_ref"))
	if processRef == "" {
		return terminateProcessArgs{}, fmt.Errorf("parameter process_ref is required")
	}
	return terminateProcessArgs{ProcessRef: processRef}, nil
}

func parseResizeProcessArgs(args map[string]any) (resizeProcessArgs, error) {
	for key := range args {
		switch key {
		case "process_ref", "rows", "cols":
		default:
			return resizeProcessArgs{}, fmt.Errorf("parameter %s is not supported", key)
		}
	}
	processRef := strings.TrimSpace(readStringArg(args, "process_ref"))
	if processRef == "" {
		return resizeProcessArgs{}, fmt.Errorf("parameter process_ref is required")
	}
	rows := readIntArg(args, "rows")
	cols := readIntArg(args, "cols")
	if rows <= 0 || cols <= 0 {
		return resizeProcessArgs{}, fmt.Errorf("parameters rows and cols must be greater than 0")
	}
	return resizeProcessArgs{ProcessRef: processRef, Rows: rows, Cols: cols}, nil
}

func buildProcessResult(resp *Response, toolName string, runID string, limits tools.PerToolSoftLimits, started time.Time) tools.ExecutionResult {
	if resp == nil {
		return errResult(errorShellError, "process response is empty", started)
	}

	stdout := sanitizeOutput(resp.Stdout)
	stderr := sanitizeOutput(resp.Stderr)

	// persist full output before truncation
	combined := stdout + stderr
	persistedPath := tools.PersistLargeOutput(runID, combined)

	// tail-truncate
	stdout, stdoutTrunc := tools.TruncateOutputField(stdout, tools.TruncateMaxChars)
	stderr, stderrTrunc := tools.TruncateOutputField(stderr, tools.TruncateMaxChars/4)

	items := make([]map[string]any, 0, len(resp.Items))
	for _, item := range resp.Items {
		text := sanitizeOutput(item.Text)
		text, _ = tools.TruncateOutputField(text, tools.TruncateMaxChars)
		items = append(items, map[string]any{
			"seq":    item.Seq,
			"stream": item.Stream,
			"text":   text,
		})
	}

	truncated := resp.Truncated || stdoutTrunc || stderrTrunc
	resultJSON := map[string]any{
		"status":      resp.Status,
		"stdout":      stdout,
		"stderr":      stderr,
		"running":     resp.Status == StatusRunning,
		"cursor":      resp.Cursor,
		"next_cursor": resp.NextCursor,
		"items":       items,
		"has_more":    resp.HasMore,
		"truncated":   truncated,
		"duration_ms": durationMs(started),
	}
	if persistedPath != "" {
		resultJSON["full_output_path"] = persistedPath
	}
	if strings.TrimSpace(resp.ProcessRef) != "" {
		resultJSON["process_ref"] = strings.TrimSpace(resp.ProcessRef)
	}
	if resp.ExitCode != nil {
		resultJSON["exit_code"] = *resp.ExitCode
	}
	if resp.AcceptedInputSeq != nil {
		resultJSON["accepted_input_seq"] = *resp.AcceptedInputSeq
	}
	outputRef := strings.TrimSpace(resp.OutputRef)
	if outputRef == "" && resp.Truncated {
		outputRef = buildOutputRef(strings.TrimSpace(resp.ProcessRef), 0, 0)
	}
	if outputRef != "" {
		resultJSON["output_ref"] = outputRef
	}
	if len(resp.Artifacts) > 0 {
		resultJSON["artifacts"] = resp.Artifacts
	}
	return tools.ExecutionResult{ResultJSON: resultJSON, DurationMs: durationMs(started)}
}

func (e *Executor) CleanupProcesses(ctx context.Context, processRefs []string, terminalStatus string) error {
	var errs []string
	for _, ref := range processRefs {
		if ctx != nil {
			select {
			case <-ctx.Done():
				if len(errs) > 0 {
					return errors.New(strings.Join(errs, "; "))
				}
				return ctx.Err()
			default:
			}
		}
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		var err error
		switch strings.TrimSpace(terminalStatus) {
		case "cancelled", "interrupted":
			_, err = e.controller.CancelProcess(TerminateProcessRequest{ProcessRef: ref})
		default:
			_, err = e.controller.TerminateProcess(TerminateProcessRequest{ProcessRef: ref})
		}
		if err != nil && !isProcessNotFound(err) {
			errs = append(errs, ref+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func resolveExecCwd(requested string, runWorkDir string, defaultWorkDir string) string {
	if strings.TrimSpace(requested) != "" {
		return strings.TrimSpace(requested)
	}
	if strings.TrimSpace(runWorkDir) != "" {
		return strings.TrimSpace(runWorkDir)
	}
	return strings.TrimSpace(defaultWorkDir)
}

func sanitizeLocalEnvPatches(overrides map[string]*string) map[string]*string {
	patches := make(map[string]*string)
	for _, pair := range os.Environ() {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if key == "" || slices.Contains(localShellAllowedEnvKeys, key) {
			continue
		}
		patches[key] = nil
	}
	for key, value := range overrides {
		patches[key] = value
	}
	if len(patches) == 0 {
		return nil
	}
	return patches
}

func isProcessNotFound(err error) bool {
	var procErr *Error
	return errors.As(err, &procErr) && procErr.Code == CodeProcessNotFound
}

func mapLocalProcessError(err error, started time.Time) tools.ExecutionResult {
	if err == nil {
		return tools.ExecutionResult{DurationMs: durationMs(started)}
	}
	var procErr *Error
	if errors.As(err, &procErr) {
		return errResult(procErr.Code, procErr.Message, started)
	}
	return errResult(errorShellError, err.Error(), started)
}

func readProcessSizeArg(raw any) *Size {
	obj, ok := raw.(map[string]any)
	if !ok || obj == nil {
		return nil
	}
	rows := readIntArg(obj, "rows")
	cols := readIntArg(obj, "cols")
	if rows <= 0 || cols <= 0 {
		return nil
	}
	return &Size{Rows: rows, Cols: cols}
}

func readNullableStringMapArg(raw any) map[string]*string {
	obj, ok := raw.(map[string]any)
	if !ok || len(obj) == 0 {
		return nil
	}
	out := make(map[string]*string, len(obj))
	for key, value := range obj {
		switch typed := value.(type) {
		case string:
			copyValue := typed
			out[key] = &copyValue
		case nil:
			out[key] = nil
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

func sanitizeOutput(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		parts := strings.Split(line, "\r")
		lines[i] = parts[len(parts)-1]
	}
	s = strings.Join(lines, "\n")

	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' && s[i] != 'J' && s[i] != 'K' && s[i] != 'H' && s[i] != 'A' && s[i] != 'B' && s[i] != 'C' && s[i] != 'D' {
				i++
			}
			i++
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

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
		arkBin := filepath.Join(home, ".arkloop", "bin", toolsDesktopRTKBinaryName())
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

func toolsDesktopRTKBinaryName() string {
	if os.PathSeparator == '\\' {
		return "rtk.exe"
	}
	return "rtk"
}
