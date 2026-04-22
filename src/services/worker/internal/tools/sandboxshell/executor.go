//go:build desktop && darwin

package sandboxshell

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"arkloop/services/worker/internal/tools"
)

const (
	errorArgsInvalid = "tool.args_invalid"
	errorSandbox     = "tool.sandbox_error"
	errorUnavailable = "tool.sandbox_unavailable"
	maxOutputBytes   = 32 * 1024
	defaultTier      = "lite"

	modeBuffered = "buffered"
	modeFollow   = "follow"
	modeStdin    = "stdin"
	modePTY      = "pty"

	statusRunning = "running"
	maxTimeoutMs  = 1800000
)

type Executor struct {
	sandboxBaseURL string
	authToken      string
	client         *http.Client
}

type processSize struct {
	Rows int `json:"rows"`
	Cols int `json:"cols"`
}

type processResponse struct {
	Status           string           `json:"status"`
	ProcessRef       string           `json:"process_ref,omitempty"`
	Stdout           string           `json:"stdout,omitempty"`
	Stderr           string           `json:"stderr,omitempty"`
	ExitCode         *int             `json:"exit_code,omitempty"`
	Cursor           string           `json:"cursor,omitempty"`
	NextCursor       string           `json:"next_cursor,omitempty"`
	Items            []map[string]any `json:"items,omitempty"`
	HasMore          bool             `json:"has_more,omitempty"`
	AcceptedInputSeq *int64           `json:"accepted_input_seq,omitempty"`
	Truncated        bool             `json:"truncated,omitempty"`
	OutputRef        string           `json:"output_ref,omitempty"`
	Artifacts        []map[string]any `json:"artifacts,omitempty"`
}

func NewExecutor(sandboxBaseURL, authToken string) *Executor {
	return &Executor{
		sandboxBaseURL: strings.TrimRight(sandboxBaseURL, "/"),
		authToken:      authToken,
		client:         &http.Client{},
	}
}

