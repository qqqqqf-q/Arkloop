package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"arkloop/services/shared/skillstore"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/environmentbindings"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	errorSandboxError        = "tool.sandbox_error"
	errorSandboxUnavailable  = "tool.sandbox_unavailable"
	errorSandboxTimeout      = "tool.sandbox_timeout"
	errorPermissionDenied    = "tool.permission_denied"
	errorArgsInvalid         = "tool.args_invalid"
	errorNotConfigured       = "config.missing"
	errorMaxSessionsExceeded = "tool.max_sessions_exceeded"

	defaultTimeoutMs  = 30_000
	maxOutputBytes    = 32 * 1024
	httpClientTimeout = 5 * time.Minute
	rtkRewriteTimeout = 1500 * time.Millisecond
)

type execRequest struct {
	SessionID     string                     `json:"session_id"`
	AccountID     string                     `json:"account_id,omitempty"`
	ProfileRef    string                     `json:"profile_ref,omitempty"`
	WorkspaceRef  string                     `json:"workspace_ref,omitempty"`
	EnabledSkills []skillstore.ResolvedSkill `json:"enabled_skills,omitempty"`
	Tier          string                     `json:"tier"`
	Language      string                     `json:"language"`
	Code          string                     `json:"code"`
	TimeoutMs     int                        `json:"timeout_ms"`
}

type execResponse struct {
	SessionID  string        `json:"session_id"`
	Stdout     string        `json:"stdout"`
	Stderr     string        `json:"stderr"`
	ExitCode   int           `json:"exit_code"`
	DurationMs int64         `json:"duration_ms"`
	Artifacts  []artifactRef `json:"artifacts,omitempty"`
}

type execCommandRequest struct {
	SessionID     string                     `json:"session_id"`
	OpenMode      string                     `json:"open_mode,omitempty"`
	AccountID     string                     `json:"account_id,omitempty"`
	ProfileRef    string                     `json:"profile_ref,omitempty"`
	WorkspaceRef  string                     `json:"workspace_ref,omitempty"`
	EnabledSkills []skillstore.ResolvedSkill `json:"enabled_skills,omitempty"`
	Tier          string                     `json:"tier,omitempty"`
	Cwd           string                     `json:"cwd,omitempty"`
	Command       string                     `json:"command"`
	TimeoutMs     int                        `json:"timeout_ms,omitempty"`
	YieldTimeMs   int                        `json:"yield_time_ms,omitempty"`
	Env           map[string]string          `json:"env,omitempty"`
}

type writeStdinRequest struct {
	SessionID   string `json:"session_id"`
	AccountID   string `json:"account_id,omitempty"`
	Chars       string `json:"chars,omitempty"`
	YieldTimeMs int    `json:"yield_time_ms,omitempty"`
}

type forkSessionRequest struct {
	AccountID     string `json:"account_id,omitempty"`
	FromSessionID string `json:"from_session_id"`
	ToSessionID   string `json:"to_session_id"`
}

type forkSessionResponse struct {
	RestoreRevision string `json:"restore_revision,omitempty"`
}

type execSessionResponse struct {
	SessionID       string        `json:"session_id"`
	Status          string        `json:"status"`
	Cwd             string        `json:"cwd"`
	Output          string        `json:"output"`
	Running         bool          `json:"running"`
	Truncated       bool          `json:"truncated"`
	TimedOut        bool          `json:"timed_out"`
	ExitCode        *int          `json:"exit_code,omitempty"`
	Artifacts       []artifactRef `json:"artifacts,omitempty"`
	Restored        bool          `json:"restored,omitempty"`
	RestoreRevision string        `json:"restore_revision,omitempty"`
}

type outputDeltasResponse struct {
	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`
	Running bool   `json:"running"`
}

type execCommandArgs struct {
	SessionMode    string
	SessionRef     string
	FromSessionRef string
	ShareScope     string
	Cwd            string
	Command        string
	TimeoutMs      int
	YieldTimeMs    int
	Env            map[string]string
}

type writeStdinArgs struct {
	SessionRef  string
	Chars       string
	YieldTimeMs int
}

type browserArgs struct {
	Command     string
	YieldTimeMs int
}

type artifactRef struct {
	Key      string `json:"key"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

type ToolExecutor struct {
	baseURL             string
	authToken           string
	client              *http.Client
	orchestrator        *sessionOrchestrator
	browserOrchestrator *sessionOrchestrator
}

func NewToolExecutor(baseURL, authToken string) *ToolExecutor {
	return NewToolExecutorWithPool(baseURL, authToken, nil)
}

func NewToolExecutorWithPool(baseURL, authToken string, pool *pgxpool.Pool) *ToolExecutor {
	return &ToolExecutor{
		baseURL:             baseURL,
		authToken:           authToken,
		client:              &http.Client{Timeout: httpClientTimeout},
		orchestrator:        newSessionOrchestrator(pool),
		browserOrchestrator: newSessionOrchestratorWithType(pool, data.ShellSessionTypeBrowser),
	}
}

func (e *ToolExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	started := time.Now()

	if e.baseURL == "" {
		return errResult(errorNotConfigured, "sandbox service not configured", started)
	}

	switch toolName {
	case "python_execute":
		return e.executePython(ctx, args, execCtx, started)
	case "exec_command":
		return e.executeExecCommand(ctx, args, execCtx, toolCallID, started)
	case "write_stdin":
		return e.executeWriteStdin(ctx, args, execCtx, toolCallID, started)
	case "browser":
		return e.executeBrowser(ctx, args, execCtx, toolCallID, started)
	default:
		return errResult(errorArgsInvalid, fmt.Sprintf("unknown sandbox tool: %s", toolName), started)
	}
}

func (e *ToolExecutor) executePython(
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

	code, _ := args["code"].(string)
	if code == "" {
		return errResult(errorArgsInvalid, "parameter code is required", started)
	}

	payload, err := json.Marshal(execRequest{
		SessionID:     execCtx.RunID.String(),
		AccountID:     resolveAccountID(execCtx),
		ProfileRef:    resolveProfileRef(execCtx),
		WorkspaceRef:  resolveWorkspaceRef(execCtx),
		EnabledSkills: append([]skillstore.ResolvedSkill(nil), execCtx.EnabledSkills...),
		Tier:          resolveTier("python_execute", execCtx.Budget),
		Language:      "python",
		Code:          code,
		TimeoutMs:     resolveTimeoutMs(args),
	})
	if err != nil {
		return errResult(errorSandboxError, fmt.Sprintf("marshal request failed: %s", err.Error()), started)
	}

	resp, reqErr := e.doJSONRequest(ctx, http.MethodPost, e.baseURL+"/v1/exec", payload, resolveAccountID(execCtx))
	if reqErr != nil {
		return errResult(reqErr.errorClass, reqErr.message, started)
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return errResult(errorSandboxError, "read response body failed", started)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mapHTTPError(resp.StatusCode, respBody, started)
	}

	var result execResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return errResult(errorSandboxError, "decode response failed", started)
	}

	resultJSON := map[string]any{
		"stdout":      truncateOutput(sanitizeShellOutput(result.Stdout)),
		"stderr":      truncateOutput(sanitizeShellOutput(result.Stderr)),
		"exit_code":   result.ExitCode,
		"duration_ms": result.DurationMs,
	}
	if len(result.Artifacts) > 0 {
		resultJSON["artifacts"] = result.Artifacts
	}
	return tools.ExecutionResult{ResultJSON: resultJSON, DurationMs: durationMs(started)}
}

