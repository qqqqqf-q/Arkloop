package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	shellapi "arkloop/services/sandbox/internal/shell"
)

func bindShellDirs(t *testing.T, workspace string) {
	t.Helper()
	home := filepath.Join(workspace, "home")
	temp := filepath.Join(workspace, "tmp")
	skills := filepath.Join(workspace, "skills")
	shellWorkspaceDir = workspace
	shellHomeDir = home
	shellTempDir = temp
	shellSkillsDir = skills
	t.Cleanup(func() {
		shellWorkspaceDir = defaultWorkloadCwd
		shellHomeDir = defaultWorkloadHome
		shellTempDir = defaultWorkloadTmp
		shellSkillsDir = defaultSkillsRoot
	})
}

func closeController(controller *ShellController) {
	controller.mu.Lock()
	controller.closeLocked()
	controller.mu.Unlock()
}

func drainShellOutput(t *testing.T, controller *ShellController, resp *shellapi.AgentSessionResponse, code, msg string) string {
	t.Helper()
	if code != "" {
		t.Fatalf("shell action failed: %s %s", code, msg)
	}
	output := resp.Output
	for resp.Running {
		resp, code, msg = controller.WriteStdin(shellapi.AgentWriteStdinRequest{YieldTimeMs: 200})
		if code != "" {
			t.Fatalf("write_stdin failed: %s %s", code, msg)
		}
		output += resp.Output
	}
	return output
}

func TestShellControllerExecCommandPreservesCwd(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	target := filepath.Join(workspace, "cdtarget")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	controller := NewShellController()
	defer closeController(controller)

	resp, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{
		Command:     fmt.Sprintf("cd %q && pwd", target),
		YieldTimeMs: 1000,
		TimeoutMs:   5000,
	})
	if code != "" {
		t.Fatalf("exec_command failed: %s %s", code, msg)
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		want = target
	}
	got, err := filepath.EvalSymlinks(resp.Cwd)
	if err != nil {
		got = resp.Cwd
	}
	if got != want {
		t.Fatalf("expected cwd %s, got %s", want, got)
	}

	resp, code, msg = controller.ExecCommand(shellapi.AgentExecCommandRequest{Command: "pwd", YieldTimeMs: 1000, TimeoutMs: 5000})
	if code != "" {
		t.Fatalf("exec_command failed: %s %s", code, msg)
	}
	var outLine string
	for _, part := range strings.Split(resp.Output, "\n") {
		if s := strings.TrimSpace(part); s != "" {
			outLine = s
			break
		}
	}
	outResolved, err := filepath.EvalSymlinks(outLine)
	if err != nil {
		outResolved = outLine
	}
	if outResolved != want {
		t.Fatalf("expected pwd output cwd %s, got %s (raw %q)", want, outResolved, resp.Output)
	}
}

func TestShellControllerWriteStdinPollDoesNotRepeatOutput(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewShellController()
	defer closeController(controller)

	resp, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{
		Command:     "printf 'first\n'; sleep 0.3; printf 'second\n'",
		YieldTimeMs: 100,
		TimeoutMs:   5000,
	})
	if code != "" {
		t.Fatalf("exec_command failed: %s %s", code, msg)
	}
	if !resp.Running {
		t.Fatalf("expected command to still be running, got %#v", resp)
	}

	poll, code, msg := controller.WriteStdin(shellapi.AgentWriteStdinRequest{YieldTimeMs: 1000})
	if code != "" {
		t.Fatalf("poll failed: %s %s", code, msg)
	}
	combined := resp.Output + poll.Output
	if !strings.Contains(combined, "first") || !strings.Contains(combined, "second") {
		t.Fatalf("expected drained combined output, got %q", combined)
	}

	again, code, msg := controller.WriteStdin(shellapi.AgentWriteStdinRequest{YieldTimeMs: 50})
	if code != "" {
		t.Fatalf("second poll failed: %s %s", code, msg)
	}
	if strings.Contains(again.Output, "first") || strings.Contains(again.Output, "second") {
		t.Fatalf("expected command output to stay drained, got %q", again.Output)
	}
}

func TestShellControllerWriteStdinInteractive(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewShellController()
	defer closeController(controller)

	resp, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{Command: "python3 -c \"name=input(); print(name)\"", YieldTimeMs: 200, TimeoutMs: 5000})
	if code != "" {
		t.Fatalf("exec_command failed: %s %s", code, msg)
	}
	if !resp.Running {
		t.Fatal("expected interactive command to keep running")
	}

	resp, code, msg = controller.WriteStdin(shellapi.AgentWriteStdinRequest{Chars: "arkloop\n", YieldTimeMs: 1000})
	if code != "" {
		t.Fatalf("write_stdin failed: %s %s", code, msg)
	}
	if !strings.Contains(resp.Output, "arkloop") {
		t.Fatalf("expected echoed output, got %q", resp.Output)
	}
}

