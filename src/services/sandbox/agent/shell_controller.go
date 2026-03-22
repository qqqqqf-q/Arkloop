package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	shellapi "arkloop/services/sandbox/internal/shell"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const (
	defaultControlTimeout = 5000
	quietOutputWindow     = 100 * time.Millisecond
	trailingOutputGrace   = 100 * time.Millisecond
	timeoutKillDelay      = 2 * time.Second

	// Output delta limits per Codex design
	MaxOutputDeltaBytes    = 8 * 1024
	MaxOutputDeltasPerCall = 10_000
)

type ShellController struct {
	mu             sync.Mutex
	cmd            *exec.Cmd
	ptyFile        *os.File
	pendingOutput  *shellapi.RingBuffer
	pendingCursor  uint64
	transcript     *shellapi.HeadTailBuffer
	tail           *shellapi.RingBuffer
	status         string
	cwd            string
	current        *shellCommand
	lastExit       *int
	lastTO         bool
	updateCh       chan struct{}
}

type shellCommand struct {
	token       string
	beginMarker string
	endPrefix   string
	raw         string
	startSeen   bool
	suppress    bool
	timer       *time.Timer
	timedOut    bool
}

type deliverySnapshot struct {
	response   *shellapi.AgentSessionResponse
	nextCursor uint64
	endCursor  uint64
}

func NewShellController() *ShellController {
	controller := &ShellController{
		status:   shellapi.StatusClosed,
		cwd:      shellWorkspaceDir,
		updateCh: make(chan struct{}),
	}
	controller.resetBuffersLocked()
	return controller
}

// ReadNewOutput returns all output accumulated since the last call to
// ReadNewOutput, as (stdout, stderr, running). This method is safe to
// call from a separate goroutine while the shell controller is running.
func (c *ShellController) ReadNewOutput() (stdout []byte, stderr []byte, running bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pendingCursor >= c.pendingOutput.EndCursor() {
		return nil, nil, c.status == shellapi.StatusRunning
	}

	// For now, we can't distinguish stdout/stderr in the buffer
	// Return all new output as stdout
	chunk, nextCursor, _, ok := c.pendingOutput.ReadFrom(c.pendingCursor, MaxOutputDeltaBytes)
	if !ok {
		nextCursor = c.pendingOutput.EndCursor()
	}
	c.pendingCursor = nextCursor
	return chunk, nil, c.status == shellapi.StatusRunning
}

func (c *ShellController) ExecCommand(req shellapi.AgentExecCommandRequest) (*shellapi.AgentSessionResponse, string, string) {
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return nil, shellapi.CodeSessionNotFound, "command is required"
	}
	if err := c.ensureStarted(req); err != nil {
		return nil, shellapi.CodeSessionNotFound, err.Error()
	}
	if _, err := c.startCommand(command, req.Cwd, shellapi.NormalizeTimeoutMs(req.TimeoutMs), false); err != nil {
		code, msg := mapShellError(err)
		return nil, code, msg
	}
	return c.waitForDelivery(req.YieldTimeMs)
}

func (c *ShellController) WriteStdin(req shellapi.AgentWriteStdinRequest) (*shellapi.AgentSessionResponse, string, string) {
	if err := c.ensureReadable(); err != nil {
		code, msg := mapShellError(err)
		return nil, code, msg
	}
	if req.Chars != "" {
		if err := c.writeInput(req.Chars); err != nil {
			code, msg := mapShellError(err)
			return nil, code, msg
		}
	}
	return c.waitForDelivery(req.YieldTimeMs)
}