func (e *ToolExecutor) executeExecCommand(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
	started time.Time,
) tools.ExecutionResult {
	resolvedCtx, bindingErr := e.ensureEnvironmentBindings(ctx, execCtx)
	if bindingErr != nil {
		return tools.ExecutionResult{Error: bindingErr, DurationMs: durationMs(started)}
	}
	execCtx = resolvedCtx

	reqArgs, argErr := parseExecCommandArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{Error: argErr, DurationMs: durationMs(started)}
	}
	resolution, resolveErr := e.orchestrator.resolveExecSession(ctx, reqArgs, execCtx)
	if resolveErr != nil {
		return tools.ExecutionResult{Error: resolveErr, DurationMs: durationMs(started)}
	}
	if strings.TrimSpace(resolution.FromSessionRef) != "" {
		forked, forkErr := e.forkSessionCheckpoint(ctx, execCtx, resolution.FromSessionRef, resolution.SessionRef)
		if forkErr != nil {
			return tools.ExecutionResult{Error: forkErr, DurationMs: durationMs(started)}
		}
		if strings.TrimSpace(forked) != "" && resolution.Record != nil {
			resolution.Record.LatestRestoreRev = stringPtr(forked)
		}
	}
	leaseOwnerID := writerLeaseOwner(execCtx, toolCallID)
	leaseErr := e.orchestrator.prepareExecWriterLease(ctx, execCtx, resolution, leaseOwnerID, reqArgs.TimeoutMs)
	if leaseErr != nil {
		if isSessionBusy(leaseErr) && isCrossRunLease(resolution, execCtx) {
			if e.tryRecoverStaleSessionLease(ctx, execCtx, resolution, leaseOwnerID, reqArgs.TimeoutMs) {
				leaseErr = nil
			}
		}
		if leaseErr != nil {
			return tools.ExecutionResult{Error: leaseErr, DurationMs: durationMs(started)}
		}
	}

	command := reqArgs.Command
	if rewritten := rtkRewriteSandbox(command); rewritten != "" {
		command = rewritten
	}

	request := execCommandRequest{
		SessionID:     resolution.SessionRef,
		OpenMode:      resolution.OpenMode,
		AccountID:     resolveAccountID(execCtx),
		ProfileRef:    resolution.ProfileRef(resolveProfileRef(execCtx)),
		WorkspaceRef:  resolution.WorkspaceRef(resolveWorkspaceRef(execCtx)),
		EnabledSkills: append([]skillstore.ResolvedSkill(nil), execCtx.EnabledSkills...),
		Tier:          resolveTier("exec_command", execCtx.Budget),
		Cwd:           reqArgs.Cwd,
		Command:       command,
		TimeoutMs:     reqArgs.TimeoutMs,
		YieldTimeMs:   reqArgs.YieldTimeMs,
		Env:           reqArgs.Env,
	}
	result := e.executeExecSessionRequest(ctx, e.baseURL+"/v1/exec_command", "exec_command", request, request.AccountID, execCtx.PerToolSoftLimits, started)
	if result.Error != nil && isSessionUnavailable(result.Error) {
		fallback, fallbackErr := e.orchestrator.resolveFallbackSession(ctx, reqArgs, execCtx, resolution)
		if fallbackErr != nil {
			return tools.ExecutionResult{Error: fallbackErr, DurationMs: durationMs(started)}
		}
		if fallback != nil {
			resolution = fallback
			if leaseErr := e.orchestrator.prepareExecWriterLease(ctx, execCtx, resolution, leaseOwnerID, reqArgs.TimeoutMs); leaseErr != nil {
				return tools.ExecutionResult{Error: leaseErr, DurationMs: durationMs(started)}
			}
			request.SessionID = resolution.SessionRef
			request.OpenMode = resolution.OpenMode
			request.ProfileRef = resolution.ProfileRef(resolveProfileRef(execCtx))
			request.WorkspaceRef = resolution.WorkspaceRef(resolveWorkspaceRef(execCtx))
			result = e.executeExecSessionRequest(ctx, e.baseURL+"/v1/exec_command", "exec_command", request, request.AccountID, execCtx.PerToolSoftLimits, started)
		}
	}
	if result.Error != nil {
		if isSessionBusy(result.Error) {
			e.orchestrator.releaseWriterLease(ctx, execCtx, resolution, leaseOwnerID)
		}
		return result
	}
	resp := decodeExecSessionResult(result.ResultJSON)
	if resp != nil {
		if !resp.Running {
			_ = e.orchestrator.clearFinishedWriterLease(ctx, execCtx, resolution)
		}
		e.orchestrator.markResult(ctx, execCtx, resolution, *resp)
		result.ResultJSON["session_ref"] = resolution.SessionRef
		result.ResultJSON["share_scope"] = resolution.ShareScope
		result.ResultJSON["resolved_via"] = resolution.ResolvedVia
		result.ResultJSON["reused"] = resolution.Reused
		result.ResultJSON["restored_from_restore_state"] = resp.Restored || resolution.RestoredFromRestoreState

		// When running=true with empty output (typical during session initialization),
		// auto-poll write_stdin so the model receives actual output instead of an
		// empty running state that causes reasoning models to produce no response.
		if resp.Running && strings.TrimSpace(resp.Output) == "" {
			// Detect heredoc syntax and emit stdin_expected event
			if strings.Contains(reqArgs.Command, "<<") {
				toolName := "exec_command"
				ev := execCtx.Emitter.Emit(
					"terminal.stdin_expected",
					map[string]any{
						"session_ref": resolution.SessionRef,
						"command":     reqArgs.Command,
						"hint":        "heredoc command detected, use write_stdin to send content",
					},
					&toolName,
					nil,
				)
				result.Events = append(result.Events, ev)
			}
			if polled := e.pollExecUntilOutputOrDone(ctx, execCtx, resolution, result.ResultJSON, reqArgs, started); polled != nil {
				result = *polled
			}
		}

		// When command is still running after initial response, poll for output deltas
		// and emit them as events for real-time streaming to the client.
		if resp.Running {
			var wg sync.WaitGroup
			var deltaEvents []events.RunEvent
			var deltaMu sync.Mutex

			emitDelta := func(stream string, chunk string) {
				deltaMu.Lock()
				defer deltaMu.Unlock()
				toolName := "exec_command"
				ev := execCtx.Emitter.Emit(
					"terminal."+stream+"_delta",
					map[string]any{
						"session_ref": resolution.SessionRef,
						"chunk":       chunk,
					},
					&toolName,
					nil,
				)
				deltaEvents = append(deltaEvents, ev)
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				e.pollOutputDeltas(ctx, resolution.SessionRef, request.AccountID, emitDelta)
			}()

			// Also poll for any remaining deltas after pollExecUntilOutputOrDone
			if resp.Running {
				wg.Add(1)
				go func() {
					defer wg.Done()
					// Brief delay to capture any trailing output
					time.Sleep(100 * time.Millisecond)
					e.pollOutputDeltas(ctx, resolution.SessionRef, request.AccountID, emitDelta)
				}()
			}

			wg.Wait()
			result.Events = append(result.Events, deltaEvents...)
		}
	}
	delete(result.ResultJSON, "session_id")
	return result
}

