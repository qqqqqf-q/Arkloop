package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	shellapi "arkloop/services/sandbox/internal/shell"
)

func (c *ShellController) CaptureState() (*shellapi.AgentStateResponse, string, string) {
	c.mu.Lock()
	if c.status == shellapi.StatusClosed || c.cmd == nil {
		c.mu.Unlock()
		return nil, shellapi.CodeSessionNotFound, "shell session not found"
	}
	if c.status == shellapi.StatusRunning {
		c.mu.Unlock()
		return nil, shellapi.CodeSessionBusy, "shell session is busy"
	}
	c.mu.Unlock()

	if _, err := c.runControlCommand("history -a >/dev/null 2>&1 || true", c.cwd, defaultControlTimeout); err != nil {
		return nil, "", err.Error()
	}
	cwd, env, err := c.captureShellState()
	if err != nil {
		return nil, "", err.Error()
	}
	return &shellapi.AgentStateResponse{Cwd: cwd, Env: env}, "", ""
}

func (c *ShellController) captureShellState() (string, map[string]string, error) {
	if cwd, env, err := c.captureShellStateFromProc(); err == nil {
		return cwd, env, nil
	}
	return c.captureShellStateFromFiles()
}

func (c *ShellController) captureShellStateFromProc() (string, map[string]string, error) {
	if runtime.GOOS != "linux" {
		return "", nil, fmt.Errorf("proc not available")
	}
	c.mu.Lock()
	pid := 0
	fallbackCwd := c.cwd
	if c.cmd != nil && c.cmd.Process != nil {
		pid = c.cmd.Process.Pid
	}
	c.mu.Unlock()
	if pid == 0 {
		return "", nil, fmt.Errorf("shell pid not available")
	}
	cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	if err != nil {
		return "", nil, fmt.Errorf("read proc cwd: %w", err)
	}
	envRaw, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return "", nil, fmt.Errorf("read proc environ: %w", err)
	}
	env := parseEnvSnapshot(envRaw)
	if cwd == "" {
		cwd = fallbackCwd
	}
	return cwd, env, nil
}

func (c *ShellController) captureShellStateFromFiles() (string, map[string]string, error) {
	tempDir, err := os.MkdirTemp(shellTempDir, "shell-state-")
	if err != nil {
		return "", nil, fmt.Errorf("create shell state temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	cwdPath := filepath.Join(tempDir, "cwd")
	envPath := filepath.Join(tempDir, "env")
	command := "pwd > " + shellQuote(cwdPath) + " && env -0 > " + shellQuote(envPath)
	if _, err := c.runControlCommand(command, c.cwd, defaultControlTimeout); err != nil {
		return "", nil, err
	}
	cwdRaw, err := os.ReadFile(cwdPath)
	if err != nil {
		return "", nil, fmt.Errorf("read shell cwd: %w", err)
	}
	envRaw, err := os.ReadFile(envPath)
	if err != nil {
		return "", nil, fmt.Errorf("read shell env: %w", err)
	}
	return strings.TrimSpace(string(cwdRaw)), parseEnvSnapshot(envRaw), nil
}

func parseEnvSnapshot(raw []byte) map[string]string {
	result := make(map[string]string)
	for _, entry := range bytes.Split(raw, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		key, value, ok := bytes.Cut(entry, []byte{'='})
		if !ok || len(key) == 0 {
			continue
		}
		result[string(key)] = string(value)
	}
	return result
}