func (e *Executor) IsNotConfigured() bool {
	return e.sandboxBaseURL == ""
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

	if e.sandboxBaseURL == "" {
		return errResult(errorUnavailable, "VM sandbox is not running", started)
	}

	switch toolName {
	case ExecCommandAgentSpec.Name:
		return e.executeExecCommand(ctx, args, execCtx, started)
	case ContinueProcessAgentSpec.Name:
		return e.executeContinueProcess(ctx, args, execCtx, started)
	case TerminateProcessAgentSpec.Name:
		return e.executeTerminateProcess(ctx, args, execCtx, started)
	case ResizeProcessAgentSpec.Name:
		return e.executeResizeProcess(ctx, args, execCtx, started)
	default:
		return errResult(errorArgsInvalid, fmt.Sprintf("unknown sandbox tool: %s", toolName), started)
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

	payload := map[string]any{
		"session_id": execCtx.RunID.String(),
		"tier":       defaultTier,
		"command":    reqArgs.Command,
		"mode":       reqArgs.Mode,
		"cwd":        reqArgs.Cwd,
		"timeout_ms": reqArgs.TimeoutMs,
	}
	if reqArgs.Size != nil {
		payload["size"] = map[string]any{
			"rows": reqArgs.Size.Rows,
			"cols": reqArgs.Size.Cols,
		}
	}
	if len(reqArgs.Env) > 0 {
		payload["env"] = reqArgs.Env
	}

	slog.Info("sandbox_shell: exec_command",
		"run_id", execCtx.RunID.String(),
		"mode", reqArgs.Mode,
		"command_len", len(reqArgs.Command),
		"cwd", reqArgs.Cwd,
	)

	reqCtx, cancel := withProcessTimeout(ctx, requestTimeoutForExec(reqArgs.Mode, reqArgs.TimeoutMs))
	defer cancel()
	resp, postErr := e.doPost(reqCtx, "/v1/process/exec", payload)
	if postErr != nil {
		return errResult(errorSandbox, postErr.Error(), started)
	}
	return buildProcessResult(resp, ExecCommandAgentSpec.Name, execCtx.RunID.String(), execCtx.PerToolSoftLimits, started)
}

func (e *Executor) executeContinueProcess(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	reqArgs, err := parseContinueProcessArgs(args)
	if err != nil {
		return errResult(errorArgsInvalid, err.Error(), started)
	}

	payload := map[string]any{
		"session_id":  execCtx.RunID.String(),
		"process_ref": reqArgs.ProcessRef,
		"cursor":      reqArgs.Cursor,
		"wait_ms":     reqArgs.WaitMs,
		"close_stdin": reqArgs.CloseStdin,
	}
	if reqArgs.StdinText != nil {
		payload["stdin_text"] = *reqArgs.StdinText
	}
	if reqArgs.InputSeq != nil {
		payload["input_seq"] = *reqArgs.InputSeq
	}
	limit := tools.ResolveToolSoftLimit(execCtx.PerToolSoftLimits, ContinueProcessAgentSpec.Name)
	if limit.MaxWaitTimeMs != nil && reqArgs.WaitMs > *limit.MaxWaitTimeMs {
		payload["wait_ms"] = *limit.MaxWaitTimeMs
	}

	reqCtx, cancel := withProcessTimeout(ctx, requestTimeoutForContinue(reqArgs.WaitMs, limit.MaxWaitTimeMs))
	defer cancel()
	resp, postErr := e.doPost(reqCtx, "/v1/process/continue", payload)
	if postErr != nil {
		return errResult(errorSandbox, postErr.Error(), started)
	}
	return buildProcessResult(resp, ContinueProcessAgentSpec.Name, execCtx.RunID.String(), execCtx.PerToolSoftLimits, started)
}

func (e *Executor) executeTerminateProcess(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	reqArgs, err := parseTerminateProcessArgs(args)
	if err != nil {
		return errResult(errorArgsInvalid, err.Error(), started)
	}

	reqCtx, cancel := withProcessTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, postErr := e.doPost(reqCtx, "/v1/process/terminate", map[string]any{
		"session_id":  execCtx.RunID.String(),
		"process_ref": reqArgs.ProcessRef,
	})
	if postErr != nil {
		return errResult(errorSandbox, postErr.Error(), started)
	}
	return buildProcessResult(resp, ContinueProcessAgentSpec.Name, execCtx.RunID.String(), execCtx.PerToolSoftLimits, started)
}

func (e *Executor) executeResizeProcess(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	reqArgs, err := parseResizeProcessArgs(args)
	if err != nil {
		return errResult(errorArgsInvalid, err.Error(), started)
	}

	reqCtx, cancel := withProcessTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, postErr := e.doPost(reqCtx, "/v1/process/resize", map[string]any{
		"session_id":  execCtx.RunID.String(),
		"process_ref": reqArgs.ProcessRef,
		"rows":        reqArgs.Rows,
		"cols":        reqArgs.Cols,
	})
	if postErr != nil {
		return errResult(errorSandbox, postErr.Error(), started)
	}
	return buildProcessResult(resp, ContinueProcessAgentSpec.Name, execCtx.RunID.String(), execCtx.PerToolSoftLimits, started)
}

type execCommandArgs struct {
	Command   string
	Mode      string
	Cwd       string
	TimeoutMs int
	Size      *processSize
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
		req.Mode = modeBuffered
	}
	switch req.Mode {
	case modeBuffered, modeFollow, modeStdin, modePTY:
	default:
		return execCommandArgs{}, fmt.Errorf("unsupported mode: %s", req.Mode)
	}
	if strings.TrimSpace(req.Command) == "" {
		return execCommandArgs{}, fmt.Errorf("command is required")
	}
	if req.Mode != modeBuffered && req.TimeoutMs <= 0 {
		return execCommandArgs{}, fmt.Errorf("timeout_ms is required for follow, stdin, and pty modes")
	}
	if req.TimeoutMs > maxTimeoutMs {
		return execCommandArgs{}, fmt.Errorf("timeout_ms must not exceed 1800000")
	}
	if req.Size != nil && (req.Mode != modePTY || req.Size.Rows <= 0 || req.Size.Cols <= 0) {
		return execCommandArgs{}, fmt.Errorf("size is only supported for pty mode and rows/cols must be positive")
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
	if req.ProcessRef == "" {
		return continueProcessArgs{}, fmt.Errorf("parameter process_ref is required")
	}
	if req.Cursor == "" {
		return continueProcessArgs{}, fmt.Errorf("parameter cursor is required")
	}
	if req.StdinText != nil && (req.InputSeq == nil || *req.InputSeq <= 0) {
		return continueProcessArgs{}, fmt.Errorf("parameter input_seq is required when stdin_text is provided")
	}
	if req.StdinText == nil && req.InputSeq != nil {
		return continueProcessArgs{}, fmt.Errorf("parameter input_seq is not supported without stdin_text")
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

func buildProcessResult(resp processResponse, toolName string, runID string, limits tools.PerToolSoftLimits, started time.Time) tools.ExecutionResult {
	stdout := resp.Stdout
	stderr := resp.Stderr

	// persist full output before truncation
	combined := stdout + stderr
	persistedPath := tools.PersistLargeOutput(runID, combined)

	// tail-truncate
	stdout, stdoutTrunc := tools.TruncateOutputField(stdout, tools.TruncateMaxChars)
	stderr, stderrTrunc := tools.TruncateOutputField(stderr, tools.TruncateMaxChars/4)

	truncated := resp.Truncated || stdoutTrunc || stderrTrunc

	// per-item truncation (consistent with localshell)
	items := make([]map[string]any, 0, len(resp.Items))
	for _, item := range resp.Items {
		text, _ := item["text"].(string)
		text, _ = tools.TruncateOutputField(text, tools.TruncateMaxChars)
		truncItem := make(map[string]any, len(item))
		for k, v := range item {
			truncItem[k] = v
		}
		truncItem["text"] = text
		items = append(items, truncItem)
	}

	resultJSON := map[string]any{
		"status":      resp.Status,
		"stdout":      stdout,
		"stderr":      stderr,
		"running":     resp.Status == statusRunning,
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
		ref := strings.TrimSpace(resp.ProcessRef)
		if ref == "" {
			ref = "buffered"
		}
		outputRef = fmt.Sprintf("process:%s:%s:%s", ref, strings.TrimSpace(resp.Cursor), strings.TrimSpace(resp.NextCursor))
	}
	if outputRef != "" {
		resultJSON["output_ref"] = outputRef
	}
	if len(resp.Artifacts) > 0 {
		resultJSON["artifacts"] = resp.Artifacts
	}
	return tools.ExecutionResult{ResultJSON: resultJSON, DurationMs: durationMs(started)}
}

func requestTimeoutForExec(mode string, timeoutMs int) time.Duration {
	switch strings.TrimSpace(mode) {
	case modeFollow, modeStdin, modePTY:
		if timeoutMs <= 0 {
			timeoutMs = maxTimeoutMs
		}
	default:
		if timeoutMs <= 0 {
			timeoutMs = 30_000
		}
	}
	return time.Duration(timeoutMs)*time.Millisecond + 30*time.Second
}

func requestTimeoutForContinue(waitMs int, maxWaitMs *int) time.Duration {
	if maxWaitMs != nil && waitMs > *maxWaitMs {
		waitMs = *maxWaitMs
	}
	if waitMs <= 0 {
		waitMs = 500
	}
	return time.Duration(waitMs)*time.Millisecond + 30*time.Second
}

func withProcessTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func (e *Executor) doPost(ctx context.Context, path string, payload map[string]any) (processResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return processResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.sandboxBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return processResponse{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.authToken)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return processResponse{}, fmt.Errorf("sandbox request timed out: %w", err)
		}
		return processResponse{}, fmt.Errorf("sandbox request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return processResponse{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return processResponse{}, fmt.Errorf("sandbox error (%d): %s", resp.StatusCode, truncateForLog(string(respBody), 500))
	}

	var result processResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return processResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}
	return result, nil
}

func readProcessSizeArg(raw any) *processSize {
	obj, ok := raw.(map[string]any)
	if !ok || obj == nil {
		return nil
	}
	rows := readIntArg(obj, "rows")
	cols := readIntArg(obj, "cols")
	if rows <= 0 || cols <= 0 {
		return nil
	}
	return &processSize{Rows: rows, Cols: cols}
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

func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