// pollExecUntilOutputOrDone polls write_stdin when exec_command yields with running=true
// and no output yet. This handles the case where session initialization takes longer than
// yield_time_ms, leaving the model with an empty state it cannot meaningfully respond to.
// Returns nil if polling should not replace the original result (error or immediate break).
func (e *ToolExecutor) pollExecUntilOutputOrDone(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	resolution *resolvedSession,
	prevMeta map[string]any,
	reqArgs execCommandArgs,
	started time.Time,
) *tools.ExecutionResult {
	const maxPolls = 6
	pollYieldMs := reqArgs.YieldTimeMs
	if pollYieldMs <= 0 {
		pollYieldMs = 10_000
	}

	var last *tools.ExecutionResult
	for range maxPolls {
		pollReq := writeStdinRequest{
			SessionID:   resolution.SessionRef,
			AccountID:   resolveAccountID(execCtx),
			YieldTimeMs: pollYieldMs,
		}
		pollResult := e.executeExecSessionRequest(
			ctx, e.baseURL+"/v1/write_stdin", "write_stdin",
			pollReq, pollReq.AccountID, execCtx.PerToolSoftLimits, started,
		)
		if pollResult.Error != nil {
			return nil
		}
		resp := decodeExecSessionResult(pollResult.ResultJSON)
		if resp == nil {
			return nil
		}
		// Carry over exec_command identity fields.
		pollResult.ResultJSON["session_ref"] = prevMeta["session_ref"]
		pollResult.ResultJSON["share_scope"] = prevMeta["share_scope"]
		pollResult.ResultJSON["resolved_via"] = prevMeta["resolved_via"]
		pollResult.ResultJSON["reused"] = prevMeta["reused"]
		pollResult.ResultJSON["restored_from_restore_state"] = prevMeta["restored_from_restore_state"]
		delete(pollResult.ResultJSON, "session_id")
		last = &pollResult

		if !resp.Running || strings.TrimSpace(resp.Output) != "" {
			break
		}
	}

	// Fallback: if we exhausted all polls and still have a running process,
	// mark the last result with a timeout error so the caller knows.
	// This handles the case where a command like "python3 <<EOF" hangs because
	// stdin was not properly closed.
	if last != nil {
		resp := decodeExecSessionResult(last.ResultJSON)
		if resp != nil && resp.Running {
			last.Error = &tools.ExecutionError{
				ErrorClass: errorSandboxTimeout,
				Message:    "exec_command timed out after " + fmt.Sprintf("%d", maxPolls) + " polls, session still running",
				Details:    map[string]any{"session_ref": resolution.SessionRef, "polls": maxPolls},
			}
		}
	}

	return last
}

func (e *ToolExecutor) executeWriteStdin(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
	started time.Time,
) tools.ExecutionResult {
	resolvedCtx, bindingErr := e.ensureEnvironmentBindings(ctx, execCtx)
	if bindingErr != nil {
		return tools.ExecutionResult{Error: bindingErr, DurationMs: durationMs(started)}
	}
	execCtx = resolvedCtx

	reqArgs, argErr := parseWriteStdinArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{Error: argErr, DurationMs: durationMs(started)}
	}
	resolution, resolveErr := e.orchestrator.resolveWriteSession(ctx, reqArgs, execCtx)
	if resolveErr != nil {
		return tools.ExecutionResult{Error: resolveErr, DurationMs: durationMs(started)}
	}
	leaseOwnerID := writerLeaseOwner(execCtx, toolCallID)
	if leaseErr := e.orchestrator.prepareWriteWriterLease(ctx, execCtx, resolution, leaseOwnerID, reqArgs.Chars != ""); leaseErr != nil {
		return tools.ExecutionResult{Error: leaseErr, DurationMs: durationMs(started)}
	}

	request := writeStdinRequest{
		SessionID:   resolution.SessionRef,
		AccountID:   resolveAccountID(execCtx),
		Chars:       reqArgs.Chars,
		YieldTimeMs: clampYieldTimeMs(reqArgs.YieldTimeMs, tools.ResolveToolSoftLimit(execCtx.PerToolSoftLimits, "write_stdin")),
	}
	result := e.executeExecSessionRequest(ctx, e.baseURL+"/v1/write_stdin", "write_stdin", request, request.AccountID, execCtx.PerToolSoftLimits, started)
	if result.Error != nil {
		if reqArgs.Chars != "" && isSessionNotRunning(result.Error) {
			_ = e.orchestrator.clearFinishedWriterLease(ctx, execCtx, resolution)
		}
		return result
	}
	resp := decodeExecSessionResult(result.ResultJSON)
	if resp != nil {
		e.orchestrator.reconcileWriteWriterLease(ctx, execCtx, resolution, leaseOwnerID, reqArgs.Chars != "", *resp)
		e.orchestrator.markResult(ctx, execCtx, resolution, *resp)
		result.ResultJSON["session_ref"] = resolution.SessionRef
		result.ResultJSON["share_scope"] = resolution.ShareScope
		result.ResultJSON["resolved_via"] = resolution.ResolvedVia
		result.ResultJSON["reused"] = true
		result.ResultJSON["restored_from_restore_state"] = false
	}

	// Emit TerminalInteraction event so the model sees what was sent to stdin.
	// This enables the model to know what input was auto-filled and respond accordingly.
	toolName := "write_stdin"
	ev := execCtx.Emitter.Emit(
		"terminal.stdin_interaction",
		map[string]any{
			"session_ref": resolution.SessionRef,
			"chars":        reqArgs.Chars,
			"running":      resp != nil && resp.Running,
		},
		&toolName,
		nil,
	)
	result.Events = []events.RunEvent{ev}

	delete(result.ResultJSON, "session_id")
	return result
}

