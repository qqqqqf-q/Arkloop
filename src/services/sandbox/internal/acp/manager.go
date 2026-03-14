package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
)

const (
	defaultCallTimeout = 30 * time.Second
	maxACPTimeout      = 10 * time.Minute
)

type managedACPSession struct {
	compute        *session.Session
	accountID      string
	processID      string
	killGraceMs    int
	cleanupDelayMs int
}

type Manager struct {
	compute *session.Manager
	logger  *logging.JSONLogger

	mu       sync.Mutex
	sessions map[string]*managedACPSession
}

func NewManager(compute *session.Manager, logger *logging.JSONLogger) *Manager {
	return &Manager{
		compute:  compute,
		logger:   logger,
		sessions: make(map[string]*managedACPSession),
	}
}

func (m *Manager) StartACPAgent(ctx context.Context, req StartACPAgentRequest) (*StartACPAgentResponse, error) {
	if len(req.Command) == 0 {
		return nil, newError(CodeInvalidRequest, "command is required", http.StatusBadRequest)
	}

	sid := req.SessionID
	tier := req.Tier
	if tier == "" {
		tier = "pro"
	}

	sn, err := m.compute.GetOrCreate(ctx, sid, tier, req.AccountID)
	if err != nil {
		if strings.Contains(err.Error(), "account mismatch") {
			return nil, accountMismatchError()
		}
		return nil, fmt.Errorf("acquire session: %w", err)
	}

	result, err := m.invokeACPAction(ctx, sn, agentRequest{
		Action: "acp_start",
		ACPStart: &acpStartPayload{
			Command:        req.Command,
			Cwd:            req.Cwd,
			Env:            req.Env,
			TimeoutMs:      req.TimeoutMs,
			KillGraceMs:    req.KillGraceMs,
			CleanupDelayMs: req.CleanupDelayMs,
		},
	})
	if err != nil {
		return nil, err
	}
	if result.ACPStart == nil {
		return nil, newError(CodeTransportError, "missing acp_start in response", http.StatusBadGateway)
	}

	m.mu.Lock()
	m.sessions[sid] = &managedACPSession{
		compute:        sn,
		accountID:      req.AccountID,
		processID:      result.ACPStart.ProcessID,
		killGraceMs:    req.KillGraceMs,
		cleanupDelayMs: req.CleanupDelayMs,
	}
	m.mu.Unlock()

	return &StartACPAgentResponse{
		SessionID:    sid,
		ProcessID:    result.ACPStart.ProcessID,
		Status:       result.ACPStart.Status,
		AgentVersion: result.ACPStart.AgentVersion,
	}, nil
}

func (m *Manager) WriteACP(ctx context.Context, req WriteACPRequest) (*WriteACPResponse, error) {
	entry, err := m.getEntry(req.SessionID, req.AccountID)
	if err != nil {
		return nil, err
	}

	result, err := m.invokeACPAction(ctx, entry.compute, agentRequest{
		Action: "acp_write",
		ACPWrite: &acpWritePayload{
			ProcessID: req.ProcessID,
			Data:      req.Data,
		},
	})
	if err != nil {
		return nil, err
	}
	if result.ACPWrite == nil {
		return nil, newError(CodeTransportError, "missing acp_write in response", http.StatusBadGateway)
	}

	return &WriteACPResponse{BytesWritten: result.ACPWrite.BytesWritten}, nil
}

func (m *Manager) ReadACP(ctx context.Context, req ReadACPRequest) (*ReadACPResponse, error) {
	entry, err := m.getEntry(req.SessionID, req.AccountID)
	if err != nil {
		return nil, err
	}

	result, err := m.invokeACPAction(ctx, entry.compute, agentRequest{
		Action: "acp_read",
		ACPRead: &acpReadPayload{
			ProcessID: req.ProcessID,
			Cursor:    req.Cursor,
			MaxBytes:  req.MaxBytes,
		},
	})
	if err != nil {
		return nil, err
	}
	if result.ACPRead == nil {
		return nil, newError(CodeTransportError, "missing acp_read in response", http.StatusBadGateway)
	}

	r := result.ACPRead
	return &ReadACPResponse{
		Data:         r.Data,
		NextCursor:   r.NextCursor,
		Truncated:    r.Truncated,
		Stderr:       r.Stderr,
		ErrorSummary: r.ErrorSummary,
		Exited:       r.Exited,
		ExitCode:     r.ExitCode,
	}, nil
}

func (m *Manager) StopACPAgent(ctx context.Context, req StopACPAgentRequest) (*StopACPAgentResponse, error) {
	entry, err := m.getEntry(req.SessionID, req.AccountID)
	if err != nil {
		return nil, err
	}

	result, err := m.invokeACPAction(ctx, entry.compute, agentRequest{
		Action: "acp_stop",
		ACPStop: &acpStopPayload{
			ProcessID:     req.ProcessID,
			Force:         req.Force,
			GracePeriodMs: req.GracePeriodMs,
		},
	})
	if err != nil {
		return nil, err
	}
	if result.ACPStop == nil {
		return nil, newError(CodeTransportError, "missing acp_stop in response", http.StatusBadGateway)
	}

	m.mu.Lock()
	delete(m.sessions, req.SessionID)
	m.mu.Unlock()

	return &StopACPAgentResponse{Status: result.ACPStop.Status}, nil
}

