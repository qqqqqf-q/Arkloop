package acp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
)

// fakeDialer returns a Dialer backed by net.Pipe.
// Each Dial call spawns a new pipe; handler runs on the server end.
func fakeDialer(handler func(net.Conn)) session.Dialer {
	return func(ctx context.Context) (net.Conn, error) {
		server, client := net.Pipe()
		go handler(server)
		return client, nil
	}
}

// fakeACPAgent reads one agentRequest, looks up a canned response by action,
// writes it back, then closes the connection.
func fakeACPAgent(responses map[string]agentResponse) func(net.Conn) {
	return func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		var req agentRequest
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			return
		}
		resp, ok := responses[req.Action]
		if !ok {
			resp = agentResponse{Action: req.Action, Error: "unknown action"}
		}
		resp.Action = req.Action
		_ = json.NewEncoder(conn).Encode(resp)
	}
}

func newTestManager() *Manager {
	return &Manager{
		logger:   logging.NewJSONLogger("test", nil),
		sessions: make(map[string]*managedACPSession),
	}
}

func newFakeSession(handler func(net.Conn)) *session.Session {
	return &session.Session{
		ID:   "test-session",
		Dial: fakeDialer(handler),
	}
}

func intPtr(v int) *int { return &v }

// ---------------------------------------------------------------------------
// WriteACP
// ---------------------------------------------------------------------------

func TestManager_WriteACP(t *testing.T) {
	m := newTestManager()

	handler := fakeACPAgent(map[string]agentResponse{
		"acp_write": {ACPWrite: &acpWriteResult{BytesWritten: 42}},
	})
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(handler),
		accountID: "acct-1",
		processID: "pid-1",
	}

	resp, err := m.WriteACP(context.Background(), WriteACPRequest{
		RuntimeSessionKey: "s1",
		AccountID:         "acct-1",
		ProcessID:         "pid-1",
		Data:              "hello",
	})
	if err != nil {
		t.Fatalf("WriteACP: %v", err)
	}
	if resp.BytesWritten != 42 {
		t.Errorf("BytesWritten = %d, want 42", resp.BytesWritten)
	}
}

// ---------------------------------------------------------------------------
// ReadACP
// ---------------------------------------------------------------------------

func TestManager_ReadACP(t *testing.T) {
	m := newTestManager()

	handler := fakeACPAgent(map[string]agentResponse{
		"acp_read": {ACPRead: &acpReadResult{
			Data:       "output data",
			NextCursor: 100,
			Truncated:  true,
			Stderr:     "warn",
			Exited:     false,
		}},
	})
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(handler),
		accountID: "acct-1",
		processID: "pid-1",
	}

	resp, err := m.ReadACP(context.Background(), ReadACPRequest{
		RuntimeSessionKey: "s1",
		AccountID:         "acct-1",
		ProcessID:         "pid-1",
		Cursor:            0,
		MaxBytes:          4096,
	})
	if err != nil {
		t.Fatalf("ReadACP: %v", err)
	}
	if resp.Data != "output data" {
		t.Errorf("Data = %q, want %q", resp.Data, "output data")
	}
	if resp.NextCursor != 100 {
		t.Errorf("NextCursor = %d, want 100", resp.NextCursor)
	}
	if !resp.Truncated {
		t.Error("Truncated = false, want true")
	}
	if resp.Stderr != "warn" {
		t.Errorf("Stderr = %q, want %q", resp.Stderr, "warn")
	}
	if resp.Exited {
		t.Error("Exited = true, want false")
	}
}

// ---------------------------------------------------------------------------
// StopACPAgent
// ---------------------------------------------------------------------------

