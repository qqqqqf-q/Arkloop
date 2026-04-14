package local

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func sendACPRequest(t *testing.T, addr string, req agentRequest) agentResponse {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("connect to %s: %v", addr, err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	var resp agentResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func startEcho(t *testing.T, addr string) string {
	t.Helper()
	resp := sendACPRequest(t, addr, agentRequest{
		Action: "acp_start",
		ACPStart: &acpStartPayload{
			Command: []string{"echo", "hello"},
		},
	})
	if resp.Error != "" {
		t.Fatalf("acp_start error: %s", resp.Error)
	}
	if resp.ACPStart == nil {
		t.Fatal("acp_start: nil result")
	}
	return resp.ACPStart.ProcessID
}

func intPtr(v int) *int { return &v }

// waitForOutput polls acp_read until non-empty data appears or timeout.
func waitForOutput(t *testing.T, addr, pid string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var cursor uint64
	for time.Now().Before(deadline) {
		resp := sendACPRequest(t, addr, agentRequest{
			Action: "acp_read",
			ACPRead: &acpReadPayload{
				ProcessID: pid,
				Cursor:    cursor,
			},
		})
		if resp.Error != "" {
			// Process may have been cleaned up already; if data was collected, return it.
			return ""
		}
		if resp.ACPRead != nil && resp.ACPRead.Data != "" {
			return resp.ACPRead.Data
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for output")
	return ""
}

// ---------------------------------------------------------------------------
// Pool tests
// ---------------------------------------------------------------------------

func TestPoolReady(t *testing.T) {
	p := New(Config{})
	if !p.Ready() {
		t.Error("pool should be ready immediately after creation")
	}
}

func TestPoolAcquireAndDestroy(t *testing.T) {
	p := New(Config{})
	defer p.Drain(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, proc, err := p.Acquire(ctx, "default")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if proc != nil {
		t.Error("local pool should return nil process")
	}
	if sess.ID == "" {
		t.Error("session ID should not be empty")
	}
	if sess.Tier != "default" {
		t.Errorf("tier = %q, want %q", sess.Tier, "default")
	}
	if sess.Dial == nil {
		t.Fatal("session Dial should not be nil")
	}

	// Verify the dialer connects to the agent.
	conn, err := sess.Dial(ctx)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	conn.Close()

	p.DestroyVM(nil, sess.SocketDir)
}

func TestPoolStats(t *testing.T) {
	p := New(Config{})
	ctx := context.Background()

	s1, _, _ := p.Acquire(ctx, "a")
	s2, _, _ := p.Acquire(ctx, "b")

	stats := p.Stats()
	if stats.TotalCreated != 2 {
		t.Errorf("TotalCreated = %d, want 2", stats.TotalCreated)
	}
	if stats.TotalDestroyed != 0 {
		t.Errorf("TotalDestroyed = %d, want 0", stats.TotalDestroyed)
	}

	p.DestroyVM(nil, s1.SocketDir)

	stats = p.Stats()
	if stats.TotalCreated != 2 {
		t.Errorf("TotalCreated = %d, want 2", stats.TotalCreated)
	}
	if stats.TotalDestroyed != 1 {
		t.Errorf("TotalDestroyed = %d, want 1", stats.TotalDestroyed)
	}

	p.DestroyVM(nil, s2.SocketDir)

	stats = p.Stats()
	if stats.TotalDestroyed != 2 {
		t.Errorf("TotalDestroyed = %d, want 2", stats.TotalDestroyed)
	}
}

func TestPoolDrain(t *testing.T) {
	p := New(Config{})
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, _, err := p.Acquire(ctx, "tier")
		if err != nil {
			t.Fatalf("Acquire %d: %v", i, err)
		}
	}

	p.Drain(ctx)

	if p.Ready() {
		t.Error("pool should not be ready after drain")
	}

	// Internal agents map should be empty.
	p.mu.Lock()
	remaining := len(p.agents)
	p.mu.Unlock()
	if remaining != 0 {
		t.Errorf("agents remaining = %d, want 0", remaining)
	}
}

// ---------------------------------------------------------------------------
// Agent tests
// ---------------------------------------------------------------------------

func TestAgentStartProcess(t *testing.T) {
	agent, err := NewAgent("test-start")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	pid := startEcho(t, agent.Addr())
	if pid == "" {
		t.Fatal("process ID should not be empty")
	}

	data := waitForOutput(t, agent.Addr(), pid, 5*time.Second)
	if !strings.Contains(data, "hello") {
		t.Errorf("output = %q, want it to contain %q", data, "hello")
	}
}

func TestAgentWriteAndRead(t *testing.T) {
	agent, err := NewAgent("test-rw")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	// Start cat to echo stdin to stdout.
	resp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_start",
		ACPStart: &acpStartPayload{
			Command: []string{"cat"},
		},
	})
	if resp.Error != "" {
		t.Fatalf("start cat: %s", resp.Error)
	}
	pid := resp.ACPStart.ProcessID

	// Write data to stdin.
	writeResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_write",
		ACPWrite: &acpWritePayload{
			ProcessID: pid,
			Data:      "ping\n",
		},
	})
	if writeResp.Error != "" {
		t.Fatalf("write: %s", writeResp.Error)
	}
	if writeResp.ACPWrite == nil || writeResp.ACPWrite.BytesWritten != 5 {
		t.Errorf("BytesWritten = %v, want 5", writeResp.ACPWrite)
	}

	data := waitForOutput(t, agent.Addr(), pid, 5*time.Second)
	if !strings.Contains(data, "ping") {
		t.Errorf("read data = %q, want it to contain %q", data, "ping")
	}

	// Stop the cat process to clean up.
	sendACPRequest(t, agent.Addr(), agentRequest{
		Action:  "acp_stop",
		ACPStop: &acpStopPayload{ProcessID: pid, Force: true},
	})
}

