package browser

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
	errorBrowserError   = "tool.browser_error"
	errorNetworkBlocked = "tool.network_blocked"
	errorTimeout        = "tool.timeout"
	errorArgsInvalid    = "tool.args_invalid"
	errorNotConfigured  = "tool.not_configured"

	defaultTimeout = 30 * time.Second
)

type ToolExecutor struct {
	baseURL string
	client  *http.Client
}

func NewToolExecutor(baseURL string) *ToolExecutor {
	return &ToolExecutor{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 60 * time.Second,
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
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorNotConfigured,
				Message:    "browser service not configured",
			},
			DurationMs: durationMs(started),
		}
	}

	sessionID := ""
	if execCtx.ThreadID != nil {
		sessionID = execCtx.ThreadID.String()
	}
	orgID := ""
	if execCtx.OrgID != nil {
		orgID = execCtx.OrgID.String()
	}
	runID := execCtx.RunID.String()

	if sessionID == "" || orgID == "" {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "missing session or org context",
			},
			DurationMs: durationMs(started),
		}
	}

	timeout := defaultTimeout
	if execCtx.TimeoutMs != nil && *execCtx.TimeoutMs > 0 {
		timeout = time.Duration(*execCtx.TimeoutMs) * time.Millisecond
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	headers := map[string]string{
		"X-Session-ID": sessionID,
		"X-Org-ID":     orgID,
		"X-Run-ID":     runID,
	}

	switch toolName {
	case "browser_navigate":
		return e.doNavigate(reqCtx, args, headers, started)
	case "browser_interact":
		return e.doInteract(reqCtx, args, headers, started)
	case "browser_extract":
		return e.doExtract(reqCtx, args, headers, started)
	case "browser_screenshot":
		return e.doScreenshot(reqCtx, args, headers, started)
	case "browser_session_close":
		return e.doSessionClose(reqCtx, sessionID, headers, started)
	default:
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    fmt.Sprintf("unknown browser tool: %s", toolName),
			},
			DurationMs: durationMs(started),
		}
	}
}

func (e *ToolExecutor) doNavigate(
	ctx context.Context,
	args map[string]any,
	headers map[string]string,
	started time.Time,
) tools.ExecutionResult {
	url, _ := args["url"].(string)
	if url == "" {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "parameter url is required",
			},
			DurationMs: durationMs(started),
		}
	}

	body := map[string]any{"url": url}
	if v, ok := args["wait_until"].(string); ok && v != "" {
		body["wait_until"] = v
	}
	if v, ok := args["fresh_session"].(bool); ok {
		body["fresh_session"] = v
	}

	return e.postJSON(ctx, "/v1/navigate", body, headers, started)
}

func (e *ToolExecutor) doInteract(
	ctx context.Context,
	args map[string]any,
	headers map[string]string,
	started time.Time,
) tools.ExecutionResult {
	action, _ := args["action"].(string)
	if action == "" {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "parameter action is required",
			},
			DurationMs: durationMs(started),
		}
	}

	body := map[string]any{"action": action}
	if v, ok := args["selector"].(string); ok && v != "" {
		body["selector"] = v
	}
	if v, ok := args["value"].(string); ok {
		body["value"] = v
	}
	if v, ok := args["coordinates"].(map[string]any); ok {
		body["coordinates"] = v
	}
	if v, ok := args["timeout_ms"]; ok {
		body["timeout_ms"] = v
	}

	return e.postJSON(ctx, "/v1/interact", body, headers, started)
}

func (e *ToolExecutor) doExtract(
	ctx context.Context,
	args map[string]any,
	headers map[string]string,
	started time.Time,
) tools.ExecutionResult {
	mode, _ := args["mode"].(string)
	if mode == "" {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "parameter mode is required",
			},
			DurationMs: durationMs(started),
		}
	}

	body := map[string]any{"mode": mode}
	if v, ok := args["selector"].(string); ok && v != "" {
		body["selector"] = v
	}

	return e.postJSON(ctx, "/v1/extract", body, headers, started)
}

func (e *ToolExecutor) doScreenshot(
	ctx context.Context,
	args map[string]any,
	headers map[string]string,
	started time.Time,
) tools.ExecutionResult {
	body := map[string]any{}
	if v, ok := args["full_page"].(bool); ok {
		body["full_page"] = v
	}
	if v, ok := args["selector"].(string); ok && v != "" {
		body["selector"] = v
	}
	if v, ok := args["quality"]; ok {
		body["quality"] = v
	}

	return e.postJSON(ctx, "/v1/screenshot", body, headers, started)
}

func (e *ToolExecutor) doSessionClose(
	ctx context.Context,
	sessionID string,
	headers map[string]string,
	started time.Time,
) tools.ExecutionResult {
	endpoint := fmt.Sprintf("%s/v1/sessions/%s", e.baseURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorBrowserError,
				Message:    fmt.Sprintf("build request failed: %s", err.Error()),
			},
			DurationMs: durationMs(started),
		}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return mapRequestError(err, started)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return tools.ExecutionResult{
			ResultJSON: map[string]any{"closed": true},
			DurationMs: durationMs(started),
		}
	}

	return mapHTTPError(resp, started)
}

func (e *ToolExecutor) postJSON(
	ctx context.Context,
	path string,
	body map[string]any,
	headers map[string]string,
	started time.Time,
) tools.ExecutionResult {
	payload, err := json.Marshal(body)
	if err != nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorBrowserError,
				Message:    fmt.Sprintf("marshal request failed: %s", err.Error()),
			},
			DurationMs: durationMs(started),
		}
	}

	endpoint := e.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorBrowserError,
				Message:    fmt.Sprintf("build request failed: %s", err.Error()),
			},
			DurationMs: durationMs(started),
		}
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return mapRequestError(err, started)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorBrowserError,
				Message:    "read response body failed",
			},
			DurationMs: durationMs(started),
		}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var result map[string]any
		if err := json.Unmarshal(respBody, &result); err != nil {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorBrowserError,
					Message:    "decode response failed",
				},
				DurationMs: durationMs(started),
			}
		}
		return tools.ExecutionResult{
			ResultJSON: result,
			DurationMs: durationMs(started),
		}
	}

	return mapHTTPErrorFromBody(resp.StatusCode, respBody, started)
}

func mapRequestError(err error, started time.Time) tools.ExecutionResult {
	if ctx := err; ctx != nil {
		if isContextDeadline(err) {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorTimeout,
					Message:    "browser request timed out",
				},
				DurationMs: durationMs(started),
			}
		}
	}
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: errorBrowserError,
			Message:    fmt.Sprintf("browser request failed: %s", err.Error()),
		},
		DurationMs: durationMs(started),
	}
}

func mapHTTPError(resp *http.Response, started time.Time) tools.ExecutionResult {
	body, _ := io.ReadAll(resp.Body)
	return mapHTTPErrorFromBody(resp.StatusCode, body, started)
}

func mapHTTPErrorFromBody(statusCode int, body []byte, started time.Time) tools.ExecutionResult {
	var parsed struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &parsed)

	errorClass := errorBrowserError
	if parsed.Code == "timeout" || statusCode == http.StatusGatewayTimeout {
		errorClass = errorTimeout
	}
	if parsed.Code == "network_blocked" {
		errorClass = errorNetworkBlocked
	}

	message := parsed.Message
	if message == "" {
		message = fmt.Sprintf("browser service returned %d", statusCode)
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