func TestManager_StopACPAgent(t *testing.T) {
	m := newTestManager()

	handler := fakeACPAgent(map[string]agentResponse{
		"acp_stop": {ACPStop: &acpStopResult{Status: "stopped"}},
	})
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(handler),
		accountID: "acct-1",
		processID: "pid-1",
	}

	resp, err := m.StopACPAgent(context.Background(), StopACPAgentRequest{
		RuntimeSessionKey: "s1",
		AccountID:         "acct-1",
		ProcessID:         "pid-1",
		Force:             true,
	})
	if err != nil {
		t.Fatalf("StopACPAgent: %v", err)
	}
	if resp.Status != "stopped" {
		t.Errorf("Status = %q, want %q", resp.Status, "stopped")
	}

	m.mu.Lock()
	_, exists := m.sessions["s1"]
	m.mu.Unlock()
	if exists {
		t.Error("session should be removed after stop")
	}
}

// ---------------------------------------------------------------------------
// WaitACPAgent (process exited)
// ---------------------------------------------------------------------------

func TestManager_WaitACPAgent_Exited(t *testing.T) {
	m := newTestManager()

	handler := fakeACPAgent(map[string]agentResponse{
		"acp_wait": {ACPWait: &acpWaitResult{
			Exited:   true,
			ExitCode: intPtr(0),
			Stdout:   "done",
		}},
	})
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(handler),
		accountID: "acct-1",
		processID: "pid-1",
	}

	resp, err := m.WaitACPAgent(context.Background(), WaitACPAgentRequest{
		RuntimeSessionKey: "s1",
		AccountID:         "acct-1",
		ProcessID:         "pid-1",
		TimeoutMs:         5000,
	})
	if err != nil {
		t.Fatalf("WaitACPAgent: %v", err)
	}
	if !resp.Exited {
		t.Error("Exited = false, want true")
	}
	if resp.ExitCode == nil || *resp.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", resp.ExitCode)
	}

	m.mu.Lock()
	_, exists := m.sessions["s1"]
	m.mu.Unlock()
	if exists {
		t.Error("session should be cleaned up after process exited")
	}
}

// ---------------------------------------------------------------------------
// WaitACPAgent (timeout, process still running)
// ---------------------------------------------------------------------------

func TestManager_WaitACPAgent_Timeout(t *testing.T) {
	m := newTestManager()

	handler := fakeACPAgent(map[string]agentResponse{
		"acp_wait": {ACPWait: &acpWaitResult{Exited: false}},
	})
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(handler),
		accountID: "acct-1",
		processID: "pid-1",
	}

	resp, err := m.WaitACPAgent(context.Background(), WaitACPAgentRequest{
		RuntimeSessionKey: "s1",
		AccountID:         "acct-1",
		ProcessID:         "pid-1",
		TimeoutMs:         100,
	})
	if err != nil {
		t.Fatalf("WaitACPAgent: %v", err)
	}
	if resp.Exited {
		t.Error("Exited = true, want false")
	}

	m.mu.Lock()
	_, exists := m.sessions["s1"]
	m.mu.Unlock()
	if !exists {
		t.Error("session should still exist when process did not exit")
	}
}

// ---------------------------------------------------------------------------
// getEntry: session not found
// ---------------------------------------------------------------------------

func TestManager_GetEntry_NotFound(t *testing.T) {
	m := newTestManager()

	_, err := m.getEntry("nonexistent", "acct-1")
	if err == nil {
		t.Fatal("expected error for missing session")
	}
	var acpErr *Error
	if !errors.As(err, &acpErr) {
		t.Fatalf("error type = %T, want *acp.Error", err)
	}
	if acpErr.Code != CodeSessionNotFound {
		t.Errorf("code = %q, want %q", acpErr.Code, CodeSessionNotFound)
	}
}

// ---------------------------------------------------------------------------
// getEntry: account mismatch
// ---------------------------------------------------------------------------