func (c *ShellController) DebugSnapshot() (*shellapi.AgentDebugResponse, string, string) {
	if err := c.ensureReadable(); err != nil {
		code, msg := mapShellError(err)
		return nil, code, msg
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.debugSnapshotLocked(), "", ""
}

func (c *ShellController) resetBuffersLocked() {
	c.pendingOutput = shellapi.NewRingBuffer(shellapi.RingBufferBytes)
	c.pendingCursor = 0
	c.transcript = shellapi.NewHeadTailBuffer(shellapi.TranscriptHeadBytes, shellapi.TranscriptTailBytes)
	c.tail = shellapi.NewRingBuffer(shellapi.TailBufferBytes)
}

func (c *ShellController) ensureStarted(req shellapi.AgentExecCommandRequest) error {
	c.mu.Lock()
	if c.status != shellapi.StatusClosed && c.cmd != nil && c.ptyFile != nil {
		c.mu.Unlock()
		return nil
	}
	if err := ensureShellBaseDirs(); err != nil {
		c.mu.Unlock()
		return err
	}
	shellPath, args := resolveShellCommand()
	cmd := exec.Command(shellPath, args...)
	prepareWorkloadCmd(cmd, shellWorkspaceDir, req.Env)
	cmd.Env = buildShellEnv(req.Env)
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
	c.resetBuffersLocked()
	c.status = shellapi.StatusIdle
	c.cwd = shellWorkspaceDir
	c.lastExit = nil
	c.lastTO = false
	c.current = nil
	c.notifyLocked()
	go c.readLoop(file)
	c.mu.Unlock()

	initCommand := "export PS1='' PROMPT_COMMAND= BASH_SILENCE_DEPRECATION_WARNING=1 HOME=" + shellQuote(shellHomeDir) +
		" PATH=" + shellQuote(defaultWorkloadPath) +
		" LANG=" + shellQuote(defaultWorkloadLang) +
		" TERM='xterm-256color' TMPDIR=" + shellQuote(shellTempDir) +
		" HISTFILE=" + shellQuote(shellHomeDir+"/.bash_history") +
		"\nstty -echo\nmkdir -p /tmp/output " + shellQuote(shellWorkspaceDir) + " " + shellQuote(shellHomeDir) + " " + shellQuote(shellTempDir)
	_, err = c.runControlCommand(initCommand, shellWorkspaceDir, defaultControlTimeout)
	return err
}

func (c *ShellController) runControlCommand(command, cwd string, timeoutMs int) (*shellapi.AgentSessionResponse, error) {
	startCursor, err := c.startCommand(command, cwd, timeoutMs, true)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for {
		c.mu.Lock()
		resp := c.snapshotLocked(startCursor).response
		if !resp.Running {
			c.mu.Unlock()
			return resp, nil
		}
		ch := c.updateCh
		c.mu.Unlock()
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, newShellError(shellapi.CodeSignalFailed, "control command timed out")
		}
		select {
		case <-ch:
		case <-time.After(remaining):
		}
	}
}

func (c *ShellController) startCommand(command, cwd string, timeoutMs int, suppress bool) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status == shellapi.StatusClosed || c.cmd == nil || c.ptyFile == nil {
		return 0, newShellError(shellapi.CodeSessionNotFound, "shell session not found")
	}
	if c.status == shellapi.StatusRunning {
		return 0, newShellError(shellapi.CodeSessionBusy, "shell session is busy")
	}
	token, err := newToken()
	if err != nil {
		return 0, err
	}
	startCursor := c.pendingOutput.EndCursor()
	c.status = shellapi.StatusRunning
	c.lastExit = nil
	c.lastTO = false
	current := &shellCommand{
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
		c.status = shellapi.StatusIdle
		return 0, err
	}
	c.notifyLocked()
	return startCursor, nil
}

func (c *ShellController) waitForDelivery(yieldTimeMs int) (*shellapi.AgentSessionResponse, string, string) {
	deadline := time.Now().Add(time.Duration(shellapi.NormalizeYieldTimeMs(yieldTimeMs)) * time.Millisecond)
	c.mu.Lock()
	snapshot := c.snapshotLocked(c.pendingCursor)
	ch := c.updateCh
	shouldWait := snapshot.response.Running && snapshot.response.Output == ""
	if !shouldWait {
		if snapshot.nextCursor > c.pendingCursor {
			c.pendingCursor = snapshot.nextCursor
		}
		resp := snapshot.response
		c.mu.Unlock()
		return resp, "", ""
	}
	cursor := c.pendingCursor
	observedEnd := snapshot.endCursor
	c.mu.Unlock()
	return c.waitWithCursor(deadline, ch, cursor, observedEnd)
}

