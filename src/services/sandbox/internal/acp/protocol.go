package acp

import (
	"context"
	"net/http"
)

// --- Service-level types (HTTP handler layer) ---

type StartACPAgentRequest struct {
	SessionID string            `json:"session_id"`
	AccountID string            `json:"account_id,omitempty"`
	Tier      string            `json:"tier,omitempty"`
	Command   []string          `json:"command"`
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	TimeoutMs int               `json:"timeout_ms,omitempty"`
}

type StartACPAgentResponse struct {
	SessionID string `json:"session_id"`
	ProcessID string `json:"process_id"`
	Status    string `json:"status"`
}

type WriteACPRequest struct {
	SessionID string `json:"session_id"`
	AccountID string `json:"account_id,omitempty"`
	ProcessID string `json:"process_id"`
	Data      string `json:"data"`
}

type WriteACPResponse struct {
	BytesWritten int `json:"bytes_written"`
}

type ReadACPRequest struct {
	SessionID string `json:"session_id"`
	AccountID string `json:"account_id,omitempty"`
	ProcessID string `json:"process_id"`
	Cursor    uint64 `json:"cursor"`
	MaxBytes  int    `json:"max_bytes,omitempty"`
}

type ReadACPResponse struct {
	Data       string `json:"data"`
	NextCursor uint64 `json:"next_cursor"`
	Truncated  bool   `json:"truncated"`
	Stderr     string `json:"stderr,omitempty"`
	Exited     bool   `json:"exited"`
	ExitCode   *int   `json:"exit_code,omitempty"`
}

type StopACPAgentRequest struct {
	SessionID string `json:"session_id"`
	AccountID string `json:"account_id,omitempty"`
	ProcessID string `json:"process_id"`
	Force     bool   `json:"force,omitempty"`
}

type StopACPAgentResponse struct {
	Status string `json:"status"`
}

type WaitACPAgentRequest struct {
	SessionID string `json:"session_id"`
	AccountID string `json:"account_id,omitempty"`
	ProcessID string `json:"process_id"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type WaitACPAgentResponse struct {
	Exited   bool   `json:"exited"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

// Service defines the ACP session management interface.
type Service interface {
	StartACPAgent(ctx context.Context, req StartACPAgentRequest) (*StartACPAgentResponse, error)
	WriteACP(ctx context.Context, req WriteACPRequest) (*WriteACPResponse, error)
	ReadACP(ctx context.Context, req ReadACPRequest) (*ReadACPResponse, error)
	StopACPAgent(ctx context.Context, req StopACPAgentRequest) (*StopACPAgentResponse, error)
	WaitACPAgent(ctx context.Context, req WaitACPAgentRequest) (*WaitACPAgentResponse, error)
	Close(ctx context.Context, sessionID, accountID string) error
}

// --- Agent-level types (JSON RPC to guest agent via Dial) ---

type agentRequest struct {
	Action   string           `json:"action"`
	ACPStart *acpStartPayload `json:"acp_start,omitempty"`
	ACPWrite *acpWritePayload `json:"acp_write,omitempty"`
	ACPRead  *acpReadPayload  `json:"acp_read,omitempty"`
	ACPStop  *acpStopPayload  `json:"acp_stop,omitempty"`
	ACPWait  *acpWaitPayload  `json:"acp_wait,omitempty"`
}

type agentResponse struct {
	Action   string          `json:"action"`
	ACPStart *acpStartResult `json:"acp_start,omitempty"`
	ACPWrite *acpWriteResult `json:"acp_write,omitempty"`
	ACPRead  *acpReadResult  `json:"acp_read,omitempty"`
	ACPStop  *acpStopResult  `json:"acp_stop,omitempty"`
	ACPWait  *acpWaitResult  `json:"acp_wait,omitempty"`
	Error    string          `json:"error,omitempty"`
}

type acpStartPayload struct {
	Command   []string          `json:"command"`
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	TimeoutMs int               `json:"timeout_ms,omitempty"`
}

type acpStartResult struct {
	ProcessID string `json:"process_id"`
	Status    string `json:"status"`
}

type acpWritePayload struct {
	ProcessID string `json:"process_id"`
	Data      string `json:"data"`
}

type acpWriteResult struct {
	BytesWritten int `json:"bytes_written"`
}

type acpReadPayload struct {
	ProcessID string `json:"process_id"`
	Cursor    uint64 `json:"cursor"`
	MaxBytes  int    `json:"max_bytes,omitempty"`
}

type acpReadResult struct {
	Data       string `json:"data"`
	NextCursor uint64 `json:"next_cursor"`
	Truncated  bool   `json:"truncated"`
	Stderr     string `json:"stderr,omitempty"`
	Exited     bool   `json:"exited"`
	ExitCode   *int   `json:"exit_code,omitempty"`
}

type acpStopPayload struct {
	ProcessID string `json:"process_id"`
	Force     bool   `json:"force,omitempty"`
}

type acpStopResult struct {
	Status string `json:"status"`
}

type acpWaitPayload struct {
	ProcessID string `json:"process_id"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type acpWaitResult struct {
	Exited   bool   `json:"exited"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

// --- Error types ---

const (
	CodeSessionNotFound = "acp.session_not_found"
	CodeProcessNotFound = "acp.process_not_found"
	CodeTransportError  = "acp.transport_error"
	CodeAccountMismatch = "acp.account_mismatch"
	CodeInvalidRequest  = "acp.invalid_request"
	CodeAgentError      = "acp.agent_error"
)

type Error struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *Error) Error() string { return e.Message }

func newError(code, message string, httpStatus int) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: httpStatus}
}

func sessionNotFoundError() *Error {
	return newError(CodeSessionNotFound, "acp session not found", http.StatusNotFound)
}

func processNotFoundError(msg string) *Error {
	return newError(CodeProcessNotFound, msg, http.StatusNotFound)
}

func accountMismatchError() *Error {
	return newError(CodeAccountMismatch, "session belongs to another account", http.StatusForbidden)
}