func (e *ToolExecutor) executeBrowser(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
	started time.Time,
) tools.ExecutionResult {
	_ = toolCallID
	resolvedCtx, bindingErr := e.ensureEnvironmentBindings(ctx, execCtx)
	if bindingErr != nil {
		return tools.ExecutionResult{Error: bindingErr, DurationMs: durationMs(started)}
	}
	execCtx = resolvedCtx

	reqArgs, argErr := parseBrowserArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{Error: argErr, DurationMs: durationMs(started)}
	}
	resolution, resolveErr := e.browserOrchestrator.resolveBrowserSession(ctx, reqArgs, execCtx)
	if resolveErr != nil {
		return tools.ExecutionResult{Error: resolveErr, DurationMs: durationMs(started)}
	}
	preparedCommand := preparedBrowserCommand(reqArgs.Command)
	request := execCommandRequest{
		SessionID:     resolution.SessionRef,
		OpenMode:      resolution.OpenMode,
		AccountID:     resolveAccountID(execCtx),
		ProfileRef:    resolution.ProfileRef(resolveProfileRef(execCtx)),
		WorkspaceRef:  resolution.WorkspaceRef(resolveWorkspaceRef(execCtx)),
		EnabledSkills: append([]skillstore.ResolvedSkill(nil), execCtx.EnabledSkills...),
		Tier:          "browser",
		Command:       buildBrowserCommand(resolution.SessionRef, preparedCommand),
		TimeoutMs:     defaultTimeoutMs,
		YieldTimeMs:   effectiveBrowserYieldTimeMs(reqArgs.Command, reqArgs.YieldTimeMs),
	}
	result := e.executeExecSessionRequest(ctx, e.baseURL+"/v1/exec_command", "exec_command", request, request.AccountID, execCtx.PerToolSoftLimits, started)
	if result.Error != nil {
		switch {
		case isSessionBusy(result.Error):
			result = e.retryBusyBrowserCommand(ctx, execCtx, resolution.SessionRef, request, reqArgs.YieldTimeMs, started)
		case isSessionUnavailable(result.Error):
			resolution, result = e.retryUnavailableBrowserCommand(ctx, execCtx, resolution, request, preparedCommand, started)
		default:
			return normalizeBrowserExecutionFailure(result.Error, started)
		}
	}
	if result.Error != nil {
		return result
	}
	result = e.settleBrowserResult(ctx, result, execCtx, resolution.SessionRef, reqArgs.YieldTimeMs, started)
	if result.Error != nil {
		return result
	}
	resp := decodeExecSessionResult(result.ResultJSON)
	if resp == nil {
		return errResult(errorSandboxError, "decode response failed", started)
	}
	e.browserOrchestrator.markResult(ctx, execCtx, resolution, *resp)
	publicResult, publicErr := buildBrowserPublicResult(reqArgs.Command, resp, execCtx.PerToolSoftLimits, started)
	if publicErr != nil {
		return tools.ExecutionResult{Error: publicErr, DurationMs: durationMs(started)}
	}
	result = tools.ExecutionResult{ResultJSON: publicResult, DurationMs: durationMs(started)}
	if shouldAutoScreenshot(reqArgs.Command) {
		screenshotReq := execCommandRequest{
			SessionID:     resolution.SessionRef,
			AccountID:     resolveAccountID(execCtx),
			EnabledSkills: append([]skillstore.ResolvedSkill(nil), execCtx.EnabledSkills...),
			Tier:          "browser",
			Command:       buildBrowserCommand(resolution.SessionRef, buildAutoScreenshotCommand()),
			TimeoutMs:     screenshotTimeoutMs,
		}
		screenshotResult := e.executeExecSessionRequest(ctx, e.baseURL+"/v1/exec_command", "exec_command", screenshotReq, screenshotReq.AccountID, execCtx.PerToolSoftLimits, started)
		if screenshotResult.Error == nil {
			mergeScreenshotArtifacts(&result.ResultJSON, screenshotResult.ResultJSON)
		}
	}
	return result
}

func (e *ToolExecutor) settleBrowserResult(
	ctx context.Context,
	result tools.ExecutionResult,
	execCtx tools.ExecutionContext,
	sessionRef string,
	requestedYieldTimeMs int,
	started time.Time,
) tools.ExecutionResult {
	resp := decodeExecSessionResult(result.ResultJSON)
	if resp == nil || !resp.Running {
		return result
	}
	pollResult, waitErr := e.waitForBrowserSessionIdle(ctx, execCtx, sessionRef, requestedYieldTimeMs, started)
	if waitErr != nil {
		return normalizeBrowserExecutionFailure(waitErr, started)
	}
	return pollResult
}

func (e *ToolExecutor) retryBusyBrowserCommand(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	sessionRef string,
	request execCommandRequest,
	requestedYieldTimeMs int,
	started time.Time,
) tools.ExecutionResult {
	_, waitErr := e.waitForBrowserSessionIdle(ctx, execCtx, sessionRef, requestedYieldTimeMs, started)
	if waitErr != nil {
		return normalizeBrowserExecutionFailure(waitErr, started)
	}
	result := e.executeExecSessionRequest(ctx, e.baseURL+"/v1/exec_command", "exec_command", request, request.AccountID, execCtx.PerToolSoftLimits, started)
	if result.Error != nil {
		return normalizeBrowserExecutionFailure(result.Error, started)
	}
	return result
}

func (e *ToolExecutor) waitForBrowserSessionIdle(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	sessionRef string,
	requestedYieldTimeMs int,
	started time.Time,
) (tools.ExecutionResult, *tools.ExecutionError) {
	pollReq := writeStdinRequest{
		SessionID:   sessionRef,
		AccountID:   resolveAccountID(execCtx),
		YieldTimeMs: browserContinuationYieldTimeMs(requestedYieldTimeMs),
	}
	for attempt := 0; attempt < browserAutoPollAttempts; attempt++ {
		pollResult := e.executeExecSessionRequest(ctx, e.baseURL+"/v1/write_stdin", "write_stdin", pollReq, pollReq.AccountID, execCtx.PerToolSoftLimits, started)
		if pollResult.Error != nil {
			return tools.ExecutionResult{}, pollResult.Error
		}
		resp := decodeExecSessionResult(pollResult.ResultJSON)
		if resp == nil || !resp.Running {
			return pollResult, nil
		}
	}
	return tools.ExecutionResult{}, &tools.ExecutionError{ErrorClass: errorSandboxTimeout, Message: "browser action did not settle in time", Details: map[string]any{"reason": "timeout"}}
}