func (c *ShellController) waitWithCursor(deadline time.Time, initial <-chan struct{}, cursor, observedEnd uint64) (*shellapi.AgentSessionResponse, string, string) {
	ch := initial
	var lastOutputAt time.Time
	var exitObservedAt time.Time
	commandRunning := true
	for {
		waitFor := time.Until(deadline)
		if !exitObservedAt.IsZero() {
			graceUntil := exitObservedAt.Add(trailingOutputGrace)
			if !lastOutputAt.IsZero() && lastOutputAt.After(exitObservedAt) {
				graceUntil = lastOutputAt.Add(trailingOutputGrace)
			}
			remainingGrace := time.Until(graceUntil)
			if remainingGrace <= 0 {
				return c.finishDelivery(cursor)
			}
			if remainingGrace < waitFor {
				waitFor = remainingGrace
			}
		} else if !lastOutputAt.IsZero() && !commandRunning {
			quietUntil := lastOutputAt.Add(quietOutputWindow)
			remainingQuiet := time.Until(quietUntil)
			if remainingQuiet <= 0 {
				return c.finishDelivery(cursor)
			}
			if remainingQuiet < waitFor {
				waitFor = remainingQuiet
			}
		}
		if waitFor <= 0 {
			return c.finishDelivery(cursor)
		}
		select {
		case <-ch:
			now := time.Now()
			c.mu.Lock()
			snapshot := c.snapshotLocked(cursor)
			commandRunning = snapshot.response.Running
			if snapshot.endCursor > observedEnd {
				observedEnd = snapshot.endCursor
				lastOutputAt = now
			}
			if !snapshot.response.Running || snapshot.response.Status == shellapi.StatusClosed {
				exitObservedAt = now
			}
			ch = c.updateCh
			c.mu.Unlock()
		case <-time.After(waitFor):
			return c.finishDelivery(cursor)
		}
	}
}

func (c *ShellController) finishDelivery(cursor uint64) (*shellapi.AgentSessionResponse, string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot := c.snapshotLocked(cursor)
	if snapshot.nextCursor > c.pendingCursor {
		c.pendingCursor = snapshot.nextCursor
	}
	return snapshot.response, "", ""
}

func (c *ShellController) snapshotLocked(cursor uint64) deliverySnapshot {
	endCursor := c.pendingOutput.EndCursor()
	chunk, nextCursor, truncated, ok := c.pendingOutput.ReadFrom(cursor, shellapi.ReadChunkBytes)
	if !ok {
		nextCursor = endCursor
	}
	resp := &shellapi.AgentSessionResponse{
		Status:    c.status,
		Cwd:       c.cwd,
		Output:    string(chunk),
		Running:   c.status == shellapi.StatusRunning,
		Truncated: truncated,
		TimedOut:  c.lastTO,
		ExitCode:  c.lastExit,
	}
	if resp.Status == "" {
		resp.Status = shellapi.StatusIdle
	}
	return deliverySnapshot{response: resp, nextCursor: nextCursor, endCursor: endCursor}
}

func (c *ShellController) debugSnapshotLocked() *shellapi.AgentDebugResponse {
	pendingBytes, pendingTruncated := c.pendingStateLocked()
	transcript := c.transcript.Snapshot()
	resp := &shellapi.AgentDebugResponse{
		Status:                 c.status,
		Cwd:                    c.cwd,
		Running:                c.status == shellapi.StatusRunning,
		TimedOut:               c.lastTO,
		ExitCode:               c.lastExit,
		PendingOutputBytes:     pendingBytes,
		PendingOutputTruncated: pendingTruncated,
		Transcript:             shellapi.DebugTranscript(transcript),
		Tail:                   string(c.tail.Bytes()),
	}
	if resp.Status == "" {
		resp.Status = shellapi.StatusIdle
	}
	return resp
}

func (c *ShellController) pendingStateLocked() (int, bool) {
	start := c.pendingOutput.StartCursor()
	end := c.pendingOutput.EndCursor()
	truncated := c.pendingCursor < start
	cursor := c.pendingCursor
	if cursor < start {
		cursor = start
	}
	if cursor > end {
		cursor = end
	}
	return int(end - cursor), truncated
}

func (c *ShellController) writeInput(input string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status == shellapi.StatusClosed || c.cmd == nil || c.ptyFile == nil {
		return newShellError(shellapi.CodeSessionNotFound, "shell session not found")
	}
	if c.status != shellapi.StatusRunning {
		return newShellError(shellapi.CodeNotRunning, "shell session is not running")
	}
	if _, err := io.WriteString(c.ptyFile, input); err != nil {
		return err
	}
	return nil
}

func (c *ShellController) ensureReadable() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status == shellapi.StatusClosed || c.cmd == nil || c.ptyFile == nil {
		return newShellError(shellapi.CodeSessionNotFound, "shell session not found")
	}
	return nil
}

func (c *ShellController) handleTimeout(token string) {
	c.mu.Lock()
	if c.current == nil || c.current.token != token || c.status != shellapi.StatusRunning {
		c.mu.Unlock()
		return
	}
	c.current.timedOut = true
	c.lastTO = true
	err := c.signalForegroundLocked(unix.SIGINT)
	if err != nil {
		c.closeLocked()
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	time.AfterFunc(timeoutKillDelay, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.current == nil || c.current.token != token || c.status != shellapi.StatusRunning {
			return
		}
		if err := c.signalForegroundLocked(unix.SIGKILL); err != nil {
			c.closeLocked()
		}
	})
}

