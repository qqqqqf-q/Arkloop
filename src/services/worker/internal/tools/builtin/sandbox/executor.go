package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"arkloop/services/worker/internal/tools"
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
	SessionID string `json:"session_id"`
	Tier      string `json:"tier"`
	Language  string `json:"language"`
	Code      string `json:"code"`
	TimeoutMs int    `json:"timeout_ms"`
}

type execResponse struct {
	SessionID  string        `json:"session_id"`
	Stdout     string        `json:"stdout"`
	Stderr     string        `json:"stderr"`
	ExitCode   int           `json:"exit_code"`
	DurationMs int64         `json:"duration_ms"`
	Artifacts  []artifactRef `json:"artifacts,omitempty"`
}

type artifactRef struct {
	Key      string `json:"key"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

type ToolExecutor struct {
	baseURL string
	client  *http.Client
}

func NewToolExecutor(baseURL string) *ToolExecutor {
	return &ToolExecutor{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: httpClientTimeout,
		},
	}
}

func (e *ToolExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	if e.baseURL == "" {
		return errResult(errorNotConfigured, "sandbox service not configured", started)
	}

	var language, code string
	switch toolName {
	case "code_execute":
		language = "python"
		code, _ = args["code"].(string)
		if code == "" {
			return errResult(errorArgsInvalid, "parameter code is required", started)
		}
	case "shell_execute":
		language = "shell"
		code, _ = args["command"].(string)
		if code == "" {
			return errResult(errorArgsInvalid, "parameter command is required", started)
		}
	default:
		return errResult(errorArgsInvalid, fmt.Sprintf("unknown sandbox tool: %s", toolName), started)
	}

	sessionID := execCtx.RunID.String()
	tier := resolveTier(execCtx.Budget)
	timeoutMs := resolveTimeoutMs(args)

	reqBody := execRequest{
		SessionID: sessionID,
		Tier:      tier,
		Language:  language,
		Code:      code,
		TimeoutMs: timeoutMs,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return errResult(errorSandboxError, fmt.Sprintf("marshal request failed: %s", err.Error()), started)
	}

	endpoint := e.baseURL + "/v1/exec"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return errResult(errorSandboxError, fmt.Sprintf("build request failed: %s", err.Error()), started)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		if isContextDeadline(err) {
			return errResult(errorSandboxTimeout, "sandbox request timed out", started)
		}
		return errResult(errorSandboxUnavailable, fmt.Sprintf("sandbox request failed: %s", err.Error()), started)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return errResult(errorSandboxError, "read response body failed", started)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
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
		return tools.ExecutionResult{
			ResultJSON: resultJSON,
			DurationMs: durationMs(started),
		}
	}

	return mapHTTPError(resp.StatusCode, respBody, started)
}

func resolveTier(budget map[string]any) string {
	if budget != nil {
		if tier, ok := budget["sandbox_tier"].(string); ok && tier != "" {
			return tier
		}
	}
	return "lite"
}

func resolveTimeoutMs(args map[string]any) int {
	if v, ok := args["timeout_ms"]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
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
		Error: &tools.ExecutionError{
			ErrorClass: errorClass,
			Message:    message,
		},
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