func (e *ToolExecutor) retryUnavailableBrowserCommand(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	resolution *resolvedSession,
	request execCommandRequest,
	preparedCommand string,
	started time.Time,
) (*resolvedSession, tools.ExecutionResult) {
	fallback, fallbackErr := e.browserOrchestrator.resolveFallbackSession(ctx, execCommandArgs{}, execCtx, resolution)
	if fallbackErr != nil {
		return resolution, normalizeBrowserExecutionFailure(fallbackErr, started)
	}
	if fallback == nil {
		return resolution, browserUnavailableResult(started)
	}
	resolution = fallback
	request.SessionID = resolution.SessionRef
	request.OpenMode = resolution.OpenMode
	request.ProfileRef = resolution.ProfileRef(resolveProfileRef(execCtx))
	request.WorkspaceRef = resolution.WorkspaceRef(resolveWorkspaceRef(execCtx))
	request.Command = buildBrowserCommand(resolution.SessionRef, preparedCommand)
	result := e.executeExecSessionRequest(ctx, e.baseURL+"/v1/exec_command", "exec_command", request, request.AccountID, execCtx.PerToolSoftLimits, started)
	if result.Error != nil {
		return resolution, normalizeBrowserExecutionFailure(result.Error, started)
	}
	return resolution, result
}

func buildBrowserCommand(sessionRef string, command string) string {
	return "agent-browser --session " + shellQuote(sessionRef) + " " + strings.TrimSpace(command)
}

func shellQuote(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func (e *ToolExecutor) forkSessionCheckpoint(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	fromSessionRef string,
	toSessionRef string,
) (string, *tools.ExecutionError) {
	payload, err := json.Marshal(forkSessionRequest{
		AccountID:     resolveAccountID(execCtx),
		FromSessionID: fromSessionRef,
		ToSessionID:   toSessionRef,
	})
	if err != nil {
		return "", &tools.ExecutionError{ErrorClass: errorSandboxError, Message: fmt.Sprintf("marshal fork request failed: %s", err.Error())}
	}
	resp, reqErr := e.doJSONRequest(ctx, http.MethodPost, e.baseURL+"/v1/sessions/fork", payload, resolveAccountID(execCtx))
	if reqErr != nil {
		return "", &tools.ExecutionError{ErrorClass: reqErr.errorClass, Message: reqErr.message}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", &tools.ExecutionError{ErrorClass: errorSandboxError, Message: "read fork response body failed"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		mapped := mapHTTPError(resp.StatusCode, body, time.Now())
		return "", mapped.Error
	}
	var result forkSessionResponse
	if len(body) > 0 {
		if err := json.Unmarshal(body, &result); err != nil {
			return "", &tools.ExecutionError{ErrorClass: errorSandboxError, Message: "decode fork response failed"}
		}
	}
	return strings.TrimSpace(result.RestoreRevision), nil
}

func (e *ToolExecutor) executeExecSessionRequest(
	ctx context.Context,
	endpoint string,
	toolName string,
	request any,
	accountID string,
	softLimits tools.PerToolSoftLimits,
	started time.Time,
) tools.ExecutionResult {
	payload, err := json.Marshal(request)
	if err != nil {
		return errResult(errorSandboxError, fmt.Sprintf("marshal request failed: %s", err.Error()), started)
	}
	resp, reqErr := e.doJSONRequest(ctx, http.MethodPost, endpoint, payload, accountID)
	if reqErr != nil {
		return errResult(reqErr.errorClass, reqErr.message, started)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return errResult(errorSandboxError, "read response body failed", started)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mapHTTPError(resp.StatusCode, body, started)
	}

	var result execSessionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return errResult(errorSandboxError, "decode response failed", started)
	}
	output, outputTruncated := truncateOutputByLimit(sanitizeShellOutput(result.Output), tools.ResolveToolSoftLimit(softLimits, toolName).MaxOutputBytes)
	resultJSON := map[string]any{
		"session_id":                  result.SessionID,
		"status":                      result.Status,
		"cwd":                         result.Cwd,
		"output":                      output,
		"running":                     result.Running,
		"timed_out":                   result.TimedOut,
		"truncated":                   result.Truncated || outputTruncated,
		"duration_ms":                 durationMs(started),
		"restored_from_restore_state": result.Restored,
	}
	if result.ExitCode != nil {
		resultJSON["exit_code"] = *result.ExitCode
	}
	if len(result.Artifacts) > 0 {
		resultJSON["artifacts"] = result.Artifacts
	}
	if strings.TrimSpace(result.RestoreRevision) != "" {
		resultJSON["restore_revision"] = strings.TrimSpace(result.RestoreRevision)
	}
	return tools.ExecutionResult{ResultJSON: resultJSON, DurationMs: durationMs(started)}
}

func clampYieldTimeMs(value int, limit tools.ToolSoftLimit) int {
	if value <= 0 || limit.MaxYieldTimeMs == nil {
		return value
	}
	if value > *limit.MaxYieldTimeMs {
		return *limit.MaxYieldTimeMs
	}
	return value
}

func truncateOutputByLimit(value string, limit *int) (string, bool) {
	if limit == nil || *limit <= 0 {
		return value, false
	}
	if len(value) <= *limit {
		return value, false
	}
	// 保留头尾上下文，但最终结果长度不能超过 limit。
	half := *limit / 2
	skipped := countOutputLines(value[half : len(value)-half])
	marker := fmt.Sprintf("\n...[%d lines truncated]\n", skipped)
	if len(marker) >= *limit {
		return marker[:*limit], true
	}
	remain := *limit - len(marker)
	headLen := remain / 2
	tailLen := remain - headLen
	head := value[:headLen]
	tail := value[len(value)-tailLen:]
	return head + marker + tail, true
}

type requestError struct {
	errorClass string
	message    string
}

func (e *ToolExecutor) doJSONRequest(
	ctx context.Context,
	method, endpoint string,
	payload []byte,
	accountID string,
) (*http.Response, *requestError) {
	var body io.Reader
	if len(payload) > 0 {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, &requestError{errorClass: errorSandboxError, message: fmt.Sprintf("build request failed: %s", err.Error())}
	}
	if len(payload) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if e.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.authToken)
	}
	if accountID != "" {
		req.Header.Set("X-Account-ID", accountID)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		if isContextDeadline(err) {
			return nil, &requestError{errorClass: errorSandboxTimeout, message: "sandbox request timed out"}
		}
		return nil, &requestError{errorClass: errorSandboxUnavailable, message: fmt.Sprintf("sandbox request failed: %s", err.Error())}
	}
	return resp, nil
}

