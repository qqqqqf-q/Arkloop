package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"arkloop/services/worker/internal/tools"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	errorSandboxError       = "tool.sandbox_error"
	errorSandboxUnavailable = "tool.sandbox_unavailable"
	errorSandboxTimeout     = "tool.sandbox_timeout"
	errorArgsInvalid        = "tool.args_invalid"
	errorNotConfigured      = "tool.not_configured"

	defaultTimeoutMs  = 30_000
	maxOutputBytes    = 32 * 1024
	httpClientTimeout = 5 * time.Minute
)

type execRequest struct {
	SessionID    string `json:"session_id"`
	OrgID        string `json:"org_id,omitempty"`
	ProfileRef   string `json:"profile_ref,omitempty"`
	WorkspaceRef string `json:"workspace_ref,omitempty"`
	Tier         string `json:"tier"`
	Language     string `json:"language"`
	Code         string `json:"code"`
	TimeoutMs    int    `json:"timeout_ms"`
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
	SessionID    string `json:"session_id"`
	OrgID        string `json:"org_id,omitempty"`
	ProfileRef   string `json:"profile_ref,omitempty"`
	WorkspaceRef string `json:"workspace_ref,omitempty"`
	Tier         string `json:"tier,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
	Command      string `json:"command"`
	TimeoutMs    int    `json:"timeout_ms,omitempty"`
	YieldTimeMs  int    `json:"yield_time_ms,omitempty"`
}

type writeStdinRequest struct {
	SessionID   string `json:"session_id"`
	OrgID       string `json:"org_id,omitempty"`
	Chars       string `json:"chars,omitempty"`
	YieldTimeMs int    `json:"yield_time_ms,omitempty"`
}

type forkSessionRequest struct {
	OrgID         string `json:"org_id,omitempty"`
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

type execCommandArgs struct {
	SessionMode    string
	SessionRef     string
	FromSessionRef string
	Cwd            string
	Command        string
	TimeoutMs      int
	YieldTimeMs    int
}

type writeStdinArgs struct {
	SessionRef  string
	Chars       string
	YieldTimeMs int
}

type artifactRef struct {
	Key      string `json:"key"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

type ToolExecutor struct {
	baseURL      string
	authToken    string
	client       *http.Client
	orchestrator *sessionOrchestrator
}

func NewToolExecutor(baseURL, authToken string) *ToolExecutor {
	return NewToolExecutorWithPool(baseURL, authToken, nil)
}

func NewToolExecutorWithPool(baseURL, authToken string, pool *pgxpool.Pool) *ToolExecutor {
	return &ToolExecutor{
		baseURL:      baseURL,
		authToken:    authToken,
		client:       &http.Client{Timeout: httpClientTimeout},
		orchestrator: newSessionOrchestrator(pool),
	}
}

func (e *ToolExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	_ = toolCallID
	started := time.Now()

	if e.baseURL == "" {
		return errResult(errorNotConfigured, "sandbox service not configured", started)
	}

	switch toolName {
	case "python_execute":
		return e.executePython(ctx, args, execCtx, started)
	case "exec_command":
		return e.executeExecCommand(ctx, args, execCtx, started)
	case "write_stdin":
		return e.executeWriteStdin(ctx, args, execCtx, started)
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
	code, _ := args["code"].(string)
	if code == "" {
		return errResult(errorArgsInvalid, "parameter code is required", started)
	}

	payload, err := json.Marshal(execRequest{
		SessionID:    execCtx.RunID.String(),
		OrgID:        resolveOrgID(execCtx),
		ProfileRef:   resolveProfileRef(execCtx),
		WorkspaceRef: resolveWorkspaceRef(execCtx),
		Tier:         resolveTier("python_execute", execCtx.Budget),
		Language:     "python",
		Code:         code,
		TimeoutMs:    resolveTimeoutMs(args),
	})
	if err != nil {
		return errResult(errorSandboxError, fmt.Sprintf("marshal request failed: %s", err.Error()), started)
	}

	resp, reqErr := e.doJSONRequest(ctx, http.MethodPost, e.baseURL+"/v1/exec", payload, resolveOrgID(execCtx))
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
		"stdout":      truncateOutput(result.Stdout),
		"stderr":      truncateOutput(result.Stderr),
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
	started time.Time,
) tools.ExecutionResult {
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

	request := execCommandRequest{
		SessionID:    resolution.SessionRef,
		OrgID:        resolveOrgID(execCtx),
		ProfileRef:   resolveProfileRef(execCtx),
		WorkspaceRef: resolveWorkspaceRef(execCtx),
		Tier:         resolveTier("exec_command", execCtx.Budget),
		Cwd:          reqArgs.Cwd,
		Command:      reqArgs.Command,
		TimeoutMs:    reqArgs.TimeoutMs,
		YieldTimeMs:  reqArgs.YieldTimeMs,
	}
	result := e.executeExecSessionRequest(ctx, e.baseURL+"/v1/exec_command", "exec_command", request, request.OrgID, execCtx.PerToolSoftLimits, started)
	if result.Error != nil {
		return result
	}
	resp := decodeExecSessionResult(result.ResultJSON)
	if resp != nil {
		e.orchestrator.markResult(ctx, execCtx, resolution, *resp)
		result.ResultJSON["session_ref"] = resolution.SessionRef
		result.ResultJSON["resolved_via"] = resolution.ResolvedVia
		result.ResultJSON["reused"] = resolution.Reused
		result.ResultJSON["restored_from_restore_state"] = resp.Restored || resolution.RestoredFromRestoreState
	}
	delete(result.ResultJSON, "session_id")
	return result
}

func (e *ToolExecutor) executeWriteStdin(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	reqArgs, argErr := parseWriteStdinArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{Error: argErr, DurationMs: durationMs(started)}
	}
	resolution, resolveErr := e.orchestrator.resolveWriteSession(ctx, reqArgs, execCtx)
	if resolveErr != nil {
		return tools.ExecutionResult{Error: resolveErr, DurationMs: durationMs(started)}
	}

	request := writeStdinRequest{
		SessionID:   resolution.SessionRef,
		OrgID:       resolveOrgID(execCtx),
		Chars:       reqArgs.Chars,
		YieldTimeMs: clampYieldTimeMs(reqArgs.YieldTimeMs, tools.ResolveToolSoftLimit(execCtx.PerToolSoftLimits, "write_stdin")),
	}
	result := e.executeExecSessionRequest(ctx, e.baseURL+"/v1/write_stdin", "write_stdin", request, request.OrgID, execCtx.PerToolSoftLimits, started)
	if result.Error != nil {
		return result
	}
	resp := decodeExecSessionResult(result.ResultJSON)
	if resp != nil {
		e.orchestrator.markResult(ctx, execCtx, resolution, *resp)
		result.ResultJSON["session_ref"] = resolution.SessionRef
		result.ResultJSON["resolved_via"] = resolution.ResolvedVia
		result.ResultJSON["reused"] = true
		result.ResultJSON["restored_from_restore_state"] = false
	}
	delete(result.ResultJSON, "session_id")
	return result
}

func (e *ToolExecutor) forkSessionCheckpoint(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	fromSessionRef string,
	toSessionRef string,
) (string, *tools.ExecutionError) {
	payload, err := json.Marshal(forkSessionRequest{
		OrgID:         resolveOrgID(execCtx),
		FromSessionID: fromSessionRef,
		ToSessionID:   toSessionRef,
	})
	if err != nil {
		return "", &tools.ExecutionError{ErrorClass: errorSandboxError, Message: fmt.Sprintf("marshal fork request failed: %s", err.Error())}
	}
	resp, reqErr := e.doJSONRequest(ctx, http.MethodPost, e.baseURL+"/v1/sessions/fork", payload, resolveOrgID(execCtx))
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
	orgID string,
	softLimits tools.PerToolSoftLimits,
	started time.Time,
) tools.ExecutionResult {
	payload, err := json.Marshal(request)
	if err != nil {
		return errResult(errorSandboxError, fmt.Sprintf("marshal request failed: %s", err.Error()), started)
	}
	resp, reqErr := e.doJSONRequest(ctx, http.MethodPost, endpoint, payload, orgID)
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
	output, outputTruncated := truncateOutputByLimit(result.Output, tools.ResolveToolSoftLimit(softLimits, toolName).MaxOutputBytes)
	resultJSON := map[string]any{
		"session_id":                  result.SessionID,
		"status":                      result.Status,
		"cwd":                         result.Cwd,
		"stdout":                      output,
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
	marker := fmt.Sprintf("\n...[truncated %d bytes]", len(value)-*limit)
	allowed := *limit - len(marker)
	if allowed < 0 {
		allowed = 0
	}
	return value[:allowed] + marker, true
}

type requestError struct {
	errorClass string
	message    string
}

func (e *ToolExecutor) doJSONRequest(
	ctx context.Context,
	method, endpoint string,
	payload []byte,
	orgID string,
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
	if orgID != "" {
		req.Header.Set("X-Org-ID", orgID)
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

func parseExecCommandArgs(args map[string]any) (execCommandArgs, *tools.ExecutionError) {
	if _, ok := args["session_id"]; ok {
		return execCommandArgs{}, sandboxArgsError("parameter session_id is not supported; use session_ref")
	}
	request := execCommandArgs{
		SessionMode:    readStringArg(args, "session_mode"),
		SessionRef:     readStringArg(args, "session_ref"),
		FromSessionRef: readStringArg(args, "from_session_ref"),
		Cwd:            readStringArg(args, "cwd"),
		Command:        readStringArg(args, "command"),
		TimeoutMs:      resolveTimeoutMs(args),
		YieldTimeMs:    readIntArg(args, "yield_time_ms"),
	}
	if strings.TrimSpace(request.Command) == "" {
		return execCommandArgs{}, sandboxArgsError("parameter command is required")
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
	if strings.TrimSpace(request.SessionRef) == "" {
		return writeStdinArgs{}, sandboxArgsError("parameter session_ref is required")
	}
	return request, nil
}

func sandboxArgsError(message string) *tools.ExecutionError {
	return &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: message}
}

func resolveOrgID(execCtx tools.ExecutionContext) string {
	if execCtx.OrgID == nil {
		return ""
	}
	return execCtx.OrgID.String()
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
	if tier, ok := resolveTierOverride(budget, toolName); ok {
		return tier
	}
	if tier, ok := resolveTierOverride(budget, sandboxWorkloadClass(toolName)); ok {
		return tier
	}
	return defaultTierForTool(toolName)
}

func defaultTierForTool(toolName string) string {
	switch sandboxWorkloadClass(toolName) {
	case "interactive_shell":
		return "pro"
	default:
		return "lite"
	}
}

func sandboxWorkloadClass(toolName string) string {
	switch strings.TrimSpace(toolName) {
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
	case "lite", "pro":
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
	return s[:maxOutputBytes] + fmt.Sprintf("\n... (truncated, total %d bytes)", len(s))
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

	message := parsed.Message
	if message == "" {
		message = fmt.Sprintf("sandbox service returned %d", statusCode)
	}

	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: errorClass,
			Message:    message,
			Details: map[string]any{
				"status_code": statusCode,
				"code":        parsed.Code,
			},
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
