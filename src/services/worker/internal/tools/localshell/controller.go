//go:build desktop

package localshell

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// safeBuf is a concurrency-safe bytes.Buffer. It implements io.Writer so it
// can be assigned to cmd.Stdout/cmd.Stderr, while providing safe read access
// for the drain goroutine.
type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// snapshot returns the total length and a copy of bytes from offset onwards.
func (s *safeBuf) snapshot(offset int) (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.buf.Len()
	if n <= offset {
		return n, ""
	}
	return n, string(s.buf.Bytes()[offset:n])
}

// String returns the full buffer content.
func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

const (
	processStartupWait = 150 * time.Millisecond
	processDrainGrace  = 100 * time.Millisecond
	processKillGrace   = 2 * time.Second
	processPollTick    = 100 * time.Millisecond
	processRingBytes   = 1 << 20
	defaultProcessHome = "/tmp"
	defaultProcessTmp  = "/tmp"
	defaultProcessPath = "/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	defaultProcessLang = "C.UTF-8"
	processUserName    = "arkloop"
)

var processTerminalRetention = 30 * time.Second

type ProcessController struct {
	mu        sync.Mutex
	processes map[string]*managedProcess
}

type managedProcess struct {
	mu sync.Mutex

	ref        string
	runID      string
	mode       string
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	ptyFile    *os.File
	allowStdin bool

	stdinClosed bool
	status      string
	exitCode    *int

	requestedStatus string
	inFlight        bool

	acceptedInputSeqs map[int64]struct{}
	output            *ItemBuffer
	updateCh          chan struct{}
	readerWG          sync.WaitGroup

	// promoted buffered process: safe buffers for final drain
	promotedStdout    *safeBuf
	promotedStderr    *safeBuf
	promotedSeededOut int // stdout bytes already seeded
	promotedSeededErr int // stderr bytes already seeded
}

func NewProcessController() *ProcessController {
	return &ProcessController{
		processes: map[string]*managedProcess{},
	}
}

func (c *ProcessController) Close() {
	c.mu.Lock()
	processes := make([]*managedProcess, 0, len(c.processes))
	for _, proc := range c.processes {
		processes = append(processes, proc)
	}
	c.processes = map[string]*managedProcess{}
	c.mu.Unlock()

	for _, proc := range processes {
		proc.mu.Lock()
		if proc.status == StatusRunning {
			killProcessLocked(proc, StatusCancelled)
		}
		proc.mu.Unlock()
	}
}

