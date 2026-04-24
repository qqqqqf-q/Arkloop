package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"arkloop/services/shared/skillstore"
	"arkloop/services/worker/internal/tools"
)

type processSize struct {
	Rows int `json:"rows"`
	Cols int `json:"cols"`
}

type processExecRequest struct {
	SessionID     string                     `json:"session_id"`
	AccountID     string                     `json:"account_id,omitempty"`
	ProfileRef    string                     `json:"profile_ref,omitempty"`
	WorkspaceRef  string                     `json:"workspace_ref,omitempty"`
	EnabledSkills []skillstore.ResolvedSkill `json:"enabled_skills,omitempty"`
	Tier          string                     `json:"tier,omitempty"`
	Command       string                     `json:"command"`
	Mode          string                     `json:"mode,omitempty"`
	Cwd           string                     `json:"cwd,omitempty"`
	TimeoutMs     int                        `json:"timeout_ms,omitempty"`
	Size          *processSize               `json:"size,omitempty"`
	Env           map[string]*string         `json:"env,omitempty"`
}

type continueProcessRequest struct {
	SessionID  string  `json:"session_id"`
	AccountID  string  `json:"account_id,omitempty"`
	ProcessRef string  `json:"process_ref"`
	Cursor     string  `json:"cursor"`
	WaitMs     int     `json:"wait_ms,omitempty"`
	StdinText  *string `json:"stdin_text,omitempty"`
	InputSeq   *int64  `json:"input_seq,omitempty"`
	CloseStdin bool    `json:"close_stdin,omitempty"`
}

type terminateProcessRequest struct {
	SessionID  string `json:"session_id"`
	AccountID  string `json:"account_id,omitempty"`
	ProcessRef string `json:"process_ref"`
}

type resizeProcessRequest struct {
	SessionID  string `json:"session_id"`
	AccountID  string `json:"account_id,omitempty"`
	ProcessRef string `json:"process_ref"`
	Rows       int    `json:"rows"`
	Cols       int    `json:"cols"`
}