func TestShellControllerExecCommandReturnsPromptWithoutWaitingForExit(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewShellController()
	defer closeController(controller)

	warmup, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{Command: "printf ready", YieldTimeMs: 500, TimeoutMs: 5000})
	_ = drainShellOutput(t, controller, warmup, code, msg)

	started := time.Now()
	resp, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{
		Command:     "python3 -c \"import sys,time; sys.stdout.write('name: '); sys.stdout.flush(); time.sleep(4)\"",
		YieldTimeMs: 1000,
		TimeoutMs:   5000,
	})
	if code != "" {
		t.Fatalf("exec_command failed: %s %s", code, msg)
	}
	if !resp.Running {
		t.Fatalf("expected command to keep running, got %#v", resp)
	}
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("prompt should return before command exit, got %v", elapsed)
	}
	if !strings.Contains(resp.Output, "name: ") {
		t.Fatalf("expected prompt output, got %q", resp.Output)
	}
}

func TestShellControllerExecCommandShortCommandPreservesOutput(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewShellController()
	defer closeController(controller)

	resp, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{
		Command:     "printf 'first\n'; sleep 0.05; printf 'second\n'",
		YieldTimeMs: 600,
		TimeoutMs:   5000,
	})
	if code != "" {
		t.Fatalf("exec_command failed: %s %s", code, msg)
	}
	output := drainShellOutput(t, controller, resp, code, msg)
	if strings.Count(output, "first\n") != 1 || strings.Count(output, "second\n") != 1 {
		t.Fatalf("expected short command output once, got %q", output)
	}

	again, code, msg := controller.WriteStdin(shellapi.AgentWriteStdinRequest{YieldTimeMs: 50})
	if code != "" {
		t.Fatalf("poll failed: %s %s", code, msg)
	}
	if strings.Contains(again.Output, "first") || strings.Contains(again.Output, "second") {
		t.Fatalf("expected no repeated short command output, got %q", again.Output)
	}
}

func TestShellControllerExecCommandPreservesTrailingOutput(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewShellController()
	defer closeController(controller)

	resp, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{
		Command:     "python3 -c \"import sys,time; sys.stdout.write('head\\n'); sys.stdout.flush(); time.sleep(0.05); sys.stdout.write('tail\\n'); sys.stdout.flush()\"",
		YieldTimeMs: 600,
		TimeoutMs:   5000,
	})
	if code != "" {
		t.Fatalf("exec_command failed: %s %s", code, msg)
	}
	output := drainShellOutput(t, controller, resp, code, msg)
	if strings.Count(output, "head\n") != 1 || strings.Count(output, "tail\n") != 1 {
		t.Fatalf("expected trailing output once, got %q", output)
	}
}

func TestShellControllerDebugSnapshotKeepsTranscriptAfterDrain(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewShellController()
	defer closeController(controller)

	resp, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{
		Command:     "printf 'first\n'; sleep 0.2; printf 'second\n'",
		YieldTimeMs: 100,
		TimeoutMs:   5000,
	})
	output := drainShellOutput(t, controller, resp, code, msg)
	if !strings.Contains(output, "first") || !strings.Contains(output, "second") {
		t.Fatalf("unexpected drained output: %q", output)
	}

	debug, code, msg := controller.DebugSnapshot()
	if code != "" {
		t.Fatalf("debug snapshot failed: %s %s", code, msg)
	}
	if debug.PendingOutputBytes != 0 {
		t.Fatalf("expected no pending output, got %d", debug.PendingOutputBytes)
	}
	if debug.Transcript.Truncated {
		t.Fatalf("unexpected transcript truncation: %#v", debug.Transcript)
	}
	if !strings.Contains(debug.Transcript.Text, "first") || !strings.Contains(debug.Transcript.Text, "second") {
		t.Fatalf("unexpected transcript: %q", debug.Transcript.Text)
	}
	if !strings.Contains(debug.Tail, "second") {
		t.Fatalf("unexpected tail: %q", debug.Tail)
	}
}

func TestShellControllerDebugSnapshotDoesNotConsumePendingOutput(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewShellController()
	defer closeController(controller)

	resp, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{
		Command: fmt.Sprintf(
			"python3 -c \"import sys,time; sys.stdout.write('A'*%d); sys.stdout.flush(); time.sleep(0.2); sys.stdout.write('B'*%d); sys.stdout.flush()\"",
			shellapi.ReadChunkBytes+1024,
			shellapi.ReadChunkBytes+1024,
		),
		YieldTimeMs: 1000,
		TimeoutMs:   5000,
	})
	if code != "" {
		t.Fatalf("exec_command failed: %s %s", code, msg)
	}
	if len(resp.Output) == 0 {
		t.Fatal("expected initial output chunk")
	}
	time.Sleep(350 * time.Millisecond)

	debug, code, msg := controller.DebugSnapshot()
	if code != "" {
		t.Fatalf("debug snapshot failed: %s %s", code, msg)
	}
	if debug.PendingOutputBytes == 0 {
		t.Fatalf("expected pending output after first chunk, got %#v", debug)
	}

	poll, code, msg := controller.WriteStdin(shellapi.AgentWriteStdinRequest{YieldTimeMs: 100})
	if code != "" {
		t.Fatalf("poll failed: %s %s", code, msg)
	}
	if poll.Output == "" {
		t.Fatalf("expected pending output to remain after debug snapshot, got %#v", poll)
	}
}