func (c *ProcessController) ExecCommand(req ExecCommandRequest) (*Response, error) {
	req.Mode = NormalizeMode(strings.TrimSpace(req.Mode))
	req.Command = strings.TrimSpace(req.Command)
	req.Cwd = strings.TrimSpace(req.Cwd)
	if err := ValidateExecRequest(req); err != nil {
		return nil, err
	}

	if req.Mode == ModeBuffered {
		return c.runBuffered(req)
	}

	proc, err := c.startManaged(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.waitForSnapshot(proc, 0, processStartupWait, nil)
	if err != nil {
		return nil, err
	}
	resp.ProcessRef = proc.ref
	c.releaseProcessIfDrained(proc, resp)
	return resp, nil
}

func (c *ProcessController) ContinueProcess(req ContinueProcessRequest) (*Response, error) {
	req.ProcessRef = strings.TrimSpace(req.ProcessRef)
	req.Cursor = strings.TrimSpace(req.Cursor)
	if err := ValidateContinueRequest(req); err != nil {
		return nil, err
	}

	cursor, err := ParseCursor(req.Cursor)
	if err != nil {
		return nil, &Error{Code: CodeInvalidCursor, Message: err.Error(), HTTPStatus: 400}
	}

	proc, err := c.getProcess(req.ProcessRef)
	if err != nil {
		return nil, err
	}

	var accepted *int64
	proc.mu.Lock()
	if proc.inFlight {
		proc.mu.Unlock()
		return nil, &Error{Code: CodeProcessBusy, Message: "process is busy", HTTPStatus: 409}
	}
	if err := validateCursorLocked(proc, cursor); err != nil {
		proc.mu.Unlock()
		return nil, err
	}
	proc.inFlight = true
	if req.StdinText != nil {
		if req.InputSeq == nil || *req.InputSeq <= 0 {
			proc.inFlight = false
			proc.mu.Unlock()
			return nil, &Error{Code: CodeInputSeqRequired, Message: "input_seq is required when stdin_text is provided", HTTPStatus: 400}
		}
		value := *req.InputSeq
		accepted = &value
		if _, exists := proc.acceptedInputSeqs[*req.InputSeq]; !exists {
			if !proc.allowStdin || proc.stdin == nil || proc.stdinClosed {
				proc.inFlight = false
				proc.mu.Unlock()
				return nil, &Error{Code: CodeStdinNotSupported, Message: "process does not accept stdin", HTTPStatus: 409}
			}
			if *req.StdinText != "" {
				if _, err := io.WriteString(proc.stdin, *req.StdinText); err != nil {
					proc.inFlight = false
					proc.mu.Unlock()
					return nil, err
				}
			}
			proc.acceptedInputSeqs[*req.InputSeq] = struct{}{}
		}
	}
	if req.CloseStdin {
		if proc.mode == ModePTY {
			proc.inFlight = false
			proc.mu.Unlock()
			return nil, &Error{Code: CodeCloseStdinUnsupported, Message: "close_stdin is not supported for mode: pty", HTTPStatus: 409}
		}
		if !proc.allowStdin || proc.stdin == nil {
			proc.inFlight = false
			proc.mu.Unlock()
			return nil, &Error{Code: CodeStdinNotSupported, Message: "process does not accept stdin", HTTPStatus: 409}
		}
		if err := closeProcessStdinLocked(proc); err != nil {
			proc.inFlight = false
			proc.mu.Unlock()
			return nil, err
		}
	}
	proc.mu.Unlock()

	resp, err := c.waitForSnapshot(proc, cursor, time.Duration(NormalizeWaitMs(req.WaitMs))*time.Millisecond, accepted)
	proc.mu.Lock()
	proc.inFlight = false
	notifyLocked(proc)
	proc.mu.Unlock()
	c.releaseProcessIfDrained(proc, resp)

	return resp, err
}

func (c *ProcessController) TerminateProcess(req TerminateProcessRequest) (*Response, error) {
	return c.stopProcess(req.ProcessRef, StatusTerminated)
}

func (c *ProcessController) CancelProcess(req TerminateProcessRequest) (*Response, error) {
	return c.stopProcess(req.ProcessRef, StatusCancelled)
}

func (c *ProcessController) stopProcess(processRef string, status string) (*Response, error) {
	processRef = strings.TrimSpace(processRef)
	proc, err := c.getProcess(processRef)
	if err != nil {
		return nil, err
	}

	proc.mu.Lock()
	head := proc.output.HeadSeq()
	if proc.status == StatusRunning {
		killProcessLocked(proc, status)
	}
	proc.mu.Unlock()

	resp, err := c.waitUntilStopped(proc, head)
	c.releaseProcessIfDrained(proc, resp)
	return resp, err
}

func (c *ProcessController) ResizeProcess(req ResizeProcessRequest) (*Response, error) {
	req.ProcessRef = strings.TrimSpace(req.ProcessRef)
	if err := ValidateResizeRequest(req); err != nil {
		return nil, err
	}

	proc, err := c.getProcess(req.ProcessRef)
	if err != nil {
		return nil, err
	}

	proc.mu.Lock()
	defer proc.mu.Unlock()
	if proc.mode != ModePTY || proc.ptyFile == nil {
		return nil, &Error{Code: CodeResizeNotSupported, Message: "process is not a PTY session", HTTPStatus: 409}
	}
	if proc.status != StatusRunning {
		return nil, &Error{Code: CodeNotRunning, Message: "process is not running", HTTPStatus: 409}
	}
	if err := pty.Setsize(proc.ptyFile, &pty.Winsize{Rows: uint16(req.Rows), Cols: uint16(req.Cols)}); err != nil {
		return nil, &Error{Code: CodeResizeNotSupported, Message: err.Error(), HTTPStatus: 409}
	}
	return &Response{Status: proc.status, ProcessRef: proc.ref}, nil
}

func (c *ProcessController) runBuffered(req ExecCommandRequest) (*Response, error) {
	shellPath, shellArgs := resolveProcessShellCommand(req.Command)
	cmd := exec.Command(shellPath, shellArgs...)
	cmd.Dir = resolveProcessCwd(req.Cwd)
	cmd.Env = buildProcessEnv(req.Env, false)
	cmd.SysProcAttr = procSysProcAttr()

	var stdout, stderr safeBuf
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	timeout := time.Duration(NormalizeTimeoutMs(ModeBuffered, req.TimeoutMs)) * time.Millisecond
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		exitCode := exitCodeFromError(err)
		return &Response{
			Status:     StatusExited,
			Stdout:     stdout.String(),
			Stderr:     stderr.String(),
			ExitCode:   &exitCode,
			Cursor:     CursorString(0),
			NextCursor: CursorString(0),
		}, nil
	case <-time.After(timeout):
		// promote to managed background process instead of killing
		proc, promoteErr := c.promoteToManaged(req, cmd, &stdout, &stderr, done)
		if promoteErr != nil {
			// fallback: kill as before
			_ = terminateProcessTree(cmd, syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(processKillGrace):
				_ = terminateProcessTree(cmd, syscall.SIGKILL)
				<-done
			}
			exitCode := 124
			return &Response{
				Status:     StatusTimedOut,
				Stdout:     stdout.String(),
				Stderr:     stderr.String(),
				ExitCode:   &exitCode,
				Cursor:     CursorString(0),
				NextCursor: CursorString(0),
			}, nil
		}
		return &Response{
			Status:     StatusRunning,
			ProcessRef: proc.ref,
			Stdout:     stdout.String(),
			Stderr:     stderr.String(),
			Cursor:     CursorString(0),
			NextCursor: CursorString(proc.output.NextSeq()),
		}, nil
	}
}

