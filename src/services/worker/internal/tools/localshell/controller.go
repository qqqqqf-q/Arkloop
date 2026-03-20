//go:build desktop

package localshell

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

const (
	statusIdle    = "idle"
	statusRunning = "running"
	statusClosed  = "closed"

	defaultYieldTimeMs    = 1000
	maxYieldTimeMs        = 30000
	defaultTimeoutMs      = 30000
	maxTimeoutMs          = 300000
	defaultControlTimeout = 5000

	ringBufferSize   = 1 << 20 // 1 MiB
	readChunkSize    = 64 * 1024
	quietWindow      = 100 * time.Millisecond
	trailingGrace    = 100 * time.Millisecond
	timeoutKillDelay = 2 * time.Second
)

type shellResponse struct {
	Status   string `json:"status"`
	Cwd      string `json:"cwd"`
	Output   string `json:"output"`
	Running  bool   `json:"running"`
	TimedOut bool   `json:"timed_out"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

type shellController struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	ptyFile  *os.File
	buf      []byte
	cursor   uint64
	endPos   uint64
	status   string
	cwd      string
	current  *pendingCommand
	lastExit *int
	lastTO   bool
	updateCh chan struct{}
	workDir  string
}

type pendingCommand struct {
	token       string
	beginMarker string
	endPrefix   string
	raw         string
	startSeen   bool
	suppress    bool
	timer       *time.Timer
	timedOut    bool
}

func newShellController(workDir string) *shellController {
	return &shellController{
		status:   statusClosed,
		cwd:      workDir,
		workDir:  workDir,
		buf:      make([]byte, 0, 4096),
		updateCh: make(chan struct{}),
	}
}

func (c *shellController) execCommand(command, cwd string, timeoutMs int) (*shellResponse, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, errors.New("command is required")
	}
	if err := c.ensureStarted(); err != nil {
		return nil, err
	}
	tm := normalizeTimeoutMs(timeoutMs)
	if err := c.startCommand(command, cwd, tm, false); err != nil {
		return nil, err
	}
	first := c.waitForDelivery(defaultYieldTimeMs)
	return c.drainUntilIdle(first, tm), nil
}

// drainUntilIdle extends the first exec yield with write_stdin polls (same idea as sandbox
// pollExecUntilOutputOrDone) so a single exec_command tool result usually ends with running=false,
// including after timeout kills the PTY.
func (c *shellController) drainUntilIdle(first *shellResponse, commandTimeoutMs int) *shellResponse {
	if first == nil || !first.Running {
		return first
	}
	var out strings.Builder
	out.WriteString(first.Output)
	last := first
	pollYield := normalizeYieldTimeMs(defaultYieldTimeMs)
	if pollYield < 2000 {
		pollYield = 2000
	}
	deadline := time.Now().Add(time.Duration(commandTimeoutMs)*time.Millisecond + timeoutKillDelay + 2*time.Second)

	for last.Running {
		if time.Now().After(deadline) {
			break
		}
		next, err := c.writeStdin("", pollYield)
		if err != nil {
			return c.mergeClosedShellResponse(&out)
		}
		out.WriteString(next.Output)
		last = next
	}
	last.Output = out.String()
	if last.Running {
		// Past overall deadline; avoid returning running=true if the PTY wedged.
		last.Running = false
		if last.ExitCode == nil {
			x := 1
			last.ExitCode = &x
		}
	}
	return last
}

func (c *shellController) mergeClosedShellResponse(out *strings.Builder) *shellResponse {
	c.mu.Lock()
	if int(c.cursor) < len(c.buf) {
		out.WriteString(string(c.buf[c.cursor:]))
		c.cursor = uint64(len(c.buf))
	}
	timedOut := c.lastTO
	var exitCode *int
	if c.lastExit != nil {
		exitCode = c.lastExit
	} else if timedOut {
		x := 124
		exitCode = &x
	} else {
		x := 1
		exitCode = &x
	}
	cwd := c.cwd
	c.mu.Unlock()

	return &shellResponse{
		Status:   statusIdle,
		Cwd:      cwd,
		Output:   out.String(),
		Running:  false,
		TimedOut: timedOut,
		ExitCode: exitCode,
	}
}

func (c *shellController) writeStdin(chars string, yieldTimeMs int) (*shellResponse, error) {
	c.mu.Lock()
	if c.status == statusClosed || c.cmd == nil || c.ptyFile == nil {
		c.mu.Unlock()
		return nil, errors.New("shell session not found")
	}
	c.mu.Unlock()

	if chars != "" {
		c.mu.Lock()
		if c.status != statusRunning {
			c.mu.Unlock()
			return nil, errors.New("shell session is not running")
		}
		if _, err := io.WriteString(c.ptyFile, chars); err != nil {
			c.mu.Unlock()
			return nil, err
		}
		c.mu.Unlock()
	}
	return c.waitForDelivery(normalizeYieldTimeMs(yieldTimeMs)), nil
}

func (c *shellController) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
}

func (c *shellController) ensureStarted() error {
	c.mu.Lock()
	if c.status != statusClosed && c.cmd != nil && c.ptyFile != nil {
		c.mu.Unlock()
		return nil
	}

	shellPath, args := resolveShell()
	cmd := exec.Command(shellPath, args...)
	cmd.Dir = c.workDir
	cmd.Env = buildLocalShellEnv(c.workDir)

	file, err := pty.Start(cmd)
	if err != nil {
		c.mu.Unlock()
		return err
	}
	if _, err := term.MakeRaw(int(file.Fd())); err != nil {
		_ = file.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		c.mu.Unlock()
		return err
	}

	c.cmd = cmd
	c.ptyFile = file
	c.buf = c.buf[:0]
	c.cursor = 0
	c.endPos = 0
	c.status = statusIdle
	c.cwd = c.workDir
	c.lastExit = nil
	c.lastTO = false
	c.current = nil
	c.notify()
	go c.readLoop(file)
	c.mu.Unlock()

	initCmd := "export PS1='' PROMPT_COMMAND=''" +
		" BASH_SILENCE_DEPRECATION_WARNING=1" +
		" TERM='xterm-256color'" +
		" LANG='en_US.UTF-8'\nstty -echo\n"
	_, err = c.runControlCommand(initCmd, c.workDir, defaultControlTimeout)
	return err
}

func (c *shellController) runControlCommand(command, cwd string, timeoutMs int) (*shellResponse, error) {
	if err := c.startCommand(command, cwd, timeoutMs, true); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for {
		c.mu.Lock()
		resp := c.snapshotLocked()
		if !resp.Running {
			c.mu.Unlock()
			return resp, nil
		}
		ch := c.updateCh
		c.mu.Unlock()
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, errors.New("control command timed out")
		}
		select {
		case <-ch:
		case <-time.After(remaining):
		}
	}
}

func (c *shellController) startCommand(command, cwd string, timeoutMs int, suppress bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status == statusClosed || c.cmd == nil || c.ptyFile == nil {
		return errors.New("shell session not found")
	}
	if c.status == statusRunning {
		return errors.New("shell session is busy")
	}

	token, err := newToken()
	if err != nil {
		return err
	}

	c.status = statusRunning
	c.lastExit = nil
	c.lastTO = false
	// Reset pending buffer for new command
	c.buf = c.buf[:0]
	c.cursor = 0
	c.endPos = 0

	current := &pendingCommand{
		token:       token,
		beginMarker: "__ARK_BEGIN__" + token,
		endPrefix:   "__ARK_END__" + token + "__RC=",
		suppress:    suppress,
	}
	if timeoutMs > 0 {
		current.timer = time.AfterFunc(time.Duration(timeoutMs)*time.Millisecond, func() {
			c.handleTimeout(token)
		})
	}
	c.current = current

	wrapped := buildWrappedCommand(token, cwd, command)
	if _, err := io.WriteString(c.ptyFile, wrapped); err != nil {
		if current.timer != nil {
			current.timer.Stop()
		}
		c.current = nil
		c.status = statusIdle
		return err
	}
	c.notify()
	return nil
}

func (c *shellController) waitForDelivery(yieldTimeMs int) *shellResponse {
	deadline := time.Now().Add(time.Duration(normalizeYieldTimeMs(yieldTimeMs)) * time.Millisecond)
	c.mu.Lock()
	resp := c.snapshotLocked()
	ch := c.updateCh
	shouldWait := resp.Running && resp.Output == ""
	if !shouldWait {
		c.advanceCursorLocked()
		c.mu.Unlock()
		return resp
	}
	c.mu.Unlock()

	var lastOutputAt time.Time
	var exitObservedAt time.Time
	for {
		waitFor := time.Until(deadline)
		if !exitObservedAt.IsZero() {
			graceUntil := exitObservedAt.Add(trailingGrace)
			if !lastOutputAt.IsZero() && lastOutputAt.After(exitObservedAt) {
				graceUntil = lastOutputAt.Add(trailingGrace)
			}
			remaining := time.Until(graceUntil)
			if remaining <= 0 {
				return c.finishDelivery()
			}
			if remaining < waitFor {
				waitFor = remaining
			}
		} else if !lastOutputAt.IsZero() {
			quietUntil := lastOutputAt.Add(quietWindow)
			remaining := time.Until(quietUntil)
			if remaining <= 0 {
				return c.finishDelivery()
			}
			if remaining < waitFor {
				waitFor = remaining
			}
		}
		if waitFor <= 0 {
			return c.finishDelivery()
		}
		select {
		case <-ch:
			now := time.Now()
			c.mu.Lock()
			resp = c.snapshotLocked()
			if resp.Output != "" {
				lastOutputAt = now
			}
			if !resp.Running || resp.Status == statusClosed {
				exitObservedAt = now
			}
			ch = c.updateCh
			c.mu.Unlock()
		case <-time.After(waitFor):
			return c.finishDelivery()
		}
	}
}

func (c *shellController) finishDelivery() *shellResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	resp := c.snapshotLocked()
	c.advanceCursorLocked()
	return resp
}

func (c *shellController) snapshotLocked() *shellResponse {
	output := ""
	if int(c.cursor) < len(c.buf) {
		end := len(c.buf)
		if end-int(c.cursor) > readChunkSize {
			end = int(c.cursor) + readChunkSize
		}
		output = string(c.buf[c.cursor:end])
	}
	return &shellResponse{
		Status:   c.status,
		Cwd:      c.cwd,
		Output:   output,
		Running:  c.status == statusRunning,
		TimedOut: c.lastTO,
		ExitCode: c.lastExit,
	}
}

func (c *shellController) advanceCursorLocked() {
	c.cursor = uint64(len(c.buf))
}

func (c *shellController) readLoop(file *os.File) {
	readBuf := make([]byte, 4096)
	for {
		n, err := file.Read(readBuf)
		if n > 0 {
			c.mu.Lock()
			if c.current != nil {
				c.current.raw += string(readBuf[:n])
				c.processCurrentLocked()
			} else {
				c.appendOutputLocked(string(readBuf[:n]), false)
			}
			c.notify()
			c.mu.Unlock()
		}
		if err != nil {
			c.mu.Lock()
			c.status = statusClosed
			if c.current != nil && c.current.timer != nil {
				c.current.timer.Stop()
			}
			c.current = nil
			c.cmd = nil
			c.ptyFile = nil
			c.notify()
			c.mu.Unlock()
			return
		}
	}
}

func (c *shellController) processCurrentLocked() {
	current := c.current
	if current == nil {
		return
	}
	raw := current.raw

	if !current.startSeen {
		idx := strings.Index(raw, current.beginMarker)
		if idx == -1 {
			keep := trailingMarkerPrefixLen(raw, current.beginMarker)
			if keep == 0 {
				current.raw = ""
				return
			}
			current.raw = raw[len(raw)-keep:]
			return
		}
		raw = raw[idx+len(current.beginMarker):]
		if strings.HasPrefix(raw, "\r\n") {
			raw = raw[2:]
		} else if strings.HasPrefix(raw, "\n") {
			raw = raw[1:]
		}
		current.startSeen = true
	}

	idx := strings.Index(raw, current.endPrefix)
	if idx == -1 {
		keep := trailingMarkerPrefixLen(raw, current.endPrefix)
		safeLen := len(raw) - keep
		c.appendOutputLocked(raw[:safeLen], current.suppress)
		current.raw = raw[safeLen:]
		return
	}

	c.appendOutputLocked(raw[:idx], current.suppress)
	rest := raw[idx:]
	lineEnd := strings.IndexByte(rest, '\n')
	if lineEnd == -1 {
		current.raw = rest
		return
	}

	line := strings.TrimRight(rest[:lineEnd], "\r")
	c.finishCommandLocked(current, line)
	trailing := rest[lineEnd+1:]
	c.current = nil
	if trailing != "" {
		c.appendOutputLocked(trailing, false)
	}
}

func (c *shellController) finishCommandLocked(current *pendingCommand, line string) {
	if current.timer != nil {
		current.timer.Stop()
	}
	if !strings.HasPrefix(line, current.endPrefix) {
		c.status = statusIdle
		return
	}
	rest := strings.TrimPrefix(line, current.endPrefix)
	parts := strings.SplitN(rest, "__PWD=", 2)
	rc, err := strconv.Atoi(parts[0])
	if err != nil {
		rc = 1
	}
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		c.cwd = parts[1]
	}
	c.lastExit = &rc
	c.lastTO = current.timedOut
	c.status = statusIdle
	current.raw = ""
}

func (c *shellController) appendOutputLocked(data string, suppress bool) {
	if suppress || data == "" {
		return
	}
	// Enforce max buffer size
	c.buf = append(c.buf, []byte(data)...)
	if len(c.buf) > ringBufferSize {
		trim := len(c.buf) - ringBufferSize
		c.buf = append([]byte(nil), c.buf[trim:]...)
		if c.cursor > uint64(trim) {
			c.cursor -= uint64(trim)
		} else {
			c.cursor = 0
		}
	}
}

func (c *shellController) handleTimeout(token string) {
	c.mu.Lock()
	if c.current == nil || c.current.token != token || c.status != statusRunning {
		c.mu.Unlock()
		return
	}
	c.current.timedOut = true
	c.lastTO = true
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Signal(os.Interrupt)
	}
	c.notify()
	c.mu.Unlock()

	time.AfterFunc(timeoutKillDelay, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.current == nil || c.current.token != token || c.status != statusRunning {
			return
		}
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	})
}

func (c *shellController) closeLocked() {
	if c.current != nil && c.current.timer != nil {
		c.current.timer.Stop()
	}
	if c.ptyFile != nil {
		_ = c.ptyFile.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
	}
	c.cmd = nil
	c.ptyFile = nil
	c.current = nil
	c.status = statusClosed
	c.notify()
}

func (c *shellController) notify() {
	close(c.updateCh)
	c.updateCh = make(chan struct{})
}

// --- helpers ---

func resolveShell() (string, []string) {
	candidates := []string{os.Getenv("SHELL"), "/bin/zsh", "/bin/bash", "/bin/sh"}
	for _, shell := range candidates {
		if shell == "" {
			continue
		}
		if _, err := os.Stat(shell); err != nil {
			continue
		}
		return shell, shellArgs(shell)
	}
	return "/bin/sh", []string{"-i"}
}

func shellArgs(path string) []string {
	base := filepath.Base(path)
	switch {
	case strings.Contains(base, "zsh"):
		return []string{"--no-rcs", "--no-globalrcs", "-i"}
	case strings.Contains(base, "bash"):
		return []string{"--noprofile", "--norc", "-i"}
	default:
		return []string{"-i"}
	}
}

func buildLocalShellEnv(workDir string) []string {
	env := os.Environ()
	overrides := map[string]string{
		"TERM": "xterm-256color",
		"LANG": "en_US.UTF-8",
		"HOME": os.Getenv("HOME"),
	}
	if workDir != "" {
		overrides["PWD"] = workDir
	}
	result := make([]string, 0, len(env)+len(overrides))
	seen := make(map[string]bool)
	for _, entry := range env {
		key := entry
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			key = entry[:idx]
		}
		if val, ok := overrides[key]; ok {
			result = append(result, key+"="+val)
			seen[key] = true
		} else {
			result = append(result, entry)
		}
	}
	for key, val := range overrides {
		if !seen[key] {
			result = append(result, key+"="+val)
		}
	}
	return result
}

func buildWrappedCommand(token, cwd, command string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(command))
	var b strings.Builder
	b.WriteString("ark_mark_a='__ARK'\n")
	b.WriteString("printf '%s%s")
	b.WriteString(token)
	b.WriteString("\\n' \"$ark_mark_a\" '_BEGIN__'; ")
	b.WriteString("ark_rc=0; ")
	if strings.TrimSpace(cwd) != "" {
		b.WriteString("cd -- ")
		b.WriteString(shellQuote(cwd))
		b.WriteString(" || ark_rc=$?; ")
	}
	b.WriteString("if [ \"$ark_rc\" -eq 0 ]; then ")
	b.WriteString("ark_cmd_b64='")
	b.WriteString(encoded)
	b.WriteString("'; ")
	b.WriteString("ark_cmd_file=$(mktemp); ")
	// Sourced user script runs with stdin /dev/null so cat/read do not block on the PTY.
	b.WriteString("if printf '%s' \"$ark_cmd_b64\" | base64 -d > \"$ark_cmd_file\"; then . \"$ark_cmd_file\" < /dev/null; ark_rc=$?; else ark_rc=1; fi; ")
	b.WriteString("rm -f \"$ark_cmd_file\"; fi; ")
	b.WriteString("printf '\\n%s%s")
	b.WriteString(token)
	b.WriteString("__RC=%s__PWD=%s\\n' \"$ark_mark_a\" '_END__' \"$ark_rc\" \"$PWD\"\n")
	return b.String()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func newToken() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func trailingMarkerPrefixLen(text, marker string) int {
	limit := len(text)
	if len(marker) < limit {
		limit = len(marker)
	}
	for size := limit; size > 0; size-- {
		if strings.HasPrefix(marker, text[len(text)-size:]) {
			return size
		}
	}
	return 0
}

func normalizeTimeoutMs(v int) int {
	if v <= 0 {
		return defaultTimeoutMs
	}
	if v > maxTimeoutMs {
		return maxTimeoutMs
	}
	return v
}

func normalizeYieldTimeMs(v int) int {
	if v <= 0 {
		return defaultYieldTimeMs
	}
	if v > maxYieldTimeMs {
		return maxYieldTimeMs
	}
	return v
}

func init() {
	_ = slog.Default() // ensure slog is available
}