func TestAgentStopProcess(t *testing.T) {
	agent, err := NewAgent("test-stop")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	// Start a long-running process.
	resp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_start",
		ACPStart: &acpStartPayload{
			Command: []string{"sleep", "60"},
		},
	})
	if resp.Error != "" {
		t.Fatalf("start: %s", resp.Error)
	}
	pid := resp.ACPStart.ProcessID

	// Force-stop it.
	stopResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action:  "acp_stop",
		ACPStop: &acpStopPayload{ProcessID: pid, Force: true},
	})
	if stopResp.Error != "" {
		t.Fatalf("stop: %s", stopResp.Error)
	}
	if stopResp.ACPStop == nil || stopResp.ACPStop.Status != "stopped" {
		t.Errorf("stop status = %v, want 'stopped'", stopResp.ACPStop)
	}

	// Process should no longer be findable.
	readResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action:  "acp_read",
		ACPRead: &acpReadPayload{ProcessID: pid},
	})
	if readResp.Error == "" {
		t.Error("expected error reading stopped process")
	}
}

func TestAgentWaitProcess(t *testing.T) {
	agent, err := NewAgent("test-wait")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	// Start a process that exits with code 42.
	resp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_start",
		ACPStart: &acpStartPayload{
			Command: []string{"sh", "-c", "exit 42"},
		},
	})
	if resp.Error != "" {
		t.Fatalf("start: %s", resp.Error)
	}
	pid := resp.ACPStart.ProcessID

	waitResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_wait",
		ACPWait: &acpWaitPayload{
			ProcessID: pid,
			TimeoutMs: 5000,
		},
	})
	if waitResp.Error != "" {
		t.Fatalf("wait: %s", waitResp.Error)
	}
	if waitResp.ACPWait == nil {
		t.Fatal("wait result is nil")
	}
	if !waitResp.ACPWait.Exited {
		t.Error("expected Exited = true")
	}
	if waitResp.ACPWait.ExitCode == nil || *waitResp.ACPWait.ExitCode != 42 {
		t.Errorf("ExitCode = %v, want 42", waitResp.ACPWait.ExitCode)
	}
}

func TestAgentWaitTimeout(t *testing.T) {
	agent, err := NewAgent("test-wait-timeout")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	// Start a long-running process.
	resp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_start",
		ACPStart: &acpStartPayload{
			Command: []string{"sleep", "60"},
		},
	})
	if resp.Error != "" {
		t.Fatalf("start: %s", resp.Error)
	}
	pid := resp.ACPStart.ProcessID

	// Wait with a very short timeout.
	waitResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_wait",
		ACPWait: &acpWaitPayload{
			ProcessID: pid,
			TimeoutMs: 100,
		},
	})
	if waitResp.Error != "" {
		t.Fatalf("wait: %s", waitResp.Error)
	}
	if waitResp.ACPWait == nil {
		t.Fatal("wait result is nil")
	}
	if waitResp.ACPWait.Exited {
		t.Error("expected Exited = false (timeout)")
	}

	// Cleanup.
	sendACPRequest(t, agent.Addr(), agentRequest{
		Action:  "acp_stop",
		ACPStop: &acpStopPayload{ProcessID: pid, Force: true},
	})
}