func (c *ShellController) signalForegroundLocked(sig unix.Signal) error {
	if c.ptyFile == nil {
		return errors.New("pty not available")
	}
	pgid, err := unix.IoctlGetInt(int(c.ptyFile.Fd()), unix.TIOCGPGRP)
	if err != nil {
		return err
	}
	if pgid <= 0 {
		return errors.New("invalid foreground process group")
	}
	return unix.Kill(-pgid, sig)
}

func (c *ShellController) readLoop(file *os.File) {
	buf := make([]byte, 4096)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			c.mu.Lock()
			if c.current != nil {
				c.current.raw += string(data)
				c.processCurrentLocked()
			} else {
				c.appendVisibleOutputLocked(string(data))
			}
			c.notifyLocked()
			c.mu.Unlock()
		}
		if err != nil {
			c.mu.Lock()
			c.status = shellapi.StatusClosed
			if c.current != nil && c.current.timer != nil {
				c.current.timer.Stop()
			}
			c.current = nil
			c.cmd = nil
			c.ptyFile = nil
			c.notifyLocked()
			c.mu.Unlock()
			return
		}
	}
}

func (c *ShellController) processCurrentLocked() {
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
		c.appendVisibleOutputLocked(trailing)
	}
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

func (c *ShellController) finishCommandLocked(current *shellCommand, line string) {
	if current.timer != nil {
		current.timer.Stop()
	}
	line = strings.TrimRight(line, "\r")
	if idx := strings.Index(line, current.endPrefix); idx >= 0 {
		line = line[idx:]
	}
	if !strings.HasPrefix(line, current.endPrefix) {
		c.status = shellapi.StatusIdle
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
	c.status = shellapi.StatusIdle
	current.raw = ""
}

func (c *ShellController) appendOutputLocked(data string, suppress bool) {
	if suppress || data == "" {
		return
	}
	c.appendVisibleOutputLocked(data)
}

func (c *ShellController) appendVisibleOutputLocked(data string) {
	if data == "" {
		return
	}
	payload := []byte(data)
	c.pendingOutput.Append(payload)
	c.transcript.Append(payload)
	c.tail.Append(payload)
}

func (c *ShellController) closeLocked() {
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
	c.pendingCursor = 0
	c.status = shellapi.StatusClosed
	c.notifyLocked()
}

func (c *ShellController) notifyLocked() {
	close(c.updateCh)
	c.updateCh = make(chan struct{})
}

func resolveShellCommand() (string, []string) {
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash", []string{"--noprofile", "--norc", "-i"}
	}
	return "/bin/sh", []string{"-i"}
}

func buildWrappedCommand(token, cwd, command string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(command))
	var builder strings.Builder
	builder.WriteString("ark_mark_a='__ARK'\n")
	builder.WriteString("printf '%s%s")
	builder.WriteString(token)
	builder.WriteString("\\n' \"$ark_mark_a\" '_BEGIN__'; ")
	builder.WriteString("ark_rc=0; ")
	if strings.TrimSpace(cwd) != "" {
		builder.WriteString("cd -- ")
		builder.WriteString(shellQuote(cwd))
		builder.WriteString(" || ark_rc=$?; ")
	}
	builder.WriteString("if [ \"$ark_rc\" -eq 0 ]; then ")
	builder.WriteString("ark_cmd_b64='")
	builder.WriteString(encoded)
	builder.WriteString("'; ")
	builder.WriteString("ark_cmd_file=$(mktemp); ")
	builder.WriteString("if printf '%s' \"$ark_cmd_b64\" | base64 -d > \"$ark_cmd_file\"; then . \"$ark_cmd_file\"; ark_rc=$?; else ark_rc=1; fi; ")
	builder.WriteString("rm -f \"$ark_cmd_file\"; fi; ")
	builder.WriteString("printf '\\n%s%s")
	builder.WriteString(token)
	builder.WriteString("__RC=%s__PWD=%s\\n' \"$ark_mark_a\" '_END__' \"$ark_rc\" \"$PWD\"\n")
	return builder.String()
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

func mapShellError(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	if shellErr, ok := err.(*shellapi.Error); ok {
		return shellErr.Code, shellErr.Message
	}
	return shellapi.CodeSessionNotFound, err.Error()
}

func newShellError(code, message string) *shellapi.Error {
	return &shellapi.Error{Code: code, Message: message}
}
