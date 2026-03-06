package main

import (
	"archive/tar"
	"bytes"
	"encoding/base64"
	"io"
	"path/filepath"
	"strings"
	"testing"

	shellapi "arkloop/services/sandbox/internal/shell"

	"github.com/klauspost/compress/zstd"
)

func bindShellDirs(t *testing.T, workspace string) {
	t.Helper()
	home := filepath.Join(workspace, "home")
	temp := filepath.Join(workspace, "tmp")
	shellWorkspaceDir = workspace
	shellHomeDir = home
	shellTempDir = temp
	t.Cleanup(func() {
		shellWorkspaceDir = defaultShellCwd
		shellHomeDir = defaultShellHome
		shellTempDir = defaultShellTempDir
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
	controller := NewShellController()
	defer closeController(controller)

	resp, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{Command: "cd /tmp && pwd", YieldTimeMs: 1000, TimeoutMs: 5000})
	if code != "" {
		t.Fatalf("exec_command failed: %s %s", code, msg)
	}
	if resp.Cwd != "/tmp" {
		t.Fatalf("expected cwd /tmp, got %s", resp.Cwd)
	}

	resp, code, msg = controller.ExecCommand(shellapi.AgentExecCommandRequest{Command: "pwd", YieldTimeMs: 1000, TimeoutMs: 5000})
	if code != "" {
		t.Fatalf("exec_command failed: %s %s", code, msg)
	}
	if !strings.Contains(resp.Output, "/tmp") {
		t.Fatalf("expected output to contain /tmp, got %q", resp.Output)
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
	if again.Output != "" {
		t.Fatalf("expected drained output, got %q", again.Output)
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

func TestShellControllerCheckpointRestorePreservesState(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewShellController()
	defer closeController(controller)

	command := "mkdir -p demo && cd demo && export FOO=bar && touch a.txt && pwd"
	resp, code, msg := controller.ExecCommand(shellapi.AgentExecCommandRequest{Command: command, YieldTimeMs: 1000, TimeoutMs: 5000})
	_ = drainShellOutput(t, controller, resp, code, msg)
	checkpoint, code, msg := controller.CheckpointExport()
	if code != "" || msg != "" {
		t.Fatalf("checkpoint failed: %s %s", code, msg)
	}
	if checkpoint.Cwd != filepath.Join(workspace, "demo") {
		t.Fatalf("unexpected checkpoint cwd: %s", checkpoint.Cwd)
	}

	restored := NewShellController()
	defer closeController(restored)
	if _, code, msg := restored.RestoreImport(shellapi.AgentCheckpointRequest{Archive: checkpoint.Archive}); code != "" || msg != "" {
		t.Fatalf("restore import failed: %s %s", code, msg)
	}
	verify, code, msg := restored.ExecCommand(shellapi.AgentExecCommandRequest{
		Cwd:         checkpoint.Cwd,
		Env:         checkpoint.Env,
		Command:     "printf '%s\n' \"$FOO\" && pwd && test -f a.txt && echo ok",
		YieldTimeMs: 1000,
		TimeoutMs:   5000,
	})
	verifyOutput := drainShellOutput(t, restored, verify, code, msg)
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

func TestShellControllerRestoreRejectsEscapingSymlink(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewShellController()
	defer closeController(controller)
	archive := base64.StdEncoding.EncodeToString(mustCheckpointArchive(t, func(tw *tar.Writer) {
		writeTarHeader(t, tw, &tar.Header{Name: "workspace/", Typeflag: tar.TypeDir, Mode: 0o755})
		writeTarHeader(t, tw, &tar.Header{Name: "workspace/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777})
	}))
	_, code, msg := controller.RestoreImport(shellapi.AgentCheckpointRequest{Archive: archive})
	if code != "" {
		t.Fatalf("unexpected shell code: %s", code)
	}
	if !strings.Contains(msg, "escapes root") {
		t.Fatalf("unexpected restore error: %s", msg)
	}
}

func mustCheckpointArchive(t *testing.T, fill func(tw *tar.Writer)) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zw, err := zstd.NewWriter(&buffer)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	tw := tar.NewWriter(zw)
	fill(tw)
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zstd: %v", err)
	}
	return buffer.Bytes()
}

func writeTarHeader(t *testing.T, tw *tar.Writer, header *tar.Header) {
	t.Helper()
	if err := tw.WriteHeader(header); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
		if _, err := io.WriteString(tw, "payload"); err != nil {
			t.Fatalf("write tar payload: %v", err)
		}
	}
}
