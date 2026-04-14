package main

import (
	"strings"
	"testing"
	"time"
)

func startProcess(t *testing.T, mgr *ACPManager, cmd []string) *ACPStartResponse {
	t.Helper()
	cwd := t.TempDir()
	resp, err := mgr.Start(ACPStartRequest{Command: cmd, Cwd: cwd})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if resp.ProcessID == "" {
		t.Fatal("empty process ID")
	}
	return resp
}

func TestACPStartAndRead(t *testing.T) {
	mgr := NewACPManager()
	resp := startProcess(t, mgr, []string{"sh", "-c", "echo hello && sleep 0.1 && echo world"})

	waitResp, err := mgr.Wait(ACPWaitRequest{ProcessID: resp.ProcessID, TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !waitResp.Exited {
		t.Fatal("process did not exit")
	}
	if waitResp.ExitCode == nil || *waitResp.ExitCode != 0 {
		t.Fatalf("exit code: %v", waitResp.ExitCode)
	}

	readResp, err := mgr.Read(ACPReadRequest{ProcessID: resp.ProcessID, Cursor: 0})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(readResp.Data, "hello") {
		t.Fatalf("stdout missing 'hello': %q", readResp.Data)
	}
	if !strings.Contains(readResp.Data, "world") {
		t.Fatalf("stdout missing 'world': %q", readResp.Data)
	}
	if readResp.ExitCode == nil || *readResp.ExitCode != 0 {
		t.Fatalf("read exit code: %v", readResp.ExitCode)
	}
}

func TestACPStartWriteRead(t *testing.T) {
	mgr := NewACPManager()
	resp := startProcess(t, mgr, []string{"cat"})

	writeResp, err := mgr.Write(ACPWriteRequest{ProcessID: resp.ProcessID, Data: "ping\n"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if writeResp.BytesWritten != 5 {
		t.Fatalf("bytes written = %d, want 5", writeResp.BytesWritten)
	}

	// cat 回显需要时间到达 ring buffer
	time.Sleep(200 * time.Millisecond)

	readResp, err := mgr.Read(ACPReadRequest{ProcessID: resp.ProcessID, Cursor: 0})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(readResp.Data, "ping") {
		t.Fatalf("stdout missing 'ping': %q", readResp.Data)
	}

	_, err = mgr.Stop(ACPStopRequest{ProcessID: resp.ProcessID, Force: true})
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestACPStop_Graceful(t *testing.T) {
	mgr := NewACPManager()
	resp := startProcess(t, mgr, []string{"sleep", "300"})

	stopResp, err := mgr.Stop(ACPStopRequest{ProcessID: resp.ProcessID, Force: false})
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if stopResp.Status != "stopped" {
		t.Fatalf("status = %q, want stopped", stopResp.Status)
	}
}

func TestACPStop_Force(t *testing.T) {
	mgr := NewACPManager()
	resp := startProcess(t, mgr, []string{"sleep", "300"})

	stopResp, err := mgr.Stop(ACPStopRequest{ProcessID: resp.ProcessID, Force: true})
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if stopResp.Status != "stopped" {
		t.Fatalf("status = %q, want stopped", stopResp.Status)
	}
}

func TestACPWait_Timeout(t *testing.T) {
	mgr := NewACPManager()
	resp := startProcess(t, mgr, []string{"sleep", "300"})

	waitResp, err := mgr.Wait(ACPWaitRequest{ProcessID: resp.ProcessID, TimeoutMs: 100})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if waitResp.Exited {
		t.Fatal("expected process not exited")
	}

	_, err = mgr.Stop(ACPStopRequest{ProcessID: resp.ProcessID, Force: true})
	if err != nil {
		t.Fatalf("cleanup stop: %v", err)
	}
}

func TestACPWait_NaturalExit(t *testing.T) {
	mgr := NewACPManager()
	resp := startProcess(t, mgr, []string{"sh", "-c", "echo done"})

	waitResp, err := mgr.Wait(ACPWaitRequest{ProcessID: resp.ProcessID, TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !waitResp.Exited {
		t.Fatal("expected process exited")
	}
	if waitResp.ExitCode == nil || *waitResp.ExitCode != 0 {
		t.Fatalf("exit code: %v", waitResp.ExitCode)
	}
	if !strings.Contains(waitResp.Stdout, "done") {
		t.Fatalf("stdout missing 'done': %q", waitResp.Stdout)
	}
}

func TestACPStartInvalidCommand(t *testing.T) {
	mgr := NewACPManager()
	_, err := mgr.Start(ACPStartRequest{Command: nil})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	if !strings.Contains(err.Error(), "command must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestACPReadNotFound(t *testing.T) {
	mgr := NewACPManager()
	_, err := mgr.Read(ACPReadRequest{ProcessID: "nonexistent", Cursor: 0})
	if err == nil {
		t.Fatal("expected error for unknown process")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestACPStartStderr(t *testing.T) {
	mgr := NewACPManager()
	resp := startProcess(t, mgr, []string{"sh", "-c", "echo err >&2 && exit 1"})

	waitResp, err := mgr.Wait(ACPWaitRequest{ProcessID: resp.ProcessID, TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !waitResp.Exited {
		t.Fatal("expected exit")
	}
	if waitResp.ExitCode == nil || *waitResp.ExitCode != 1 {
		t.Fatalf("exit code: %v", waitResp.ExitCode)
	}

	readResp, err := mgr.Read(ACPReadRequest{ProcessID: resp.ProcessID, Cursor: 0})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(readResp.Stderr, "err") {
		t.Fatalf("stderr missing 'err': %q", readResp.Stderr)
	}
	if readResp.ExitCode == nil || *readResp.ExitCode != 1 {
		t.Fatalf("read exit code: %v", readResp.ExitCode)
	}
}

func TestACPV2Dispatch(t *testing.T) {
	oldCwd := shellWorkspaceDir
	shellWorkspaceDir = t.TempDir()
	defer func() { shellWorkspaceDir = oldCwd }()

	resp := invokeAgentRequest(t, AgentRequest{
		Action: "acp_start",
		ACPStart: &ACPStartRequest{
			Command: []string{"echo", "hi"},
		},
	})
	if resp.Error != "" {
		t.Fatalf("error: %s", resp.Error)
	}
	if resp.ACPStart == nil {
		t.Fatal("expected ACPStart response")
	}
	if resp.ACPStart.ProcessID == "" {
		t.Fatal("empty process ID")
	}
	if resp.ACPStart.Status != "running" {
		t.Fatalf("status = %q, want running", resp.ACPStart.Status)
	}

	// 等待进程退出后清理
	time.Sleep(200 * time.Millisecond)
	_, _ = acpManager.Stop(ACPStopRequest{ProcessID: resp.ACPStart.ProcessID, Force: true})
}

func TestACPV2Dispatch_MissingPayload(t *testing.T) {
	resp := invokeAgentRequest(t, AgentRequest{Action: "acp_start"})
	if resp.Error == "" {
		t.Fatal("expected error for missing payload")
	}
	if !strings.Contains(resp.Error, "acp_start is required") {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
}