func TestManager_GetEntry_AccountMismatch(t *testing.T) {
	m := newTestManager()
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(func(c net.Conn) { _ = c.Close() }),
		accountID: "acct-1",
		processID: "pid-1",
	}

	_, err := m.getEntry("s1", "acct-other")
	if err == nil {
		t.Fatal("expected error for account mismatch")
	}
	var acpErr *Error
	if !errors.As(err, &acpErr) {
		t.Fatalf("error type = %T, want *acp.Error", err)
	}
	if acpErr.Code != CodeAccountMismatch {
		t.Errorf("code = %q, want %q", acpErr.Code, CodeAccountMismatch)
	}
}

// ---------------------------------------------------------------------------
// Close: session exists, sends acp_stop to agent
// ---------------------------------------------------------------------------

func TestManager_Close(t *testing.T) {
	stopCalled := make(chan struct{}, 1)
	handler := func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		var req agentRequest
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			return
		}
		if req.Action == "acp_stop" {
			stopCalled <- struct{}{}
		}
		_ = json.NewEncoder(conn).Encode(agentResponse{
			Action:  req.Action,
			ACPStop: &acpStopResult{Status: "stopped"},
		})
	}

	m := newTestManager()
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(handler),
		accountID: "acct-1",
		processID: "pid-1",
	}

	if err := m.Close(context.Background(), "s1", "acct-1"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	m.mu.Lock()
	_, exists := m.sessions["s1"]
	m.mu.Unlock()
	if exists {
		t.Error("session should be removed after Close")
	}

	select {
	case <-stopCalled:
	case <-time.After(2 * time.Second):
		t.Error("acp_stop was not sent to agent")
	}
}

// ---------------------------------------------------------------------------
// Close: session does not exist (no-op)
// ---------------------------------------------------------------------------

func TestManager_Close_NoSession(t *testing.T) {
	m := newTestManager()
	if err := m.Close(context.Background(), "nonexistent", "acct-1"); err != nil {
		t.Fatalf("Close on missing session should not error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Close: account mismatch
// ---------------------------------------------------------------------------

func TestManager_Close_AccountMismatch(t *testing.T) {
	m := newTestManager()
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(func(c net.Conn) { _ = c.Close() }),
		accountID: "acct-1",
		processID: "pid-1",
	}

	err := m.Close(context.Background(), "s1", "acct-other")
	if err == nil {
		t.Fatal("expected error for account mismatch")
	}
	var acpErr *Error
	if !errors.As(err, &acpErr) {
		t.Fatalf("error type = %T, want *acp.Error", err)
	}
	if acpErr.Code != CodeAccountMismatch {
		t.Errorf("code = %q, want %q", acpErr.Code, CodeAccountMismatch)
	}

	m.mu.Lock()
	_, exists := m.sessions["s1"]
	m.mu.Unlock()
	if !exists {
		t.Error("session should still exist after failed Close")
	}
}

// ---------------------------------------------------------------------------
// invokeACPAction: agent returns an error string
// ---------------------------------------------------------------------------

func TestManager_InvokeACPAction_AgentError(t *testing.T) {
	m := newTestManager()

	handler := fakeACPAgent(map[string]agentResponse{
		"acp_write": {Error: "disk full"},
	})
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(handler),
		accountID: "acct-1",
		processID: "pid-1",
	}

	_, err := m.WriteACP(context.Background(), WriteACPRequest{
		RuntimeSessionKey: "s1",
		AccountID:         "acct-1",
		ProcessID:         "pid-1",
		Data:              "x",
	})
	if err == nil {
		t.Fatal("expected error from agent")
	}
	var acpErr *Error
	if !errors.As(err, &acpErr) {
		t.Fatalf("error type = %T, want *acp.Error", err)
	}
	if acpErr.Code != CodeAgentError {
		t.Errorf("code = %q, want %q", acpErr.Code, CodeAgentError)
	}
}

// ---------------------------------------------------------------------------
// invokeACPAction: agent returns "not found" -> CodeProcessNotFound
// ---------------------------------------------------------------------------

func TestManager_InvokeACPAction_ProcessNotFound(t *testing.T) {
	m := newTestManager()

	handler := fakeACPAgent(map[string]agentResponse{
		"acp_read": {Error: "process not found"},
	})
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(handler),
		accountID: "acct-1",
		processID: "pid-1",
	}

	_, err := m.ReadACP(context.Background(), ReadACPRequest{
		RuntimeSessionKey: "s1",
		AccountID:         "acct-1",
		ProcessID:         "pid-1",
	})
	if err == nil {
		t.Fatal("expected error from agent")
	}
	var acpErr *Error
	if !errors.As(err, &acpErr) {
		t.Fatalf("error type = %T, want *acp.Error", err)
	}
	if acpErr.Code != CodeProcessNotFound {
		t.Errorf("code = %q, want %q", acpErr.Code, CodeProcessNotFound)
	}
}