func TestAgentProcessNotFound(t *testing.T) {
	agent, err := NewAgent("test-notfound")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	bogusID := "nonexistent-process-id"

	// read
	readResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action:  "acp_read",
		ACPRead: &acpReadPayload{ProcessID: bogusID},
	})
	if readResp.Error == "" {
		t.Error("expected error for read with bogus process ID")
	}
	if !strings.Contains(readResp.Error, "not found") {
		t.Errorf("error = %q, want it to contain 'not found'", readResp.Error)
	}

	// write
	writeResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action:   "acp_write",
		ACPWrite: &acpWritePayload{ProcessID: bogusID, Data: "x"},
	})
	if writeResp.Error == "" {
		t.Error("expected error for write with bogus process ID")
	}

	// stop
	stopResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action:  "acp_stop",
		ACPStop: &acpStopPayload{ProcessID: bogusID},
	})
	if stopResp.Error == "" {
		t.Error("expected error for stop with bogus process ID")
	}

	// wait
	waitResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action:  "acp_wait",
		ACPWait: &acpWaitPayload{ProcessID: bogusID, TimeoutMs: 100},
	})
	if waitResp.Error == "" {
		t.Error("expected error for wait with bogus process ID")
	}
}

func TestAgentUnknownAction(t *testing.T) {
	agent, err := NewAgent("test-unknown")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	resp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "bogus_action",
	})
	if resp.Error == "" {
		t.Error("expected error for unknown action")
	}
	if !strings.Contains(resp.Error, "unknown action") {
		t.Errorf("error = %q, want it to contain 'unknown action'", resp.Error)
	}
}

func TestAgentStartMissingPayload(t *testing.T) {
	agent, err := NewAgent("test-missing-payload")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	resp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_start",
		// ACPStart is nil
	})
	if resp.Error == "" {
		t.Error("expected error when acp_start payload is nil")
	}
}

func TestAgentConcurrentSessions(t *testing.T) {
	agent, err := NewAgent("test-concurrent")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	const n = 5
	pids := make([]string, n)

	// Start N processes that each output a unique string.
	for i := 0; i < n; i++ {
		msg := strings.Repeat(string(rune('A'+i)), 10)
		resp := sendACPRequest(t, agent.Addr(), agentRequest{
			Action: "acp_start",
			ACPStart: &acpStartPayload{
				Command: []string{"echo", msg},
			},
		})
		if resp.Error != "" {
			t.Fatalf("start %d: %s", i, resp.Error)
		}
		pids[i] = resp.ACPStart.ProcessID
	}

	// Read from each and verify isolation.
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			expected := strings.Repeat(string(rune('A'+idx)), 10)
			data := waitForOutput(t, agent.Addr(), pids[idx], 5*time.Second)
			if !strings.Contains(data, expected) {
				t.Errorf("process %d: output = %q, want it to contain %q", idx, data, expected)
			}
		}(i)
	}
	wg.Wait()
}

func TestAgentWaitWithStdoutCapture(t *testing.T) {
	agent, err := NewAgent("test-wait-stdout")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	resp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_start",
		ACPStart: &acpStartPayload{
			Command: []string{"sh", "-c", "echo captured-output"},
		},
	})
	if resp.Error != "" {
		t.Fatalf("start: %s", resp.Error)
	}
	pid := resp.ACPStart.ProcessID

	waitResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_wait",
		ACPWait: &acpWaitPayload{
			ProcessID: pid,
			TimeoutMs: 5000,
		},
	})
	if waitResp.Error != "" {
		t.Fatalf("wait: %s", waitResp.Error)
	}
	if !waitResp.ACPWait.Exited {
		t.Fatal("expected process to have exited")
	}
	if waitResp.ACPWait.ExitCode == nil || *waitResp.ACPWait.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", waitResp.ACPWait.ExitCode)
	}
	if !strings.Contains(waitResp.ACPWait.Stdout, "captured-output") {
		t.Errorf("Stdout = %q, want it to contain %q", waitResp.ACPWait.Stdout, "captured-output")
	}
}