// pollOutputDeltas polls the sandbox for new output deltas and returns them.
// It stops when ctx is cancelled, session finishes, or maxPolls is reached.
func (e *ToolExecutor) pollOutputDeltas(
	ctx context.Context,
	sessionID, accountID string,
	emit func(stream string, chunk string),
) {
	const maxPolls = 100
	const pollInterval = 100 * time.Millisecond

	for i := 0; i < maxPolls; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		resp, reqErr := e.doJSONRequest(ctx, http.MethodGet,
			e.baseURL+"/v1/sessions/"+sessionID+"/output_deltas", nil, accountID)
		if reqErr != nil {
			return
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return
		}

		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return
		}

		var deltas outputDeltasResponse
		if err := json.Unmarshal(body, &deltas); err != nil {
			return
		}

		if deltas.Stdout != "" {
			emit("stdout", deltas.Stdout)
		}
		if deltas.Stderr != "" {
			emit("stderr", deltas.Stderr)
		}

		if !deltas.Running {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}
}

func parseExecCommandArgs(args map[string]any) (execCommandArgs, *tools.ExecutionError) {
	if _, ok := args["session_id"]; ok {
		return execCommandArgs{}, sandboxArgsError("parameter session_id is not supported; use session_ref")
	}
	request := execCommandArgs{
		SessionMode:    readStringArg(args, "session_mode"),
		SessionRef:     readStringArg(args, "session_ref"),
		FromSessionRef: readStringArg(args, "from_session_ref"),
		ShareScope:     readStringArg(args, "share_scope"),
		Cwd:            readStringArg(args, "cwd"),
		Command:        readStringArg(args, "command"),
		TimeoutMs:      resolveTimeoutMs(args),
		YieldTimeMs:    readIntArg(args, "yield_time_ms"),
		Env:            readMapStringArg(args, "env"),
	}
	if strings.TrimSpace(request.Command) == "" {
		return execCommandArgs{}, sandboxArgsError("parameter command is required")
	}
	if request.YieldTimeMs <= 0 {
		request.YieldTimeMs = min(request.TimeoutMs, 30_000)
		if request.YieldTimeMs <= 0 {
			request.YieldTimeMs = 30_000
		}
	}
	return request, nil
}

func parseWriteStdinArgs(args map[string]any) (writeStdinArgs, *tools.ExecutionError) {
	if _, ok := args["session_id"]; ok {
		return writeStdinArgs{}, sandboxArgsError("parameter session_id is not supported; use session_ref")
	}
	request := writeStdinArgs{
		SessionRef:  readStringArg(args, "session_ref"),
		Chars:       readStringArg(args, "chars"),
		YieldTimeMs: readIntArg(args, "yield_time_ms"),
	}
	if _, ok := args["share_scope"]; ok {
		return writeStdinArgs{}, sandboxArgsError("parameter share_scope is not supported for write_stdin")
	}
	if strings.TrimSpace(request.SessionRef) == "" {
		return writeStdinArgs{}, sandboxArgsError("parameter session_ref is required")
	}
	return request, nil
}

func parseBrowserArgs(args map[string]any) (browserArgs, *tools.ExecutionError) {
	request := browserArgs{
		Command:     readStringArg(args, "command"),
		YieldTimeMs: readIntArg(args, "yield_time_ms"),
	}
	if strings.TrimSpace(request.Command) == "" {
		return browserArgs{}, sandboxArgsError("parameter command is required")
	}
	for key := range args {
		switch key {
		case "command", "yield_time_ms":
		default:
			return browserArgs{}, sandboxArgsError(fmt.Sprintf("parameter %s is not supported for browser", key))
		}
	}
	return request, nil
}

func sandboxArgsError(message string) *tools.ExecutionError {
	return &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: message}
}

func sandboxPermissionDenied(message string, details map[string]any) *tools.ExecutionError {
	return &tools.ExecutionError{ErrorClass: errorPermissionDenied, Message: message, Details: details}
}

func (e *ToolExecutor) ensureEnvironmentBindings(
	ctx context.Context,
	execCtx tools.ExecutionContext,
) (tools.ExecutionContext, *tools.ExecutionError) {
	if e == nil || e.orchestrator == nil || e.orchestrator.pool == nil {
		return execCtx, nil
	}
	if strings.TrimSpace(execCtx.ProfileRef) != "" && strings.TrimSpace(execCtx.WorkspaceRef) != "" {
		return execCtx, nil
	}
	if execCtx.AccountID == nil || *execCtx.AccountID == uuid.Nil {
		return execCtx, nil
	}

	run := data.Run{
		ID:              execCtx.RunID,
		AccountID:       *execCtx.AccountID,
		ProjectID:       execCtx.ProjectID,
		CreatedByUserID: execCtx.UserID,
		ProfileRef:      stringPtr(execCtx.ProfileRef),
		WorkspaceRef:    stringPtr(execCtx.WorkspaceRef),
	}
	if execCtx.ThreadID != nil {
		run.ThreadID = *execCtx.ThreadID
	}

	resolvedRun, err := environmentbindings.ResolveAndPersistRun(ctx, e.orchestrator.pool, run)
	if err != nil {
		return execCtx, &tools.ExecutionError{
			ErrorClass: errorSandboxError,
			Message:    "resolve environment bindings failed",
			Details:    map[string]any{"error": err.Error()},
		}
	}
	execCtx.ProfileRef = strings.TrimSpace(stringPtrValue(resolvedRun.ProfileRef))
	execCtx.WorkspaceRef = strings.TrimSpace(stringPtrValue(resolvedRun.WorkspaceRef))
	return execCtx, nil
}

func resolveAccountID(execCtx tools.ExecutionContext) string {
	if execCtx.AccountID == nil {
		return ""
	}
	return execCtx.AccountID.String()
}

func resolveProfileRef(execCtx tools.ExecutionContext) string {
	return strings.TrimSpace(execCtx.ProfileRef)
}

func resolveWorkspaceRef(execCtx tools.ExecutionContext) string {
	return strings.TrimSpace(execCtx.WorkspaceRef)
}

func defaultExecSessionID(runID string) string {
	return runID + "/shell/default"
}

func readStringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
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