// ---------------------------------------------------------------------------
// invokeACPAction: dial failure -> CodeTransportError
// ---------------------------------------------------------------------------

func TestManager_InvokeACPAction_DialFailure(t *testing.T) {
	m := newTestManager()

	failSession := &session.Session{
		ID: "fail-session",
		Dial: func(ctx context.Context) (net.Conn, error) {
			return nil, errors.New("connection refused")
		},
	}
	m.sessions["s1"] = &managedACPSession{
		compute:   failSession,
		accountID: "acct-1",
		processID: "pid-1",
	}

	_, err := m.WriteACP(context.Background(), WriteACPRequest{
		RuntimeSessionKey: "s1",
		AccountID:         "acct-1",
		ProcessID:         "pid-1",
		Data:              "x",
	})
	if err == nil {
		t.Fatal("expected transport error")
	}
	var acpErr *Error
	if !errors.As(err, &acpErr) {
		t.Fatalf("error type = %T, want *acp.Error", err)
	}
	if acpErr.Code != CodeTransportError {
		t.Errorf("code = %q, want %q", acpErr.Code, CodeTransportError)
	}
}

// ---------------------------------------------------------------------------
// invokeACPAction: agent returns invalid JSON -> CodeTransportError
// ---------------------------------------------------------------------------

func TestManager_InvokeACPAction_InvalidJSON(t *testing.T) {
	m := newTestManager()

	handler := func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		// read the JSON request so the writer unblocks
		var req agentRequest
		_ = json.NewDecoder(conn).Decode(&req)
		// write garbage instead of valid JSON
		_, _ = conn.Write([]byte("not json"))
	}
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(handler),
		accountID: "acct-1",
		processID: "pid-1",
	}

	_, err := m.WriteACP(context.Background(), WriteACPRequest{
		RuntimeSessionKey: "s1",
		AccountID:         "acct-1",
		ProcessID:         "pid-1",
		Data:              "x",
	})
	if err == nil {
		t.Fatal("expected error for empty response")
	}
	var acpErr *Error
	if !errors.As(err, &acpErr) {
		t.Fatalf("error type = %T, want *acp.Error", err)
	}
	if acpErr.Code != CodeTransportError {
		t.Errorf("code = %q, want %q", acpErr.Code, CodeTransportError)
	}
}

// ---------------------------------------------------------------------------
// getEntry: empty accountID skips mismatch check
// ---------------------------------------------------------------------------

func TestManager_GetEntry_EmptyAccountID(t *testing.T) {
	m := newTestManager()
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(func(c net.Conn) { _ = c.Close() }),
		accountID: "acct-1",
		processID: "pid-1",
	}

	entry, err := m.getEntry("s1", "")
	if err != nil {
		t.Fatalf("getEntry with empty accountID should succeed: %v", err)
	}
	if entry.accountID != "acct-1" {
		t.Errorf("accountID = %q, want %q", entry.accountID, "acct-1")
	}
}

// ---------------------------------------------------------------------------
// ReadACP: response missing ACPRead field -> transport error
// ---------------------------------------------------------------------------