// promoteToManaged converts a buffered process that timed out into a managed
// background process, allowing the agent to poll it via continue_process.
func (c *ProcessController) promoteToManaged(
	req ExecCommandRequest,
	cmd *exec.Cmd,
	stdout *safeBuf,
	stderr *safeBuf,
	done <-chan error,
) (*managedProcess, error) {
	ref, err := newProcessRef()
	if err != nil {
		return nil, err
	}

	proc := &managedProcess{
		ref:               ref,
		runID:             strings.TrimSpace(req.RunID),
		mode:              ModeFollow,
		cmd:               cmd,
		allowStdin:        false,
		status:            StatusRunning,
		acceptedInputSeqs: map[int64]struct{}{},
		output:            NewItemBuffer(processRingBytes),
		updateCh:          make(chan struct{}),
	}

	// seed buffer with output captured so far
	_, seededOutStr := stdout.snapshot(0)
	_, seededErrStr := stderr.snapshot(0)
	seededOut := len(seededOutStr)
	seededErr := len(seededErrStr)
	if seededOut > 0 {
		proc.output.Append(StreamStdout, seededOutStr)
	}
	if seededErr > 0 {
		proc.output.Append(StreamStderr, seededErrStr)
	}
	proc.promotedStdout = stdout
	proc.promotedStderr = stderr
	proc.promotedSeededOut = seededOut
	proc.promotedSeededErr = seededErr

	// monitor exit in background; periodically drain buffer content while running
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		lastOut := seededOut
		lastErr := seededErr

		// drainIncrement flushes new bytes from the promoted buffers into ItemBuffer.
		// Returns true if any content was flushed.
		drainIncrement := func() bool {
			flushed := false
			if proc.promotedStdout != nil {
				n, data := proc.promotedStdout.snapshot(lastOut)
				if n > lastOut {
					proc.output.Append(StreamStdout, data)
					lastOut = n
					flushed = true
				}
			}
			if proc.promotedStderr != nil {
				n, data := proc.promotedStderr.snapshot(lastErr)
				if n > lastErr {
					proc.output.Append(StreamStderr, data)
					lastErr = n
					flushed = true
				}
			}
			return flushed
		}

		// periodic drain while process is running
		for {
			select {
			case <-ticker.C:
				proc.mu.Lock()
				if drainIncrement() {
					notifyLocked(proc)
				}
				proc.mu.Unlock()
			case exitErr := <-done:
				// final drain after process exit; writer goroutines are done
				proc.mu.Lock()
				drainIncrement()
				proc.promotedStdout = nil
				proc.promotedStderr = nil
				code := exitCodeFromError(exitErr)
				proc.exitCode = &code
				proc.status = StatusExited
				notifyLocked(proc)
				proc.mu.Unlock()
				return
			}
		}
	}()

	c.mu.Lock()
	c.processes[ref] = proc
	c.mu.Unlock()

	return proc, nil
}