func readIntArg(args map[string]any, key string) int {
	value, ok := args[key]
	if !ok {
		return 0
	}
	switch number := value.(type) {
	case float64:
		return int(number)
	case int:
		return number
	case int64:
		return int(number)
	case json.Number:
		parsed, err := number.Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return 0
}

func resolveTier(toolName string, budget map[string]any) string {
	if strings.TrimSpace(toolName) == "browser" {
		return "browser"
	}
	if tier, ok := resolveTierOverride(budget, toolName); ok {
		return tier
	}
	if tier, ok := resolveTierOverride(budget, sandboxWorkloadClass(toolName)); ok {
		return tier
	}
	return defaultTierForTool(toolName)
}

func defaultTierForTool(toolName string) string {
	if strings.TrimSpace(toolName) == "browser" {
		return "browser"
	}
	switch sandboxWorkloadClass(toolName) {
	case "interactive_shell":
		return "pro"
	default:
		return "lite"
	}
}

func sandboxWorkloadClass(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "browser":
		return "browser"
	case "exec_command", "write_stdin":
		return "interactive_shell"
	case "python_execute":
		return "ephemeral_exec"
	default:
		return "ephemeral_exec"
	}
}

func resolveTierOverride(budget map[string]any, key string) (string, bool) {
	if budget == nil || strings.TrimSpace(key) == "" {
		return "", false
	}
	rawProfiles, ok := budget["sandbox_profiles"]
	if !ok || rawProfiles == nil {
		return "", false
	}
	profiles, ok := rawProfiles.(map[string]any)
	if !ok {
		return "", false
	}
	tier, ok := normalizeTierValue(profiles[key])
	return tier, ok
}

func normalizeTierValue(value any) (string, bool) {
	raw, ok := value.(string)
	if !ok {
		return "", false
	}
	switch strings.TrimSpace(raw) {
	case "lite", "pro", "browser":
		return strings.TrimSpace(raw), true
	default:
		return "", false
	}
}

func resolveTimeoutMs(args map[string]any) int {
	if v, ok := args["timeout_ms"]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return int(i)
			}
		}
	}
	return defaultTimeoutMs
}

func truncateOutput(s string) string {
	if len(s) <= maxOutputBytes {
		return s
	}
	half := maxOutputBytes / 2
	head := s[:half]
	tail := s[len(s)-half:]
	skipped := countOutputLines(s[half : len(s)-half])
	return fmt.Sprintf("%s\n...[%d lines truncated]\n%s", head, skipped, tail)
}

func countOutputLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

var (
	// CSI sequences: ESC [ ... final-byte (colors, cursor movement, erase, etc.)
	ansiCSIRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)
	// OSC sequences: ESC ] ... (BEL or ESC \)
	ansiOSCRe = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
	// Remaining lone ESC + single char (e.g. ESC M, ESC c)
	ansiTwoRe = regexp.MustCompile(`\x1b[^[\]]`)
)

// collapseCarriageReturns simulates terminal line overwriting caused by \r.
// git, wget, curl and similar tools use \r to rewrite progress lines in place.
// Without a real TTY the captured output contains all intermediate states;
// this reduces each "virtual line" to its final written value.
func collapseCarriageReturns(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "\r") {
			continue
		}
		parts := strings.Split(line, "\r")
		last := ""
		for _, p := range parts {
			if p != "" {
				last = p
			}
		}
		lines[i] = last
	}
	return strings.Join(lines, "\n")
}

// collapseBackspaces simulates terminal backspace (\b) overwriting.
// pip and similar tools use \b sequences like "\b \b" to update spinner/progress
// in place. Each \b moves the virtual cursor one position left; a subsequent
// character overwrites. This function resolves each line to its final visible state.
func collapseBackspaces(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if !strings.ContainsRune(line, '\b') {
			continue
		}
		buf := make([]rune, 0, len(line))
		for _, ch := range line {
			if ch == '\b' {
				if len(buf) > 0 {
					buf = buf[:len(buf)-1]
				}
			} else {
				buf = append(buf, ch)
			}
		}
		lines[i] = string(buf)
	}
	return strings.Join(lines, "\n")
}

// stripANSI removes ANSI/VT100 escape sequences from s.
func stripANSI(s string) string {
	s = ansiOSCRe.ReplaceAllString(s, "")
	s = ansiCSIRe.ReplaceAllString(s, "")
	s = ansiTwoRe.ReplaceAllString(s, "")
	return s
}

// sanitizeShellOutput strips terminal control sequences, collapses carriage-return
// and backspace overwrites before the output reaches the LLM.
// This can cut token count by 10-100x for commands that produce progress bars.
func sanitizeShellOutput(s string) string {
	s = collapseCarriageReturns(s)
	s = collapseBackspaces(s)
	s = stripANSI(s)
	return s
}

func isSessionUnavailable(err *tools.ExecutionError) bool {
	if err == nil {
		return false
	}
	code, _ := err.Details["code"].(string)
	switch strings.TrimSpace(code) {
	case "sandbox.session_not_found", "shell.session_not_found":
		return true
	case "sandbox.shell_error":
		message := strings.ToLower(strings.TrimSpace(err.Message))
		if strings.Contains(message, "connect to agent") || strings.Contains(message, "no route to host") || strings.Contains(message, "connection refused") {
			return true
		}
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(err.Message)), "session not found")
}

func mapHTTPError(statusCode int, body []byte, started time.Time) tools.ExecutionResult {
	var parsed struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &parsed)

	errorClass := errorSandboxError
	if statusCode == http.StatusGatewayTimeout || parsed.Code == "timeout" {
		errorClass = errorSandboxTimeout
	}
	if statusCode == http.StatusServiceUnavailable || statusCode == http.StatusBadGateway {
		errorClass = errorSandboxUnavailable
	}
	if strings.TrimSpace(parsed.Code) == "shell.max_sessions_exceeded" {
		errorClass = errorMaxSessionsExceeded
	}

	message := parsed.Message
	if message == "" {
		message = fmt.Sprintf("sandbox service returned %d", statusCode)
	}
	details := map[string]any{
		"status_code": statusCode,
		"code":        parsed.Code,
	}
	if strings.TrimSpace(parsed.Code) == "shell.session_busy" {
		details["retry_via"] = "fork"
	}

	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: errorClass,
			Message:    message,
			Details:    details,
		},
		DurationMs: durationMs(started),
	}
}

func errResult(errorClass, message string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error:      &tools.ExecutionError{ErrorClass: errorClass, Message: message},
		DurationMs: durationMs(started),
	}
}

func isContextDeadline(err error) bool {
	if err == context.DeadlineExceeded {
		return true
	}
	if unwrap, ok := err.(interface{ Unwrap() error }); ok {
		return isContextDeadline(unwrap.Unwrap())
	}
	return false
}

func durationMs(started time.Time) int {
	elapsed := time.Since(started)
	millis := int(elapsed / time.Millisecond)
	if millis < 0 {
		return 0
	}
	return millis
}