func TestManager_ReadACP_MissingPayload(t *testing.T) {
	m := newTestManager()

	handler := fakeACPAgent(map[string]agentResponse{
		"acp_read": {}, // no ACPRead
	})
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(handler),
		accountID: "acct-1",
		processID: "pid-1",
	}

	_, err := m.ReadACP(context.Background(), ReadACPRequest{
		RuntimeSessionKey: "s1",
		AccountID:         "acct-1",
		ProcessID:         "pid-1",
	})
	if err == nil {
		t.Fatal("expected error for missing payload")
	}
	var acpErr *Error
	if !errors.As(err, &acpErr) {
		t.Fatalf("error type = %T, want *acp.Error", err)
	}
	if acpErr.Code != CodeTransportError {
		t.Errorf("code = %q, want %q", acpErr.Code, CodeTransportError)
	}
}

// ---------------------------------------------------------------------------
// WriteACP: response missing ACPWrite field -> transport error
// ---------------------------------------------------------------------------

func TestManager_WriteACP_MissingPayload(t *testing.T) {
	m := newTestManager()

	handler := fakeACPAgent(map[string]agentResponse{
		"acp_write": {}, // no ACPWrite
	})
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(handler),
		accountID: "acct-1",
		processID: "pid-1",
	}

	_, err := m.WriteACP(context.Background(), WriteACPRequest{
		RuntimeSessionKey: "s1",
		AccountID:         "acct-1",
		ProcessID:         "pid-1",
		Data:              "x",
	})
	if err == nil {
		t.Fatal("expected error for missing payload")
	}
	var acpErr *Error
	if !errors.As(err, &acpErr) {
		t.Fatalf("error type = %T, want *acp.Error", err)
	}
	if acpErr.Code != CodeTransportError {
		t.Errorf("code = %q, want %q", acpErr.Code, CodeTransportError)
	}
}

// ---------------------------------------------------------------------------
// StopACPAgent: response missing ACPStop field -> transport error
// ---------------------------------------------------------------------------

func TestManager_StopACPAgent_MissingPayload(t *testing.T) {
	m := newTestManager()

	handler := fakeACPAgent(map[string]agentResponse{
		"acp_stop": {}, // no ACPStop
	})
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(handler),
		accountID: "acct-1",
		processID: "pid-1",
	}

	_, err := m.StopACPAgent(context.Background(), StopACPAgentRequest{
		RuntimeSessionKey: "s1",
		AccountID:         "acct-1",
		ProcessID:         "pid-1",
	})
	if err == nil {
		t.Fatal("expected error for missing payload")
	}
	var acpErr *Error
	if !errors.As(err, &acpErr) {
		t.Fatalf("error type = %T, want *acp.Error", err)
	}
	if acpErr.Code != CodeTransportError {
		t.Errorf("code = %q, want %q", acpErr.Code, CodeTransportError)
	}
}

// ---------------------------------------------------------------------------
// WaitACPAgent: response missing ACPWait field -> transport error
// ---------------------------------------------------------------------------

func TestManager_WaitACPAgent_MissingPayload(t *testing.T) {
	m := newTestManager()

	handler := fakeACPAgent(map[string]agentResponse{
		"acp_wait": {}, // no ACPWait
	})
	m.sessions["s1"] = &managedACPSession{
		compute:   newFakeSession(handler),
		accountID: "acct-1",
		processID: "pid-1",
	}

	_, err := m.WaitACPAgent(context.Background(), WaitACPAgentRequest{
		RuntimeSessionKey: "s1",
		AccountID:         "acct-1",
		ProcessID:         "pid-1",
	})
	if err == nil {
		t.Fatal("expected error for missing payload")
	}
	var acpErr *Error
	if !errors.As(err, &acpErr) {
		t.Fatalf("error type = %T, want *acp.Error", err)
	}
	if acpErr.Code != CodeTransportError {
		t.Errorf("code = %q, want %q", acpErr.Code, CodeTransportError)
	}
}