func (c *ProcessController) startManaged(req ExecCommandRequest) (*managedProcess, error) {
	ref, err := newProcessRef()
	if err != nil {
		return nil, err
	}

	shellPath, shellArgs := resolveProcessShellCommand(req.Command)
	cmd := exec.Command(shellPath, shellArgs...)
	cmd.Dir = resolveProcessCwd(req.Cwd)
	cmd.Env = buildProcessEnv(req.Env, req.Mode == ModePTY)
	cmd.SysProcAttr = procSysProcAttr()

	proc := &managedProcess{
		ref:               ref,
		runID:             strings.TrimSpace(req.RunID),
		mode:              req.Mode,
		cmd:               cmd,
		allowStdin:        req.Mode == ModeStdin || req.Mode == ModePTY,
		status:            StatusRunning,
		acceptedInputSeqs: map[int64]struct{}{},
		output:            NewItemBuffer(processRingBytes),
		updateCh:          make(chan struct{}),
	}

	switch req.Mode {
	case ModePTY:
		size := req.Size
		if size == nil {
			size = &Size{Rows: 24, Cols: 80}
		}
		file, err := pty.StartWithAttrs(cmd, &pty.Winsize{Rows: uint16(size.Rows), Cols: uint16(size.Cols)}, cmd.SysProcAttr)
		if err != nil {
			return nil, err
		}
		proc.stdin = file
		proc.ptyFile = file
		proc.readerWG.Add(1)
		go c.readStream(proc, StreamPTY, file)
	default:
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return nil, err
		}
		if proc.allowStdin {
			stdin, err := cmd.StdinPipe()
			if err != nil {
				return nil, err
			}
			proc.stdin = stdin
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		proc.readerWG.Add(2)
		go c.readStream(proc, StreamStdout, stdout)
		go c.readStream(proc, StreamStderr, stderr)
	}

	if req.Mode != ModePTY && cmd.Process == nil {
		return nil, errors.New("process failed to start")
	}

	c.mu.Lock()
	c.processes[proc.ref] = proc
	c.mu.Unlock()
	go c.waitForExit(proc, req.TimeoutMs)
	return proc, nil
}

func (c *ProcessController) waitUntilStopped(proc *managedProcess, cursor uint64) (*Response, error) {
	deadline := time.Now().Add(processKillGrace + processDrainGrace)
	for {
		proc.mu.Lock()
		resp, err := snapshotLocked(proc, cursor)
		ch := proc.updateCh
		running := proc.status == StatusRunning
		proc.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if !running {
			return resp, nil
		}
		if time.Now().After(deadline) {
			return resp, nil
		}
		select {
		case <-ch:
		case <-time.After(processPollTick):
		}
	}
}

