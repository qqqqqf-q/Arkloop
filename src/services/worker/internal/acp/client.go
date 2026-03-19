package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const httpClientTimeout = 5 * time.Minute

// Client talks to the Sandbox service's ACP HTTP endpoints.
type Client struct {
	baseURL   string
	authToken string
	http      *http.Client
}

func NewClient(baseURL, authToken string) *Client {
	return &Client{
		baseURL:   baseURL,
		authToken: authToken,
		http:      &http.Client{Timeout: httpClientTimeout},
	}
}

// --- Request / Response types (worker-local, mirrors sandbox protocol) ---

type StartRequest struct {
	RuntimeSessionKey string            `json:"runtime_session_key"`
	AccountID         string            `json:"account_id,omitempty"`
	Tier              string            `json:"tier,omitempty"`
	Command           []string          `json:"command"`
	Cwd               string            `json:"cwd,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	TimeoutMs         int               `json:"timeout_ms,omitempty"`
	KillGraceMs       int               `json:"kill_grace_ms,omitempty"`
	CleanupDelayMs    int               `json:"cleanup_delay_ms,omitempty"`
}

type StartResponse struct {
	ProcessID    string `json:"process_id"`
	Status       string `json:"status"`
	AgentVersion string `json:"agent_version,omitempty"`
}

type WriteRequest struct {
	RuntimeSessionKey string `json:"runtime_session_key"`
	AccountID         string `json:"account_id,omitempty"`
	ProcessID         string `json:"process_id"`
	Data              string `json:"data"`
}

type ReadRequest struct {
	RuntimeSessionKey string `json:"runtime_session_key"`
	AccountID         string `json:"account_id,omitempty"`
	ProcessID         string `json:"process_id"`
	Cursor            uint64 `json:"cursor"`
	MaxBytes          int    `json:"max_bytes,omitempty"`
}

type ReadResponse struct {
	Data         string `json:"data"`
	NextCursor   uint64 `json:"next_cursor"`
	Truncated    bool   `json:"truncated"`
	Stderr       string `json:"stderr,omitempty"`
	ErrorSummary string `json:"error_summary,omitempty"`
	Exited       bool   `json:"exited"`
	ExitCode     *int   `json:"exit_code,omitempty"`
}

type StopRequest struct {
	RuntimeSessionKey string `json:"runtime_session_key"`
	AccountID         string `json:"account_id,omitempty"`
	ProcessID         string `json:"process_id"`
	Force             bool   `json:"force,omitempty"`
	GracePeriodMs     int    `json:"grace_period_ms,omitempty"`
}

type WaitRequest struct {
	RuntimeSessionKey string `json:"runtime_session_key"`
	AccountID         string `json:"account_id,omitempty"`
	ProcessID         string `json:"process_id"`
	TimeoutMs         int    `json:"timeout_ms,omitempty"`
}

type WaitResponse struct {
	Exited   bool   `json:"exited"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

type StatusRequest struct {
	RuntimeSessionKey string `json:"runtime_session_key"`
	AccountID         string `json:"account_id,omitempty"`
	ProcessID         string `json:"process_id"`
}

type StatusResponse struct {
	RuntimeSessionKey string `json:"runtime_session_key"`
	ProcessID         string `json:"process_id"`
	Running           bool   `json:"running"`
	StdoutCursor      uint64 `json:"stdout_cursor"`
	Exited            bool   `json:"exited"`
	ExitCode          *int   `json:"exit_code,omitempty"`
}

// --- Structured errors ---

type ClientError struct {
	Code       string
	Message    string
	StatusCode int
}

func (e *ClientError) Error() string {
	return fmt.Sprintf("acp client: %s (HTTP %d)", e.Message, e.StatusCode)
}

// --- Public API ---

func (c *Client) Start(ctx context.Context, req StartRequest) (*StartResponse, error) {
	var resp StartResponse
	if err := c.post(ctx, "/v1/acp/start", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Write(ctx context.Context, req WriteRequest) error {
	return c.post(ctx, "/v1/acp/write", req, nil)
}

func (c *Client) Read(ctx context.Context, req ReadRequest) (*ReadResponse, error) {
	var resp ReadResponse
	if err := c.post(ctx, "/v1/acp/read", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Stop(ctx context.Context, req StopRequest) error {
	return c.post(ctx, "/v1/acp/stop", req, nil)
}

func (c *Client) Wait(ctx context.Context, req WaitRequest) (*WaitResponse, error) {
	var resp WaitResponse
	if err := c.post(ctx, "/v1/acp/wait", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Status(ctx context.Context, req StatusRequest) (*StatusResponse, error) {
	var resp StatusResponse
	if err := c.post(ctx, "/v1/acp/status", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// --- Internal HTTP plumbing ---

func (c *Client) post(ctx context.Context, path string, reqBody any, respBody any) error {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return &ClientError{Code: "marshal_error", Message: fmt.Sprintf("marshal request: %s", err), StatusCode: 0}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return &ClientError{Code: "request_error", Message: fmt.Sprintf("build request: %s", err), StatusCode: 0}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return &ClientError{Code: "timeout", Message: "request cancelled or timed out", StatusCode: 0}
		}
		return &ClientError{Code: "transport_error", Message: fmt.Sprintf("request failed: %s", err), StatusCode: 0}
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return &ClientError{Code: "read_error", Message: "read response body failed", StatusCode: httpResp.StatusCode}
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		msg := string(body)
		if msg == "" {
			msg = http.StatusText(httpResp.StatusCode)
		}
		return &ClientError{Code: "http_error", Message: msg, StatusCode: httpResp.StatusCode}
	}

	if respBody != nil && len(body) > 0 {
		if err := json.Unmarshal(body, respBody); err != nil {
			return &ClientError{Code: "decode_error", Message: fmt.Sprintf("decode response: %s", err), StatusCode: httpResp.StatusCode}
		}
	}

	return nil
}