func TestShellControllerDebugSnapshotShowsTranscriptTruncation(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewShellController()
	defer closeController(controller)

	command := fmt.Sprintf(
		"python3 -c \"import sys; sys.stdout.write('HEAD\\n' + 'x'*%d + '\\nTAIL')\"",
		shellapi.RingBufferBytes+shellapi.TranscriptTailBytes,
	)
	resp, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{
		Command:     command,
		YieldTimeMs: 100,
		TimeoutMs:   10000,
	})
	if code != "" {
		t.Fatalf("exec_command failed: %s %s", code, msg)
	}
	if !resp.Running {
		t.Fatalf("expected command to keep running, got %#v", resp)
	}
	var debug *shellapi.AgentDebugResponse
	sawPendingTruncated := false
	deadline := time.Now().Add(5 * time.Second)
	for {
		debug, code, msg = controller.DebugSnapshot()
		if code != "" {
			t.Fatalf("debug snapshot failed: %s %s", code, msg)
		}
		if debug.PendingOutputTruncated {
			sawPendingTruncated = true
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !sawPendingTruncated {
		t.Fatalf("expected pending truncation flag, got %#v", debug)
	}
	sawPollTruncated := false
	for resp.Running {
		resp, code, msg = controller.WriteStdin(shellapi.AgentWriteStdinRequest{YieldTimeMs: 200})
		if code != "" {
			t.Fatalf("poll failed: %s %s", code, msg)
		}
		if resp.Truncated {
			sawPollTruncated = true
		}
	}
	if !sawPollTruncated {
		t.Fatalf("expected truncated poll while draining output")
	}
	debug, code, msg = controller.DebugSnapshot()
	if code != "" {
		t.Fatalf("debug snapshot failed: %s %s", code, msg)
	}
	if !debug.Transcript.Truncated || debug.Transcript.OmittedBytes <= 0 {
		t.Fatalf("expected transcript truncation, got %#v", debug.Transcript)
	}
	if !strings.HasPrefix(debug.Transcript.Text, "HEAD\n") {
		t.Fatalf("unexpected transcript head: %q", debug.Transcript.Text[:minInt(len(debug.Transcript.Text), 16)])
	}
	if !strings.Contains(debug.Transcript.Text, "TAIL") {
		t.Fatalf("expected transcript tail marker, got %q", debug.Transcript.Text)
	}
	if strings.Contains(debug.Tail, "HEAD\n") {
		t.Fatalf("tail should not keep old head, got %q", debug.Tail)
	}
	if !strings.Contains(debug.Tail, "TAIL") {
		t.Fatalf("expected tail marker in tail window, got %q", debug.Tail)
	}
}

func TestShellControllerCaptureStatePreservesShellView(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewShellController()
	defer closeController(controller)

	command := "mkdir -p demo && cd demo && export FOO=bar && touch a.txt && pwd"
	resp, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{Command: command, YieldTimeMs: 1000, TimeoutMs: 5000})
	_ = drainShellOutput(t, controller, resp, code, msg)
	state, code, msg := controller.CaptureState()
	if code != "" || msg != "" {
		t.Fatalf("capture state failed: %s %s", code, msg)
	}
	if state.Cwd != filepath.Join(workspace, "demo") {
		t.Fatalf("unexpected state cwd: %s", state.Cwd)
	}
	verify, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{
		Cwd:         state.Cwd,
		Env:         state.Env,
		Command:     "printf '%s\n' \"$FOO\" && pwd && test -f a.txt && echo ok",
		YieldTimeMs: 1000,
		TimeoutMs:   5000,
	})
	verifyOutput := drainShellOutput(t, controller, verify, code, msg)
	if !strings.Contains(verifyOutput, "bar") || !strings.Contains(verifyOutput, filepath.Join(workspace, "demo")) || !strings.Contains(verifyOutput, "ok") {
		t.Fatalf("unexpected restored output: %q", verifyOutput)
	}
}

func TestShellControllerExecCommandWithEnvKeepsFixedHome(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewShellController()
	defer closeController(controller)

	resp, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{
		Env:         map[string]string{"HOME": "/tmp/evil", "FOO": "bar"},
		Command:     "printf '%s|%s' \"$HOME\" \"$FOO\"",
		YieldTimeMs: 1000,
		TimeoutMs:   5000,
	})
	output := drainShellOutput(t, controller, resp, code, msg)
	if !strings.Contains(output, shellHomeDir+"|bar") {
		t.Fatalf("unexpected env output: %q", output)
	}
}

func TestShellControllerCaptureStateReturnsBusyWhileRunning(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewShellController()
	defer closeController(controller)

	resp, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{Command: "sleep 1", YieldTimeMs: 10, TimeoutMs: 5000})
	if code != "" || msg != "" {
		t.Fatalf("exec command failed: %s %s", code, msg)
	}
	if resp == nil || !resp.Running {
		t.Fatalf("expected running response, got %#v", resp)
	}

	_, code, msg = controller.CaptureState()
	if code != shellapi.CodeSessionBusy {
		t.Fatalf("expected busy code, got %q (%s)", code, msg)
	}
}