func (c *ProcessController) waitForSnapshot(proc *managedProcess, cursor uint64, wait time.Duration, accepted *int64) (*Response, error) {
	deadline := time.Now().Add(wait)
	for {
		proc.mu.Lock()
		resp, err := snapshotLocked(proc, cursor)
		ch := proc.updateCh
		done := resp != nil && (len(resp.Items) > 0 || proc.status != StatusRunning || time.Now().After(deadline) || wait <= 0)
		if resp != nil && accepted != nil {
			value := *accepted
			resp.AcceptedInputSeq = &value
		}
		proc.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if done {
			return resp, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return resp, nil
		}
		select {
		case <-ch:
		case <-time.After(minDuration(processPollTick, remaining)):
		}
	}
}

func (c *ProcessController) readStream(proc *managedProcess, stream string, reader io.ReadCloser) {
	defer proc.readerWG.Done()
	defer reader.Close()

	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			proc.mu.Lock()
			proc.output.Append(stream, string(buf[:n]))
			notifyLocked(proc)
			proc.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (c *ProcessController) waitForExit(proc *managedProcess, timeoutMs int) {
	var timer *time.Timer
	if timeoutMs > 0 {
		timer = time.AfterFunc(time.Duration(timeoutMs)*time.Millisecond, func() {
			proc.mu.Lock()
			defer proc.mu.Unlock()
			if proc.status != StatusRunning {
				return
			}
			killProcessLocked(proc, StatusTimedOut)
		})
	}

	err := proc.cmd.Wait()
	if timer != nil {
		timer.Stop()
	}
	proc.readerWG.Wait()
	time.Sleep(processDrainGrace)

	proc.mu.Lock()
	defer proc.mu.Unlock()
	exitCode := exitCodeFromError(err)
	proc.exitCode = &exitCode
	if proc.requestedStatus != "" {
		proc.status = proc.requestedStatus
	} else {
		proc.status = StatusExited
	}
	notifyLocked(proc)
	c.scheduleProcessRelease(proc)
}

func (c *ProcessController) releaseProcessIfDrained(proc *managedProcess, resp *Response) {
	if proc == nil || resp == nil {
		return
	}
	if resp.Status == StatusRunning || resp.HasMore {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if current, ok := c.processes[proc.ref]; ok && current == proc {
		delete(c.processes, proc.ref)
	}
}

func (c *ProcessController) scheduleProcessRelease(proc *managedProcess) {
	if proc == nil || processTerminalRetention <= 0 {
		return
	}
	time.AfterFunc(processTerminalRetention, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if current, ok := c.processes[proc.ref]; ok && current == proc {
			delete(c.processes, proc.ref)
		}
	})
}

func (c *ProcessController) getProcess(processRef string) (*managedProcess, error) {
	ref := strings.TrimSpace(processRef)
	if ref == "" {
		return nil, &Error{Code: CodeProcessNotFound, Message: "process not found", HTTPStatus: 404}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	proc, ok := c.processes[ref]
	if !ok {
		return nil, &Error{Code: CodeProcessNotFound, Message: "process not found", HTTPStatus: 404}
	}
	return proc, nil
}

func snapshotLocked(proc *managedProcess, cursor uint64) (*Response, error) {
	if err := validateCursorLocked(proc, cursor); err != nil {
		return nil, err
	}
	items, next, hasMore, truncated, ok := proc.output.ReadFrom(cursor, 0)
	if !ok {
		return nil, &Error{Code: CodeInvalidCursor, Message: "cursor is invalid", HTTPStatus: 400}
	}
	stdout, stderr := flattenItems(items)
	resp := &Response{
		Status:     proc.status,
		ProcessRef: proc.ref,
		Stdout:     stdout,
		Stderr:     stderr,
		Cursor:     CursorString(cursor),
		NextCursor: CursorString(next),
		Items:      items,
		HasMore:    hasMore,
		Truncated:  truncated,
	}
	if truncated {
		resp.OutputRef = buildOutputRef(proc.ref, cursor, next)
	}
	if proc.exitCode != nil {
		value := *proc.exitCode
		resp.ExitCode = &value
	}
	return resp, nil
}

func validateCursorLocked(proc *managedProcess, cursor uint64) error {
	if cursor < proc.output.HeadSeq() {
		return &Error{Code: CodeCursorExpired, Message: "cursor has expired", HTTPStatus: 409}
	}
	if cursor > proc.output.NextSeq() {
		return &Error{Code: CodeInvalidCursor, Message: "cursor is invalid", HTTPStatus: 400}
	}
	return nil
}

func closeProcessStdinLocked(proc *managedProcess) error {
	if proc.stdinClosed || proc.stdin == nil {
		return nil
	}
	if err := proc.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	proc.stdinClosed = true
	return nil
}

func killProcessLocked(proc *managedProcess, status string) {
	if proc.cmd == nil || proc.cmd.Process == nil || proc.requestedStatus != "" {
		return
	}
	proc.requestedStatus = status
	_ = closeProcessStdinLocked(proc)
	pid := proc.cmd.Process.Pid
	_ = killProcessGroupSoft(pid)
	time.AfterFunc(processKillGrace, func() {
		_ = killProcessGroupHard(pid)
	})
}

func notifyLocked(proc *managedProcess) {
	close(proc.updateCh)
	proc.updateCh = make(chan struct{})
}

func flattenItems(items []OutputItem) (string, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	for _, item := range items {
		switch item.Stream {
		case StreamStderr:
			stderr.WriteString(item.Text)
		default:
			stdout.WriteString(item.Text)
		}
	}
	return stdout.String(), stderr.String()
}

func buildOutputRef(processRef string, cursor uint64, next uint64) string {
	ref := strings.TrimSpace(processRef)
	if ref == "" {
		ref = "buffered"
	}
	return fmt.Sprintf("process:%s:%d:%d", ref, cursor, next)
}

func resolveProcessShellCommand(command string) (string, []string) {
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash", []string{"--noprofile", "--norc", "-lc", command}
	}
	return "/bin/sh", []string{"-lc", command}
}

func resolveProcessCwd(cwd string) string {
	if strings.TrimSpace(cwd) != "" {
		return strings.TrimSpace(cwd)
	}
	if wd, err := os.Getwd(); err == nil && strings.TrimSpace(wd) != "" {
		return wd
	}
	return "."
}

func buildProcessEnv(extra map[string]*string, includeTerm bool) []string {
	env := map[string]string{
		"HOME":    processHomeDir(),
		"PATH":    processPath(),
		"LANG":    defaultProcessLang,
		"TMPDIR":  processTempDir(),
		"USER":    processUserName,
		"LOGNAME": processUserName,
	}
	if includeTerm {
		env["TERM"] = "xterm-256color"
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		if key == "" || strings.ContainsRune(key, '=') {
			continue
		}
		if value == nil {
			delete(env, key)
			continue
		}
		env[key] = *value
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+env[key])
	}
	return result
}

func processHomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return strings.TrimSpace(home)
	}
	return defaultProcessHome
}

func processPath() string {
	entries := []string{}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		entries = append(entries, filepath.Join(strings.TrimSpace(home), ".arkloop", "bin"))
	}
	entries = append(entries, filepath.SplitList(defaultProcessPath)...)
	return strings.Join(uniqueProcessPathEntries(entries), string(os.PathListSeparator))
}

func uniqueProcessPathEntries(entries []string) []string {
	seen := make(map[string]struct{}, len(entries))
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		result = append(result, entry)
	}
	return result
}

func processTempDir() string {
	if temp := strings.TrimSpace(os.TempDir()); temp != "" {
		return temp
	}
	return defaultProcessTmp
}

func (c *ProcessController) CleanupRun(runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	c.mu.Lock()
	processes := make([]*managedProcess, 0, len(c.processes))
	for _, proc := range c.processes {
		if proc.runID == runID {
			processes = append(processes, proc)
		}
	}
	c.mu.Unlock()
	for _, proc := range processes {
		proc.mu.Lock()
		if proc.status == StatusRunning {
			killProcessLocked(proc, StatusCancelled)
		}
		proc.mu.Unlock()
	}
}

func newProcessRef() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "proc_" + hex.EncodeToString(buf), nil
}

func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	return 1
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}