func decodeExecSessionResult(resultJSON map[string]any) *execSessionResponse {
	if resultJSON == nil {
		return nil
	}
	payload, err := json.Marshal(resultJSON)
	if err != nil {
		return nil
	}
	var result execSessionResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil
	}
	return &result
}

func normalizeBrowserExecutionFailure(err *tools.ExecutionError, started time.Time) tools.ExecutionResult {
	if err == nil {
		return tools.ExecutionResult{DurationMs: durationMs(started)}
	}
	if isSessionBusy(err) || err.ErrorClass == errorSandboxTimeout {
		return browserTimeoutResult(started)
	}
	if isSessionUnavailable(err) || err.ErrorClass == errorSandboxUnavailable {
		return browserUnavailableResult(started)
	}
	if reason, _ := err.Details["reason"].(string); strings.TrimSpace(reason) == "snapshot_parse_failed" {
		return browserSnapshotParseResult(started)
	}
	return tools.ExecutionResult{Error: err, DurationMs: durationMs(started)}
}

func browserUnavailableResult(started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: errorSandboxUnavailable,
			Message:    "browser session unavailable",
			Details:    map[string]any{"reason": "unavailable"},
		},
		DurationMs: durationMs(started),
	}
}

func browserTimeoutResult(started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: errorSandboxTimeout,
			Message:    "browser action did not settle in time",
			Details:    map[string]any{"reason": "timeout"},
		},
		DurationMs: durationMs(started),
	}
}

func browserSnapshotParseResult(started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: errorSandboxError,
			Message:    "browser snapshot parsing failed",
			Details:    map[string]any{"reason": "snapshot_parse_failed"},
		},
		DurationMs: durationMs(started),
	}
}

func shellBusyError(sessionRef string, retryVia string) *tools.ExecutionError {
	retryVia = strings.TrimSpace(retryVia)
	if retryVia == "" {
		retryVia = "fork"
	}
	details := map[string]any{
		"code":      "shell.session_busy",
		"retry_via": retryVia,
	}
	if strings.TrimSpace(sessionRef) != "" {
		details["session_ref"] = strings.TrimSpace(sessionRef)
	}
	return &tools.ExecutionError{
		ErrorClass: errorSandboxError,
		Message:    "shell session is busy",
		Details:    details,
	}
}

func isSessionBusy(err *tools.ExecutionError) bool {
	if err == nil {
		return false
	}
	code, _ := err.Details["code"].(string)
	if strings.TrimSpace(code) == "shell.session_busy" {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(err.Message)), "session is busy")
}

// isCrossRunLease reports whether the session's current lease belongs to a different run.
func isCrossRunLease(resolution *resolvedSession, execCtx tools.ExecutionContext) bool {
	if resolution == nil || resolution.Record == nil || resolution.Record.LeaseOwnerID == nil {
		return false
	}
	return !isLeaseFromSameRun(strings.TrimSpace(*resolution.Record.LeaseOwnerID), execCtx.RunID)
}

// tryRecoverStaleSessionLease polls the sandbox once to verify whether the "busy"
// session's command has actually finished. If it has, the stale lease is cleared
// and a fresh lease is acquired. Returns true only when recovery fully succeeds.
func (e *ToolExecutor) tryRecoverStaleSessionLease(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	resolution *resolvedSession,
	newOwnerID string,
	timeoutMs int,
) bool {
	pollReq := writeStdinRequest{
		SessionID:   resolution.SessionRef,
		AccountID:   resolveAccountID(execCtx),
		YieldTimeMs: 2000,
	}
	pollResult := e.executeExecSessionRequest(
		ctx, e.baseURL+"/v1/write_stdin", "write_stdin",
		pollReq, pollReq.AccountID, execCtx.PerToolSoftLimits, time.Now(),
	)
	if pollResult.Error != nil {
		return false
	}
	resp := decodeExecSessionResult(pollResult.ResultJSON)
	if resp == nil || resp.Running {
		return false
	}
	_ = e.orchestrator.clearFinishedWriterLease(ctx, execCtx, resolution)
	return e.orchestrator.prepareExecWriterLease(ctx, execCtx, resolution, newOwnerID, timeoutMs) == nil
}

func isSessionNotRunning(err *tools.ExecutionError) bool {
	if err == nil {
		return false
	}
	code, _ := err.Details["code"].(string)
	if strings.TrimSpace(code) == "shell.not_running" {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(err.Message)), "not running")
}

var (
	sandboxRTKOnce  sync.Once
	sandboxRTKCache string
	sandboxRTKRun   = func(ctx context.Context, bin string, command string) (string, error) {
		var out bytes.Buffer
		cmd := exec.CommandContext(ctx, bin, append([]string{"rewrite"}, strings.Fields(command)...)...)
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return "", err
		}
		return strings.TrimSpace(out.String()), nil
	}
)

// rtkRewriteSandbox rewrites a shell command to its RTK-optimized equivalent,
// running rtk on the worker host (not inside the sandbox container).
// Returns "" when RTK is unavailable or has no wrapper for the command.
func rtkRewriteSandbox(command string) string {
	sandboxRTKOnce.Do(func() {
		// Container path first, then ~/.arkloop/bin/rtk, then PATH.
		if _, err := os.Stat("/usr/local/bin/rtk"); err == nil {
			sandboxRTKCache = "/usr/local/bin/rtk"
			return
		}
		if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, ".arkloop", "bin", "rtk")
			if _, err := os.Stat(candidate); err == nil {
				sandboxRTKCache = candidate
				return
			}
		}
		if p, err := exec.LookPath("rtk"); err == nil {
			sandboxRTKCache = p
		}
	})
	if sandboxRTKCache == "" {
		return ""
	}
	if !shouldAttemptRTKRewriteSandbox(command) {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), rtkRewriteTimeout)
	defer cancel()
	rewritten, err := sandboxRTKRun(ctx, sandboxRTKCache, command)
	if err != nil {
		return ""
	}
	return rewritten
}

func shouldAttemptRTKRewriteSandbox(command string) bool {
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

func (e *ToolExecutor) resolveEnabledSkills(ctx context.Context, execCtx tools.ExecutionContext) ([]skillstore.ResolvedSkill, error) {
	if e == nil || e.orchestrator == nil || e.orchestrator.pool == nil || execCtx.AccountID == nil {
		return nil, nil
	}
	if strings.TrimSpace(execCtx.ProfileRef) == "" || strings.TrimSpace(execCtx.WorkspaceRef) == "" {
		return nil, nil
	}
	repo := data.NewSkillsRepository(e.orchestrator.pool)
	return repo.ResolveEnabledSkills(ctx, *execCtx.AccountID, execCtx.ProfileRef, execCtx.WorkspaceRef)
}