type processOutputItem struct {
	Seq    uint64 `json:"seq"`
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

type processResponse struct {
	Status           string              `json:"status"`
	ProcessRef       string              `json:"process_ref,omitempty"`
	Stdout           string              `json:"stdout,omitempty"`
	Stderr           string              `json:"stderr,omitempty"`
	ExitCode         *int                `json:"exit_code,omitempty"`
	Cursor           string              `json:"cursor,omitempty"`
	NextCursor       string              `json:"next_cursor,omitempty"`
	Items            []processOutputItem `json:"items,omitempty"`
	HasMore          bool                `json:"has_more,omitempty"`
	AcceptedInputSeq *int64              `json:"accepted_input_seq,omitempty"`
	Truncated        bool                `json:"truncated,omitempty"`
	OutputRef        string              `json:"output_ref,omitempty"`
	Artifacts        []artifactRef       `json:"artifacts,omitempty"`
}

type processExecArgs struct {
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

var validProcessModes = []string{"buffered", "follow", "stdin", "pty"}

func (e *ToolExecutor) executeProcessCommand(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	resolvedCtx, bindingErr := e.ensureEnvironmentBindings(ctx, execCtx)
	if bindingErr != nil {
		return tools.ExecutionResult{Error: bindingErr, DurationMs: durationMs(started)}
	}
	execCtx = resolvedCtx

	reqArgs, argErr := parseProcessExecArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{Error: argErr, DurationMs: durationMs(started)}
	}

	payload, err := json.Marshal(processExecRequest{
		SessionID:     execCtx.RunID.String(),
		AccountID:     resolveAccountID(execCtx),
		ProfileRef:    resolveProfileRef(execCtx),
		WorkspaceRef:  resolveWorkspaceRef(execCtx),
		EnabledSkills: append([]skillstore.ResolvedSkill(nil), execCtx.EnabledSkills...),
		Tier:          resolveTier("exec_command", execCtx.Budget),
		Command:       reqArgs.Command,
		Mode:          reqArgs.Mode,
		Cwd:           reqArgs.Cwd,
		TimeoutMs:     reqArgs.TimeoutMs,
		Size:          reqArgs.Size,
		Env:           reqArgs.Env,
	})
	if err != nil {
		return errResult(errorSandboxError, fmt.Sprintf("marshal request failed: %s", err.Error()), started)
	}
	return e.executeProcessRequest(ctx, e.baseURL+"/v1/process/exec", payload, "exec_command", resolveAccountID(execCtx), execCtx.RunID.String(), execCtx.PerToolSoftLimits, started)
}

func (e *ToolExecutor) executeContinueProcess(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	resolvedCtx, bindingErr := e.ensureEnvironmentBindings(ctx, execCtx)
	if bindingErr != nil {
		return tools.ExecutionResult{Error: bindingErr, DurationMs: durationMs(started)}
	}
	execCtx = resolvedCtx

	reqArgs, argErr := parseContinueProcessArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{Error: argErr, DurationMs: durationMs(started)}
	}
	limit := tools.ResolveToolSoftLimit(execCtx.PerToolSoftLimits, "continue_process")
	if limit.MaxWaitTimeMs != nil && reqArgs.WaitMs > *limit.MaxWaitTimeMs {
		reqArgs.WaitMs = *limit.MaxWaitTimeMs
	}

	payload, err := json.Marshal(continueProcessRequest{
		SessionID:  execCtx.RunID.String(),
		AccountID:  resolveAccountID(execCtx),
		ProcessRef: reqArgs.ProcessRef,
		Cursor:     reqArgs.Cursor,
		WaitMs:     reqArgs.WaitMs,
		StdinText:  reqArgs.StdinText,
		InputSeq:   reqArgs.InputSeq,
		CloseStdin: reqArgs.CloseStdin,
	})
	if err != nil {
		return errResult(errorSandboxError, fmt.Sprintf("marshal request failed: %s", err.Error()), started)
	}
	return e.executeProcessRequest(ctx, e.baseURL+"/v1/process/continue", payload, "continue_process", resolveAccountID(execCtx), execCtx.RunID.String(), execCtx.PerToolSoftLimits, started)
}

func (e *ToolExecutor) executeTerminateProcess(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	reqArgs, argErr := parseTerminateProcessArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{Error: argErr, DurationMs: durationMs(started)}
	}
	payload, err := json.Marshal(terminateProcessRequest{
		SessionID:  execCtx.RunID.String(),
		AccountID:  resolveAccountID(execCtx),
		ProcessRef: reqArgs.ProcessRef,
	})
	if err != nil {
		return errResult(errorSandboxError, fmt.Sprintf("marshal request failed: %s", err.Error()), started)
	}
	return e.executeProcessRequest(ctx, e.baseURL+"/v1/process/terminate", payload, "terminate_process", resolveAccountID(execCtx), execCtx.RunID.String(), execCtx.PerToolSoftLimits, started)
}

func (e *ToolExecutor) executeResizeProcess(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	reqArgs, argErr := parseResizeProcessArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{Error: argErr, DurationMs: durationMs(started)}
	}
	payload, err := json.Marshal(resizeProcessRequest{
		SessionID:  execCtx.RunID.String(),
		AccountID:  resolveAccountID(execCtx),
		ProcessRef: reqArgs.ProcessRef,
		Rows:       reqArgs.Rows,
		Cols:       reqArgs.Cols,
	})
	if err != nil {
		return errResult(errorSandboxError, fmt.Sprintf("marshal request failed: %s", err.Error()), started)
	}
	return e.executeProcessRequest(ctx, e.baseURL+"/v1/process/resize", payload, "resize_process", resolveAccountID(execCtx), execCtx.RunID.String(), execCtx.PerToolSoftLimits, started)
}

