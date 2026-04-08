package fileops

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SandboxExecBackend performs file operations by executing shell commands
// through the buffered process API inside the sandbox session.
type SandboxExecBackend struct {
	baseURL      string
	authToken    string
	sessionID    string
	accountID    string
	profileRef   string
	workspaceRef string
	client       *http.Client
}

type sandboxProcessExecRequest struct {
	SessionID    string `json:"session_id"`
	AccountID    string `json:"account_id,omitempty"`
	ProfileRef   string `json:"profile_ref,omitempty"`
	WorkspaceRef string `json:"workspace_ref,omitempty"`
	Command      string `json:"command"`
	Mode         string `json:"mode,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
	TimeoutMs    int    `json:"timeout_ms,omitempty"`
	Tier         string `json:"tier,omitempty"`
}

type sandboxProcessExecResponse struct {
	Status   string `json:"status"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

var (
	ansiCSIRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)
	ansiOSCRe = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
	ansiTwoRe = regexp.MustCompile(`\x1b[^[\]]`)
)

func stripANSI(s string) string {
	s = ansiOSCRe.ReplaceAllString(s, "")
	s = ansiCSIRe.ReplaceAllString(s, "")
	s = ansiTwoRe.ReplaceAllString(s, "")
	return s
}

func (b *SandboxExecBackend) httpClient() *http.Client {
	if b.client != nil {
		return b.client
	}
	return &http.Client{}
}

func (b *SandboxExecBackend) NormalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func (b *SandboxExecBackend) exec(ctx context.Context, command string, timeoutMs int) (string, string, int, error) {
	if timeoutMs == 0 {
		timeoutMs = 30_000
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs+5000)*time.Millisecond)
	defer cancel()

	payload, err := json.Marshal(sandboxProcessExecRequest{
		SessionID:    b.sessionID,
		AccountID:    b.accountID,
		ProfileRef:   b.profileRef,
		WorkspaceRef: b.workspaceRef,
		Command:      command,
		Mode:         "buffered",
		TimeoutMs:    timeoutMs,
		Tier:         "lite",
	})
	if err != nil {
		return "", "", -1, fmt.Errorf("marshal sandbox request: %w", err)
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, b.baseURL+"/v1/process/exec", bytes.NewReader(payload))
	if err != nil {
		return "", "", -1, fmt.Errorf("build sandbox request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if b.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+b.authToken)
	}
	if b.accountID != "" {
		req.Header.Set("X-Account-ID", b.accountID)
	}

	resp, err := b.httpClient().Do(req)
	if err != nil {
		return "", "", -1, fmt.Errorf("sandbox request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", -1, fmt.Errorf("read sandbox response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", -1, fmt.Errorf("sandbox returned %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxProcessExecResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", -1, fmt.Errorf("decode sandbox response: %w", err)
	}
	stdout := stripANSI(result.Stdout)
	stderr := stripANSI(result.Stderr)
	if result.Status != "" && result.Status != "exited" {
		return stdout, stderr, 0, fmt.Errorf("sandbox command ended with status %s", result.Status)
	}

	exitCode := 0
	if result.ExitCode != nil {
		exitCode = *result.ExitCode
	}
	return stdout, stderr, exitCode, nil
}

func (b *SandboxExecBackend) ReadFile(ctx context.Context, path string) ([]byte, error) {
	output, _, _, err := b.exec(ctx, fmt.Sprintf("cat %s", shellQuote(path)), 30_000)
	if err != nil {
		return nil, err
	}
	// PTY uses \r\n; normalize to \n so string comparisons in edit work correctly.
	output = strings.ReplaceAll(output, "\r\n", "\n")
	return []byte(output), nil
}

func (b *SandboxExecBackend) WriteFile(ctx context.Context, path string, data []byte) error {
	encoded := base64.StdEncoding.EncodeToString(data)
	dir := filepath.Dir(path)
	cmd := fmt.Sprintf("mkdir -p %s && printf '%%s' %s | base64 -d > %s",
		shellQuote(dir), shellQuote(encoded), shellQuote(path))
	_, stderr, exitCode, err := b.exec(ctx, cmd, 30_000)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		if strings.TrimSpace(stderr) != "" {
			return fmt.Errorf("write command exited %d: %s", exitCode, strings.TrimSpace(stderr))
		}
		return fmt.Errorf("write command exited %d", exitCode)
	}
	return nil
}

func (b *SandboxExecBackend) Stat(ctx context.Context, path string) (FileInfo, error) {
	// GNU stat only; sandbox is always Linux.
	// Use | as separator to handle multi-word %F values like "regular file", "symbolic link".
	output, stderr, exitCode, err := b.exec(ctx,
		fmt.Sprintf("stat -c '%%s|%%F|%%Y' %s 2>/dev/null; echo $?", shellQuote(path)),
		10_000)
	if err != nil {
		return FileInfo{}, err
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	// last line is the exit code of stat
	if len(lines) == 0 {
		return FileInfo{}, fmt.Errorf("unexpected empty stat output")
	}
	statusLine := strings.TrimSpace(lines[len(lines)-1])
	if statusLine == "1" {
		return FileInfo{}, os.ErrNotExist
	}
	if exitCode != 0 {
		if strings.TrimSpace(stderr) != "" {
			return FileInfo{}, fmt.Errorf("stat command exited %d: %s", exitCode, strings.TrimSpace(stderr))
		}
		return FileInfo{}, fmt.Errorf("stat command exited %d", exitCode)
	}
	if len(lines) < 2 {
		return FileInfo{}, fmt.Errorf("unexpected stat output: %q", output)
	}
	return parseStat(strings.TrimSpace(lines[0]))
}

func (b *SandboxExecBackend) Exec(ctx context.Context, command string) (string, string, int, error) {
	stdout, stderr, exitCode, err := b.exec(ctx, command, 60_000)
	if err != nil {
		return "", "", -1, err
	}
	return stdout, stderr, exitCode, nil
}

func parseStat(line string) (FileInfo, error) {
	// line format: "size|file type|epoch" (pipe-separated to handle multi-word %F values)
	parts := strings.SplitN(line, "|", 3)
	if len(parts) < 3 {
		return FileInfo{}, fmt.Errorf("unexpected stat output: %q", line)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return FileInfo{}, fmt.Errorf("parse size: %w", err)
	}
	isDir := strings.Contains(strings.ToLower(parts[1]), "directory")
	epoch, err := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
	if err != nil {
		return FileInfo{}, fmt.Errorf("parse mtime: %w", err)
	}
	return FileInfo{
		Size:    size,
		IsDir:   isDir,
		ModTime: time.Unix(epoch, 0),
	}, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