func TestAgentWaitWithStderrCapture(t *testing.T) {
	agent, err := NewAgent("test-wait-stderr")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	resp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_start",
		ACPStart: &acpStartPayload{
			Command: []string{"sh", "-c", "echo err-msg >&2"},
		},
	})
	if resp.Error != "" {
		t.Fatalf("start: %s", resp.Error)
	}
	pid := resp.ACPStart.ProcessID

	waitResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_wait",
		ACPWait: &acpWaitPayload{
			ProcessID: pid,
			TimeoutMs: 5000,
		},
	})
	if waitResp.Error != "" {
		t.Fatalf("wait: %s", waitResp.Error)
	}
	if !strings.Contains(waitResp.ACPWait.Stderr, "err-msg") {
		t.Errorf("Stderr = %q, want it to contain %q", waitResp.ACPWait.Stderr, "err-msg")
	}
}

func TestAgentStopAlreadyExited(t *testing.T) {
	agent, err := NewAgent("test-stop-exited")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	// Start a process that exits immediately.
	resp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_start",
		ACPStart: &acpStartPayload{
			Command: []string{"true"},
		},
	})
	if resp.Error != "" {
		t.Fatalf("start: %s", resp.Error)
	}
	pid := resp.ACPStart.ProcessID

	// Wait for the process to exit.
	sendACPRequest(t, agent.Addr(), agentRequest{
		Action:  "acp_wait",
		ACPWait: &acpWaitPayload{ProcessID: pid, TimeoutMs: 5000},
	})

	// Stop should report already_exited.
	stopResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action:  "acp_stop",
		ACPStop: &acpStopPayload{ProcessID: pid},
	})
	if stopResp.Error != "" {
		t.Fatalf("stop: %s", stopResp.Error)
	}
	if stopResp.ACPStop == nil || stopResp.ACPStop.Status != "already_exited" {
		t.Errorf("stop status = %v, want 'already_exited'", stopResp.ACPStop)
	}
}

func TestAgentReadExitedProcess(t *testing.T) {
	agent, err := NewAgent("test-read-exited")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	resp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_start",
		ACPStart: &acpStartPayload{
			Command: []string{"sh", "-c", "echo done; exit 7"},
		},
	})
	if resp.Error != "" {
		t.Fatalf("start: %s", resp.Error)
	}
	pid := resp.ACPStart.ProcessID

	// Wait for exit.
	sendACPRequest(t, agent.Addr(), agentRequest{
		Action:  "acp_wait",
		ACPWait: &acpWaitPayload{ProcessID: pid, TimeoutMs: 5000},
	})

	// Read should reflect exited state and exit code.
	readResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action:  "acp_read",
		ACPRead: &acpReadPayload{ProcessID: pid},
	})
	if readResp.Error != "" {
		t.Fatalf("read: %s", readResp.Error)
	}
	if readResp.ACPRead == nil {
		t.Fatal("read result is nil")
	}
	if !readResp.ACPRead.Exited {
		t.Error("expected Exited = true")
	}
	if readResp.ACPRead.ExitCode == nil || *readResp.ACPRead.ExitCode != 7 {
		t.Errorf("ExitCode = %v, want 7", readResp.ACPRead.ExitCode)
	}
}

func TestAgentStartWithCwd(t *testing.T) {
	agent, err := NewAgent("test-cwd")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	resp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_start",
		ACPStart: &acpStartPayload{
			Command: []string{"pwd"},
			Cwd:     "/tmp",
		},
	})
	if resp.Error != "" {
		t.Fatalf("start: %s", resp.Error)
	}
	pid := resp.ACPStart.ProcessID

	waitResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action:  "acp_wait",
		ACPWait: &acpWaitPayload{ProcessID: pid, TimeoutMs: 5000},
	})
	if waitResp.Error != "" {
		t.Fatalf("wait: %s", waitResp.Error)
	}
	// On macOS /tmp is a symlink to /private/tmp, so accept both.
	stdout := waitResp.ACPWait.Stdout
	if !strings.Contains(stdout, "/tmp") {
		t.Errorf("Stdout = %q, want it to contain '/tmp'", stdout)
	}
}