func (e *ToolExecutor) executeProcessRequest(
	ctx context.Context,
	endpoint string,
	payload []byte,
	toolName string,
	accountID string,
	runID string,
	softLimits tools.PerToolSoftLimits,
	started time.Time,
) tools.ExecutionResult {
	resp, reqErr := e.doJSONRequest(ctx, http.MethodPost, endpoint, payload, accountID)
	if reqErr != nil {
		return errResult(reqErr.errorClass, reqErr.message, started)
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return errResult(errorSandboxError, "read response body failed", started)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mapHTTPError(resp.StatusCode, body, started)
	}
	var result processResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return errResult(errorSandboxError, "decode response failed", started)
	}

	stdout := sanitizeShellOutput(result.Stdout)
	stderr := sanitizeShellOutput(result.Stderr)

	// persist full output before truncation
	combined := stdout + stderr
	persistedPath := tools.PersistLargeOutput(runID, combined)

	// tail-truncate
	stdout, stdoutTrunc := tools.TruncateOutputField(stdout, tools.TruncateMaxChars)
	stderr, stderrTrunc := tools.TruncateOutputField(stderr, tools.TruncateMaxChars/4)
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		text := sanitizeShellOutput(item.Text)
		text, _ = tools.TruncateOutputField(text, tools.TruncateMaxChars)
		items = append(items, map[string]any{
			"seq":    item.Seq,
			"stream": item.Stream,
			"text":   text,
		})
	}

	truncated := result.Truncated || stdoutTrunc || stderrTrunc
	resultJSON := map[string]any{
		"status":      result.Status,
		"stdout":      stdout,
		"stderr":      stderr,
		"running":     result.Status == "running",
		"cursor":      result.Cursor,
		"next_cursor": result.NextCursor,
		"items":       items,
		"has_more":    result.HasMore,
		"truncated":   truncated,
		"duration_ms": durationMs(started),
	}
	if persistedPath != "" {
		resultJSON["full_output_path"] = persistedPath
	}
	if ref := strings.TrimSpace(result.ProcessRef); ref != "" {
		resultJSON["process_ref"] = ref
	}
	if result.ExitCode != nil {
		resultJSON["exit_code"] = *result.ExitCode
	}
	if result.AcceptedInputSeq != nil {
		resultJSON["accepted_input_seq"] = *result.AcceptedInputSeq
	}
	outputRef := strings.TrimSpace(result.OutputRef)
	if outputRef == "" && result.Truncated {
		ref := strings.TrimSpace(result.ProcessRef)
		if ref == "" {
			ref = "buffered"
		}
		outputRef = fmt.Sprintf("process:%s:%s:%s", ref, strings.TrimSpace(result.Cursor), strings.TrimSpace(result.NextCursor))
	}
	if outputRef != "" {
		resultJSON["output_ref"] = outputRef
	}
	if len(result.Artifacts) > 0 {
		resultJSON["artifacts"] = result.Artifacts
	}
	return tools.ExecutionResult{ResultJSON: resultJSON, DurationMs: durationMs(started)}
}

func parseProcessExecArgs(args map[string]any) (processExecArgs, *tools.ExecutionError) {
	for key := range args {
		switch key {
		case "command", "mode", "cwd", "timeout_ms", "size", "env":
		case "session_mode", "session_ref", "from_session_ref", "share_scope", "yield_time_ms", "background":
			return processExecArgs{}, sandboxArgsError(fmt.Sprintf("parameter %s is not supported", key))
		default:
			return processExecArgs{}, sandboxArgsError(fmt.Sprintf("parameter %s is not supported", key))
		}
	}
	command := readStringArg(args, "command")
	if strings.TrimSpace(command) == "" {
		return processExecArgs{}, sandboxArgsError("parameter command is required")
	}
	mode := strings.TrimSpace(readStringArg(args, "mode"))
	if mode == "" {
		mode = "buffered"
	}
	if !slices.Contains(validProcessModes, mode) {
		return processExecArgs{}, sandboxArgsError(fmt.Sprintf("parameter mode must be one of %s", strings.Join(validProcessModes, ", ")))
	}
	req := processExecArgs{
		Command:   command,
		Mode:      mode,
		Cwd:       readStringArg(args, "cwd"),
		TimeoutMs: readIntArg(args, "timeout_ms"),
		Size:      readProcessSizeArg(args["size"]),
		Env:       readNullableStringMapArg(args["env"]),
	}
	if _, ok := args["size"]; ok && req.Size == nil {
		return processExecArgs{}, sandboxArgsError("parameter size must include positive rows and cols")
	}
	if req.Size != nil && mode != "pty" {
		return processExecArgs{}, sandboxArgsError("parameter size is only supported when mode is pty")
	}
	if mode != "buffered" && req.TimeoutMs <= 0 {
		return processExecArgs{}, sandboxArgsError("parameter timeout_ms is required for follow, stdin, and pty modes")
	}
	return req, nil
}

