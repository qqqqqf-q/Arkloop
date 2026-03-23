//go:build desktop && darwin

package sandboxshell

import (
	"bytes"
	"context"
	"encoding/json"
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
)

// Executor implements tools.Executor by forwarding shell commands to an
// embedded VZ sandbox HTTP service.
type Executor struct {
	sandboxBaseURL string
	authToken      string
	client         *http.Client
}

func NewExecutor(sandboxBaseURL, authToken string) *Executor {
	return &Executor{
		sandboxBaseURL: strings.TrimRight(sandboxBaseURL, "/"),
		authToken:      authToken,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
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
	started := time.Now()

	if e.sandboxBaseURL == "" {
		return errResult(errorUnavailable, "VM sandbox is not running", started)
	}

	switch toolName {
	case "exec_command":
		return e.executeExecCommand(ctx, args, execCtx, started)
	case "write_stdin":
		return e.executeWriteStdin(ctx, args, execCtx, started)
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
	command := readStringArg(args, "command")
	if strings.TrimSpace(command) == "" {
		return errResult(errorArgsInvalid, "parameter command is required", started)
	}

	sessionID := "desktop-" + execCtx.RunID.String()
	cwd := readStringArg(args, "cwd")
	timeoutMs := readIntArg(args, "timeout_ms")
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}

	slog.Info("sandbox_shell: exec_command",
		"run_id", execCtx.RunID.String(),
		"command", truncateForLog(command, 200),
		"cwd", cwd,
	)

	env := readMapStringArg(args, "env")

	payload := map[string]any{
		"session_id":    sessionID,
		"command":       command,
		"timeout_ms":    timeoutMs,
		"yield_time_ms": 2000,
		"tier":          defaultTier,
	}
	if cwd != "" {
		payload["cwd"] = cwd
	}
	if len(env) > 0 {
		payload["env"] = env
	}

	resp, err := e.doPost(ctx, "/v1/exec_command", payload)
	if err != nil {
		return errResult(errorSandbox, err.Error(), started)
	}

	return buildResult(resp, started)
}

func (e *Executor) executeWriteStdin(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	sessionID := "desktop-" + execCtx.RunID.String()
	chars := readStringArg(args, "chars")
	yieldTimeMs := readIntArg(args, "yield_time_ms")
	if yieldTimeMs <= 0 {
		yieldTimeMs = 2000
	}

	if chars != "" {
		slog.Info("sandbox_shell: write_stdin",
			"run_id", execCtx.RunID.String(),
			"chars_len", len(chars),
		)
	}

	payload := map[string]any{
		"session_id":    sessionID,
		"yield_time_ms": yieldTimeMs,
	}
	if chars != "" {
		payload["chars"] = chars
	}

	resp, err := e.doPost(ctx, "/v1/write_stdin", payload)
	if err != nil {
		return errResult(errorSandbox, err.Error(), started)
	}

	return buildResult(resp, started)
}

func (e *Executor) doPost(ctx context.Context, path string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.sandboxBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.authToken)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sandbox request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("sandbox error (%d): %s", resp.StatusCode, truncateForLog(string(respBody), 500))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return result, nil
}

func buildResult(resp map[string]any, started time.Time) tools.ExecutionResult {
	output, _ := resp["output"].(string)
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
		"status":    resp["status"],
		"cwd":       resp["cwd"],
		"output":    output,
		"stdout":    output,
		"running":   resp["running"],
		"timed_out": resp["timed_out"],
		"truncated": truncated,
	}
	if exitCode, ok := resp["exit_code"]; ok {
		resultJSON["exit_code"] = exitCode
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