func (m *Manager) WaitACPAgent(ctx context.Context, req WaitACPAgentRequest) (*WaitACPAgentResponse, error) {
	entry, err := m.getEntry(req.SessionID, req.AccountID)
	if err != nil {
		return nil, err
	}

	timeout := defaultCallTimeout
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs)*time.Millisecond + 5*time.Second
	}
	if timeout > maxACPTimeout {
		timeout = maxACPTimeout
	}

	result, err := m.invokeACPActionWithTimeout(ctx, entry.compute, timeout, agentRequest{
		Action: "acp_wait",
		ACPWait: &acpWaitPayload{
			ProcessID: req.ProcessID,
			TimeoutMs: req.TimeoutMs,
		},
	})
	if err != nil {
		return nil, err
	}
	if result.ACPWait == nil {
		return nil, newError(CodeTransportError, "missing acp_wait in response", http.StatusBadGateway)
	}

	r := result.ACPWait
	if r.Exited {
		m.mu.Lock()
		delete(m.sessions, req.SessionID)
		m.mu.Unlock()
	}

	return &WaitACPAgentResponse{
		Exited:   r.Exited,
		ExitCode: r.ExitCode,
		Stdout:   r.Stdout,
		Stderr:   r.Stderr,
	}, nil
}

func (m *Manager) Close(ctx context.Context, sessionID, accountID string) error {
	m.mu.Lock()
	entry, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	if accountID != "" && entry.accountID != "" && entry.accountID != accountID {
		m.mu.Unlock()
		return accountMismatchError()
	}
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	if entry.processID != "" {
		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, _ = m.invokeACPAction(stopCtx, entry.compute, agentRequest{
			Action: "acp_stop",
			ACPStop: &acpStopPayload{ProcessID: entry.processID, Force: true},
		})
	}
	return nil
}

// --- internal helpers ---

func (m *Manager) StatusACP(ctx context.Context, req StatusACPRequest) (*StatusACPResponse, error) {
	entry, err := m.getEntry(req.SessionID, req.AccountID)
	if err != nil {
		return nil, err
	}

	result, err := m.invokeACPAction(ctx, entry.compute, agentRequest{
		Action: "acp_status",
		ACPStatus: &acpStatusPayload{
			ProcessID: req.ProcessID,
		},
	})
	if err != nil {
		return nil, err
	}
	if result.ACPStatus == nil {
		return nil, newError(CodeTransportError, "missing acp_status in response", http.StatusBadGateway)
	}

	r := result.ACPStatus
	return &StatusACPResponse{
		SessionID:    req.SessionID,
		ProcessID:    req.ProcessID,
		Running:      r.Running,
		StdoutCursor: r.StdoutCursor,
		Exited:       r.Exited,
		ExitCode:     r.ExitCode,
	}, nil
}

// --- internal helpers (invoke / getEntry) ---

func (m *Manager) invokeACPAction(ctx context.Context, sn *session.Session, payload agentRequest) (*agentResponse, error) {
	return m.invokeACPActionWithTimeout(ctx, sn, defaultCallTimeout, payload)
}

func (m *Manager) invokeACPActionWithTimeout(ctx context.Context, sn *session.Session, timeout time.Duration, payload agentRequest) (*agentResponse, error) {
	sn.TouchActivity()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := sn.Dial(ctx)
	if err != nil {
		return nil, newError(CodeTransportError, fmt.Sprintf("connect to agent: %v", err), http.StatusBadGateway)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	if err := json.NewEncoder(conn).Encode(payload); err != nil {
		return nil, newError(CodeTransportError, fmt.Sprintf("send request: %v", err), http.StatusBadGateway)
	}

	body, err := io.ReadAll(conn)
	if err != nil {
		return nil, newError(CodeTransportError, fmt.Sprintf("read response: %v", err), http.StatusBadGateway)
	}

	var resp agentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, newError(CodeTransportError, fmt.Sprintf("decode response: %v", err), http.StatusBadGateway)
	}

	if resp.Error != "" {
		if strings.Contains(resp.Error, "not found") {
			return nil, processNotFoundError(resp.Error)
		}
		return nil, newError(CodeAgentError, resp.Error, http.StatusInternalServerError)
	}

	return &resp, nil
}

func (m *Manager) getEntry(sessionID, accountID string) (*managedACPSession, error) {
	m.mu.Lock()
	entry, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return nil, sessionNotFoundError()
	}
	if accountID != "" && entry.accountID != "" && entry.accountID != accountID {
		return nil, accountMismatchError()
	}
	return entry, nil
}