func parseContinueProcessArgs(args map[string]any) (continueProcessArgs, *tools.ExecutionError) {
	for key := range args {
		switch key {
		case "process_ref", "cursor", "wait_ms", "stdin_text", "input_seq", "close_stdin":
		case "session_ref", "chars", "yield_time_ms":
			return continueProcessArgs{}, sandboxArgsError(fmt.Sprintf("parameter %s is not supported", key))
		default:
			return continueProcessArgs{}, sandboxArgsError(fmt.Sprintf("parameter %s is not supported", key))
		}
	}
	processRef := strings.TrimSpace(readStringArg(args, "process_ref"))
	if processRef == "" {
		return continueProcessArgs{}, sandboxArgsError("parameter process_ref is required")
	}
	cursor := strings.TrimSpace(readStringArg(args, "cursor"))
	if cursor == "" {
		return continueProcessArgs{}, sandboxArgsError("parameter cursor is required")
	}
	req := continueProcessArgs{
		ProcessRef: processRef,
		Cursor:     cursor,
		WaitMs:     readIntArg(args, "wait_ms"),
		CloseStdin: readBoolArg(args, "close_stdin"),
	}
	if raw, ok := args["stdin_text"]; ok {
		text, ok := raw.(string)
		if !ok {
			return continueProcessArgs{}, sandboxArgsError("parameter stdin_text must be a string")
		}
		req.StdinText = &text
	}
	if raw, ok := args["input_seq"]; ok {
		value := int64(readIntArg(map[string]any{"input_seq": raw}, "input_seq"))
		req.InputSeq = &value
	}
	if req.StdinText != nil && (req.InputSeq == nil || *req.InputSeq <= 0) {
		return continueProcessArgs{}, sandboxArgsError("parameter input_seq is required when stdin_text is provided")
	}
	if req.StdinText == nil && req.InputSeq != nil {
		return continueProcessArgs{}, sandboxArgsError("parameter input_seq is not supported without stdin_text")
	}
	return req, nil
}

func parseTerminateProcessArgs(args map[string]any) (terminateProcessArgs, *tools.ExecutionError) {
	for key := range args {
		switch key {
		case "process_ref":
		default:
			return terminateProcessArgs{}, sandboxArgsError(fmt.Sprintf("parameter %s is not supported", key))
		}
	}
	processRef := strings.TrimSpace(readStringArg(args, "process_ref"))
	if processRef == "" {
		return terminateProcessArgs{}, sandboxArgsError("parameter process_ref is required")
	}
	return terminateProcessArgs{ProcessRef: processRef}, nil
}

func parseResizeProcessArgs(args map[string]any) (resizeProcessArgs, *tools.ExecutionError) {
	for key := range args {
		switch key {
		case "process_ref", "rows", "cols":
		default:
			return resizeProcessArgs{}, sandboxArgsError(fmt.Sprintf("parameter %s is not supported", key))
		}
	}
	processRef := strings.TrimSpace(readStringArg(args, "process_ref"))
	if processRef == "" {
		return resizeProcessArgs{}, sandboxArgsError("parameter process_ref is required")
	}
	rows := readIntArg(args, "rows")
	cols := readIntArg(args, "cols")
	if rows <= 0 || cols <= 0 {
		return resizeProcessArgs{}, sandboxArgsError("parameters rows and cols must be greater than 0")
	}
	return resizeProcessArgs{ProcessRef: processRef, Rows: rows, Cols: cols}, nil
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
			copy := typed
			out[key] = &copy
		case nil:
			out[key] = nil
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