func TestAgentStartWithEnv(t *testing.T) {
	agent, err := NewAgent("test-env")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	resp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_start",
		ACPStart: &acpStartPayload{
			Command: []string{"sh", "-c", "echo $TEST_LOCAL_VAR"},
			Env:     map[string]string{"TEST_LOCAL_VAR": "magic_value_42"},
		},
	})
	if resp.Error != "" {
		t.Fatalf("start: %s", resp.Error)
	}
	pid := resp.ACPStart.ProcessID

	waitResp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action:  "acp_wait",
		ACPWait: &acpWaitPayload{ProcessID: pid, TimeoutMs: 5000},
	})
	if waitResp.Error != "" {
		t.Fatalf("wait: %s", waitResp.Error)
	}
	if !strings.Contains(waitResp.ACPWait.Stdout, "magic_value_42") {
		t.Errorf("Stdout = %q, want it to contain 'magic_value_42'", waitResp.ACPWait.Stdout)
	}
}

func TestAgentStartEmptyCommand(t *testing.T) {
	agent, err := NewAgent("test-empty-cmd")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	resp := sendACPRequest(t, agent.Addr(), agentRequest{
		Action: "acp_start",
		ACPStart: &acpStartPayload{
			Command: []string{},
		},
	})
	if resp.Error == "" {
		t.Error("expected error for empty command")
	}
}

// ---------------------------------------------------------------------------
// limitedBuffer tests
// ---------------------------------------------------------------------------

func TestLimitedBufferEmpty(t *testing.T) {
	b := newLimitedBuffer(100)
	if s := b.String(); s != "" {
		t.Errorf("empty buffer String() = %q, want %q", s, "")
	}
}

func TestLimitedBuffer(t *testing.T) {
	b := newLimitedBuffer(10)

	n, err := b.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 5 {
		t.Errorf("Write returned %d, want 5", n)
	}

	// Write more than remaining capacity.
	n, err = b.Write([]byte("worldextra"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Write always reports full len(p) as written.
	if n != 10 {
		t.Errorf("Write returned %d, want 10", n)
	}

	if s := b.String(); s != "helloworld" {
		t.Errorf("String() = %q, want %q", s, "helloworld")
	}

	// Further writes should be silently discarded.
	n, err = b.Write([]byte("overflow"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 8 {
		t.Errorf("Write returned %d, want 8", n)
	}
	if s := b.String(); s != "helloworld" {
		t.Errorf("after overflow, String() = %q, want %q", s, "helloworld")
	}
}

func TestLimitedBufferZeroLimit(t *testing.T) {
	b := newLimitedBuffer(0)
	n, err := b.Write([]byte("data"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 4 {
		t.Errorf("Write returned %d, want 4", n)
	}
	if s := b.String(); s != "" {
		t.Errorf("zero-limit buffer String() = %q, want %q", s, "")
	}
}

func TestLimitedBufferNegativeLimit(t *testing.T) {
	b := newLimitedBuffer(-5)
	n, _ := b.Write([]byte("anything"))
	if n != 8 {
		t.Errorf("Write returned %d, want 8", n)
	}
	if s := b.String(); s != "" {
		t.Errorf("negative-limit buffer String() = %q, want %q", s, "")
	}
}

func TestLimitedBufferExactFit(t *testing.T) {
	b := newLimitedBuffer(5)
	_, _ = b.Write([]byte("exact"))
	if s := b.String(); s != "exact" {
		t.Errorf("String() = %q, want %q", s, "exact")
	}
	// Subsequent write should be discarded.
	_, _ = b.Write([]byte("x"))
	if s := b.String(); s != "exact" {
		t.Errorf("after extra write, String() = %q, want %q", s, "exact")
	}
}

// ---------------------------------------------------------------------------
// PR-10: acp_status tests
// ---------------------------------------------------------------------------

func TestACPStatus(t *testing.T) {
	agent, err := NewAgent("test-status")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	addr := agent.Addr()
	pid := startEcho(t, addr)

	// Wait for echo to exit
	time.Sleep(200 * time.Millisecond)

	// Query status
	resp := sendACPRequest(t, addr, agentRequest{
		Action:    "acp_status",
		ACPStatus: &acpStatusPayload{ProcessID: pid},
	})
	if resp.Error != "" {
		t.Fatalf("acp_status error: %s", resp.Error)
	}
	if resp.ACPStatus == nil {
		t.Fatal("acp_status: nil result")
	}
	// echo exits quickly, so we expect exited
	if !resp.ACPStatus.Exited {
		t.Error("expected exited=true for echo process")
	}
}
